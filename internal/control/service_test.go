package control

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJoinLeaveAndTokenLifecycle(t *testing.T) {
	store := NewMemoryStore()
	token, err := store.CreateJoinToken(time.Hour, 1)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	joined, err := store.JoinNode(JoinRequest{
		Token:      token.Token,
		NodeID:     "Edge A",
		PublicAddr: "203.0.113.10",
		BindPort:   7000,
		Region:     "cn-east",
	})
	if err != nil {
		t.Fatalf("join node: %v", err)
	}
	if joined.Node.ID != "edge-a" {
		t.Fatalf("node id sanitized incorrectly: %q", joined.Node.ID)
	}
	if joined.NodeToken == "" {
		t.Fatal("node token is empty")
	}
	if _, err := store.JoinNode(JoinRequest{Token: token.Token, NodeID: "edge-b", PublicAddr: "203.0.113.11"}); err != ErrTokenUsed {
		t.Fatalf("reusing exhausted token error = %v, want %v", err, ErrTokenUsed)
	}
	if _, err := store.LeaveNode(joined.Node.ID, "bad-token"); err != ErrNodeTokenMismatch {
		t.Fatalf("leave with bad token error = %v, want %v", err, ErrNodeTokenMismatch)
	}
	left, err := store.LeaveNode(joined.Node.ID, joined.NodeToken)
	if err != nil {
		t.Fatalf("leave node: %v", err)
	}
	if left.Status != NodeStatusOffline {
		t.Fatalf("left status = %s, want offline", left.Status)
	}
}

func TestHeartbeatRefreshesExpiredNode(t *testing.T) {
	store := NewMemoryStore()
	joined := joinTestNode(t, store, "edge-a", "203.0.113.10")
	store.mu.Lock()
	store.state.Nodes["edge-a"].LastSeenAt = time.Now().UTC().Add(-2 * time.Minute)
	store.mu.Unlock()
	if got := store.Snapshot().Summary.OnlineNodes; got != 0 {
		t.Fatalf("online nodes before heartbeat = %d, want 0", got)
	}
	if _, err := store.HeartbeatNode("edge-a", joined.NodeToken); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if got := store.Snapshot().Summary.OnlineNodes; got != 1 {
		t.Fatalf("online nodes after heartbeat = %d, want 1", got)
	}
}

func TestSnapshotRedactsNodeToken(t *testing.T) {
	store := NewMemoryStore()
	joined := joinTestNode(t, store, "edge-a", "203.0.113.10")
	if joined.NodeToken == "" {
		t.Fatal("join did not return node token")
	}
	snapshot := store.Snapshot()
	if snapshot.Nodes[0].NodeToken != "" {
		t.Fatalf("snapshot leaked node token %q", snapshot.Nodes[0].NodeToken)
	}
}

func TestJoinStoresNodeControlURLAndPeers(t *testing.T) {
	store := NewMemoryStore()
	if err := store.ConfigureControlPlane("http://edge-a:8080/", []string{"http://edge-b:8080/"}); err != nil {
		t.Fatalf("configure control plane: %v", err)
	}
	token, err := store.CreateJoinToken(time.Hour, 1)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	joined, err := store.JoinNode(JoinRequest{
		Token:      token.Token,
		NodeID:     "edge-c",
		PublicAddr: "203.0.113.12",
		ControlURL: "http://edge-c:8080/",
	})
	if err != nil {
		t.Fatalf("join node: %v", err)
	}
	if joined.Node.ControlURL != "http://edge-c:8080" {
		t.Fatalf("control url = %q, want normalized edge-c URL", joined.Node.ControlURL)
	}
	peers := strings.Join(store.PeerURLs(), ",")
	if !strings.Contains(peers, "http://edge-b:8080") || !strings.Contains(peers, "http://edge-c:8080") || strings.Contains(peers, "http://edge-a:8080") {
		t.Fatalf("peers = %q, want edge-b and edge-c only", peers)
	}
}

