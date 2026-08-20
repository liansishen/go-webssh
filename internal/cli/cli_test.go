package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/ws"
)

func TestParseDestination(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in         string
		user, host string
		port       int
		wantErr    bool
	}{
		{"root@example.com", "root", "example.com", 0, false},
		{"example.com:2222", "", "example.com", 2222, false},
		{"user@example.com:2222", "user", "example.com", 2222, false},
		{"[2001:db8::1]", "", "2001:db8::1", 0, false},
		{"user@[2001:db8::1]:22", "user", "2001:db8::1", 22, false},
		{"https://example.com", "", "", 0, true},
		{"", "", "", 0, true},
		{"@host", "", "", 0, true},
	}
	for _, tt := range tests {
		user, host, port, err := ParseDestination(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseDestination(%q) = %s %s %d, want error", tt.in, user, host, port)
			}
			continue
		}
		if err != nil || user != tt.user || host != tt.host || port != tt.port {
			t.Errorf("ParseDestination(%q) = %s %s %d %v, want %s %s %d", tt.in, user, host, port, err, tt.user, tt.host, tt.port)
		}
	}
}

func TestWebsocketAndOriginURLs(t *testing.T) {
	t.Parallel()
	wsURL, err := WebsocketURL("https://webssh.example.com")
	if err != nil || wsURL != "wss://webssh.example.com/api/ws/ssh" {
		t.Fatalf("ws url=%q err=%v", wsURL, err)
	}
	wsURL, err = WebsocketURL("https://webssh.example.com/prefix")
	if err != nil || wsURL != "wss://webssh.example.com/prefix/api/ws/ssh" {
		t.Fatalf("prefixed ws url=%q err=%v", wsURL, err)
	}
	origin, err := OriginFromServerURL("https://webssh.example.com:443")
	if err != nil || origin != "https://webssh.example.com" {
		t.Fatalf("origin=%q err=%v", origin, err)
	}
}

