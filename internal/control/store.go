package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu    sync.RWMutex
	path  string
	state ClusterState
}

type ControlPlaneOptions struct {
	PublicURL       string
	PeerURLs        []string
	PublicEntryHost string
	DNSUpdateHook   string
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func NewMemoryStore() *Store {
	s := &Store{}
	s.state = defaultState()
	return s
}

func defaultState() ClusterState {
	autoMigration := true
	return ClusterState{
		Config: ClusterConfig{
			Name:              "frp-cluster",
			AuthToken:         "change-me",
			DashboardPort:     7500,
			PluginPath:        "/handler",
			PluginOps:         "Login,NewProxy,CloseProxy,Ping",
			AutoMigration:     &autoMigration,
			MigrationScoreGap: 15,
		},
		Nodes:         map[string]*ServerNode{},
		Clients:       map[string]*Client{},
		Proxies:       map[string]*Proxy{},
		Tokens:        map[string]*JoinToken{},
		Events:        []Event{},
		SwitchMetrics: map[string]*MonthlySwitch{},
	}
}

func (s *Store) load() error {
	s.state = defaultState()
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	if len(data) == 0 {
		return s.saveLocked()
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return fmt.Errorf("decode state: %w", err)
	}
	s.ensureMaps()
	return nil
}

func (s *Store) ensureMaps() {
	if s.state.Nodes == nil {
		s.state.Nodes = map[string]*ServerNode{}
	}
	if s.state.Clients == nil {
		s.state.Clients = map[string]*Client{}
	}
	if s.state.Proxies == nil {
		s.state.Proxies = map[string]*Proxy{}
	}
	if s.state.Tokens == nil {
		s.state.Tokens = map[string]*JoinToken{}
	}
	if s.state.Events == nil {
		s.state.Events = []Event{}
	}
	if s.state.SwitchMetrics == nil {
		s.state.SwitchMetrics = map[string]*MonthlySwitch{}
	}
	if s.state.Config.Name == "" {
		s.state.Config.Name = "frp-cluster"
	}
	if s.state.Config.DashboardPort == 0 {
		s.state.Config.DashboardPort = 7500
	}
	if s.state.Config.PluginPath == "" {
		s.state.Config.PluginPath = "/handler"
	}
	if s.state.Config.PluginOps == "" {
		s.state.Config.PluginOps = "Login,NewProxy,CloseProxy,Ping"
	}
	if s.state.Config.AutoMigration == nil {
		autoMigration := true
		s.state.Config.AutoMigration = &autoMigration
	}
	if s.state.Config.MigrationScoreGap == 0 {
		s.state.Config.MigrationScoreGap = 15
	}
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func (s *Store) Snapshot() ClusterSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	nodes := make([]ServerNode, 0, len(s.state.Nodes))
	nodeOnline := map[string]bool{}
	for _, n := range s.state.Nodes {
		node := *n
		if node.Status == NodeStatusOnline && now.Sub(node.LastSeenAt) > 90*time.Second {
			node.Status = NodeStatusOffline
		}
		node.Network = refreshNetworkStatus(node.Network, now)
		nodeOnline[node.ID] = node.Status == NodeStatusOnline
		node.NodeToken = ""
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	proxiesByClient := map[string][]Proxy{}
	for _, p := range s.state.Proxies {
		proxy := *p
		if !nodeOnline[proxy.NodeID] {
			proxy.Status = ProxyStatusClosed
		}
		proxiesByClient[proxy.ClientID] = append(proxiesByClient[proxy.ClientID], proxy)
	}
	for clientID := range proxiesByClient {
		sort.Slice(proxiesByClient[clientID], func(i, j int) bool {
			if proxiesByClient[clientID][i].NodeID != proxiesByClient[clientID][j].NodeID {
				return proxiesByClient[clientID][i].NodeID < proxiesByClient[clientID][j].NodeID
			}
			return proxiesByClient[clientID][i].Name < proxiesByClient[clientID][j].Name
		})
	}

	clients := make([]ClientView, 0, len(s.state.Clients))
	for _, c := range s.state.Clients {
		client := *c
		if !nodeOnline[client.NodeID] {
			client.Status = ClientStatusOffline
		}
		clients = append(clients, ClientView{Client: client, Proxies: append([]Proxy(nil), proxiesByClient[client.ID]...)})
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].ID < clients[j].ID })

	proxies := make([]Proxy, 0, len(s.state.Proxies))
	for _, p := range s.state.Proxies {
		proxy := *p
		if !nodeOnline[proxy.NodeID] {
			proxy.Status = ProxyStatusClosed
		}
		proxies = append(proxies, proxy)
	}
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].ID < proxies[j].ID })

	tokens := make([]JoinToken, 0, len(s.state.Tokens))
	for _, t := range s.state.Tokens {
		if now.Before(t.ExpiresAt) && t.UsesLeft > 0 {
			tokens = append(tokens, *t)
		}
	}
	sort.Slice(tokens, func(i, j int) bool { return tokens[i].CreatedAt.After(tokens[j].CreatedAt) })

	events := append([]Event(nil), s.state.Events...)
	sort.Slice(events, func(i, j int) bool { return events[i].CreatedAt.After(events[j].CreatedAt) })
	if len(events) > 100 {
		events = events[:100]
	}

	var summary ClusterSummary
	for _, n := range nodes {
		if n.Status == NodeStatusOnline {
			summary.OnlineNodes++
		} else {
			summary.OfflineNodes++
		}
	}
	for _, c := range clients {
		if c.Status == ClientStatusOnline {
			summary.OnlineClients++
		}
	}
	for _, p := range proxies {
		if p.Status == ProxyStatusOnline {
			summary.OnlineProxies++
		}
	}
	summary.AvailablePorts = len(nodes)
	metrics := make([]MonthlySwitch, 0, len(s.state.SwitchMetrics))
	for _, metric := range s.state.SwitchMetrics {
		if metric != nil {
			metrics = append(metrics, *metric)
			if metric.Month == currentMonth(time.Now().UTC()) {
				summary.SwitchesThisMonth = metric.Count
			}
		}
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Month > metrics[j].Month })

	return ClusterSnapshot{
		Config:        s.state.Config,
		Nodes:         nodes,
		Clients:       clients,
		Proxies:       proxies,
		Tokens:        tokens,
		Events:        events,
		Summary:       summary,
		SwitchMetrics: metrics,
	}
}