func TestMergeStatePropagatesNodeLeaveAndTokenExhaustion(t *testing.T) {
	left := NewMemoryStore()
	right := NewMemoryStore()
	token, err := left.CreateJoinToken(time.Hour, 1)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := right.MergeState(left.RawState(), "http://left:8080"); err != nil {
		t.Fatalf("merge token to right: %v", err)
	}
	joined, err := left.JoinNode(JoinRequest{
		Token:      token.Token,
		NodeID:     "edge-a",
		PublicAddr: "203.0.113.10",
		ControlURL: "http://edge-a:8080",
	})
	if err != nil {
		t.Fatalf("join node: %v", err)
	}
	if _, err := left.AdminLeaveNode(joined.Node.ID); err != nil {
		t.Fatalf("admin leave: %v", err)
	}
	if err := right.MergeState(left.RawState(), "http://left:8080"); err != nil {
		t.Fatalf("merge left to right: %v", err)
	}
	snapshot := right.Snapshot()
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Status != NodeStatusOffline {
		t.Fatalf("merged node = %+v, want offline edge-a", snapshot.Nodes)
	}
	if _, err := right.JoinNode(JoinRequest{Token: token.Token, NodeID: "edge-b", PublicAddr: "203.0.113.11"}); err != ErrTokenUsed {
		t.Fatalf("merged exhausted token error = %v, want %v", err, ErrTokenUsed)
	}
}

