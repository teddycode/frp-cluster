package control

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNodeNotFound       = errors.New("node not found")
	ErrClientNotFound     = errors.New("client not found")
	ErrNodeTokenRequired  = errors.New("node token required")
	ErrNodeTokenMismatch  = errors.New("node token mismatch")
	ErrNoAvailableNode    = errors.New("no available node")
	ErrInvalidJoinRequest = errors.New("invalid join request")
)

const networkMetricsStaleAfter = 2 * time.Minute

func (s *Store) JoinNode(req JoinRequest) (*JoinResponse, error) {
	req.NodeID = sanitizeID(req.NodeID)
	req.Name = strings.TrimSpace(req.Name)
	req.PublicAddr = strings.TrimSpace(req.PublicAddr)
	req.ControlURL = normalizeControlURL(req.ControlURL)
	if req.NodeID == "" || req.PublicAddr == "" {
		return nil, ErrInvalidJoinRequest
	}
	if req.BindPort == 0 {
		req.BindPort = 7000
	}
	if req.Name == "" {
		req.Name = req.NodeID
	}
	now := time.Now().UTC()
	nodeToken, err := NewRandomToken("node")
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.consumeJoinTokenLocked(req.Token, req.NodeID, now); err != nil {
		return nil, err
	}
	node := &ServerNode{
		ID:             req.NodeID,
		Name:           req.Name,
		PublicAddr:     req.PublicAddr,
		ControlURL:     req.ControlURL,
		BindPort:       req.BindPort,
		VhostHTTPPort:  req.VhostHTTPPort,
		VhostHTTPSPort: req.VhostHTTPSPort,
		Region:         strings.TrimSpace(req.Region),
		Tags:           cleanList(req.Tags),
		Status:         NodeStatusOnline,
		NodeToken:      nodeToken,
		JoinedAt:       now,
		LastSeenAt:     now,
		UpdatedAt:      now,
	}
	if existing, ok := s.state.Nodes[req.NodeID]; ok {
		node.ClientCount = existing.ClientCount
		node.ProxyCount = existing.ProxyCount
		if !existing.JoinedAt.IsZero() {
			node.JoinedAt = existing.JoinedAt
		}
	}
	s.state.Nodes[req.NodeID] = node
	if req.ControlURL != "" {
		s.state.Config.PeerURLs = mergePeerURLs(s.state.Config.PeerURLs, req.ControlURL)
		s.state.Config.PeerURLs = removePeerURL(s.state.Config.PeerURLs, s.state.Config.PublicControlURL)
	}
	s.addEventLocked("node.joined", fmt.Sprintf("node %s joined", req.NodeID), req.NodeID, "", "", nil, now)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	copyNode := *node
	return &JoinResponse{
		Node:          &copyNode,
		NodeToken:     nodeToken,
		FrpsConfigURL: fmt.Sprintf("/api/v1/config/frps?node_id=%s", req.NodeID),
	}, nil
}

func (s *Store) HeartbeatNode(nodeID, nodeToken string, networks ...NetworkStatus) (*ServerNode, error) {
	req := HeartbeatRequest{NodeToken: nodeToken}
	if len(networks) > 0 {
		req.Network = networks[0]
	}
	return s.HeartbeatNodeWithRequest(nodeID, req)
}

func (s *Store) HeartbeatNodeWithRequest(nodeID string, req HeartbeatRequest) (*ServerNode, error) {
	nodeID = sanitizeID(nodeID)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.state.Nodes[nodeID]
	if !ok {
		return nil, ErrNodeNotFound
	}
	if err := validateNodeToken(node, req.NodeToken); err != nil {
		return nil, err
	}
	node.Status = NodeStatusOnline
	node.LastSeenAt = now
	node.UpdatedAt = now
	if network, ok := normalizeHeartbeatNetwork(req, node.Network, now); ok {
		node.Network = network
	}
	s.recomputeNodeCountsLocked()
	s.applyMigrationRecommendationsLocked(now)
	s.addEventLocked("node.heartbeat", fmt.Sprintf("node %s heartbeat", nodeID), nodeID, "", "", networkEventMetadata(node.Network), now)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	copyNode := *node
	return &copyNode, nil
}