func (s *Store) ConfigureControlPlane(publicURL string, peerURLs []string, publicEntryHost ...string) error {
	opts := ControlPlaneOptions{PublicURL: publicURL, PeerURLs: peerURLs}
	if len(publicEntryHost) > 0 {
		opts.PublicEntryHost = publicEntryHost[0]
	}
	return s.ConfigureControlPlaneWithOptions(opts)
}

func (s *Store) ConfigureControlPlaneWithOptions(opts ControlPlaneOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	publicURL := normalizeControlURL(opts.PublicURL)
	if publicURL != "" {
		s.state.Config.PublicControlURL = publicURL
	}
	if opts.PublicEntryHost != "" {
		s.state.Config.PublicEntryHost = strings.TrimSpace(opts.PublicEntryHost)
	}
	if opts.DNSUpdateHook != "" {
		s.state.Config.DNSUpdateHook = strings.TrimSpace(opts.DNSUpdateHook)
	}
	s.state.Config.PeerURLs = mergePeerURLs(s.state.Config.PeerURLs, opts.PeerURLs...)
	if publicURL != "" {
		s.state.Config.PeerURLs = removePeerURL(s.state.Config.PeerURLs, publicURL)
	}
	return s.saveLocked()
}

func (s *Store) UpdateConfig(update ConfigUpdate) (ClusterConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if update.AutoMigration != nil {
		value := *update.AutoMigration
		s.state.Config.AutoMigration = &value
	}
	if update.MigrationScoreGap != nil {
		if *update.MigrationScoreGap < 0 {
			return ClusterConfig{}, fmt.Errorf("migration score gap must be non-negative")
		}
		s.state.Config.MigrationScoreGap = *update.MigrationScoreGap
	}
	if update.PublicEntryHost != nil {
		s.state.Config.PublicEntryHost = strings.TrimSpace(*update.PublicEntryHost)
	}
	if update.DNSUpdateHook != nil {
		s.state.Config.DNSUpdateHook = strings.TrimSpace(*update.DNSUpdateHook)
	}
	if err := s.saveLocked(); err != nil {
		return ClusterConfig{}, err
	}
	return s.state.Config, nil
}

