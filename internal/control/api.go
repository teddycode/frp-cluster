package control

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleWeb)
	mux.HandleFunc("/api/v1/auth/login", a.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", a.handleLogout)
	mux.HandleFunc("/api/v1/auth/me", a.handleAuthMe)
	mux.HandleFunc("/api/v1/health", a.handleHealth)
	mux.HandleFunc("/api/v1/network/probe", a.handleNetworkProbe)
	mux.HandleFunc("/api/v1/cluster", a.requireAdmin(a.handleCluster))
	mux.HandleFunc("/api/v1/dns/test", a.requireAdmin(a.handleDNSTest))
	mux.HandleFunc("/api/v1/settings", a.requireAdmin(a.handleSettings))
	mux.HandleFunc("/api/v1/tokens", a.requireAdmin(a.handleTokens))
	mux.HandleFunc("/api/v1/nodes/join", a.handleJoinNode)
	mux.HandleFunc("/api/v1/nodes/", a.handleNodeAction)
	mux.HandleFunc("/api/v1/clients/", a.requireAdmin(a.handleClientAction))
	mux.HandleFunc("/api/v1/config/frps", a.handleFrpsConfig)
	mux.HandleFunc("/api/v1/config/frpc", a.handleFrpcConfig)
	mux.HandleFunc("/api/v1/commands/join", a.requireAdmin(a.handleJoinCommand))
	mux.HandleFunc("/api/v1/peer/state", a.handlePeerState)
	mux.HandleFunc("/api/v1/frp/plugin", a.handlePlugin)
	mux.HandleFunc("/api/v1/frp/plugin/", a.handlePlugin)
	return withCommonHeaders(mux)
}

func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *API) handleWeb(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	webDir := resolveWebDir(a.webDir)
	if webDir == "" {
		writeError(w, http.StatusNotFound, "web assets not configured")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	fullPath := filepath.Join(webDir, path)
	if rel, err := filepath.Rel(webDir, fullPath); err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, fullPath)
		return
	}
	http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
}

func (a *API) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.validSession(r) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		next(w, r)
	}
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	password := a.currentAdminPassword()
	if password == "" {
		writeError(w, http.StatusPreconditionRequired, "admin password not configured")
		return
	}
	if subtleConstantTimeCompare(req.Password, password) == false {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	token, err := a.createSession()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	a.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (a *API) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"auth_enabled":  a.isAdminAuthEnabled(),
		"authenticated": a.validSession(r),
	})
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleNetworkProbe(w http.ResponseWriter, r *http.Request) {
	const (
		defaultProbeBytes = 256 * 1024
		maxProbeBytes     = 8 * 1024 * 1024
	)
	size := defaultProbeBytes
	if raw := r.URL.Query().Get("size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid probe size")
			return
		}
		size = parsed
	}
	if size > maxProbeBytes {
		size = maxProbeBytes
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/octet-stream")
		chunk := make([]byte, 32*1024)
		for i := range chunk {
			chunk[i] = byte('a' + i%26)
		}
		remaining := size
		for remaining > 0 {
			n := len(chunk)
			if remaining < n {
				n = remaining
			}
			if _, err := w.Write(chunk[:n]); err != nil {
				return
			}
			remaining -= n
		}
	case http.MethodPost:
		n, _ := io.Copy(io.Discard, io.LimitReader(r.Body, int64(maxProbeBytes)+1))
		_ = r.Body.Close()
		if n > maxProbeBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "probe body too large")
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"bytes": n})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *API) handleCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, a.store.Snapshot())
}

func (a *API) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.store.Snapshot().Config)
	case http.MethodPatch:
		var req struct {
			AutoMigration     *bool   `json:"auto_migration"`
			MigrationScoreGap *int    `json:"migration_score_gap"`
			PublicEntryHost   *string `json:"public_entry_host"`
			DNSUpdateHook     *string `json:"dns_update_hook"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		config, err := a.store.UpdateConfig(ConfigUpdate{
			AutoMigration:     req.AutoMigration,
			MigrationScoreGap: req.MigrationScoreGap,
			PublicEntryHost:   req.PublicEntryHost,
			DNSUpdateHook:     req.DNSUpdateHook,
		})
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, config)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *API) handleDNSTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		NodeID   string `json:"node_id"`
		ClientID string `json:"client_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "node_id required")
		return
	}
	result, err := a.store.TestDNSUpdate(req.ClientID, req.NodeID)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dns": result})
}