func TestSchedulerPicksLeastLoadedOnlineNode(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "client-a"}); err != nil {
		t.Fatalf("apply event: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	selected, err := store.SelectNodes(ConfigModeSingle, 0)
	if err != nil {
		t.Fatalf("select nodes: %v", err)
	}
	if len(selected) != 1 || selected[0].ID != "edge-b" {
		t.Fatalf("selected = %+v, want edge-b", selected)
	}
}

func TestHeartbeatStoresNetworkMetrics(t *testing.T) {
	store := NewMemoryStore()
	joined := joinTestNode(t, store, "edge-a", "203.0.113.10")
	node, err := store.HeartbeatNode(joined.Node.ID, joined.NodeToken, NetworkStatus{
		LatencyMS:            25,
		DownloadBandwidthBps: 120_000_000,
		UploadBandwidthBps:   80_000_000,
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if node.Network.LatencyMS != 25 || node.Network.BandwidthBps != 80_000_000 {
		t.Fatalf("network = %+v, want latency and effective bandwidth", node.Network)
	}
	if node.Network.Score <= 50 {
		t.Fatalf("network score = %d, want better than neutral", node.Network.Score)
	}
	snapshot := store.Snapshot()
	if snapshot.Nodes[0].Network.LatencyMS != 25 || snapshot.Nodes[0].Network.Score == 0 {
		t.Fatalf("snapshot network = %+v", snapshot.Nodes[0].Network)
	}
}

func TestSchedulerPrefersBetterNetworkBeforeLoad(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "client-a"}); err != nil {
		t.Fatalf("apply event: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeA.Node.ID, nodeA.NodeToken, NetworkStatus{LatencyMS: 20, DownloadBandwidthBps: 500_000_000, UploadBandwidthBps: 500_000_000}); err != nil {
		t.Fatalf("heartbeat edge-a: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken, NetworkStatus{LatencyMS: 300, DownloadBandwidthBps: 5_000_000, UploadBandwidthBps: 5_000_000}); err != nil {
		t.Fatalf("heartbeat edge-b: %v", err)
	}
	selected, err := store.SelectNodes(ConfigModeSingle, 0)
	if err != nil {
		t.Fatalf("select nodes: %v", err)
	}
	if len(selected) != 1 || selected[0].ID != "edge-a" {
		t.Fatalf("selected = %+v, want edge-a by network quality", selected)
	}
}

func TestGenerateFrpcSingleConfigUsesBestNetworkNode(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if _, err := store.HeartbeatNode(nodeA.Node.ID, nodeA.NodeToken, NetworkStatus{LatencyMS: 300, DownloadBandwidthBps: 5_000_000, UploadBandwidthBps: 5_000_000}); err != nil {
		t.Fatalf("heartbeat edge-a: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken, NetworkStatus{LatencyMS: 15, DownloadBandwidthBps: 500_000_000, UploadBandwidthBps: 500_000_000}); err != nil {
		t.Fatalf("heartbeat edge-b: %v", err)
	}
	config, err := store.GenerateFrpcConfig(FrpcConfigOptions{ClientID: "app-1", Mode: ConfigModeSingle})
	if err != nil {
		t.Fatalf("generate frpc: %v", err)
	}
	if !strings.Contains(config, `serverAddr = "203.0.113.11"`) {
		t.Fatalf("single config did not use best network node:\n%s", config)
	}
	if strings.Contains(config, `serverAddr = "203.0.113.10"`) {
		t.Fatalf("single config included worse node:\n%s", config)
	}
}

func TestGenerateFrpcFailoverSticksToCurrentClientNodeUntilManualSwitch(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if _, err := store.HeartbeatNode(nodeA.Node.ID, nodeA.NodeToken, NetworkStatus{LatencyMS: 300, DownloadBandwidthBps: 5_000_000, UploadBandwidthBps: 5_000_000}); err != nil {
		t.Fatalf("heartbeat edge-a: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken, NetworkStatus{LatencyMS: 15, DownloadBandwidthBps: 500_000_000, UploadBandwidthBps: 500_000_000}); err != nil {
		t.Fatalf("heartbeat edge-b: %v", err)
	}
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "app-1"}); err != nil {
		t.Fatalf("apply plugin login: %v", err)
	}
	files, err := store.GenerateFrpcConfigFiles(FrpcConfigOptions{
		ClientID: "app-1",
		Mode:     ConfigModeFailover,
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("generate files before manual switch: %v", err)
	}
	if _, ok := files["frpc-app-1-edge-a.toml"]; !ok {
		t.Fatalf("client did not stick to current node edge-a: %#v", files)
	}
	if _, ok := files["frpc-app-1-edge-b.toml"]; ok {
		t.Fatalf("client switched to better node without manual target: %#v", files)
	}
	if _, err := store.SetClientTarget("app-1", nodeB.Node.ID); err != nil {
		t.Fatalf("set manual target: %v", err)
	}
	files, err = store.GenerateFrpcConfigFiles(FrpcConfigOptions{
		ClientID: "app-1",
		Mode:     ConfigModeFailover,
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("generate files after manual switch: %v", err)
	}
	if _, ok := files["frpc-app-1-edge-b.toml"]; !ok {
		t.Fatalf("client did not switch to manual target edge-b: %#v", files)
	}
	if _, ok := files["frpc-app-1-edge-a.toml"]; ok {
		t.Fatalf("client kept old node after manual switch: %#v", files)
	}
}

func TestGenerateFrpcFailoverCanExcludeFailedCurrentNode(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if _, err := store.HeartbeatNode(nodeA.Node.ID, nodeA.NodeToken); err != nil {
		t.Fatalf("heartbeat edge-a: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken); err != nil {
		t.Fatalf("heartbeat edge-b: %v", err)
	}
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "app-1"}); err != nil {
		t.Fatalf("apply plugin login: %v", err)
	}
	files, err := store.GenerateFrpcConfigFiles(FrpcConfigOptions{
		ClientID:       "app-1",
		Mode:           ConfigModeFailover,
		Limit:          1,
		ExcludeNodeIDs: []string{"edge-a"},
	})
	if err != nil {
		t.Fatalf("generate files: %v", err)
	}
	if _, ok := files["frpc-app-1-edge-b.toml"]; !ok {
		t.Fatalf("excluded current node did not fail over to edge-b: %#v", files)
	}
}

func TestMigrationRecommendationTargetsBetterNetworkNode(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if _, err := store.HeartbeatNode(nodeA.Node.ID, nodeA.NodeToken, NetworkStatus{LatencyMS: 250, DownloadBandwidthBps: 5_000_000, UploadBandwidthBps: 5_000_000}); err != nil {
		t.Fatalf("heartbeat edge-a: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken, NetworkStatus{LatencyMS: 20, DownloadBandwidthBps: 200_000_000, UploadBandwidthBps: 200_000_000}); err != nil {
		t.Fatalf("heartbeat edge-b: %v", err)
	}
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "app-1"}); err != nil {
		t.Fatalf("apply event: %v", err)
	}
	snapshot := store.Snapshot()
	if snapshot.Clients[0].PreferredNodeID != "edge-b" || snapshot.Clients[0].MigrationState != MigrationStatePending {
		t.Fatalf("client migration = %+v, want pending edge-b", snapshot.Clients[0])
	}
	foundEvent := false
	for _, event := range snapshot.Events {
		if event.Type == "client.migration_recommended" && event.ClientID == "app-1" {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("migration event not found: %+v", snapshot.Events)
	}
}

func TestGenerateFrpcAggregateConfig(t *testing.T) {
	store := NewMemoryStore()
	joinTestNode(t, store, "edge-a", "203.0.113.10")
	joinTestNode(t, store, "edge-b", "203.0.113.11")
	config, err := store.GenerateFrpcConfig(FrpcConfigOptions{ClientID: "app-1", Mode: ConfigModeAggregate})
	if err != nil {
		t.Fatalf("generate frpc: %v", err)
	}
	for _, want := range []string{`serverAddr = "203.0.113.10"`, `serverAddr = "203.0.113.11"`, `app-1-health-edge-a`, `app-1-health-edge-b`} {
		if !strings.Contains(config, want) {
			t.Fatalf("config missing %q:\n%s", want, config)
		}
	}
}

func TestGenerateFrpcBusinessProxyConfig(t *testing.T) {
	store := NewMemoryStore()
	joinTestNode(t, store, "edge-a", "203.0.113.10")
	joinTestNode(t, store, "edge-b", "203.0.113.11")
	spec, err := ParseProxySpec("web:tcp:127.0.0.1:8080:18080")
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	files, err := store.GenerateFrpcConfigFiles(FrpcConfigOptions{
		ClientID: "app-1",
		Mode:     ConfigModeAggregate,
		Proxies:  []ProxySpec{spec},
	})
	if err != nil {
		t.Fatalf("generate files: %v", err)
	}
	for name, content := range files {
		for _, want := range []string{`name = "web"`, `localPort = 8080`, `remotePort = 18080`} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing %q:\n%s", name, want, content)
			}
		}
		if strings.Contains(content, "health") {
			t.Fatalf("%s contains default health proxy when business proxy was provided:\n%s", name, content)
		}
	}
}

func TestGenerateFrpcFailoverConfigFilesHonorsLimit(t *testing.T) {
	store := NewMemoryStore()
	joinTestNode(t, store, "edge-a", "203.0.113.10")
	joinTestNode(t, store, "edge-b", "203.0.113.11")
	joinTestNode(t, store, "edge-c", "203.0.113.12")
	files, err := store.GenerateFrpcConfigFiles(FrpcConfigOptions{
		ClientID: "app-1",
		Mode:     ConfigModeFailover,
		Limit:    2,
	})
	if err != nil {
		t.Fatalf("generate failover files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	if _, ok := files["frpc-app-1-edge-c.toml"]; ok {
		t.Fatalf("failover limit was not honored: %#v", files)
	}
}

func TestPluginEventsUpdateClientAndProxy(t *testing.T) {
	store := NewMemoryStore()
	joinTestNode(t, store, "edge-a", "203.0.113.10")
	if err := store.ApplyPluginEvent("edge-a", PluginEvent{
		Op:         "NewProxy",
		ClientID:   "app-1",
		User:       "app-user",
		ProxyName:  "ssh",
		ProxyType:  "tcp",
		RemotePort: 22001,
	}); err != nil {
		t.Fatalf("apply plugin event: %v", err)
	}
	snapshot := store.Snapshot()
	if snapshot.Summary.OnlineClients != 1 || snapshot.Summary.OnlineProxies != 1 {
		t.Fatalf("summary = %+v, want one online client and proxy", snapshot.Summary)
	}
	if snapshot.Clients[0].ProxyCount != 1 {
		t.Fatalf("client proxy count = %d, want 1", snapshot.Clients[0].ProxyCount)
	}
	if err := store.ApplyPluginEvent("edge-a", PluginEvent{Op: "CloseProxy", ClientID: "app-1", ProxyName: "ssh"}); err != nil {
		t.Fatalf("close proxy: %v", err)
	}
	if got := store.Snapshot().Summary.OnlineProxies; got != 0 {
		t.Fatalf("online proxies = %d, want 0", got)
	}
}

func TestSnapshotMarksClientsOfflineWhenNodeExpires(t *testing.T) {
	store := NewMemoryStore()
	joinTestNode(t, store, "edge-a", "203.0.113.10")
	if err := store.ApplyPluginEvent("edge-a", PluginEvent{Op: "NewProxy", ClientID: "app-1", ProxyName: "ssh"}); err != nil {
		t.Fatalf("apply plugin event: %v", err)
	}
	store.mu.Lock()
	store.state.Nodes["edge-a"].LastSeenAt = time.Now().UTC().Add(-2 * time.Minute)
	store.mu.Unlock()

	snapshot := store.Snapshot()
	if snapshot.Summary.OnlineNodes != 0 || snapshot.Summary.OnlineClients != 0 || snapshot.Summary.OnlineProxies != 0 {
		t.Fatalf("summary = %+v, want all online counts zero", snapshot.Summary)
	}
	if snapshot.Clients[0].Status != ClientStatusOffline {
		t.Fatalf("client status = %s, want offline", snapshot.Clients[0].Status)
	}
	if snapshot.Proxies[0].Status != ProxyStatusClosed {
		t.Fatalf("proxy status = %s, want closed", snapshot.Proxies[0].Status)
	}
}

func TestAPIJoinAndConfig(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	var token JoinToken
	postTestJSON(t, server.URL+"/api/v1/tokens", map[string]any{"ttl": "1h", "uses": 1}, &token)
	var joined JoinResponse
	postTestJSON(t, server.URL+"/api/v1/nodes/join", JoinRequest{Token: token.Token, NodeID: "edge-a", PublicAddr: "203.0.113.10"}, &joined)

	resp, err := http.Get(server.URL + "/api/v1/config/frps?node_id=edge-a")
	if err != nil {
		t.Fatalf("get frps config: %v", err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("frps config status=%d body=%s", resp.StatusCode, buf.String())
	}
	if !strings.Contains(buf.String(), `bindPort = 7000`) || !strings.Contains(buf.String(), `/api/v1/frp/plugin/edge-a`) {
		t.Fatalf("unexpected frps config:\n%s", buf.String())
	}
}

func TestAPIAdminLeaveAndPeerState(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	var token JoinToken
	postTestJSON(t, server.URL+"/api/v1/tokens", map[string]any{"ttl": "1h", "uses": 1}, &token)
	var joined JoinResponse
	postTestJSON(t, server.URL+"/api/v1/nodes/join", JoinRequest{
		Token:      token.Token,
		NodeID:     "edge-a",
		PublicAddr: "203.0.113.10",
		ControlURL: server.URL,
	}, &joined)

	resp, err := http.Post(server.URL+"/api/v1/nodes/edge-a/admin-leave", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("admin leave: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin leave status = %d", resp.StatusCode)
	}

	peerResp, err := http.Get(server.URL + "/api/v1/peer/state")
	if err != nil {
		t.Fatalf("get peer state: %v", err)
	}
	defer peerResp.Body.Close()
	if peerResp.StatusCode != http.StatusOK {
		t.Fatalf("peer state status = %d", peerResp.StatusCode)
	}
	var state ClusterState
	if err := json.NewDecoder(peerResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode peer state: %v", err)
	}
	if state.Nodes["edge-a"] == nil || state.Nodes["edge-a"].Status != NodeStatusOffline {
		t.Fatalf("peer state node = %+v, want offline edge-a", state.Nodes["edge-a"])
	}
}

func TestAPIManualSwitchRequiresDNSHookByDefault(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	joinTestNode(t, store, "edge-b", "203.0.113.11")
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "app-1"}); err != nil {
		t.Fatalf("apply login: %v", err)
	}
	resp, err := http.Post(server.URL+"/api/v1/clients/app-1/target", "application/json", strings.NewReader(`{"node_id":"edge-b"}`))
	if err != nil {
		t.Fatalf("post target: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want precondition required", resp.StatusCode)
	}
	client := store.Snapshot().Clients[0]
	if client.PreferredNodeID != "" || client.MigrationState != "" {
		t.Fatalf("client target changed despite missing DNS hook: %+v", client)
	}
}

func TestAPIManualSwitchRunsDNSHookBeforeSettingTarget(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	hookLog := filepath.Join(t.TempDir(), "dns.log")
	hook := filepath.Join(t.TempDir(), "dns-hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf '%s %s %s %s\\n' \"$FRP_CLUSTER_DNS_HOST\" \"$FRP_CLUSTER_DNS_TARGET_IP\" \"$FRP_CLUSTER_NODE_ID\" \"$FRP_CLUSTER_CLIENT_ID\" >> "+shellQuoteForTest(hookLog)+"\n"), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	if err := store.ConfigureControlPlaneWithOptions(ControlPlaneOptions{PublicEntryHost: "ssh-proxy.example.com", DNSUpdateHook: hook}); err != nil {
		t.Fatalf("configure control plane: %v", err)
	}
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "app-1"}); err != nil {
		t.Fatalf("apply login: %v", err)
	}
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/clients/app-1/target", "application/json", strings.NewReader(`{"node_id":"edge-b"}`))
	if err != nil {
		t.Fatalf("post target: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want ok", resp.StatusCode)
	}
	client := store.Snapshot().Clients[0]
	if client.PreferredNodeID != nodeB.Node.ID || client.MigrationState != MigrationStateManual {
		t.Fatalf("client target = %+v, want manual edge-b", client)
	}
	logData, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatalf("read hook log: %v", err)
	}
	want := "ssh-proxy.example.com 203.0.113.11 edge-b app-1"
	if !strings.Contains(string(logData), want) {
		t.Fatalf("hook log = %q, want %q", logData, want)
	}
	snapshot := store.Snapshot()
	if snapshot.Summary.SwitchesThisMonth != 1 || len(snapshot.SwitchMetrics) == 0 || snapshot.SwitchMetrics[0].Manual != 1 {
		t.Fatalf("switch metrics = %+v summary=%+v", snapshot.SwitchMetrics, snapshot.Summary)
	}
}

