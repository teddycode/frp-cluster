package control

import "time"

const (
	NodeStatusOnline   = "online"
	NodeStatusOffline  = "offline"
	NodeStatusDraining = "draining"

	ClientStatusOnline  = "online"
	ClientStatusOffline = "offline"

	ProxyStatusOnline = "online"
	ProxyStatusClosed = "closed"

	ConfigModeSingle    = "single"
	ConfigModeFailover  = "failover"
	ConfigModeAggregate = "aggregate"

	MigrationStatePending = "pending"
	MigrationStateManual  = "manual"
)

type ClusterConfig struct {
	Name              string   `json:"name"`
	AuthToken         string   `json:"auth_token"`
	DashboardPort     int      `json:"dashboard_port"`
	PluginPath        string   `json:"plugin_path"`
	PluginOps         string   `json:"plugin_ops"`
	AutoMigration     *bool    `json:"auto_migration"`
	MigrationScoreGap int      `json:"migration_score_gap"`
	PublicEntryHost   string   `json:"public_entry_host,omitempty"`
	PublicControlURL  string   `json:"public_control_url,omitempty"`
	PeerURLs          []string `json:"peer_urls,omitempty"`
}

type ServerNode struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	PublicAddr     string        `json:"public_addr"`
	ControlURL     string        `json:"control_url,omitempty"`
	BindPort       int           `json:"bind_port"`
	VhostHTTPPort  int           `json:"vhost_http_port"`
	VhostHTTPSPort int           `json:"vhost_https_port"`
	Region         string        `json:"region"`
	Tags           []string      `json:"tags"`
	Status         string        `json:"status"`
	NodeToken      string        `json:"node_token"`
	ClientCount    int           `json:"client_count"`
	ProxyCount     int           `json:"proxy_count"`
	Network        NetworkStatus `json:"network"`
	JoinedAt       time.Time     `json:"joined_at"`
	LastSeenAt     time.Time     `json:"last_seen_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type NetworkStatus struct {
	LatencyMS            int64     `json:"latency_ms"`
	DownloadBandwidthBps int64     `json:"download_bandwidth_bps"`
	UploadBandwidthBps   int64     `json:"upload_bandwidth_bps"`
	ObservedRxBps        int64     `json:"observed_rx_bps"`
	ObservedTxBps        int64     `json:"observed_tx_bps"`
	BandwidthBps         int64     `json:"bandwidth_bps"`
	Score                int       `json:"score"`
	MeasuredAt           time.Time `json:"measured_at"`
	Stale                bool      `json:"stale"`
}

type Client struct {
	ID                 string    `json:"id"`
	User               string    `json:"user"`
	Status             string    `json:"status"`
	NodeID             string    `json:"node_id"`
	PreferredNodeID    string    `json:"preferred_node_id,omitempty"`
	MigrationState     string    `json:"migration_state,omitempty"`
	MigrationReason    string    `json:"migration_reason,omitempty"`
	MigrationUpdatedAt time.Time `json:"migration_updated_at,omitempty"`
	RemoteAddr         string    `json:"remote_addr"`
	ProxyCount         int       `json:"proxy_count"`
	FirstSeenAt        time.Time `json:"first_seen_at"`
	LastSeenAt         time.Time `json:"last_seen_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Proxy struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	ClientID   string    `json:"client_id"`
	User       string    `json:"user"`
	NodeID     string    `json:"node_id"`
	Status     string    `json:"status"`
	RemotePort int       `json:"remote_port"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type JoinToken struct {
	Token     string    `json:"token"`
	UsesLeft  int       `json:"uses_left"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	UsedBy    []string  `json:"used_by"`
}

type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Message   string            `json:"message"`
	NodeID    string            `json:"node_id,omitempty"`
	ClientID  string            `json:"client_id,omitempty"`
	ProxyID   string            `json:"proxy_id,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type ClusterState struct {
	Config  ClusterConfig          `json:"config"`
	Nodes   map[string]*ServerNode `json:"nodes"`
	Clients map[string]*Client     `json:"clients"`
	Proxies map[string]*Proxy      `json:"proxies"`
	Tokens  map[string]*JoinToken  `json:"tokens"`
	Events  []Event                `json:"events"`
}

type JoinRequest struct {
	Token          string   `json:"token"`
	NodeID         string   `json:"node_id"`
	Name           string   `json:"name"`
	PublicAddr     string   `json:"public_addr"`
	ControlURL     string   `json:"control_url"`
	BindPort       int      `json:"bind_port"`
	VhostHTTPPort  int      `json:"vhost_http_port"`
	VhostHTTPSPort int      `json:"vhost_https_port"`
	Region         string   `json:"region"`
	Tags           []string `json:"tags"`
}

type HeartbeatRequest struct {
	NodeToken            string        `json:"node_token"`
	Network              NetworkStatus `json:"network"`
	LatencyMS            int64         `json:"latency_ms,omitempty"`
	DownloadBandwidthBps int64         `json:"download_bandwidth_bps,omitempty"`
	UploadBandwidthBps   int64         `json:"upload_bandwidth_bps,omitempty"`
	ObservedRxBps        int64         `json:"observed_rx_bps,omitempty"`
	ObservedTxBps        int64         `json:"observed_tx_bps,omitempty"`
	BandwidthBps         int64         `json:"bandwidth_bps,omitempty"`
}

type JoinResponse struct {
	Node          *ServerNode `json:"node"`
	NodeToken     string      `json:"node_token"`
	FrpsConfigURL string      `json:"frps_config_url"`
}

type PluginEvent struct {
	Version    string `json:"version,omitempty"`
	Op         string `json:"op"`
	Content    any    `json:"content,omitempty"`
	User       string `json:"user,omitempty"`
	ProxyName  string `json:"proxy_name,omitempty"`
	ProxyType  string `json:"proxy_type,omitempty"`
	RemoteAddr string `json:"remote_addr,omitempty"`
	RemotePort int    `json:"remote_port,omitempty"`
	ClientID   string `json:"client_id,omitempty"`
	NodeID     string `json:"node_id,omitempty"`
}

type ClusterSnapshot struct {
	Config  ClusterConfig  `json:"config"`
	Nodes   []ServerNode   `json:"nodes"`
	Clients []Client       `json:"clients"`
	Proxies []Proxy        `json:"proxies"`
	Tokens  []JoinToken    `json:"tokens"`
	Events  []Event        `json:"events"`
	Summary ClusterSummary `json:"summary"`
}

type ClusterSummary struct {
	OnlineNodes    int `json:"online_nodes"`
	OfflineNodes   int `json:"offline_nodes"`
	OnlineClients  int `json:"online_clients"`
	OnlineProxies  int `json:"online_proxies"`
	AvailablePorts int `json:"available_ports"`
}