func (a *API) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"tokens": a.store.Snapshot().Tokens})
	case http.MethodPost:
		var req struct {
			TTL  string `json:"ttl"`
			Uses int    `json:"uses"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		ttl := 2 * time.Hour
		if req.TTL != "" {
			parsed, err := time.ParseDuration(req.TTL)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid ttl")
				return
			}
			ttl = parsed
		}
		token, err := a.store.CreateJoinToken(ttl, req.Uses)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, token)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *API) handleJoinNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req JoinRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := a.store.JoinNode(req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) handleNodeAction(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	nodeID, action := parts[0], parts[1]
	var req HeartbeatRequest
	if r.Method == http.MethodPost {
		_ = readJSON(r, &req)
	}
	nodeToken := firstNonEmpty(req.NodeToken, bearerToken(r), r.URL.Query().Get("node_token"))
	switch action {
	case "heartbeat":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		req.NodeToken = nodeToken
		node, err := a.store.HeartbeatNodeWithRequest(nodeID, req)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, node)
	case "leave":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		node, err := a.store.LeaveNode(nodeID, nodeToken)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, node)
	case "admin-leave":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !a.validSession(r) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		node, err := a.store.AdminLeaveNode(nodeID)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, node)
	default:
		http.NotFound(w, r)
	}
}

func (a *API) handleClientAction(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/clients/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	clientID, action := parts[0], parts[1]
	switch action {
	case "target":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			NodeID string `json:"node_id"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var dnsResult DNSUpdateResult
		if strings.TrimSpace(req.NodeID) != "" {
			var err error
			dnsResult, err = a.store.UpdateDNSForNode(clientID, req.NodeID)
			if err != nil {
				writeMappedError(w, err)
				return
			}
		}
		client, err := a.store.SetClientTarget(clientID, req.NodeID)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"client": client, "dns": dnsResult})
	case "auto-target":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			NodeID string `json:"node_id"`
			Reason string `json:"reason"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(req.NodeID) == "" {
			writeError(w, http.StatusBadRequest, "node_id required")
			return
		}
		dnsResult, err := a.store.UpdateDNSForNode(clientID, req.NodeID)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		client, err := a.store.AutoSwitchClientTarget(clientID, req.NodeID, req.Reason)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"client": client, "dns": dnsResult})
	default:
		http.NotFound(w, r)
	}
}

func (a *API) handleFrpsConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	config, err := a.store.GenerateFrpsConfig(FrpsConfigOptions{
		NodeID:     r.URL.Query().Get("node_id"),
		ControlURL: controlURLFromRequest(r),
	})
	if err != nil {
		writeMappedError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, config)
}

func (a *API) handleFrpcConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	proxies, err := proxySpecsFromQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	opts := FrpcConfigOptions{
		ClientID:       r.URL.Query().Get("client_id"),
		Mode:           r.URL.Query().Get("mode"),
		Limit:          limit,
		Proxies:        proxies,
		ExcludeNodeIDs: cleanList(r.URL.Query()["exclude_node"]),
	}
	if r.URL.Query().Get("format") == "json" {
		files, err := a.store.GenerateFrpcConfigFiles(opts)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files})
		return
	}
	config, err := a.store.GenerateFrpcConfig(opts)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, config)
}

func proxySpecsFromQuery(r *http.Request) ([]ProxySpec, error) {
	values := r.URL.Query()["proxy"]
	specs := make([]ProxySpec, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		spec, err := ParseProxySpec(value)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func (a *API) handleJoinCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	controlURL := q.Get("control_url")
	if controlURL == "" {
		controlURL = controlURLFromRequest(r)
	}
	bin := q.Get("bin")
	if bin == "" {
		bin = "frp-cluster"
	}
	args := []string{
		bin, "join",
		"--control-url", shellQuote(controlURL),
		"--token", shellQuote(q.Get("token")),
		"--node-id", shellQuote(q.Get("node_id")),
		"--public-addr", shellQuote(q.Get("public_addr")),
	}
	if q.Get("bind_port") != "" {
		args = append(args, "--bind-port", shellQuote(q.Get("bind_port")))
	}
	if q.Get("region") != "" {
		args = append(args, "--region", shellQuote(q.Get("region")))
	}
	if q.Get("node_control_url") != "" {
		args = append(args, "--node-control-url", shellQuote(q.Get("node_control_url")))
	}
	if q.Get("write_frps_config") != "" {
		args = append(args, "--write-frps-config", shellQuote(q.Get("write_frps_config")))
	}
	command := strings.Join(args, " ")
	writeJSON(w, http.StatusOK, map[string]string{"command": command})
}

func (a *API) handlePeerState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.store.RawState())
	case http.MethodPost:
		var state ClusterState
		if err := readJSON(r, &state); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sourceURL := r.URL.Query().Get("source_url")
		if sourceURL == "" {
			sourceURL = controlURLFromRequest(r)
		}
		if err := a.store.MergeState(state, sourceURL); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "merged"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *API) handlePlugin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var event PluginEvent
	if err := readLooseJSON(r, &event); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if event.Op == "" {
		event.Op = r.URL.Query().Get("op")
	}
	if event.NodeID == "" {
		event.NodeID = r.URL.Query().Get("node_id")
	}
	pathNodeID := strings.TrimPrefix(r.URL.Path, "/api/v1/frp/plugin/")
	if pathNodeID == r.URL.Path {
		pathNodeID = ""
	}
	if err := a.store.ApplyPluginEvent(firstNonEmpty(r.URL.Query().Get("node_id"), pathNodeID), event); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reject": false, "unchange": true})
}

func readJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func readLooseJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeMappedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidJoinRequest), errors.Is(err, ErrInvalidToken), errors.Is(err, ErrTokenExpired), errors.Is(err, ErrTokenUsed), errors.Is(err, ErrNodeTokenRequired), errors.Is(err, ErrNodeTokenMismatch):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrNodeNotFound), errors.Is(err, ErrNoAvailableNode), errors.Is(err, ErrClientNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDNSHookNotConfigured):
		writeError(w, http.StatusPreconditionRequired, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func subtleConstantTimeCompare(got, want string) bool {
	gotBytes := []byte(got)
	wantBytes := []byte(want)
	if len(gotBytes) != len(wantBytes) {
		return false
	}
	return subtle.ConstantTimeCompare(gotBytes, wantBytes) == 1
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

func controlURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	return scheme + "://" + strings.TrimSpace(host)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.ContainsAny(value, " \t\n'\"$`\\") {
		return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
	}
	return value
}
