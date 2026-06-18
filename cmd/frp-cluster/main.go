package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"frp-cluster/internal/control"
)

func main() {
	if err := loadProjectEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "server":
		err = runServer(os.Args[2:])
	case "token":
		err = runToken(os.Args[2:])
	case "join":
		err = runJoin(os.Args[2:])
	case "leave":
		err = runLeave(os.Args[2:])
	case "agent":
		err = runAgent(os.Args[2:])
	case "client":
		err = runClient(os.Args[2:])
	case "config":
		err = runConfig(os.Args[2:])
	case "health":
		err = runHealth(os.Args[2:])
	case "dns":
		err = runDNS(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `frp-cluster controls frps clusters.

Usage:
  frp-cluster server --listen :8080 --data ./data/cluster.json --public-url http://203.0.113.10:8080 --public-entry-host ssh.buaadcl.tech --dns-update-hook /usr/local/bin/frp-cluster-alidns-update --peer http://203.0.113.11:8080
  frp-cluster token --control-url http://127.0.0.1:8080 --ttl 2h --uses 1
  frp-cluster join --control-url http://127.0.0.1:8080 --token TOKEN --node-id edge-a --public-addr 203.0.113.10 --node-control-url http://203.0.113.10:8080 --write-frps-config ./frps.toml
  frp-cluster agent --control-url http://127.0.0.1:8080 --node-id edge-a --token NODE_TOKEN
  frp-cluster client --control-url http://127.0.0.1:8080 --client-id app-1 --proxy web:tcp:127.0.0.1:8080:18080
  frp-cluster leave --control-url http://127.0.0.1:8080 --node-id edge-a --token NODE_TOKEN
  frp-cluster health --control-url http://127.0.0.1:8080 --timeout 5s
  frp-cluster dns alidns-update --config /etc/frp-cluster/alidns.env
  frp-cluster config frps --control-url http://127.0.0.1:8080 --node-id edge-a
  frp-cluster config frpc --control-url http://127.0.0.1:8080 --client-id app-1 --mode aggregate
  frp-cluster config frpc --control-url http://127.0.0.1:8080 --client-id app-1 --mode aggregate --out-dir ./frpc.d`)
}

func runServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "HTTP listen address")
	data := fs.String("data", "./data/cluster.json", "state file path")
	publicURL := fs.String("public-url", "", "public control plane URL for this node")
	publicEntryHost := fs.String("public-entry-host", "", "stable DNS entry users should connect to")
	dnsUpdateHook := fs.String("dns-update-hook", "", "executable hook to update the stable DNS entry before manual switch")
	webDir := fs.String("web-dir", "./web/dist", "static web frontend directory")
	authToken := fs.String("auth-token", envString("FRP_CLUSTER_AUTH_TOKEN", ""), "shared frp auth token; prefer FRP_CLUSTER_AUTH_TOKEN in .env")
	adminPassword := fs.String("admin-password", envString("FRP_CLUSTER_ADMIN_PASSWORD", ""), "admin web password; prefer FRP_CLUSTER_ADMIN_PASSWORD in .env or --admin-password-file in production")
	adminPasswordFile := fs.String("admin-password-file", envString("FRP_CLUSTER_ADMIN_PASSWORD_FILE", ""), "file containing admin web password")
	authConfigFile := fs.String("auth-config-file", envString("FRP_CLUSTER_AUTH_CONFIG_FILE", "/etc/frp-cluster/auth.env"), "TOTP authenticator config file")
	aliDNSConfigFile := fs.String("alidns-config-file", envString("FRP_CLUSTER_ALIDNS_CONFIG_FILE", "/etc/frp-cluster/alidns.env"), "AliDNS config file managed by the admin UI")
	nodeEnvFile := fs.String("node-env-file", envString("FRP_CLUSTER_NODE_ENV_FILE", "/etc/frp-cluster/node.env"), "node env file managed by the admin UI")
	autoSwitchInterval := fs.Duration("auto-switch-interval", 30*time.Second, "interval for applying enabled automatic switch recommendations")
	peerSyncInterval := fs.Duration("peer-sync-interval", 10*time.Second, "peer state synchronization interval")
	peers := multiFlag{}
	fs.Var(&peers, "peer", "peer control plane URL; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := control.NewStore(*data)
	if err != nil {
		return err
	}
	if err := store.ConfigureControlPlaneWithOptions(control.ControlPlaneOptions{
		PublicURL:       *publicURL,
		PeerURLs:        peers,
		PublicEntryHost: *publicEntryHost,
		DNSUpdateHook:   *dnsUpdateHook,
		AuthToken:       *authToken,
	}); err != nil {
		return err
	}
	if len(peers) > 0 || *publicURL != "" {
		go runPeerSync(store, *publicURL, *peerSyncInterval)
	}
	go runAutoSwitch(store, *autoSwitchInterval)
	api := control.NewAPIWithOptions(store, control.RuntimeOptions{
		WebDir:            *webDir,
		AdminPassword:     *adminPassword,
		AdminPasswordFile: *adminPasswordFile,
		AuthConfigFile:    *authConfigFile,
		AliDNSConfigFile:  *aliDNSConfigFile,
		NodeEnvFile:       *nodeEnvFile,
	})
	server := &http.Server{
		Addr:              *listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("frp-cluster control plane listening on %s public_url=%s peers=%d", *listen, *publicURL, len(peers))
	return server.ListenAndServe()
}

func runAutoSwitch(store *control.Store, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		for _, candidate := range store.AutoSwitchCandidates() {
			if _, err := store.UpdateDNSForNode(candidate.ClientID, candidate.NodeID); err != nil {
				log.Printf("auto switch dns update failed client=%s node=%s: %v", candidate.ClientID, candidate.NodeID, err)
				continue
			}
			if _, err := store.AutoSwitchClientTarget(candidate.ClientID, candidate.NodeID, candidate.Reason); err != nil {
				log.Printf("auto switch target failed client=%s node=%s: %v", candidate.ClientID, candidate.NodeID, err)
			}
		}
	}
}