func (s *Store) PeerURLs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.state.Config.PeerURLs...)
}

func (s *Store) RawState() ClusterState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyClusterState(s.state)
}

func (s *Store) MergeState(remote ClusterState, sourceURL string) error {
	now := time.Now().UTC()
	remote = normalizeClusterState(remote)
	sourceURL = normalizeControlURL(sourceURL)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Config.PeerURLs = mergePeerURLs(s.state.Config.PeerURLs, remote.Config.PeerURLs...)
	s.state.Config.PeerURLs = mergePeerURLs(s.state.Config.PeerURLs, sourceURL, remote.Config.PublicControlURL)
	if s.state.Config.PublicControlURL != "" {
		s.state.Config.PeerURLs = removePeerURL(s.state.Config.PeerURLs, s.state.Config.PublicControlURL)
	}
	if s.state.Config.AuthToken == "" && remote.Config.AuthToken != "" {
		s.state.Config.AuthToken = remote.Config.AuthToken
	}

	for id, remoteNode := range remote.Nodes {
		if remoteNode == nil {
			continue
		}
		node := *remoteNode
		if node.ID == "" {
			node.ID = sanitizeID(id)
		}
		if node.ID == "" {
			continue
		}
		node.ControlURL = normalizeControlURL(node.ControlURL)
		local := s.state.Nodes[node.ID]
		if local == nil || node.UpdatedAt.After(local.UpdatedAt) || (local.NodeToken == "" && node.NodeToken != "") {
			if node.NodeToken == "" && local != nil {
				node.NodeToken = local.NodeToken
			}
			s.state.Nodes[node.ID] = &node
		}
		if node.ControlURL != "" {
			s.state.Config.PeerURLs = mergePeerURLs(s.state.Config.PeerURLs, node.ControlURL)
		}
	}

	for tokenValue, remoteToken := range remote.Tokens {
		if remoteToken == nil || !now.Before(remoteToken.ExpiresAt) {
			continue
		}
		local := s.state.Tokens[tokenValue]
		if local == nil {
			token := *remoteToken
			token.UsedBy = append([]string(nil), remoteToken.UsedBy...)
			s.state.Tokens[tokenValue] = &token
			continue
		}
		if remoteToken.UsesLeft < local.UsesLeft || len(remoteToken.UsedBy) > len(local.UsedBy) || remoteToken.ExpiresAt.Before(local.ExpiresAt) {
			local.UsesLeft = remoteToken.UsesLeft
			local.ExpiresAt = remoteToken.ExpiresAt
			local.UsedBy = mergeStrings(local.UsedBy, remoteToken.UsedBy...)
		}
	}

	for id, remoteClient := range remote.Clients {
		if remoteClient == nil {
			continue
		}
		client := *remoteClient
		if client.ID == "" {
			client.ID = sanitizeID(id)
		}
		if client.ID == "" {
			continue
		}
		local := s.state.Clients[client.ID]
		if local == nil || client.UpdatedAt.After(local.UpdatedAt) {
			s.state.Clients[client.ID] = &client
		}
	}

	for id, remoteProxy := range remote.Proxies {
		if remoteProxy == nil {
			continue
		}
		proxy := *remoteProxy
		if proxy.ID == "" {
			proxy.ID = id
		}
		if proxy.ID == "" {
			continue
		}
		local := s.state.Proxies[proxy.ID]
		if local == nil || proxy.UpdatedAt.After(local.UpdatedAt) {
			s.state.Proxies[proxy.ID] = &proxy
		}
	}

	eventsByID := map[string]Event{}
	for _, event := range s.state.Events {
		eventsByID[event.ID] = event
	}
	for _, event := range remote.Events {
		if event.ID == "" {
			continue
		}
		if local, ok := eventsByID[event.ID]; !ok || event.CreatedAt.After(local.CreatedAt) {
			eventsByID[event.ID] = event
		}
	}
	s.state.Events = make([]Event, 0, len(eventsByID))
	for _, event := range eventsByID {
		s.state.Events = append(s.state.Events, event)
	}
	sort.Slice(s.state.Events, func(i, j int) bool { return s.state.Events[i].CreatedAt.Before(s.state.Events[j].CreatedAt) })
	if len(s.state.Events) > 500 {
		s.state.Events = s.state.Events[len(s.state.Events)-500:]
	}
	s.recomputeNodeCountsLocked()
	return s.saveLocked()
}