func TestAPIDNSTestRequiresHook(t *testing.T) {
	store := NewMemoryStore()
	joinTestNode(t, store, "edge-a", "203.0.113.10")
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/dns/test", "application/json", strings.NewReader(`{"node_id":"edge-a"}`))
	if err != nil {
		t.Fatalf("post dns test: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want precondition required", resp.StatusCode)
	}
}

func TestAPIDNSTestRunsHookAndRecordsEvent(t *testing.T) {
	store := NewMemoryStore()
	joinTestNode(t, store, "edge-a", "203.0.113.10")
	hookLog := filepath.Join(t.TempDir(), "dns-test.log")
	hook := filepath.Join(t.TempDir(), "dns-hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf '%s %s %s %s\\n' \"$FRP_CLUSTER_DNS_HOST\" \"$FRP_CLUSTER_DNS_TARGET_IP\" \"$FRP_CLUSTER_NODE_ID\" \"$FRP_CLUSTER_CLIENT_ID\" >> "+shellQuoteForTest(hookLog)+"\n"), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	if err := store.ConfigureControlPlaneWithOptions(ControlPlaneOptions{PublicEntryHost: "ssh-proxy.example.com", DNSUpdateHook: hook}); err != nil {
		t.Fatalf("configure control plane: %v", err)
	}
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/dns/test", "application/json", strings.NewReader(`{"node_id":"edge-a","client_id":"app-1"}`))
	if err != nil {
		t.Fatalf("post dns test: %v", err)
	}
	var body struct {
		DNS DNSUpdateResult `json:"dns"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode dns test response: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want ok", resp.StatusCode)
	}
	if !body.DNS.Updated || body.DNS.TargetIP != "203.0.113.10" || body.DNS.NodeID != "edge-a" {
		t.Fatalf("dns response = %+v", body.DNS)
	}
	logData, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatalf("read hook log: %v", err)
	}
	want := "ssh-proxy.example.com 203.0.113.10 edge-a app-1"
	if !strings.Contains(string(logData), want) {
		t.Fatalf("hook log = %q, want %q", logData, want)
	}
	events := store.Snapshot().Events
	if len(events) == 0 || events[0].Type != "dns.tested" {
		t.Fatalf("latest event = %+v, want dns.tested", events)
	}
}

func TestAPIAdminAuthProtectsCluster(t *testing.T) {
	store := NewMemoryStore()
	authFile := filepath.Join(t.TempDir(), "auth.env")
	server := httptest.NewServer(NewAPIWithOptions(store, RuntimeOptions{AdminPassword: "secret", AuthConfigFile: authFile}).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/cluster")
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cluster status = %d, want unauthorized", resp.StatusCode)
	}

	client := server.Client()
	setupResp, err := client.Post(server.URL+"/api/v1/auth/totp/setup", "application/json", strings.NewReader(`{"bootstrap_password":"secret","account":"admin@example.test"}`))
	if err != nil {
		t.Fatalf("setup totp: %v", err)
	}
	var setup struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(setupResp.Body).Decode(&setup); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	_ = setupResp.Body.Close()
	code := generateTOTP(setup.Secret, time.Now().Unix()/30)
	confirmResp, err := client.Post(server.URL+"/api/v1/auth/totp/confirm", "application/json", strings.NewReader(`{"bootstrap_password":"secret","secret":"`+setup.Secret+`","code":"`+code+`","account":"admin@example.test"}`))
	if err != nil {
		t.Fatalf("confirm totp: %v", err)
	}
	_ = confirmResp.Body.Close()
	if confirmResp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want ok", confirmResp.StatusCode)
	}
	loginCode := generateTOTP(setup.Secret, time.Now().Unix()/30)
	loginResp, err := client.Post(server.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"code":"`+loginCode+`"}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want ok", loginResp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/cluster", nil)
	for _, cookie := range loginResp.Cookies() {
		req.AddCookie(cookie)
	}
	authedResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get authed cluster: %v", err)
	}
	_ = authedResp.Body.Close()
	if authedResp.StatusCode != http.StatusOK {
		t.Fatalf("authed cluster status = %d, want ok", authedResp.StatusCode)
	}
}

func TestAPIAdminConfigUpdatesAliDNSAndAgentSettings(t *testing.T) {
	store := NewMemoryStore()
	dir := t.TempDir()
	aliPath := filepath.Join(dir, "alidns.env")
	nodeEnvPath := filepath.Join(dir, "node.env")
	if err := os.WriteFile(nodeEnvPath, []byte("NODE_ID=edge-a\nPROBE_SIZE=262144\nAGENT_INTERVAL=30s\nFRPS_DASHBOARD_URL=http://127.0.0.1:7500\n"), 0o600); err != nil {
		t.Fatalf("write node env: %v", err)
	}
	server := httptest.NewServer(NewAPIWithOptions(store, RuntimeOptions{AliDNSConfigFile: aliPath, NodeEnvFile: nodeEnvPath}).Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/admin/config", strings.NewReader(`{"alidns":{"access_key_id":"kid","access_key_secret":"secret","domain_name":"buaadcl.tech","ttl":"600"},"agent":{"interval":"15s","probe_size":"131072","frps_dashboard_url":"http://127.0.0.1:7500"}}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("patch admin config: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want ok", resp.StatusCode)
	}
	aliValues, err := ReadEnvFile(aliPath)
	if err != nil {
		t.Fatalf("read alidns: %v", err)
	}
	if aliValues["ALIDNS_ACCESS_KEY_ID"] != "kid" || aliValues["ALIDNS_ACCESS_KEY_SECRET"] != "secret" {
		t.Fatalf("alidns values = %+v", aliValues)
	}
	nodeValues, err := ReadEnvFile(nodeEnvPath)
	if err != nil {
		t.Fatalf("read node env: %v", err)
	}
	if nodeValues["AGENT_INTERVAL"] != "15s" || nodeValues["PROBE_SIZE"] != "131072" || nodeValues["FRPS_DASHBOARD_URL"] != "http://127.0.0.1:7500" {
		t.Fatalf("node env = %+v", nodeValues)
	}
}