func runDNS(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("dns requires alidns-update")
	}
	switch args[0] {
	case "alidns-update":
		fs := flag.NewFlagSet("dns alidns-update", flag.ExitOnError)
		configPath := fs.String("config", "/etc/frp-cluster/alidns.env", "AliDNS env config file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runAliDNSUpdate(*configPath)
	default:
		return fmt.Errorf("unknown dns command %q", args[0])
	}
}

func loadProjectEnv() error {
	envFile := strings.TrimSpace(os.Getenv("FRP_CLUSTER_ENV_FILE"))
	if envFile == "" {
		envFile = ".env"
	}
	values, err := control.ReadEnvFile(envFile)
	if err != nil {
		if os.IsNotExist(err) && envFile == ".env" {
			return nil
		}
		return fmt.Errorf("load env file %s: %w", envFile, err)
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set env %s: %w", key, err)
			}
		}
	}
	return nil
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func runPeerSync(store *control.Store, publicURL string, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	syncOnce := func() {
		peers := store.PeerURLs()
		for _, peer := range peers {
			if err := syncPeer(store, publicURL, peer); err != nil {
				log.Printf("peer sync %s failed: %v", peer, err)
			}
		}
	}
	syncOnce()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		syncOnce()
	}
}

func syncPeer(store *control.Store, publicURL, peer string) error {
	peer = strings.TrimRight(strings.TrimSpace(peer), "/")
	if peer == "" {
		return nil
	}
	var remote control.ClusterState
	if err := getJSONWithTimeout(peer+"/api/v1/peer/state", &remote, 5*time.Second); err != nil {
		return err
	}
	if err := store.MergeState(remote, peer); err != nil {
		return err
	}
	local := store.RawState()
	url := peer + "/api/v1/peer/state"
	if publicURL != "" {
		url += "?source_url=" + urlQueryEscape(publicURL)
	}
	return postJSONWithTimeout(url, local, nil, 5*time.Second)
}