func (s *Store) LeaveNode(nodeID, nodeToken string) (*ServerNode, error) {
	return s.leaveNode(nodeID, nodeToken, true)
}

func (s *Store) AdminLeaveNode(nodeID string) (*ServerNode, error) {
	return s.leaveNode(nodeID, "", false)
}

func (s *Store) leaveNode(nodeID, nodeToken string, requireToken bool) (*ServerNode, error) {
	nodeID = sanitizeID(nodeID)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.state.Nodes[nodeID]
	if !ok {
		return nil, ErrNodeNotFound
	}
	if requireToken {
		if err := validateNodeToken(node, nodeToken); err != nil {
			return nil, err
		}
	} else if nodeToken != "" {
		if err := validateNodeToken(node, nodeToken); err != nil {
			return nil, err
		}
	}
	node.Status = NodeStatusOffline
	node.LastSeenAt = now
	node.UpdatedAt = now
	for _, client := range s.state.Clients {
		if client.NodeID == nodeID {
			client.Status = ClientStatusOffline
			client.UpdatedAt = now
		}
	}
	for _, proxy := range s.state.Proxies {
		if proxy.NodeID == nodeID {
			proxy.Status = ProxyStatusClosed
			proxy.UpdatedAt = now
		}
	}
	s.recomputeNodeCountsLocked()
	s.applyMigrationRecommendationsLocked(now)
	s.addEventLocked("node.left", fmt.Sprintf("node %s left", nodeID), nodeID, "", "", nil, now)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	copyNode := *node
	return &copyNode, nil
}

func validateNodeToken(node *ServerNode, token string) error {
	if token == "" {
		return ErrNodeTokenRequired
	}
	if node.NodeToken != token {
		return ErrNodeTokenMismatch
	}
	return nil
}

func (s *Store) SelectNodes(mode string, limit int) ([]ServerNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectNodesLocked("", mode, limit)
}

func (s *Store) selectNodesLocked(clientID, mode string, limit int, excludeNodeIDs ...string) ([]ServerNode, error) {
	nodes := filterExcludedNodes(s.onlineNodesLocked(), excludeNodeIDs)
	return selectNodesForClient(nodes, s.state.Clients[sanitizeID(clientID)], mode, limit)
}

func (s *Store) onlineNodesLocked() []ServerNode {
	now := time.Now().UTC()
	nodes := make([]ServerNode, 0, len(s.state.Nodes))
	for _, node := range s.state.Nodes {
		copyNode := *node
		if copyNode.Status == NodeStatusOnline && now.Sub(copyNode.LastSeenAt) <= 90*time.Second {
			copyNode.Network = refreshNetworkStatus(copyNode.Network, now)
			nodes = append(nodes, copyNode)
		}
	}
	return nodes
}

func selectNodesForClient(nodes []ServerNode, client *Client, mode string, limit int) ([]ServerNode, error) {
	if len(nodes) == 0 {
		return nil, ErrNoAvailableNode
	}
	sortNodesByPreference(nodes)
	switch mode {
	case ConfigModeAggregate:
		return nodes, nil
	case ConfigModeFailover:
		if limit <= 0 || limit > len(nodes) {
			limit = len(nodes)
		}
		if limit == 1 {
			if pinned, ok := pinnedNode(nodes, client, true); ok {
				return []ServerNode{pinned}, nil
			}
		}
		return nodes[:limit], nil
	case ConfigModeSingle, "":
		if pinned, ok := pinnedNode(nodes, client, true); ok {
			return []ServerNode{pinned}, nil
		}
		return nodes[:1], nil
	default:
		return nil, fmt.Errorf("unsupported config mode %q", mode)
	}
}