func TestRunRelaysPTYThroughWebSocket(t *testing.T) {
	t.Parallel()
	var (
		gotOrigin string
		gotCookie string
		gotConnect ws.ConnectData
		gotInput  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "tok", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/ws/ssh":
			gotOrigin = r.Header.Get("Origin")
			if c, err := r.Cookie("gowebssh_session"); err == nil {
				gotCookie = c.Value
			}
			if gotCookie != "tok" {
				http.Error(w, `{"ok":false,"error":{"code":"AUTH_REQUIRED","message":"authentication required"}}`, http.StatusUnauthorized)
				return
			}
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg, err := ws.DecodeMessage(data)
			if err != nil || msg.Type != "connect" {
				return
			}
			_ = json.Unmarshal(msg.Data, &gotConnect)
			_ = conn.WriteJSON(map[string]any{"type": "connected", "data": ws.ConnectedData{SessionID: "s1", Host: gotConnect.Host, Port: gotConnect.Port, Username: gotConnect.Username}})
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("hello-from-remote"))
			_, data, err = conn.ReadMessage()
			if err == nil {
				msg, err = ws.DecodeMessage(data)
				if err == nil && msg.Type == "input" {
					_ = json.Unmarshal(msg.Data, &gotInput)
				}
			}
			zero := 0
			_ = conn.WriteJSON(map[string]any{"type": "closed", "data": ws.ClosedData{Reason: "ssh session closed", ExitStatus: &zero}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := Run(ctx, Options{
		ServerURL:    srv.URL,
		WebUser:      "admin",
		WebPassword:  "secret",
		IdentityFile: writeTempKey(t),
		Host:         "target.example.com",
		Port:         22,
		SSHUser:      "root",
		Timeout:      5 * time.Second,
		NoProxy:      true,
	}, Stdio{Stdin: strings.NewReader("pwd\r"), Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, stderr.String())
	}
	if gotCookie != "tok" {
		t.Fatalf("cookie=%q", gotCookie)
	}
	if gotOrigin != srv.URL {
		t.Fatalf("origin=%q want %q", gotOrigin, srv.URL)
	}
	if gotConnect.Host != "target.example.com" || gotConnect.Username != "root" || gotConnect.PrivateKey == "" {
		t.Fatalf("connect=%+v", gotConnect)
	}
	if !strings.Contains(stdout.String(), "hello-from-remote") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if gotInput != "pwd\r" {
		t.Fatalf("input=%q", gotInput)
	}
}

func TestRunLoginFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusUnauthorized, "AUTH_FAILED", "invalid username or password")
	}))
	t.Cleanup(srv.Close)

	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		ServerURL:    srv.URL,
		WebUser:      "admin",
		WebPassword:  "wrong",
		IdentityFile: writeTempKey(t),
		Host:         "target.example.com",
		SSHUser:      "root",
		Timeout:      3 * time.Second,
		NoProxy:      true,
	}, Stdio{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: &stderr})
	if err == nil || !strings.Contains(err.Error(), "AUTH_FAILED") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunConnectError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "tok", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/ws/ssh":
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_, _, _ = conn.ReadMessage()
			_ = conn.WriteJSON(map[string]any{"type": "error", "data": ws.ErrorData{Code: "NETWORK_DENIED", Message: "destination is not allowed"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	err := Run(context.Background(), Options{
		ServerURL:    srv.URL,
		WebUser:      "admin",
		WebPassword:  "secret",
		IdentityFile: writeTempKey(t),
		Host:         "10.0.0.1",
		SSHUser:      "root",
		Timeout:      3 * time.Second,
		NoProxy:      true,
	}, Stdio{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "NETWORK_DENIED") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunListAndSavedCredential(t *testing.T) {
	t.Parallel()
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "tok", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/credentials":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"credentials": []credentialSummary{{ID: "abc123", Name: "prod", Host: "prod.example.com", Port: 22, Username: "root"}},
			})
		case r.URL.Path == "/api/credentials/abc123":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(savedCredential{
				ID: "abc123", Name: "prod", Host: "prod.example.com", Port: 22, Username: "root",
				PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n",
			})
		case r.URL.Path == "/api/ws/ssh":
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg, _ := ws.DecodeMessage(data)
			var cd ws.ConnectData
			_ = json.Unmarshal(msg.Data, &cd)
			gotHost = cd.Host
			_ = conn.WriteJSON(map[string]any{"type": "connected", "data": ws.ConnectedData{SessionID: "s1", Host: cd.Host, Port: cd.Port, Username: cd.Username}})
			zero := 0
			_ = conn.WriteJSON(map[string]any{"type": "closed", "data": ws.ClosedData{Reason: "ssh session closed", ExitStatus: &zero}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var listed bytes.Buffer
	if err := Run(context.Background(), Options{
		ServerURL:   srv.URL,
		WebUser:     "admin",
		WebPassword: "secret",
		ListSaved:   true,
		Timeout:     3 * time.Second,
		NoProxy:     true,
	}, Stdio{Stdin: strings.NewReader(""), Stdout: &listed, Stderr: io.Discard}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listed.String(), "prod") || !strings.Contains(listed.String(), "abc123") {
		t.Fatalf("list output=%q", listed.String())
	}

	if err := Run(context.Background(), Options{
		ServerURL:   srv.URL,
		WebUser:     "admin",
		WebPassword: "secret",
		Saved:       "prod",
		Timeout:     3 * time.Second,
		NoProxy:     true,
	}, Stdio{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}); err != nil {
		t.Fatalf("saved: %v", err)
	}
	if gotHost != "prod.example.com" {
		t.Fatalf("host=%q", gotHost)
	}
}

func TestRunThroughHTTPConnectProxy(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "tok", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/ws/ssh":
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_, _, _ = conn.ReadMessage()
			_ = conn.WriteJSON(map[string]any{"type": "connected", "data": ws.ConnectedData{SessionID: "s1", Host: "target.example.com", Port: 22, Username: "root"}})
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("via-proxy"))
			zero := 0
			_ = conn.WriteJSON(map[string]any{"type": "closed", "data": ws.ClosedData{Reason: "ssh session closed", ExitStatus: &zero}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(backend.Close)

	var used atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "only CONNECT", http.StatusMethodNotAllowed)
			return
		}
		used.Store(true)
		dest, err := net.DialTimeout("tcp", r.Host, 3*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer dest.Close()
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		client, _, err := hj.Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		errc := make(chan struct{}, 2)
		go func() { io.Copy(dest, client); errc <- struct{}{} }()
		go func() { io.Copy(client, dest); errc <- struct{}{} }()
		<-errc
	}))
	t.Cleanup(proxy.Close)

	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
		ServerURL:    backend.URL,
		WebUser:      "admin",
		WebPassword:  "secret",
		ProxyURL:     proxy.URL,
		IdentityFile: writeTempKey(t),
		Host:         "target.example.com",
		SSHUser:      "root",
		Timeout:      5 * time.Second,
	}, Stdio{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("Run via proxy: %v", err)
	}
	if !used.Load() {
		t.Fatal("HTTP CONNECT proxy was not used")
	}
	if !strings.Contains(stdout.String(), "via-proxy") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestInvalidProxyURL(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), Options{
		ServerURL:   "https://webssh.example.com",
		WebUser:     "admin",
		WebPassword: "secret",
		ProxyURL:    "socks5://127.0.0.1:1080",
		Host:        "target.example.com",
		SSHUser:     "root",
		NoProxy:     false,
	}, Stdio{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Fatalf("err=%v", err)
	}
}

func TestMatchSaved(t *testing.T) {
	t.Parallel()
	items := []credentialSummary{
		{ID: "abc123def", Name: "prod"},
		{ID: "zzz999", Name: "staging"},
	}
	id, err := matchSaved(items, "prod")
	if err != nil || id != "abc123def" {
		t.Fatalf("name match: %s %v", id, err)
	}
	id, err = matchSaved(items, "abc123")
	if err != nil || id != "abc123def" {
		t.Fatalf("prefix match: %s %v", id, err)
	}
	if _, err := matchSaved(items, "missing"); err == nil {
		t.Fatal("expected missing error")
	}
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeTempKey(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "id_test")
	if err := os.WriteFile(path, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