func runToken(args []string) error {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
	ttl := fs.String("ttl", "2h", "token TTL")
	uses := fs.Int("uses", 1, "token uses")
	adminPasswordFile := fs.String("admin-password-file", "", "admin password file for protected control planes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var resp control.JoinToken
	if err := postAdminJSON(*controlURL, "/api/v1/tokens", map[string]any{"ttl": *ttl, "uses": *uses}, &resp, *adminPasswordFile); err != nil {
		return err
	}
	fmt.Println(resp.Token)
	return nil
}

func runJoin(args []string) error {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
	token := fs.String("token", "", "join token")
	nodeID := fs.String("node-id", "", "node id")
	name := fs.String("name", "", "node name")
	publicAddr := fs.String("public-addr", "", "public frps address")
	nodeControlURL := fs.String("node-control-url", "", "public control plane URL exposed by this node")
	bindPort := fs.Int("bind-port", 7000, "frps bind port")
	httpPort := fs.Int("vhost-http-port", 0, "frps vhost HTTP port")
	httpsPort := fs.Int("vhost-https-port", 0, "frps vhost HTTPS port")
	region := fs.String("region", "", "node region")
	tags := fs.String("tags", "", "comma-separated tags")
	writeFrpsConfig := fs.String("write-frps-config", "", "write generated frps config to path after joining")
	if err := fs.Parse(args); err != nil {
		return err
	}
	req := control.JoinRequest{
		Token:          *token,
		NodeID:         *nodeID,
		Name:           *name,
		PublicAddr:     *publicAddr,
		ControlURL:     *nodeControlURL,
		BindPort:       *bindPort,
		VhostHTTPPort:  *httpPort,
		VhostHTTPSPort: *httpsPort,
		Region:         *region,
		Tags:           splitCSV(*tags),
	}
	var resp control.JoinResponse
	if err := postJSON(*controlURL+"/api/v1/nodes/join", req, &resp); err != nil {
		return err
	}
	frpsConfigURL := strings.TrimRight(*controlURL, "/") + resp.FrpsConfigURL
	if *writeFrpsConfig != "" {
		text, err := getText(frpsConfigURL)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(*writeFrpsConfig), 0o755); err != nil && filepath.Dir(*writeFrpsConfig) != "." {
			return err
		}
		if err := os.WriteFile(*writeFrpsConfig, []byte(text), 0o600); err != nil {
			return err
		}
	}
	fmt.Printf("node_id=%s\nnode_token=%s\nfrps_config=%s\n", resp.Node.ID, resp.NodeToken, frpsConfigURL)
	if *writeFrpsConfig != "" {
		fmt.Printf("frps_config_file=%s\n", *writeFrpsConfig)
	}
	fmt.Printf("agent_command=frp-cluster agent --control-url %s --node-id %s --token %s\n", *controlURL, resp.Node.ID, resp.NodeToken)
	return nil
}

