package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/liansishen/go-webssh/internal/auth"
	"github.com/liansishen/go-webssh/internal/config"
	"github.com/liansishen/go-webssh/internal/vault"
	"github.com/liansishen/go-webssh/internal/ws"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.Auth.Username = "admin"
	cfg.Auth.Password = "secret"
	cfg.Server.SessionSecret = "test-session-secret"
	cfg.SSH.HostKeyPolicy = "insecure-ignore"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	a := &auth.Authenticator{
		Username:      cfg.Auth.Username,
		Password:      cfg.Auth.Password,
		Store:         auth.NewStore(time.Hour),
		TTL:           time.Hour,
		SessionSecret: cfg.Server.SessionSecret,
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("index")}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, a, &ws.Handler{}, nil, http.FS(assets), nil, logger)
}

func TestDocumentedHTTPAPI(t *testing.T) {
	s := testServer(t)

	health := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"ok":true`) {
		t.Fatalf("health: status=%d body=%s", health.Code, health.Body.String())
	}

	ui := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/api/config/ui", nil))
	if ui.Code != http.StatusOK || !strings.Contains(ui.Body.String(), `"privateKeyPersistence":"memory-only"`) {
		t.Fatalf("ui config: status=%d body=%s", ui.Code, ui.Body.String())
	}

	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	s.server.Handler.ServeHTTP(login, loginReq)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login: status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]

	session := httptest.NewRecorder()
	sessionReq := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	sessionReq.AddCookie(cookie)
	s.server.Handler.ServeHTTP(session, sessionReq)
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"authenticated":true`) {
		t.Fatalf("session: status=%d body=%s", session.Code, session.Body.String())
	}

	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	logoutReq.AddCookie(cookie)
	s.server.Handler.ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout: status=%d body=%s", logout.Code, logout.Body.String())
	}
}