func filterExcludedNodes(nodes []ServerNode, excludeNodeIDs []string) []ServerNode {
	if len(excludeNodeIDs) == 0 {
		return nodes
	}
	excluded := map[string]bool{}
	for _, nodeID := range excludeNodeIDs {
		if nodeID = sanitizeID(nodeID); nodeID != "" {
			excluded[nodeID] = true
		}
	}
	if len(excluded) == 0 {
		return nodes
	}
	out := nodes[:0]
	for _, node := range nodes {
		if !excluded[node.ID] {
			out = append(out, node)
		}
	}
	return out
}

func pinnedNode(nodes []ServerNode, client *Client, allowCurrentFallback bool) (ServerNode, bool) {
	if client == nil {
		return ServerNode{}, false
	}
	if client.PreferredNodeID != "" && client.MigrationState == MigrationStateManual {
		if node, ok := findNode(nodes, client.PreferredNodeID); ok {
			return node, true
		}
	}
	if allowCurrentFallback && client.NodeID != "" {
		if node, ok := findNode(nodes, client.NodeID); ok {
			return node, true
		}
	}
	return ServerNode{}, false
}

func findNode(nodes []ServerNode, nodeID string) (ServerNode, bool) {
	for _, node := range nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return ServerNode{}, false
}

func (s *Store) ApplyPluginEvent(nodeID string, event PluginEvent) error {
	event = normalizePluginEvent(event)
	nodeID = sanitizeID(firstNonEmpty(nodeID, event.NodeID))
	if nodeID == "" {
		nodeID = "unknown"
	}
	now := time.Now().UTC()
	clientID := sanitizeID(firstNonEmpty(event.ClientID, event.User, event.RemoteAddr))
	if clientID == "" {
		clientID = "anonymous"
	}
	op := strings.ToLower(strings.TrimSpace(event.Op))

	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.state.Nodes[nodeID]
	if ok {
		node.LastSeenAt = now
		node.Status = NodeStatusOnline
		node.UpdatedAt = now
	}

	switch op {
	case "login", "ping":
		client := s.upsertClientLocked(clientID, event.User, nodeID, event.RemoteAddr, now)
		client.Status = ClientStatusOnline
		s.addEventLocked("client."+op, fmt.Sprintf("client %s %s", client.ID, op), nodeID, client.ID, "", nil, now)
	case "newproxy", "new_proxy":
		client := s.upsertClientLocked(clientID, event.User, nodeID, event.RemoteAddr, now)
		client.Status = ClientStatusOnline
		proxyID := proxyID(nodeID, client.ID, event.ProxyName)
		proxy := s.state.Proxies[proxyID]
		if proxy == nil {
			proxy = &Proxy{ID: proxyID, CreatedAt: now}
			s.state.Proxies[proxyID] = proxy
		}
		proxy.Name = firstNonEmpty(event.ProxyName, proxy.ID)
		proxy.Type = firstNonEmpty(event.ProxyType, "tcp")
		proxy.ClientID = client.ID
		proxy.User = event.User
		proxy.NodeID = nodeID
		proxy.Status = ProxyStatusOnline
		proxy.RemotePort = event.RemotePort
		proxy.UpdatedAt = now
		s.addEventLocked("proxy.online", fmt.Sprintf("proxy %s online", proxy.Name), nodeID, client.ID, proxy.ID, nil, now)
	case "closeproxy", "close_proxy":
		proxyID := proxyID(nodeID, clientID, event.ProxyName)
		if proxy := s.state.Proxies[proxyID]; proxy != nil {
			proxy.Status = ProxyStatusClosed
			proxy.UpdatedAt = now
			s.addEventLocked("proxy.closed", fmt.Sprintf("proxy %s closed", proxy.Name), nodeID, clientID, proxy.ID, nil, now)
		}
	case "logout":
		if client := s.state.Clients[clientID]; client != nil {
			client.Status = ClientStatusOffline
			client.UpdatedAt = now
			s.addEventLocked("client.logout", fmt.Sprintf("client %s logout", client.ID), nodeID, client.ID, "", nil, now)
		}
	default:
		s.addEventLocked("plugin."+op, fmt.Sprintf("plugin event %s", op), nodeID, clientID, "", nil, now)
	}

	s.recomputeNodeCountsLocked()
	s.applyMigrationRecommendationsLocked(now)
	return s.saveLocked()
}