func runLeave(args []string) error {
	fs := flag.NewFlagSet("leave", flag.ExitOnError)
	controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
	nodeID := fs.String("node-id", "", "node id")
	nodeToken := fs.String("token", "", "node token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var node control.ServerNode
	if err := postJSON(fmt.Sprintf("%s/api/v1/nodes/%s/leave", strings.TrimRight(*controlURL, "/"), *nodeID), map[string]string{"node_token": *nodeToken}, &node); err != nil {
		return err
	}
	fmt.Printf("node %s status=%s\n", node.ID, node.Status)
	return nil
}

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
	nodeID := fs.String("node-id", "", "node id")
	nodeToken := fs.String("token", "", "node token")
	interval := fs.Duration("interval", 30*time.Second, "heartbeat interval")
	probeSize := fs.Int("probe-size", 256*1024, "bytes used for active bandwidth probe per direction; set 0 to disable")
	frpsDashboardURL := fs.String("frps-dashboard-url", "http://127.0.0.1:7500", "local frps dashboard URL used for low-cost traffic counters; empty disables traffic collection")
	leaveOnExit := fs.Bool("leave-on-exit", false, "mark node offline when the agent exits")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nodeID == "" || *nodeToken == "" {
		return fmt.Errorf("node-id and token are required")
	}
	if *interval <= 0 {
		*interval = 30 * time.Second
	}
	controlBase := strings.TrimRight(*controlURL, "/")
	collector := newNetworkCollector()
	heartbeat := func() error {
		var node control.ServerNode
		network := collector.measure(controlBase, *probeSize)
		req := control.HeartbeatRequest{
			NodeToken: *nodeToken,
			Network:   network,
		}
		if traffic, err := scrapeFRPSTraffic(*frpsDashboardURL); err == nil {
			req.Traffic = traffic
		}
		return postJSON(fmt.Sprintf("%s/api/v1/nodes/%s/heartbeat", controlBase, *nodeID), req, &node)
	}
	if err := heartbeat(); err != nil {
		return err
	}
	fmt.Printf("agent started node_id=%s interval=%s probe_size=%d\n", *nodeID, interval.String(), *probeSize)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := heartbeat(); err != nil {
				return err
			}
		case <-signals:
			if *leaveOnExit {
				var node control.ServerNode
				if err := postJSON(fmt.Sprintf("%s/api/v1/nodes/%s/leave", controlBase, *nodeID), map[string]string{"node_token": *nodeToken}, &node); err != nil {
					return err
				}
			}
			return nil
		}
	}
}

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
	clientID := fs.String("client-id", "client", "client id")
	mode := fs.String("mode", control.ConfigModeFailover, "single, failover, or aggregate")
	limit := fs.Int("limit", 1, "node limit for single/failover process set")
	interval := fs.Duration("interval", 30*time.Second, "config refresh interval")
	failoverInterval := fs.Duration("failover-interval", 10*time.Second, "retry interval after frpc exits or control plane is unreachable")
	drainTimeout := fs.Duration("drain-timeout", 30*time.Second, "time to keep old frpc processes after a better node starts")
	workDir := fs.String("work-dir", "./frpc.d", "directory for synchronized frpc configs")
	frpcBin := fs.String("frpc-bin", "frpc", "frpc executable path")
	noRun := fs.Bool("no-run", false, "only synchronize configs, do not start frpc processes")
	once := fs.Bool("once", false, "synchronize once and exit")
	proxies := multiFlag{}
	fs.Var(&proxies, "proxy", "business proxy as name:type:localIP:localPort:remotePort; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *interval <= 0 {
		*interval = 30 * time.Second
	}
	if *limit <= 0 && *mode != control.ConfigModeAggregate {
		*limit = 1
	}
	if *clientID == "" {
		return fmt.Errorf("client-id is required")
	}
	if *once && !*noRun {
		return fmt.Errorf("--once requires --no-run")
	}
	proxyQuery, err := proxyQueryString(proxies)
	if err != nil {
		return err
	}
	manager := newFrpcManager(*frpcBin, *noRun, *drainTimeout)
	defer manager.stopAll()
	syncOnce := func(excludeNodes ...string) error {
		files, err := fetchFrpcConfigFiles(*controlURL, *clientID, *mode, *limit, proxyQuery, excludeNodes...)
		if err != nil {
			return err
		}
		paths, err := writeConfigFiles(*workDir, files)
		if err != nil {
			return err
		}
		if err := manager.reconcile(paths); err != nil {
			return err
		}
		fmt.Printf("client synced client_id=%s mode=%s files=%d run=%t\n", *clientID, *mode, len(paths), !*noRun)
		return nil
	}
	if err := syncOnce(); err != nil {
		return err
	}
	if *once {
		return nil
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(*interval)
	failoverTicker := time.NewTicker(*failoverInterval)
	defer ticker.Stop()
	defer failoverTicker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := syncOnce(); err != nil {
				fmt.Fprintf(os.Stderr, "client sync failed: %v\n", err)
			}
		case <-failoverTicker.C:
			exited := manager.exitedNodeIDs()
			if len(exited) > 0 {
				fmt.Fprintf(os.Stderr, "frpc process exited, retrying config sync exclude=%s\n", strings.Join(exited, ","))
				if err := syncOnce(exited...); err != nil {
					fmt.Fprintf(os.Stderr, "client failover sync failed: %v\n", err)
				}
			}
		case <-signals:
			return nil
		}
	}
}

