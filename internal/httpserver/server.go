package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/liansishen/go-webssh/internal/auth"
	"github.com/liansishen/go-webssh/internal/config"
	"github.com/liansishen/go-webssh/internal/vault"
	"github.com/liansishen/go-webssh/internal/ws"
	"github.com/liansishen/go-webssh/themes"
)

type Server struct {
	cfg          *config.Config
	auth         *auth.Authenticator
	ws           *ws.Handler
	logger       *slog.Logger
	mux          *http.ServeMux
	server       *http.Server
	staticFS     http.FileSystem
	vault        *vault.Store
	themeCatalog *themes.Catalog
}

func New(cfg *config.Config, authenticator *auth.Authenticator, wsHandler *ws.Handler, credentialStore *vault.Store, staticFS http.FileSystem, themeCatalog *themes.Catalog, logger *slog.Logger) *Server {
	if themeCatalog == nil {
		themeCatalog = &themes.Catalog{Dir: cfg.UI.ThemesDir}
	}
	s := &Server{
		cfg:          cfg,
		auth:         authenticator,
		ws:           wsHandler,
		logger:       logger,
		mux:          http.NewServeMux(),
		staticFS:     staticFS,
		vault:        credentialStore,
		themeCatalog: themeCatalog,
	}
	s.routes()
	s.server = &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           s.withSecurityHeaders(s.mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout left zero because of long-lived WebSocket.
		IdleTimeout: 120 * time.Second,
	}
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("GET /api/config/ui", s.handleUIConfig)
	s.mux.HandleFunc("GET /api/credentials", s.handleCredentialList)
	s.mux.HandleFunc("POST /api/credentials", s.handleCredentialPut)
	s.mux.HandleFunc("POST /api/credentials/migrate", s.handleCredentialMigrate)
	s.mux.HandleFunc("GET /api/credentials/{id}", s.handleCredentialGet)
	s.mux.HandleFunc("DELETE /api/credentials/{id}", s.handleCredentialDelete)
	s.mux.Handle("GET /api/ws/ssh", s.ws)
	s.mux.HandleFunc("GET /api/ws/tunnel", s.ws.ServeTunnelHTTP)

	// Static assets and SPA entry.
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /app.js", s.serveStatic)
	s.mux.HandleFunc("GET /style.css", s.serveStatic)
	s.mux.HandleFunc("GET /themes.js", s.handleThemesJS)
	s.mux.HandleFunc("GET /favicon.svg", s.serveStatic)
	s.mux.HandleFunc("GET /vendor/", s.serveStatic)
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// Conservative CSP: scripts/styles from self; connect to self (ws/wss same origin).
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.serveFile(w, r, "index.html")
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	s.serveFile(w, r, path)
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	// The frontend and embedded assets are released together. Force clients and
	// reverse proxies to revalidate so an old app.js is never paired with a new
	// index.html after deployment.
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	f, err := s.staticFS.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, stat.ModTime(), f)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}
	ip := clientIP(r)
	if err := s.auth.Login(w, req.Username, req.Password, req.Remember); err != nil {
		s.logger.Warn("login failed", "remote_ip", ip, "username", req.Username)
		writeErr(w, http.StatusUnauthorized, "AUTH_FAILED", "invalid username or password")
		return
	}
	s.logger.Info("login success", "remote_ip", ip, "username", req.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.auth.SessionFromRequest(r); !ok {
		writeErr(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	s.auth.Logout(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if info, ok := s.auth.SessionFromRequest(r); ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"username":      info.Username,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (s *Server) handleUIConfig(w http.ResponseWriter, r *http.Request) {
	themeNames, err := s.themeCatalog.Names()
	if err != nil {
		s.logger.Error("load themes", "err", err)
		writeErr(w, http.StatusInternalServerError, "THEMES_LOAD_FAILED", "failed to load terminal themes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hostKeyPolicy":         s.cfg.SSH.HostKeyPolicy,
		"privateKeyPersistence": "memory-only",
		"maxPrivateKeyBytes":    131072,
		"allowPrivateRanges":    s.cfg.NetworkPolicy.AllowPrivateRanges,
		"credentialStorage":     s.vault != nil,
		"themesDir":             s.cfg.UI.ThemesDir,
		"themes":                themeNames,
	})
}

func (s *Server) handleThemesJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	if err := s.themeCatalog.WriteJS(w); err != nil {
		s.logger.Error("write themes.js", "err", err)
		http.Error(w, "failed to load themes", http.StatusInternalServerError)
	}
}

type credentialSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Username      string    `json:"username"`
	Term          string    `json:"term"`
	UseHerdr      bool      `json:"useHerdr"`
	LegacyUseTmux bool      `json:"useTmux"`
	HasPrivateKey bool      `json:"hasPrivateKey"`
	HasPassphrase bool      `json:"hasPassphrase"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (s *Server) credentialSession(w http.ResponseWriter, r *http.Request) (auth.SessionInfo, bool) {
	info, ok := s.auth.SessionFromRequest(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return auth.SessionInfo{}, false
	}
	if s.vault == nil {
		writeErr(w, http.StatusServiceUnavailable, "CREDENTIAL_STORAGE_DISABLED", "credential storage is disabled")
		return auth.SessionInfo{}, false
	}
	return info, true
}

func (s *Server) handleCredentialList(w http.ResponseWriter, r *http.Request) {
	info, ok := s.credentialSession(w, r)
	if !ok {
		return
	}
	items, unmigrated, err := s.vault.ListSummaries(info.Username)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "VAULT_DECRYPT_FAILED", "credential vault could not be decrypted")
		return
	}
	summaries := make([]credentialSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, credentialSummary{ID: item.ID, Name: item.Name, Host: item.Host, Port: item.Port, Username: item.Username, Term: item.Term, UseHerdr: item.UseHerdr, LegacyUseTmux: item.UseHerdr, HasPrivateKey: item.HasPrivateKey, HasPassphrase: item.HasPassphrase, UpdatedAt: item.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": summaries, "removedCredentials": 0, "unmigratedCredentials": unmigrated})
}

func (s *Server) handleCredentialGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.credentialSession(w, r); !ok {
		return
	}
	item, err := s.vault.Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "credential not found")
			return
		}
		writeErr(w, http.StatusUnprocessableEntity, "VAULT_DECRYPT_FAILED", "credential vault could not be decrypted")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleCredentialPut(w http.ResponseWriter, r *http.Request) {
	info, ok := s.credentialSession(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024+1))
	if err != nil || len(body) > 256*1024 {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "credential is too large")
		return
	}
	defer clearBytes(body)
	var item vault.Credential
	if json.Unmarshal(body, &item) != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}
	item.Name, item.Host, item.Username = strings.TrimSpace(item.Name), strings.TrimSpace(item.Host), strings.TrimSpace(item.Username)
	if item.Name == "" || item.Host == "" || item.Username == "" || item.PrivateKey == "" {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "name, host, username and private key are required")
		return
	}
	if item.Port != 0 && (item.Port < 1 || item.Port > 65535) {
		writeErr(w, http.StatusBadRequest, "INVALID_PORT", "port must be between 1 and 65535")
		return
	}
	if len(item.PrivateKey) > 128*1024 || len(item.Passphrase) > 8*1024 {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "credential is too large")
		return
	}
	saved, err := s.vault.PutForUser(info.Username, item)
	item.PrivateKey, item.Passphrase = "", ""
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save credential")
		return
	}
	writeJSON(w, http.StatusOK, credentialSummary{ID: saved.ID, Name: saved.Name, Host: saved.Host, Port: saved.Port, Username: saved.Username, Term: saved.Term, UseHerdr: saved.UseHerdr, LegacyUseTmux: saved.UseHerdr, HasPrivateKey: true, HasPassphrase: saved.Passphrase != "", UpdatedAt: saved.UpdatedAt})
}

func (s *Server) handleCredentialMigrate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.credentialSession(w, r); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024+1))
	if err != nil || len(body) > 16*1024 {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "migration request is too large")
		return
	}
	defer clearBytes(body)
	var req struct {
		Password string `json:"password"`
	}
	if json.Unmarshal(body, &req) != nil || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "earlier login password is required")
		return
	}
	password := []byte(req.Password)
	req.Password = ""
	migrated, remaining, err := s.vault.MigrateLegacyFromPassword(password)
	clearBytes(password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "VAULT_MIGRATE_FAILED", "credential migration failed")
		return
	}
	if migrated == 0 && remaining > 0 {
		writeErr(w, http.StatusUnprocessableEntity, "VAULT_MIGRATE_FAILED", "the supplied password could not decrypt older credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "migrated": migrated, "remaining": remaining})
}

func (s *Server) handleCredentialDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.credentialSession(w, r); !ok {
		return
	}
	if err := s.vault.Delete(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "failed to delete credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.cfg.Server.Listen)
	if err != nil {
		return err
	}
	s.logger.Info("server listening", "addr", s.cfg.Server.Listen)
	return s.server.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