func normalizePluginEvent(event PluginEvent) PluginEvent {
	content, ok := event.Content.(map[string]any)
	if !ok {
		return event
	}
	if event.User == "" {
		event.User = stringField(content, "user")
	}
	if nestedUser, ok := content["user"].(map[string]any); ok {
		if event.User == "" {
			event.User = stringField(nestedUser, "user")
		}
		if event.ClientID == "" {
			event.ClientID = firstNonEmpty(metaField(nestedUser, "client_id"), stringField(nestedUser, "run_id"))
		}
	}
	if event.RemoteAddr == "" {
		event.RemoteAddr = firstNonEmpty(stringField(content, "client_address"), stringField(content, "remote_addr"))
	}
	if event.ClientID == "" {
		event.ClientID = firstNonEmpty(metaField(content, "client_id"), stringField(content, "run_id"), event.User)
	}
	if event.ProxyName == "" {
		event.ProxyName = stringField(content, "proxy_name")
	}
	if event.ProxyType == "" {
		event.ProxyType = stringField(content, "proxy_type")
	}
	if event.RemotePort == 0 {
		event.RemotePort = intField(content, "remote_port")
	}
	return event
}

func stringField(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func intField(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func metaField(values map[string]any, key string) string {
	metas, ok := values["metas"].(map[string]any)
	if !ok {
		return ""
	}
	return stringField(metas, key)
}

func (s *Store) upsertClientLocked(clientID, user, nodeID, remoteAddr string, now time.Time) *Client {
	client := s.state.Clients[clientID]
	if client == nil {
		client = &Client{ID: clientID, FirstSeenAt: now}
		s.state.Clients[clientID] = client
	}
	client.User = strings.TrimSpace(user)
	client.NodeID = nodeID
	client.RemoteAddr = strings.TrimSpace(remoteAddr)
	client.LastSeenAt = now
	client.UpdatedAt = now
	return client
}

func (s *Store) SetClientTarget(clientID, nodeID string) (*Client, error) {
	clientID = sanitizeID(clientID)
	nodeID = sanitizeID(nodeID)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	client := s.state.Clients[clientID]
	if client == nil {
		return nil, ErrClientNotFound
	}
	if nodeID != "" {
		node := s.state.Nodes[nodeID]
		if node == nil {
			return nil, ErrNodeNotFound
		}
		if node.Status != NodeStatusOnline || now.Sub(node.LastSeenAt) > 90*time.Second {
			return nil, ErrNoAvailableNode
		}
		client.PreferredNodeID = nodeID
		client.MigrationState = MigrationStateManual
		client.MigrationReason = "manual switch from control plane"
		s.recordSwitchLocked(now, false)
		s.addEventLocked("client.manual_switch", fmt.Sprintf("client %s manually switched to %s", client.ID, nodeID), nodeID, client.ID, "", map[string]string{
			"to_node": nodeID,
		}, now)
	} else {
		client.PreferredNodeID = ""
		client.MigrationState = ""
		client.MigrationReason = ""
		s.addEventLocked("client.manual_switch_cleared", fmt.Sprintf("client %s manual target cleared", client.ID), client.NodeID, client.ID, "", nil, now)
	}
	client.MigrationUpdatedAt = now
	client.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	copyClient := *client
	return &copyClient, nil
}

func (s *Store) AutoSwitchClientTarget(clientID, nodeID, reason string) (*Client, error) {
	clientID = sanitizeID(clientID)
	nodeID = sanitizeID(nodeID)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	client := s.state.Clients[clientID]
	if client == nil {
		return nil, ErrClientNotFound
	}
	node := s.state.Nodes[nodeID]
	if node == nil {
		return nil, ErrNodeNotFound
	}
	if node.Status != NodeStatusOnline || now.Sub(node.LastSeenAt) > 90*time.Second {
		return nil, ErrNoAvailableNode
	}
	client.PreferredNodeID = nodeID
	client.MigrationState = MigrationStateManual
	client.MigrationReason = firstNonEmpty(strings.TrimSpace(reason), "automatic switch from control plane")
	client.MigrationUpdatedAt = now
	client.UpdatedAt = now
	s.recordSwitchLocked(now, true)
	s.addEventLocked("client.auto_switch", fmt.Sprintf("client %s automatically switched to %s", client.ID, nodeID), nodeID, client.ID, "", map[string]string{
		"to_node": nodeID,
		"reason":  client.MigrationReason,
	}, now)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	copyClient := *client
	return &copyClient, nil
}

func (s *Store) AutoSwitchCandidates() []AutoSwitchCandidate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.Config.AutoMigration != nil && !*s.state.Config.AutoMigration {
		return nil
	}
	candidates := make([]AutoSwitchCandidate, 0)
	for _, client := range s.state.Clients {
		if client == nil || client.MigrationState != MigrationStatePending || client.PreferredNodeID == "" {
			continue
		}
		candidates = append(candidates, AutoSwitchCandidate{
			ClientID: client.ID,
			NodeID:   client.PreferredNodeID,
			Reason:   client.MigrationReason,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ClientID != candidates[j].ClientID {
			return candidates[i].ClientID < candidates[j].ClientID
		}
		return candidates[i].NodeID < candidates[j].NodeID
	})
	return candidates
}

func (s *Store) recordSwitchLocked(now time.Time, automatic bool) {
	month := currentMonth(now)
	metric := s.state.SwitchMetrics[month]
	if metric == nil {
		metric = &MonthlySwitch{Month: month}
		s.state.SwitchMetrics[month] = metric
	}
	metric.Count++
	if automatic {
		metric.Automatic++
	} else {
		metric.Manual++
	}
	metric.UpdatedAt = now
}

func (s *Store) recomputeNodeCountsLocked() {
	for _, node := range s.state.Nodes {
		node.ClientCount = 0
		node.ProxyCount = 0
	}
	for _, client := range s.state.Clients {
		client.ProxyCount = 0
		if client.Status == ClientStatusOnline {
			if node := s.state.Nodes[client.NodeID]; node != nil {
				node.ClientCount++
			}
		}
	}
	for _, proxy := range s.state.Proxies {
		if proxy.Status != ProxyStatusOnline {
			continue
		}
		if client := s.state.Clients[proxy.ClientID]; client != nil {
			client.ProxyCount++
		}
		if node := s.state.Nodes[proxy.NodeID]; node != nil {
			node.ProxyCount++
		}
	}
}

func normalizeHeartbeatNetwork(req HeartbeatRequest, previous NetworkStatus, now time.Time) (NetworkStatus, bool) {
	network := req.Network
	if network.LatencyMS == 0 {
		network.LatencyMS = req.LatencyMS
	}
	if network.DownloadBandwidthBps == 0 {
		network.DownloadBandwidthBps = req.DownloadBandwidthBps
	}
	if network.UploadBandwidthBps == 0 {
		network.UploadBandwidthBps = req.UploadBandwidthBps
	}
	if network.ObservedRxBps == 0 {
		network.ObservedRxBps = req.ObservedRxBps
	}
	if network.ObservedTxBps == 0 {
		network.ObservedTxBps = req.ObservedTxBps
	}
	if network.BandwidthBps == 0 {
		network.BandwidthBps = req.BandwidthBps
	}
	network.LatencyMS = nonNegative(network.LatencyMS)
	network.DownloadBandwidthBps = nonNegative(network.DownloadBandwidthBps)
	network.UploadBandwidthBps = nonNegative(network.UploadBandwidthBps)
	network.ObservedRxBps = nonNegative(network.ObservedRxBps)
	network.ObservedTxBps = nonNegative(network.ObservedTxBps)
	network.BandwidthBps = nonNegative(network.BandwidthBps)
	if !hasNetworkMeasurement(network) {
		return refreshNetworkStatus(previous, now), false
	}
	if network.BandwidthBps == 0 {
		network.BandwidthBps = effectiveBandwidth(network)
	}
	if network.MeasuredAt.IsZero() {
		network.MeasuredAt = now
	}
	network.Stale = false
	network.Score = scoreNetworkStatus(network, now)
	return network, true
}

func refreshNetworkStatus(network NetworkStatus, now time.Time) NetworkStatus {
	if network.BandwidthBps == 0 {
		network.BandwidthBps = effectiveBandwidth(network)
	}
	network.Stale = network.MeasuredAt.IsZero() || now.Sub(network.MeasuredAt) > networkMetricsStaleAfter
	network.Score = scoreNetworkStatus(network, now)
	return network
}

func hasNetworkMeasurement(network NetworkStatus) bool {
	return network.LatencyMS > 0 ||
		network.DownloadBandwidthBps > 0 ||
		network.UploadBandwidthBps > 0 ||
		network.ObservedRxBps > 0 ||
		network.ObservedTxBps > 0 ||
		network.BandwidthBps > 0
}

func effectiveBandwidth(network NetworkStatus) int64 {
	if network.BandwidthBps > 0 {
		return network.BandwidthBps
	}
	if network.DownloadBandwidthBps > 0 && network.UploadBandwidthBps > 0 {
		if network.DownloadBandwidthBps < network.UploadBandwidthBps {
			return network.DownloadBandwidthBps
		}
		return network.UploadBandwidthBps
	}
	return maxInt64(network.DownloadBandwidthBps, network.UploadBandwidthBps, network.ObservedRxBps, network.ObservedTxBps)
}

func scoreNetworkStatus(network NetworkStatus, now time.Time) int {
	if !hasNetworkMeasurement(network) {
		return 50
	}
	latencyScore := 50
	switch {
	case network.LatencyMS <= 0:
		latencyScore = 50
	case network.LatencyMS <= 20:
		latencyScore = 100
	case network.LatencyMS <= 50:
		latencyScore = 90
	case network.LatencyMS <= 100:
		latencyScore = 75
	case network.LatencyMS <= 200:
		latencyScore = 55
	case network.LatencyMS <= 500:
		latencyScore = 30
	default:
		latencyScore = 15
	}

	bandwidthScore := 50
	bandwidthMbps := effectiveBandwidth(network) / 1_000_000
	switch {
	case bandwidthMbps >= 1000:
		bandwidthScore = 100
	case bandwidthMbps >= 500:
		bandwidthScore = 95
	case bandwidthMbps >= 100:
		bandwidthScore = 85
	case bandwidthMbps >= 50:
		bandwidthScore = 75
	case bandwidthMbps >= 20:
		bandwidthScore = 60
	case bandwidthMbps >= 10:
		bandwidthScore = 50
	case bandwidthMbps >= 5:
		bandwidthScore = 40
	case bandwidthMbps >= 1:
		bandwidthScore = 25
	case bandwidthMbps > 0:
		bandwidthScore = 10
	}

	score := (latencyScore*45 + bandwidthScore*55 + 50) / 100
	if network.MeasuredAt.IsZero() || now.Sub(network.MeasuredAt) > networkMetricsStaleAfter {
		if score > 40 {
			score = 40
		}
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func sortNodesByPreference(nodes []ServerNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Network.Score != nodes[j].Network.Score {
			return nodes[i].Network.Score > nodes[j].Network.Score
		}
		if nodes[i].ClientCount != nodes[j].ClientCount {
			return nodes[i].ClientCount < nodes[j].ClientCount
		}
		if nodes[i].ProxyCount != nodes[j].ProxyCount {
			return nodes[i].ProxyCount < nodes[j].ProxyCount
		}
		if nodes[i].Network.LatencyMS != nodes[j].Network.LatencyMS {
			if nodes[i].Network.LatencyMS == 0 {
				return false
			}
			if nodes[j].Network.LatencyMS == 0 {
				return true
			}
			return nodes[i].Network.LatencyMS < nodes[j].Network.LatencyMS
		}
		if nodes[i].Network.BandwidthBps != nodes[j].Network.BandwidthBps {
			return nodes[i].Network.BandwidthBps > nodes[j].Network.BandwidthBps
		}
		return nodes[i].ID < nodes[j].ID
	})
}

func (s *Store) applyMigrationRecommendationsLocked(now time.Time) {
	if s.state.Config.AutoMigration != nil && !*s.state.Config.AutoMigration {
		return
	}
	nodes := s.onlineNodesLocked()
	if len(nodes) == 0 {
		return
	}
	sortNodesByPreference(nodes)
	best := nodes[0]
	onlineByID := make(map[string]ServerNode, len(nodes))
	for _, node := range nodes {
		onlineByID[node.ID] = node
	}
	scoreGap := s.state.Config.MigrationScoreGap
	if scoreGap <= 0 {
		scoreGap = 15
	}
	for _, client := range s.state.Clients {
		if client.Status != ClientStatusOnline || client.NodeID == "" {
			continue
		}
		current, currentOnline := onlineByID[client.NodeID]
		shouldMigrate := !currentOnline || (best.ID != current.ID && best.Network.Score-current.Network.Score >= scoreGap)
		if shouldMigrate {
			reason := fmt.Sprintf("prefer node %s score=%d over %s score=%d", best.ID, best.Network.Score, client.NodeID, current.Network.Score)
			if !currentOnline {
				reason = fmt.Sprintf("prefer node %s because current node %s is offline", best.ID, client.NodeID)
			}
			if client.PreferredNodeID != best.ID || client.MigrationState != "pending" || client.MigrationReason != reason {
				client.PreferredNodeID = best.ID
				client.MigrationState = MigrationStatePending
				client.MigrationReason = reason
				client.MigrationUpdatedAt = now
				client.UpdatedAt = now
				s.addEventLocked("client.migration_recommended", fmt.Sprintf("client %s should migrate to %s", client.ID, best.ID), best.ID, client.ID, "", map[string]string{
					"from_node": client.NodeID,
					"to_node":   best.ID,
					"reason":    reason,
				}, now)
			}
			continue
		}
		if client.PreferredNodeID != "" || client.MigrationState != "" || client.MigrationReason != "" {
			client.PreferredNodeID = ""
			client.MigrationState = ""
			client.MigrationReason = ""
			client.MigrationUpdatedAt = time.Time{}
			client.UpdatedAt = now
		}
	}
}

func networkEventMetadata(network NetworkStatus) map[string]string {
	if !hasNetworkMeasurement(network) {
		return nil
	}
	return map[string]string{
		"latency_ms":             strconv.FormatInt(network.LatencyMS, 10),
		"bandwidth_bps":          strconv.FormatInt(network.BandwidthBps, 10),
		"download_bandwidth_bps": strconv.FormatInt(network.DownloadBandwidthBps, 10),
		"upload_bandwidth_bps":   strconv.FormatInt(network.UploadBandwidthBps, 10),
		"observed_rx_bps":        strconv.FormatInt(network.ObservedRxBps, 10),
		"observed_tx_bps":        strconv.FormatInt(network.ObservedTxBps, 10),
		"score":                  strconv.Itoa(network.Score),
	}
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func maxInt64(values ...int64) int64 {
	var max int64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func sanitizeID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		if r == ' ' || r == '/' || r == ':' || r == '@' {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-_.")
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func proxyID(nodeID, clientID, name string) string {
	return sanitizeID(nodeID + "-" + clientID + "-" + firstNonEmpty(name, "proxy"))
}
