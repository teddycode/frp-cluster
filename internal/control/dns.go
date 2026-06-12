package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var ErrDNSHookNotConfigured = errors.New("dns update hook not configured")

type DNSUpdateRequest struct {
	Host     string
	TargetIP string
	NodeID   string
	ClientID string
}

type DNSUpdateResult struct {
	Updated  bool   `json:"updated"`
	Host     string `json:"host,omitempty"`
	TargetIP string `json:"target_ip,omitempty"`
	NodeID   string `json:"node_id,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Output   string `json:"output,omitempty"`
}

func (s *Store) UpdateDNSForNode(clientID, nodeID string) (DNSUpdateResult, error) {
	return s.runDNSUpdateForNode(clientID, nodeID, true, "dns.updated")
}

func (s *Store) TestDNSUpdate(clientID, nodeID string) (DNSUpdateResult, error) {
	if strings.TrimSpace(clientID) == "" {
		clientID = "dns-test"
	}
	return s.runDNSUpdateForNode(clientID, nodeID, false, "dns.tested")
}

func (s *Store) runDNSUpdateForNode(clientID, nodeID string, requireClient bool, eventType string) (DNSUpdateResult, error) {
	clientID = sanitizeID(clientID)
	nodeID = sanitizeID(nodeID)
	hook, req, err := s.dnsUpdateRequestForNode(clientID, nodeID, requireClient)
	if err != nil {
		return DNSUpdateResult{}, err
	}
	result, err := runDNSUpdateHook(hook, req)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.addEventLocked(eventType, fmt.Sprintf("dns %s updated to %s", req.Host, req.TargetIP), req.NodeID, req.ClientID, "", map[string]string{
		"host":      req.Host,
		"target_ip": req.TargetIP,
		"hook":      hook,
		"output":    result.Output,
	}, now)
	err = s.saveLocked()
	s.mu.Unlock()
	return result, err
}

func (s *Store) dnsUpdateRequestForNode(clientID, nodeID string, requireClient bool) (string, DNSUpdateRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host := strings.TrimSpace(s.state.Config.PublicEntryHost)
	hook := strings.TrimSpace(s.state.Config.DNSUpdateHook)
	node := s.state.Nodes[nodeID]
	if node == nil {
		return "", DNSUpdateRequest{}, ErrNodeNotFound
	}
	if requireClient && s.state.Clients[clientID] == nil {
		return "", DNSUpdateRequest{}, ErrClientNotFound
	}
	if node.Status != NodeStatusOnline || time.Since(node.LastSeenAt) > 90*time.Second {
		return "", DNSUpdateRequest{}, ErrNoAvailableNode
	}
	if host == "" || hook == "" {
		return "", DNSUpdateRequest{}, ErrDNSHookNotConfigured
	}
	return hook, DNSUpdateRequest{
		Host:     host,
		TargetIP: node.PublicAddr,
		NodeID:   node.ID,
		ClientID: clientID,
	}, nil
}

func runDNSUpdateHook(hook string, req DNSUpdateRequest) (DNSUpdateResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, hook)
	cmd.Env = append(os.Environ(),
		"FRP_CLUSTER_DNS_HOST="+req.Host,
		"FRP_CLUSTER_DNS_TARGET_IP="+req.TargetIP,
		"FRP_CLUSTER_NODE_ID="+req.NodeID,
		"FRP_CLUSTER_CLIENT_ID="+req.ClientID,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimSpace(out.String())
	result := DNSUpdateResult{
		Host:     req.Host,
		TargetIP: req.TargetIP,
		NodeID:   req.NodeID,
		ClientID: req.ClientID,
		Output:   output,
	}
	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("dns update hook timed out")
	}
	if err != nil {
		return result, fmt.Errorf("dns update hook failed: %w: %s", err, output)
	}
	result.Updated = true
	return result, nil
}
