package main

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"frp-cluster/internal/control"
)

func TestRunClientNoRunSticksToCurrentNodeUntilManualSwitch(t *testing.T) {
	store := control.NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if _, err := store.HeartbeatNode(nodeA.Node.ID, nodeA.NodeToken, control.NetworkStatus{LatencyMS: 300, DownloadBandwidthBps: 5_000_000, UploadBandwidthBps: 5_000_000}); err != nil {
		t.Fatalf("heartbeat edge-a: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken, control.NetworkStatus{LatencyMS: 15, DownloadBandwidthBps: 500_000_000, UploadBandwidthBps: 500_000_000}); err != nil {
		t.Fatalf("heartbeat edge-b: %v", err)
	}
	if err := store.ApplyPluginEvent(nodeA.Node.ID, control.PluginEvent{Op: "Login", ClientID: "app-1"}); err != nil {
		t.Fatalf("apply plugin login: %v", err)
	}
	server := httptest.NewServer(control.NewAPI(store).Handler())
	defer server.Close()

	workDir := t.TempDir()
	err := runClient([]string{
		"--control-url", server.URL,
		"--client-id", "app-1",
		"--mode", control.ConfigModeFailover,
		"--limit", "1",
		"--interval", "24h",
		"--work-dir", workDir,
		"--no-run",
		"--once",
	})
	if err != nil {
		t.Fatalf("run client: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, "frpc-app-1-edge-a.toml"))
	if err != nil {
		t.Fatalf("read synced config: %v", err)
	}
	if !bytes.Contains(data, []byte(`serverAddr = "203.0.113.10"`)) {
		t.Fatalf("synced config did not stick to current node:\n%s", data)
	}
	if matches, _ := filepath.Glob(filepath.Join(workDir, "frpc-app-1-edge-b.toml")); len(matches) != 0 {
		t.Fatalf("synced better-node config without manual switch: %v", matches)
	}
	if _, err := store.SetClientTarget("app-1", nodeB.Node.ID); err != nil {
		t.Fatalf("set manual target: %v", err)
	}
	if err := runClient([]string{
		"--control-url", server.URL,
		"--client-id", "app-1",
		"--mode", control.ConfigModeFailover,
		"--limit", "1",
		"--interval", "24h",
		"--work-dir", workDir,
		"--no-run",
		"--once",
	}); err != nil {
		t.Fatalf("run client after manual switch: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(workDir, "frpc-app-1-edge-b.toml"))
	if err != nil {
		t.Fatalf("read manual target config: %v", err)
	}
	if !bytes.Contains(data, []byte(`serverAddr = "203.0.113.11"`)) {
		t.Fatalf("synced config did not switch to manual target:\n%s", data)
	}
}

func TestRunClientOnceRequiresNoRun(t *testing.T) {
	err := runClient([]string{"--once"})
	if err == nil || !strings.Contains(err.Error(), "--once requires --no-run") {
		t.Fatalf("error = %v, want --once requires --no-run", err)
	}
}

func TestRunHealth(t *testing.T) {
	store := control.NewMemoryStore()
	server := httptest.NewServer(control.NewAPI(store).Handler())
	defer server.Close()

	if err := runHealth([]string{"--control-url", server.URL, "--timeout", "1s"}); err != nil {
		t.Fatalf("run health: %v", err)
	}
}

func TestSyncPeerPropagatesAdminLeave(t *testing.T) {
	storeA := control.NewMemoryStore()
	storeB := control.NewMemoryStore()
	serverA := httptest.NewServer(control.NewAPI(storeA).Handler())
	defer serverA.Close()
	serverB := httptest.NewServer(control.NewAPI(storeB).Handler())
	defer serverB.Close()

	if err := storeA.ConfigureControlPlane(serverA.URL, []string{serverB.URL}); err != nil {
		t.Fatalf("configure store A: %v", err)
	}
	if err := storeB.ConfigureControlPlane(serverB.URL, []string{serverA.URL}); err != nil {
		t.Fatalf("configure store B: %v", err)
	}
	token, err := storeA.CreateJoinToken(time.Hour, 2)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := storeA.JoinNode(control.JoinRequest{Token: token.Token, NodeID: "edge-a", PublicAddr: "203.0.113.10", ControlURL: serverA.URL}); err != nil {
		t.Fatalf("join edge-a: %v", err)
	}
	if _, err := storeA.JoinNode(control.JoinRequest{Token: token.Token, NodeID: "edge-b", PublicAddr: "203.0.113.11", ControlURL: serverB.URL}); err != nil {
		t.Fatalf("join edge-b: %v", err)
	}
	if err := syncPeer(storeB, serverB.URL, serverA.URL); err != nil {
		t.Fatalf("sync A to B: %v", err)
	}
	if _, err := storeB.AdminLeaveNode("edge-a"); err != nil {
		t.Fatalf("admin leave on B: %v", err)
	}
	if err := syncPeer(storeB, serverB.URL, serverA.URL); err != nil {
		t.Fatalf("sync B to A: %v", err)
	}
	nodes := map[string]control.ServerNode{}
	for _, node := range storeA.Snapshot().Nodes {
		nodes[node.ID] = node
	}
	if nodes["edge-a"].Status != control.NodeStatusOffline {
		t.Fatalf("edge-a status on A = %s, want offline", nodes["edge-a"].Status)
	}
	if nodes["edge-b"].ControlURL != serverB.URL {
		t.Fatalf("edge-b control url on A = %q, want %q", nodes["edge-b"].ControlURL, serverB.URL)
	}
}

func TestFrpcManagerStartsNewProcessBeforeStoppingOld(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "frpc.log")
	frpcPath := filepath.Join(dir, "fake-frpc.sh")
	script := fmt.Sprintf(`#!/bin/sh
cfg="$2"
base="$(basename "$cfg")"
echo "start:$base" >> %q
trap 'echo "stop:$base" >> %q; exit 0' TERM
while true; do sleep 1; done
`, logPath, logPath)
	if err := os.WriteFile(frpcPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake frpc: %v", err)
	}
	oldPath := filepath.Join(dir, "frpc-app-edge-a.toml")
	newPath := filepath.Join(dir, "frpc-app-edge-b.toml")
	if err := os.WriteFile(oldPath, []byte("serverAddr = \"203.0.113.10\"\n"), 0o600); err != nil {
		t.Fatalf("write old config: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("serverAddr = \"203.0.113.11\"\n"), 0o600); err != nil {
		t.Fatalf("write new config: %v", err)
	}

	manager := newFrpcManager(frpcPath, false, 10*time.Millisecond)
	defer manager.stopAll()
	if err := manager.reconcile([]string{oldPath}); err != nil {
		t.Fatalf("reconcile old: %v", err)
	}
	if err := manager.reconcile([]string{newPath}); err != nil {
		t.Fatalf("reconcile new: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		if strings.Contains(string(data), "stop:frpc-app-edge-a.toml") {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			newStarted := indexOf(lines, "start:frpc-app-edge-b.toml")
			oldStopped := indexOf(lines, "stop:frpc-app-edge-a.toml")
			if newStarted == -1 || oldStopped == -1 || newStarted > oldStopped {
				t.Fatalf("unexpected process order: %q", data)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("old process was not stopped, log=%q", data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

func joinTestNode(t *testing.T, store *control.Store, nodeID, publicAddr string) *control.JoinResponse {
	t.Helper()
	token, err := store.CreateJoinToken(time.Hour, 1)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	resp, err := store.JoinNode(control.JoinRequest{Token: token.Token, NodeID: nodeID, PublicAddr: publicAddr, BindPort: 7000})
	if err != nil {
		t.Fatalf("join %s: %v", nodeID, err)
	}
	if strings.TrimSpace(resp.NodeToken) == "" {
		t.Fatalf("join %s returned empty node token", nodeID)
	}
	return resp
}