func TestAPISettingsUpdateAutoMigration(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/settings", strings.NewReader(`{"auto_migration":false,"migration_score_gap":42,"public_entry_host":"ssh.buaadcl.tech","dns_update_hook":"/usr/local/bin/frp-cluster-dns"}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("patch settings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	config := store.Snapshot().Config
	if config.AutoMigration == nil || *config.AutoMigration || config.MigrationScoreGap != 42 || config.PublicEntryHost != "ssh.buaadcl.tech" || config.DNSUpdateHook != "/usr/local/bin/frp-cluster-dns" {
		t.Fatalf("config = %+v", config)
	}
}

func TestAutoSwitchCandidatesAndMetrics(t *testing.T) {
	store := NewMemoryStore()
	nodeA := joinTestNode(t, store, "edge-a", "203.0.113.10")
	nodeB := joinTestNode(t, store, "edge-b", "203.0.113.11")
	if _, err := store.HeartbeatNode(nodeA.Node.ID, nodeA.NodeToken, NetworkStatus{LatencyMS: 200, DownloadBandwidthBps: 5_000_000, UploadBandwidthBps: 5_000_000}); err != nil {
		t.Fatalf("heartbeat edge-a: %v", err)
	}
	if _, err := store.HeartbeatNode(nodeB.Node.ID, nodeB.NodeToken, NetworkStatus{LatencyMS: 10, DownloadBandwidthBps: 500_000_000, UploadBandwidthBps: 500_000_000}); err != nil {
		t.Fatalf("heartbeat edge-b: %v", err)
	}
	if err := store.ApplyPluginEvent(nodeA.Node.ID, PluginEvent{Op: "Login", ClientID: "app-1"}); err != nil {
		t.Fatalf("apply login: %v", err)
	}
	candidates := store.AutoSwitchCandidates()
	if len(candidates) != 1 || candidates[0].NodeID != "edge-b" {
		t.Fatalf("candidates = %+v", candidates)
	}
	if _, err := store.AutoSwitchClientTarget(candidates[0].ClientID, candidates[0].NodeID, candidates[0].Reason); err != nil {
		t.Fatalf("auto switch: %v", err)
	}
	snapshot := store.Snapshot()
	if snapshot.Clients[0].PreferredNodeID != "edge-b" || snapshot.Clients[0].MigrationState != MigrationStateManual {
		t.Fatalf("client = %+v", snapshot.Clients[0])
	}
	if snapshot.Summary.SwitchesThisMonth != 1 || snapshot.SwitchMetrics[0].Automatic != 1 {
		t.Fatalf("switch metrics = %+v summary=%+v", snapshot.SwitchMetrics, snapshot.Summary)
	}
}