func (s *Store) addEventLocked(eventType, message, nodeID, clientID, proxyID string, metadata map[string]string, now time.Time) {
	event := Event{
		ID:        fmt.Sprintf("%d-%s", now.UnixNano(), eventType),
		Type:      eventType,
		Message:   message,
		NodeID:    nodeID,
		ClientID:  clientID,
		ProxyID:   proxyID,
		Metadata:  metadata,
		CreatedAt: now,
	}
	s.state.Events = append(s.state.Events, event)
	if len(s.state.Events) > 500 {
		s.state.Events = s.state.Events[len(s.state.Events)-500:]
	}
}

func currentMonth(now time.Time) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC().Format("2006-01")
}

func copyClusterState(state ClusterState) ClusterState {
	data, err := json.Marshal(state)
	if err != nil {
		return normalizeClusterState(ClusterState{})
	}
	var out ClusterState
	if err := json.Unmarshal(data, &out); err != nil {
		return normalizeClusterState(ClusterState{})
	}
	return normalizeClusterState(out)
}

func normalizeClusterState(state ClusterState) ClusterState {
	if state.Nodes == nil {
		state.Nodes = map[string]*ServerNode{}
	}
	if state.Clients == nil {
		state.Clients = map[string]*Client{}
	}
	if state.Proxies == nil {
		state.Proxies = map[string]*Proxy{}
	}
	if state.Tokens == nil {
		state.Tokens = map[string]*JoinToken{}
	}
	if state.Events == nil {
		state.Events = []Event{}
	}
	if state.Config.Name == "" {
		state.Config.Name = "frp-cluster"
	}
	if state.Config.DashboardPort == 0 {
		state.Config.DashboardPort = 7500
	}
	if state.Config.PluginOps == "" {
		state.Config.PluginOps = "Login,NewProxy,CloseProxy,Ping"
	}
	state.Config.PublicControlURL = normalizeControlURL(state.Config.PublicControlURL)
	state.Config.PeerURLs = mergePeerURLs(nil, state.Config.PeerURLs...)
	if state.Config.PublicControlURL != "" {
		state.Config.PeerURLs = removePeerURL(state.Config.PeerURLs, state.Config.PublicControlURL)
	}
	return state
}

func normalizeControlURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.TrimRight(value, "/")
}

func mergePeerURLs(existing []string, values ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(values))
	for _, value := range append(append([]string(nil), existing...), values...) {
		value = normalizeControlURL(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func removePeerURL(values []string, remove string) []string {
	remove = normalizeControlURL(remove)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeControlURL(value)
		if value == "" || value == remove {
			continue
		}
		out = append(out, value)
	}
	return out
}

func mergeStrings(existing []string, values ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(values))
	for _, value := range append(append([]string(nil), existing...), values...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