func runConfig(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("config requires frps or frpc")
	}
	switch args[0] {
	case "frps":
		fs := flag.NewFlagSet("config frps", flag.ExitOnError)
		controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
		nodeID := fs.String("node-id", "", "node id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		text, err := getText(fmt.Sprintf("%s/api/v1/config/frps?node_id=%s", strings.TrimRight(*controlURL, "/"), *nodeID))
		if err != nil {
			return err
		}
		fmt.Print(text)
	case "frpc":
		fs := flag.NewFlagSet("config frpc", flag.ExitOnError)
		controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
		clientID := fs.String("client-id", "client", "client id")
		mode := fs.String("mode", control.ConfigModeAggregate, "single, failover, or aggregate")
		limit := fs.Int("limit", 0, "node limit for failover")
		outDir := fs.String("out-dir", "", "write frpc config files to directory")
		proxies := multiFlag{}
		fs.Var(&proxies, "proxy", "business proxy as name:type:localIP:localPort:remotePort; repeatable")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		proxyQuery, err := proxyQueryString(proxies)
		if err != nil {
			return err
		}
		if *outDir != "" {
			url := fmt.Sprintf("%s/api/v1/config/frpc?client_id=%s&mode=%s&limit=%d&format=json%s", strings.TrimRight(*controlURL, "/"), *clientID, *mode, *limit, proxyQuery)
			var payload struct {
				Files map[string]string `json:"files"`
			}
			if err := getJSON(url, &payload); err != nil {
				return err
			}
			if err := os.MkdirAll(*outDir, 0o755); err != nil {
				return err
			}
			for name, content := range payload.Files {
				if err := os.WriteFile(filepath.Join(*outDir, filepath.Base(name)), []byte(content), 0o600); err != nil {
					return err
				}
				fmt.Println(filepath.Join(*outDir, filepath.Base(name)))
			}
			return nil
		}
		url := fmt.Sprintf("%s/api/v1/config/frpc?client_id=%s&mode=%s&limit=%d%s", strings.TrimRight(*controlURL, "/"), *clientID, *mode, *limit, proxyQuery)
		text, err := getText(url)
		if err != nil {
			return err
		}
		fmt.Print(text)
	default:
		return fmt.Errorf("unknown config target %q", args[0])
	}
	return nil
}

func runHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	controlURL := fs.String("control-url", "http://127.0.0.1:8080", "control plane URL")
	timeout := fs.Duration("timeout", 5*time.Second, "HTTP request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	payload := map[string]string{}
	if err := getJSONWithTimeout(strings.TrimRight(*controlURL, "/")+"/api/v1/health", &payload, *timeout); err != nil {
		return err
	}
	if payload["status"] != "ok" {
		return fmt.Errorf("unexpected health status %q", payload["status"])
	}
	fmt.Println("ok")
	return nil
}

func postJSON(url string, value any, target any) error {
	return postJSONWithTimeout(url, value, target, 0)
}

func postAdminJSON(controlURL, path string, value any, target any, adminPasswordFile string) error {
	client := &http.Client{}
	base := strings.TrimRight(controlURL, "/")
	err := postJSONWithClient(client, base+path, value, target)
	if err == nil || adminPasswordFile == "" || !strings.Contains(err.Error(), "401") {
		return err
	}
	password, readErr := os.ReadFile(adminPasswordFile)
	if readErr != nil {
		return err
	}
	var loginResp map[string]bool
	if loginErr := postJSONWithClient(client, base+"/api/v1/auth/login", map[string]string{"code": strings.TrimSpace(string(password))}, &loginResp); loginErr != nil {
		if setupErr := bootstrapTOTPForCLI(client, base, strings.TrimSpace(string(password))); setupErr != nil {
			return loginErr
		}
	}
	return postJSONWithClient(client, base+path, value, target)
}

func bootstrapTOTPForCLI(client *http.Client, base, bootstrapPassword string) error {
	var setup struct {
		Secret string `json:"secret"`
	}
	if err := postJSONWithClient(client, base+"/api/v1/auth/totp/setup", map[string]string{
		"bootstrap_password": bootstrapPassword,
		"account":            "cli-bootstrap",
	}, &setup); err != nil {
		return err
	}
	code := control.GenerateTOTPForCLI(setup.Secret, time.Now())
	var confirm map[string]bool
	return postJSONWithClient(client, base+"/api/v1/auth/totp/confirm", map[string]string{
		"bootstrap_password": bootstrapPassword,
		"secret":             setup.Secret,
		"code":               code,
		"account":            "cli-bootstrap",
	}, &confirm)
}

