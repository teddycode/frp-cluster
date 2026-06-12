package control

import (
	"crypto/rand"
	"encoding/hex"
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
	SessionTTL        time.Duration
}

type API struct {
	store *Store

	webDir            string
	adminPassword     string
	adminPasswordFile string
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
		sessionTTL:        ttl,
		sessions:          map[string]time.Time{},
	}
}

func (a *API) isAdminAuthEnabled() bool {
	return a.currentAdminPassword() != ""
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
