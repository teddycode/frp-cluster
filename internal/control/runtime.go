package control

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type RuntimeOptions struct {
	WebDir            string
	AdminPassword     string
	AdminPasswordFile string
	AuthConfigFile    string
	AliDNSConfigFile  string
	NodeEnvFile       string
	SessionTTL        time.Duration
}

type TOTPConfig struct {
	Secret  string
	Issuer  string
	Account string
}

type API struct {
	store *Store

	webDir            string
	adminPassword     string
	adminPasswordFile string
	authConfigFile    string
	aliDNSConfigFile  string
	nodeEnvFile       string
	sessionTTL        time.Duration

	sessionMu sync.Mutex
	sessions  map[string]time.Time
}

func NewAPI(store *Store) *API {
	return NewAPIWithOptions(store, RuntimeOptions{})
}

func NewAPIWithOptions(store *Store, opts RuntimeOptions) *API {
	ttl := opts.SessionTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &API{
		store:             store,
		webDir:            strings.TrimSpace(opts.WebDir),
		adminPassword:     strings.TrimSpace(opts.AdminPassword),
		adminPasswordFile: strings.TrimSpace(opts.AdminPasswordFile),
		authConfigFile:    strings.TrimSpace(opts.AuthConfigFile),
		aliDNSConfigFile:  strings.TrimSpace(opts.AliDNSConfigFile),
		nodeEnvFile:       strings.TrimSpace(opts.NodeEnvFile),
		sessionTTL:        ttl,
		sessions:          map[string]time.Time{},
	}
}

func (a *API) isAdminAuthEnabled() bool {
	return a.totpConfigured() || a.currentAdminPassword() != ""
}

func (a *API) currentAdminPassword() string {
	if a.adminPassword != "" {
		return a.adminPassword
	}
	if a.adminPasswordFile == "" {
		return ""
	}
	data, err := os.ReadFile(a.adminPasswordFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a *API) currentTOTPConfig() (TOTPConfig, error) {
	if a.authConfigFile == "" {
		return TOTPConfig{}, os.ErrNotExist
	}
	values, err := ReadEnvFile(a.authConfigFile)
	if err != nil {
		return TOTPConfig{}, err
	}
	cfg := TOTPConfig{
		Secret:  normalizeTOTPSecret(values["TOTP_SECRET"]),
		Issuer:  strings.TrimSpace(values["TOTP_ISSUER"]),
		Account: strings.TrimSpace(values["TOTP_ACCOUNT"]),
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "frp-cluster"
	}
	if cfg.Account == "" {
		cfg.Account = "admin"
	}
	if cfg.Secret == "" {
		return TOTPConfig{}, os.ErrNotExist
	}
	return cfg, nil
}

func (a *API) totpConfigured() bool {
	_, err := a.currentTOTPConfig()
	return err == nil
}

func (a *API) writeTOTPConfig(cfg TOTPConfig) error {
	if a.authConfigFile == "" {
		return errors.New("auth config file not configured")
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "frp-cluster"
	}
	if cfg.Account == "" {
		cfg.Account = "admin"
	}
	return WriteEnvFile(a.authConfigFile, map[string]string{
		"TOTP_SECRET":  normalizeTOTPSecret(cfg.Secret),
		"TOTP_ISSUER":  cfg.Issuer,
		"TOTP_ACCOUNT": cfg.Account,
	}, 0o600)
}

func (a *API) createSession() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw[:])
	a.sessionMu.Lock()
	a.sessions[token] = time.Now().Add(a.sessionTTL)
	a.sessionMu.Unlock()
	return token, nil
}

func (a *API) clearSession(token string) {
	if token == "" {
		return
	}
	a.sessionMu.Lock()
	delete(a.sessions, token)
	a.sessionMu.Unlock()
}

func (a *API) validSession(r *http.Request) bool {
	if !a.isAdminAuthEnabled() {
		return true
	}
	cookie, err := r.Cookie("frp_cluster_session")
	if err != nil || cookie.Value == "" {
		return false
	}
	now := time.Now()
	a.sessionMu.Lock()
	expires, ok := a.sessions[cookie.Value]
	if ok && now.After(expires) {
		delete(a.sessions, cookie.Value)
		ok = false
	}
	if ok {
		a.sessions[cookie.Value] = now.Add(a.sessionTTL)
	}
	a.sessionMu.Unlock()
	return ok
}

func (a *API) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "frp_cluster_session",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(a.sessionTTL),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("frp_cluster_session"); err == nil {
		a.clearSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "frp_cluster_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func resolveWebDir(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return value
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return abs
}