func postJSONWithClient(client *http.Client, url string, value any, target any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func postJSONWithTimeout(url string, value any, target any, timeout time.Duration) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	client := http.DefaultClient
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func getText(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

func getJSON(url string, target any) error {
	return getJSONWithTimeout(url, target, 0)
}

func getJSONWithTimeout(url string, target any, timeout time.Duration) error {
	client := http.DefaultClient
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func fetchFrpcConfigFiles(controlURL, clientID, mode string, limit int, proxyQuery string, excludeNodeIDs ...string) (map[string]string, error) {
	url := fmt.Sprintf("%s/api/v1/config/frpc?client_id=%s&mode=%s&limit=%d&format=json%s", strings.TrimRight(controlURL, "/"), urlQueryEscape(clientID), urlQueryEscape(mode), limit, proxyQuery)
	for _, nodeID := range excludeNodeIDs {
		if nodeID = strings.TrimSpace(nodeID); nodeID != "" {
			url += "&exclude_node=" + urlQueryEscape(nodeID)
		}
	}
	var payload struct {
		Files map[string]string `json:"files"`
	}
	if err := getJSON(url, &payload); err != nil {
		return nil, err
	}
	if len(payload.Files) == 0 {
		return nil, fmt.Errorf("control plane returned no frpc config files")
	}
	return payload.Files, nil
}

func writeConfigFiles(dir string, files map[string]string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	paths := make([]string, 0, len(names))
	keep := map[string]bool{}
	for _, name := range names {
		base := filepath.Base(name)
		path := filepath.Join(dir, base)
		keep[path] = true
		if err := writeFileIfChanged(path, []byte(files[name])); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	existing, err := filepath.Glob(filepath.Join(dir, "frpc-*.toml"))
	if err == nil {
		for _, path := range existing {
			if keep[path] {
				continue
			}
			_ = os.Remove(path)
		}
	}
	return paths, nil
}

func writeFileIfChanged(path string, data []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type frpcManager struct {
	bin       string
	noRun     bool
	drain     time.Duration
	processes map[string]*frpcProcess
}

type frpcProcess struct {
	cmd  *exec.Cmd
	hash [32]byte
	done chan struct{}
}

func newFrpcManager(bin string, noRun bool, drain time.Duration) *frpcManager {
	if drain < 0 {
		drain = 0
	}
	return &frpcManager{
		bin:       bin,
		noRun:     noRun,
		drain:     drain,
		processes: map[string]*frpcProcess{},
	}
}

func (m *frpcManager) reconcile(paths []string) error {
	desired := map[string][32]byte{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		desired[path] = sha256.Sum256(data)
	}
	for path, hash := range desired {
		if existing := m.processes[path]; existing != nil && existing.hash == hash && processRunning(existing) {
			continue
		}
		if existing := m.processes[path]; existing != nil {
			m.stop(path)
		}
		if m.noRun {
			m.processes[path] = &frpcProcess{hash: hash}
			continue
		}
		cmd := exec.Command(m.bin, "-c", path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start frpc for %s: %w", path, err)
		}
		process := &frpcProcess{cmd: cmd, hash: hash, done: make(chan struct{})}
		go func() {
			_ = cmd.Wait()
			close(process.done)
		}()
		m.processes[path] = process
	}
	stale := make([]string, 0)
	for path := range m.processes {
		if _, ok := desired[path]; ok {
			continue
		}
		stale = append(stale, path)
	}
	for _, path := range stale {
		m.stopAfterDrain(path)
	}
	return nil
}

func (m *frpcManager) stopAfterDrain(path string) {
	if m.drain == 0 || m.noRun {
		m.stop(path)
		return
	}
	process := m.processes[path]
	delete(m.processes, path)
	time.Sleep(m.drain)
	stopProcess(process)
}

func (m *frpcManager) stop(path string) {
	process := m.processes[path]
	delete(m.processes, path)
	stopProcess(process)
}

func stopProcess(process *frpcProcess) {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return
	}
	_ = process.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		_ = process.cmd.Process.Kill()
		<-process.done
	}
}

func (m *frpcManager) stopAll() {
	paths := make([]string, 0, len(m.processes))
	for path := range m.processes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		m.stop(path)
	}
}

func (m *frpcManager) exitedNodeIDs() []string {
	nodes := make([]string, 0)
	seen := map[string]bool{}
	for path, process := range m.processes {
		if !processRunning(process) {
			nodeID := readClusterNodeFromFrpcConfig(path)
			if nodeID != "" && !seen[nodeID] {
				nodes = append(nodes, nodeID)
				seen[nodeID] = true
			}
		}
	}
	sort.Strings(nodes)
	return nodes
}

func processRunning(process *frpcProcess) bool {
	if process == nil || process.cmd == nil {
		return true
	}
	if process.done == nil {
		return true
	}
	select {
	case <-process.done:
		return false
	default:
		return true
	}
}

func readClusterNodeFromFrpcConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "metadatas.cluster_node") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}