func TestLoginRequiresJSON(t *testing.T) {
	s := testServer(t)
	rr := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("admin=secret")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLoginRememberFlag(t *testing.T) {
	s := testServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"secret","remember":true}`))
	req.Header.Set("Content-Type", "application/json")
	s.server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if remaining := time.Until(rr.Result().Cookies()[0].Expires); remaining < 29*24*time.Hour {
		t.Fatalf("remembered cookie lifetime=%s", remaining)
	}
}

func TestEncryptedCredentialAPI(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Username, cfg.Auth.Password, cfg.Server.SessionSecret, cfg.SSH.HostKeyPolicy = "admin", "secret", "test-session-secret", "insecure-ignore"
	store, err := vault.Open(t.TempDir() + "/credentials.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &auth.Authenticator{Username: "admin", Password: "secret", Store: auth.NewStore(time.Hour), TTL: time.Hour, SessionSecret: cfg.Server.SessionSecret, VaultSalt: store.Salt()}
	assets := fstest.MapFS{"index.html": {Data: []byte("index")}}
	s := New(cfg, a, &ws.Handler{}, store, http.FS(assets), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	s.server.Handler.ServeHTTP(login, req)
	cookie := login.Result().Cookies()[0]
	call := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		s.server.Handler.ServeHTTP(rr, request)
		return rr
	}
	created := call(http.MethodPost, "/api/credentials", `{"name":"prod","host":"example.com","port":22,"username":"root","privateKey":"private-secret","passphrase":"pass-secret","term":"xterm-256color"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var summary struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(created.Body.Bytes(), &summary) != nil || summary.ID == "" {
		t.Fatal("missing credential id")
	}
	list := call(http.MethodGet, "/api/credentials", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "private-secret") {
		t.Fatalf("list leaked secret: %s", list.Body.String())
	}
	got := call(http.MethodGet, "/api/credentials/"+summary.ID, "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "private-secret") {
		t.Fatalf("get: %d %s", got.Code, got.Body.String())
	}
	deleted := call(http.MethodDelete, "/api/credentials/"+summary.ID, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete: %d", deleted.Code)
	}
}

func TestCredentialListRemovesUnmigratableLegacyRecords(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Username, cfg.Auth.Password, cfg.Server.SessionSecret, cfg.SSH.HostKeyPolicy = "admin", "secret", "test-session-secret", "insecure-ignore"
	store, err := vault.Open(t.TempDir() + "/credentials.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Put(bytes.Repeat([]byte{9}, 32), vault.Credential{Name: "old", Host: "old.example.com", Username: "root", PrivateKey: "old-secret"}); err != nil {
		t.Fatal(err)
	}
	a := &auth.Authenticator{Username: "admin", Password: "secret", Store: auth.NewStore(time.Hour), TTL: time.Hour, SessionSecret: cfg.Server.SessionSecret, VaultSalt: store.Salt()}
	assets := fstest.MapFS{"index.html": {Data: []byte("index")}}
	s := New(cfg, a, &ws.Handler{}, store, http.FS(assets), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	s.server.Handler.ServeHTTP(login, req)
	cookie := login.Result().Cookies()[0]
	call := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		s.server.Handler.ServeHTTP(rr, request)
		return rr
	}
	created := call(http.MethodPost, "/api/credentials", `{"name":"new","host":"new.example.com","port":22,"username":"root","privateKey":"new-secret","term":"xterm-256color"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	list := call(http.MethodGet, "/api/credentials", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var body struct {
		Credentials        []credentialSummary `json:"credentials"`
		RemovedCredentials int                 `json:"removedCredentials"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Credentials) != 1 || body.Credentials[0].Name != "new" || body.RemovedCredentials != 1 {
		t.Fatalf("unexpected list response: %+v", body)
	}
	listAgain := call(http.MethodGet, "/api/credentials", "")
	if listAgain.Code != http.StatusOK {
		t.Fatalf("list again: %d %s", listAgain.Code, listAgain.Body.String())
	}
	var bodyAgain struct {
		Credentials        []credentialSummary `json:"credentials"`
		RemovedCredentials int                 `json:"removedCredentials"`
	}
	if err := json.Unmarshal(listAgain.Body.Bytes(), &bodyAgain); err != nil {
		t.Fatal(err)
	}
	if len(bodyAgain.Credentials) != 1 || bodyAgain.RemovedCredentials != 0 {
		t.Fatalf("unexpected repeated list response: %+v", bodyAgain)
	}
}

func TestCredentialListSurvivesPasswordChange(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Username, cfg.Auth.Password, cfg.Server.SessionSecret, cfg.SSH.HostKeyPolicy = "admin", "secret", "test-session-secret", "insecure-ignore"
	store, err := vault.Open(t.TempDir() + "/credentials.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets := fstest.MapFS{"index.html": {Data: []byte("index")}}
	newServer := func(password string) *Server {
		a := &auth.Authenticator{Username: "admin", Password: password, Store: auth.NewStore(time.Hour), TTL: time.Hour, SessionSecret: cfg.Server.SessionSecret, VaultSalt: store.Salt()}
		return New(cfg, a, &ws.Handler{}, store, http.FS(assets), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	login := func(s *Server, password string) *http.Cookie {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		s.server.Handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("login: %d %s", rr.Code, rr.Body.String())
		}
		return rr.Result().Cookies()[0]
	}
	call := func(s *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		s.server.Handler.ServeHTTP(rr, request)
		return rr
	}

	first := newServer("secret")
	firstCookie := login(first, "secret")
	created := call(first, firstCookie, http.MethodPost, "/api/credentials", `{"name":"prod","host":"example.com","port":22,"username":"root","privateKey":"private-secret","term":"xterm-256color"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var createdSummary credentialSummary
	if err := json.Unmarshal(created.Body.Bytes(), &createdSummary); err != nil {
		t.Fatal(err)
	}

	changed := newServer("changed-secret")
	changedCookie := login(changed, "changed-secret")
	list := call(changed, changedCookie, http.MethodGet, "/api/credentials", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var listBody struct {
		Credentials        []credentialSummary `json:"credentials"`
		RemovedCredentials int                 `json:"removedCredentials"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Credentials) != 1 || listBody.Credentials[0].ID != createdSummary.ID || listBody.Credentials[0].Name != "prod" {
		t.Fatalf("unexpected list after password change: %+v", listBody)
	}
	if listBody.RemovedCredentials != 0 {
		t.Fatalf("unexpected removed count after password change: %+v", listBody)
	}
	got := call(changed, changedCookie, http.MethodGet, "/api/credentials/"+createdSummary.ID, "")
	if got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("get with changed password: %d %s", got.Code, got.Body.String())
	}
}

func TestThemesJSAndUIConfig(t *testing.T) {
	s := testServer(t)

	js := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(js, httptest.NewRequest(http.MethodGet, "/themes.js", nil))
	if js.Code != http.StatusOK || !strings.Contains(js.Body.String(), "window.GOWEBSSH_THEMES") {
		t.Fatalf("themes.js: status=%d body=%s", js.Code, js.Body.String())
	}
	if !strings.Contains(js.Body.String(), "catppuccin-mocha") {
		t.Fatalf("themes.js missing default theme: %s", js.Body.String())
	}

	ui := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/api/config/ui", nil))
	if ui.Code != http.StatusOK || !strings.Contains(ui.Body.String(), `"themes"`) {
		t.Fatalf("ui config themes: status=%d body=%s", ui.Code, ui.Body.String())
	}
}