func TestAPIHeartbeatAcceptsNetworkMetrics(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	var token JoinToken
	postTestJSON(t, server.URL+"/api/v1/tokens", map[string]any{"ttl": "1h", "uses": 1}, &token)
	var joined JoinResponse
	postTestJSON(t, server.URL+"/api/v1/nodes/join", JoinRequest{Token: token.Token, NodeID: "edge-a", PublicAddr: "203.0.113.10"}, &joined)

	var node ServerNode
	postTestJSON(t, server.URL+"/api/v1/nodes/edge-a/heartbeat", HeartbeatRequest{
		NodeToken: joined.NodeToken,
		Network: NetworkStatus{
			LatencyMS:            35,
			DownloadBandwidthBps: 90_000_000,
			UploadBandwidthBps:   60_000_000,
			ObservedRxBps:        10_000_000,
			ObservedTxBps:        8_000_000,
		},
	}, &node)
	if node.Network.LatencyMS != 35 || node.Network.BandwidthBps != 60_000_000 || node.Network.Score == 0 {
		t.Fatalf("node network = %+v", node.Network)
	}
}

func TestAPIHeartbeatStoresTrafficSamples(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	var token JoinToken
	postTestJSON(t, server.URL+"/api/v1/tokens", map[string]any{"ttl": "1h", "uses": 1}, &token)
	var joined JoinResponse
	postTestJSON(t, server.URL+"/api/v1/nodes/join", JoinRequest{Token: token.Token, NodeID: "edge-a", PublicAddr: "203.0.113.10"}, &joined)

	postTestJSON(t, server.URL+"/api/v1/nodes/edge-a/heartbeat", HeartbeatRequest{
		NodeToken: joined.NodeToken,
		Traffic: TrafficCounters{
			TotalInBytes:       1000,
			TotalOutBytes:      2000,
			CurrentConnections: 2,
			Proxies: []ProxyTraffic{{
				Name:          "local-ssh.ssh",
				Type:          "tcp",
				TotalInBytes:  1000,
				TotalOutBytes: 2000,
			}},
		},
	}, &ServerNode{})

	series := store.TrafficSeries(time.Hour)
	if len(series.Samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(series.Samples))
	}
	if series.Samples[0].NodeID != "edge-a" || series.Samples[0].TotalInBytes != 1000 || series.Samples[0].CurrentConnections != 2 {
		t.Fatalf("sample = %+v", series.Samples[0])
	}
	if series.Totals.TotalOutBytes != 2000 || len(series.Nodes) != 1 || series.Nodes[0].NodeID != "edge-a" {
		t.Fatalf("series = %+v", series)
	}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestAPINetworkProbe(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/network/probe?size=4096")
	if err != nil {
		t.Fatalf("get probe: %v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(data) != 4096 {
		t.Fatalf("probe get status=%d bytes=%d", resp.StatusCode, len(data))
	}

	uploadResp, err := http.Post(server.URL+"/api/v1/network/probe", "application/octet-stream", bytes.NewReader(bytes.Repeat([]byte("x"), 2048)))
	if err != nil {
		t.Fatalf("post probe: %v", err)
	}
	defer uploadResp.Body.Close()
	var payload struct {
		Bytes int64 `json:"bytes"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode probe: %v", err)
	}
	if payload.Bytes != 2048 {
		t.Fatalf("probe post bytes = %d, want 2048", payload.Bytes)
	}
}

func TestAPIFrpcConfigFiles(t *testing.T) {
	store := NewMemoryStore()
	server := httptest.NewServer(NewAPI(store).Handler())
	defer server.Close()

	token, err := store.CreateJoinToken(time.Hour, 2)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	postTestJSON(t, server.URL+"/api/v1/nodes/join", JoinRequest{Token: token.Token, NodeID: "edge-a", PublicAddr: "203.0.113.10"}, &JoinResponse{})
	postTestJSON(t, server.URL+"/api/v1/nodes/join", JoinRequest{Token: token.Token, NodeID: "edge-b", PublicAddr: "203.0.113.11"}, &JoinResponse{})

	var payload struct {
		Files map[string]string `json:"files"`
	}
	resp, err := http.Get(server.URL + "/api/v1/config/frpc?client_id=app-1&mode=aggregate&format=json&proxy=web:tcp:127.0.0.1:8080:18080")
	if err != nil {
		t.Fatalf("get frpc config files: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(payload.Files))
	}
	if !strings.Contains(payload.Files["frpc-app-1-edge-a.toml"], `serverAddr = "203.0.113.10"`) {
		t.Fatalf("edge-a config missing server address: %#v", payload.Files)
	}
	if !strings.Contains(payload.Files["frpc-app-1-edge-a.toml"], `name = "web"`) {
		t.Fatalf("edge-a config missing business proxy: %#v", payload.Files)
	}
}

func joinTestNode(t *testing.T, store *Store, nodeID, publicAddr string) *JoinResponse {
	t.Helper()
	token, err := store.CreateJoinToken(time.Hour, 1)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	resp, err := store.JoinNode(JoinRequest{Token: token.Token, NodeID: nodeID, PublicAddr: publicAddr, BindPort: 7000})
	if err != nil {
		t.Fatalf("join %s: %v", nodeID, err)
	}
	return resp
}

func postTestJSON(t *testing.T, url string, value any, target any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, buf.String())
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