type networkCollector struct {
	lastSample networkDeviceSample
}

type networkDeviceSample struct {
	rxBytes int64
	txBytes int64
	at      time.Time
	ok      bool
}

func newNetworkCollector() *networkCollector {
	return &networkCollector{lastSample: readNetworkDeviceSample()}
}

func (c *networkCollector) measure(controlURL string, probeSize int) control.NetworkStatus {
	now := time.Now().UTC()
	status := control.NetworkStatus{MeasuredAt: now}
	status.LatencyMS = measureLatencyMS(controlURL)
	if probeSize > 0 {
		down, up := measureProbeBandwidth(controlURL, probeSize)
		status.DownloadBandwidthBps = down
		status.UploadBandwidthBps = up
	}
	sample := readNetworkDeviceSample()
	if sample.ok && c.lastSample.ok && sample.at.After(c.lastSample.at) {
		elapsed := sample.at.Sub(c.lastSample.at).Seconds()
		if elapsed > 0 {
			status.ObservedRxBps = int64(float64(sample.rxBytes-c.lastSample.rxBytes) / elapsed)
			status.ObservedTxBps = int64(float64(sample.txBytes-c.lastSample.txBytes) / elapsed)
			if status.ObservedRxBps < 0 {
				status.ObservedRxBps = 0
			}
			if status.ObservedTxBps < 0 {
				status.ObservedTxBps = 0
			}
		}
	}
	if sample.ok {
		c.lastSample = sample
	}
	return status
}

func measureLatencyMS(controlURL string) int64 {
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, controlURL+"/api/v1/health", nil)
	if err != nil {
		return 0
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0
	}
	ms := time.Since(start).Milliseconds()
	if ms < 1 {
		return 1
	}
	return ms
}

func measureProbeBandwidth(controlURL string, probeSize int) (int64, int64) {
	downloadBps := measureDownloadBandwidth(controlURL, probeSize)
	uploadBps := measureUploadBandwidth(controlURL, probeSize)
	return downloadBps, uploadBps
}

func measureDownloadBandwidth(controlURL string, probeSize int) int64 {
	start := time.Now()
	resp, err := http.Get(controlURL + "/api/v1/network/probe?size=" + strconv.Itoa(probeSize))
	if err != nil {
		return 0
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0
	}
	return bytesPerSecond(n, time.Since(start))
}

func measureUploadBandwidth(controlURL string, probeSize int) int64 {
	if probeSize <= 0 {
		return 0
	}
	payload := bytes.NewReader(make([]byte, probeSize))
	start := time.Now()
	resp, err := http.Post(controlURL+"/api/v1/network/probe", "application/octet-stream", payload)
	if err != nil {
		return 0
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0
	}
	return bytesPerSecond(int64(probeSize), time.Since(start))
}

func bytesPerSecond(bytes int64, elapsed time.Duration) int64 {
	if bytes <= 0 || elapsed <= 0 {
		return 0
	}
	return int64(float64(bytes) / elapsed.Seconds())
}

