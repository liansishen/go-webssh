package ws

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/auth"
	"github.com/liansishen/go-webssh/internal/config"
	"github.com/liansishen/go-webssh/internal/security"
)

func TestReserveTunnelHonorsConcurrentLimit(t *testing.T) {
	handler := &Handler{Cfg: &config.Config{Tunnel: config.TunnelConfig{MaxConnections: 2}}}
	if !handler.reserveTunnel() || !handler.reserveTunnel() {
		t.Fatal("expected first two tunnel reservations to succeed")
	}
	if handler.reserveTunnel() {
		t.Fatal("reservation above tunnel limit succeeded")
	}
	if handler.activeTunnels.Load() != 2 {
		t.Fatalf("active tunnels=%d", handler.activeTunnels.Load())
	}
}

func TestTunnelRequiresAuthenticationAndEnabledConfig(t *testing.T) {
	cfg, authenticator, policy, cookie := tunnelTestDependencies(t)
	handler := NewHandler(cfg, authenticator, policy, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	unauthorized := httptest.NewRecorder()
	handler.ServeTunnelHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/ws/tunnel", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	disabled := httptest.NewRecorder()
	disabledRequest := httptest.NewRequest(http.MethodGet, "/api/ws/tunnel", nil)
	disabledRequest.AddCookie(cookie)
	handler.ServeTunnelHTTP(disabled, disabledRequest)
	if disabled.Code != http.StatusForbidden || !strings.Contains(disabled.Body.String(), "TUNNEL_DISABLED") {
		t.Fatalf("disabled status=%d body=%s", disabled.Code, disabled.Body.String())
	}
}

func TestTunnelRelaysBinaryData(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	cfg, authenticator, policy, cookie := tunnelTestDependencies(t)
	cfg.Tunnel.Enabled = true
	cfg.Tunnel.AllowedPorts = []int{listener.Addr().(*net.TCPAddr).Port}
	handler := NewHandler(cfg, authenticator, policy, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(http.HandlerFunc(handler.ServeTunnelHTTP))
	defer server.Close()

	header := http.Header{}
	header.Set("Origin", server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	connection, response, err := websocket.DefaultDialer.Dial(websocketURL, header)
	if err != nil {
		if response != nil {
			t.Fatalf("dial: %v status=%d", err, response.StatusCode)
		}
		t.Fatal(err)
	}
	defer connection.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	connectPayload, err := EncodeMessage("tunnel-connect", TunnelConnectData{Host: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteMessage(websocket.TextMessage, connectPayload); err != nil {
		t.Fatal(err)
	}
	messageType, _, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		t.Fatalf("connected message type=%d err=%v", messageType, err)
	}

	payload := []byte{0, 1, 2, 3, 0xff, '\n'}
	if err := connection.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatal(err)
	}
	messageType, echoed, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.BinaryMessage || !bytes.Equal(echoed, payload) {
		t.Fatalf("echo type=%d payload=%v", messageType, echoed)
	}

	eofPayload, _ := EncodeMessage("tunnel-eof", nil)
	if err := connection.WriteMessage(websocket.TextMessage, eofPayload); err != nil {
		t.Fatal(err)
	}
}

func tunnelTestDependencies(t *testing.T) (*config.Config, *auth.Authenticator, *security.NetworkPolicy, *http.Cookie) {
	t.Helper()
	cfg := config.Default()
	cfg.Auth.Username = "admin"
	cfg.Auth.Password = "secret"
	cfg.Server.SessionSecret = "test-session-secret"
	cfg.SSH.HostKeyPolicy = "insecure-ignore"
	cfg.Tunnel.ConnectTimeout = 2 * time.Second
	cfg.Tunnel.IdleTimeout = 5 * time.Second

	authenticator := &auth.Authenticator{
		Username:      cfg.Auth.Username,
		Password:      cfg.Auth.Password,
		Store:         auth.NewStore(time.Hour),
		TTL:           time.Hour,
		SessionSecret: cfg.Server.SessionSecret,
	}
	login := httptest.NewRecorder()
	if err := authenticator.Login(login, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	cookie := login.Result().Cookies()[0]
	policy, err := security.NewNetworkPolicy(true, nil, []string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, authenticator, policy, cookie
}

func TestTunnelRejectsPortOutsideAllowlist(t *testing.T) {
	cfg, authenticator, policy, cookie := tunnelTestDependencies(t)
	cfg.Tunnel.Enabled = true
	cfg.Tunnel.AllowedPorts = []int{22}
	handler := NewHandler(cfg, authenticator, policy, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(http.HandlerFunc(handler.ServeTunnelHTTP))
	defer server.Close()

	header := http.Header{}
	header.Set("Origin", server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	payload, _ := EncodeMessage("tunnel-connect", TunnelConnectData{Host: "127.0.0.1", Port: 2222})
	if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatal(err)
	}
	_, response, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "TUNNEL_PORT_DENIED") {
		t.Fatalf("response=%s allowed=%s", response, strconv.Itoa(cfg.Tunnel.AllowedPorts[0]))
	}
}