func readNetworkDeviceSample() networkDeviceSample {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return networkDeviceSample{}
	}
	var sample networkDeviceSample
	sample.at = time.Now().UTC()
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 17 || !strings.HasSuffix(parts[0], ":") {
			continue
		}
		iface := strings.TrimSuffix(parts[0], ":")
		if iface == "lo" {
			continue
		}
		rx, errRx := strconv.ParseInt(parts[1], 10, 64)
		tx, errTx := strconv.ParseInt(parts[9], 10, 64)
		if errRx != nil || errTx != nil {
			continue
		}
		sample.rxBytes += rx
		sample.txBytes += tx
		sample.ok = true
	}
	return sample
}

func scrapeFRPSTraffic(baseURL string) (control.TrafficCounters, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return control.TrafficCounters{}, fmt.Errorf("frps dashboard url is empty")
	}
	client := http.Client{Timeout: 3 * time.Second}
	traffic := control.TrafficCounters{MeasuredAt: time.Now().UTC()}
	serverInfo, err := getDashboardJSON(client, baseURL+"/api/serverinfo")
	if err != nil {
		return control.TrafficCounters{}, err
	}
	traffic.TotalInBytes = int64Field(serverInfo, "totalTrafficIn", "total_traffic_in", "trafficIn", "traffic_in")
	traffic.TotalOutBytes = int64Field(serverInfo, "totalTrafficOut", "total_traffic_out", "trafficOut", "traffic_out")
	traffic.CurrentConnections = int64Field(serverInfo, "curConns", "cur_conns", "currentConnections")
	for _, proxyType := range []string{"tcp", "udp", "http", "https", "tcpmux", "stcp", "xtcp", "sudp"} {
		proxyPayload, err := getDashboardJSON(client, baseURL+"/api/proxy/"+proxyType)
		if err != nil {
			continue
		}
		proxies, _ := proxyPayload["proxies"].([]any)
		for _, raw := range proxies {
			proxy, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			item := control.ProxyTraffic{
				Name:               stringFieldAny(proxy, "name"),
				Type:               proxyType,
				ClientID:           stringFieldAny(proxy, "clientID", "client_id"),
				TotalInBytes:       int64Field(proxy, "todayTrafficIn", "totalTrafficIn", "trafficIn", "inBytes"),
				TotalOutBytes:      int64Field(proxy, "todayTrafficOut", "totalTrafficOut", "trafficOut", "outBytes"),
				CurrentConnections: int64Field(proxy, "curConns", "cur_conns", "currentConnections"),
				Status:             stringFieldAny(proxy, "status"),
			}
			if item.Name == "" {
				continue
			}
			traffic.Proxies = append(traffic.Proxies, item)
		}
	}
	if (traffic.TotalInBytes == 0 && traffic.TotalOutBytes == 0) && len(traffic.Proxies) > 0 {
		for _, proxy := range traffic.Proxies {
			traffic.TotalInBytes += proxy.TotalInBytes
			traffic.TotalOutBytes += proxy.TotalOutBytes
			traffic.CurrentConnections += proxy.CurrentConnections
		}
	}
	return traffic, nil
}

func getDashboardJSON(client http.Client, url string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("frps dashboard %s status=%d", url, resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func int64Field(value map[string]any, names ...string) int64 {
	for _, name := range names {
		raw, ok := value[name]
		if !ok {
			continue
		}
		switch typed := raw.(type) {
		case float64:
			if typed > 0 {
				return int64(typed)
			}
		case int64:
			if typed > 0 {
				return typed
			}
		case json.Number:
			if parsed, err := typed.Int64(); err == nil && parsed > 0 {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return 0
}

func stringFieldAny(value map[string]any, names ...string) string {
	for _, name := range names {
		if raw, ok := value[name]; ok {
			return strings.TrimSpace(fmt.Sprint(raw))
		}
	}
	return ""
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func proxyQueryString(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if _, err := control.ParseProxySpec(value); err != nil {
			return "", err
		}
		parts = append(parts, "proxy="+urlQueryEscape(value))
	}
	return "&" + strings.Join(parts, "&"), nil
}

func urlQueryEscape(value string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		" ", "%20",
		":", "%3A",
		"/", "%2F",
		"&", "%26",
		"=", "%3D",
		"?", "%3F",
		"#", "%23",
	)
	return replacer.Replace(value)
}
