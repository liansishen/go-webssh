package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/ws"
)

func TestRunTunnelRelaysRawBytesWithoutStdoutNoise(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "token", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/ws/tunnel":
			cookie, err := r.Cookie("gowebssh_session")
			if err != nil || cookie.Value != "token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			connection, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			messageType, payload, err := connection.ReadMessage()
			if err != nil || messageType != websocket.TextMessage {
				return
			}
			message, err := ws.DecodeMessage(payload)
			if err != nil || message.Type != "tunnel-connect" {
				return
			}
			connected, _ := ws.EncodeMessage("tunnel-connected", ws.TunnelConnectedData{Host: "target.example.com", Port: 22})
			if err := connection.WriteMessage(websocket.TextMessage, connected); err != nil {
				return
			}
			for {
				messageType, payload, err = connection.ReadMessage()
				if err != nil {
					return
				}
				if messageType == websocket.BinaryMessage {
					if err := connection.WriteMessage(websocket.BinaryMessage, payload); err != nil {
						return
					}
					continue
				}
				message, err = ws.DecodeMessage(payload)
				if err == nil && message.Type == "tunnel-eof" {
					_ = connection.WriteControl(
						websocket.CloseMessage,
						websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
						time.Now().Add(time.Second),
					)
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	input := []byte{0, 1, 2, 0xff, '\n'}
	var stdout, stderr bytes.Buffer
	err := RunTunnel(context.Background(), Options{
		ServerURL:   server.URL,
		WebUser:     "admin",
		WebPassword: "secret",
		Host:        "target.example.com",
		Port:        22,
		NoProxy:     true,
		Timeout:     3 * time.Second,
		UserAgent:   "tunnel-test",
	}, Stdio{
		Stdin:  bytes.NewReader(input),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), input) {
		t.Fatalf("stdout=%v want=%v", stdout.Bytes(), input)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunTunnelThroughHTTPConnectProxy(t *testing.T) {
	t.Parallel()
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "token", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/ws/tunnel":
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			connection, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
			connected, _ := ws.EncodeMessage("tunnel-connected", ws.TunnelConnectedData{Host: "target.example.com", Port: 22})
			_ = connection.WriteMessage(websocket.TextMessage, connected)
			_ = connection.WriteMessage(websocket.BinaryMessage, []byte("tunnel-via-proxy"))
			_, _, _ = connection.ReadMessage()
			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				time.Now().Add(time.Second),
			)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	var connectCount atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		connectCount.Add(1)
		target, err := net.DialTimeout("tcp", r.Host, 3*time.Second)
		if err != nil {
			http.Error(w, "connect failed", http.StatusBadGateway)
			return
		}
		defer target.Close()
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
			return
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(target, client); done <- struct{}{} }()
		go func() { _, _ = io.Copy(client, target); done <- struct{}{} }()
		<-done
	}))
	defer proxy.Close()

	var stdout bytes.Buffer
	err := RunTunnel(context.Background(), Options{
		ServerURL:   backend.URL,
		WebUser:     "admin",
		WebPassword: "secret",
		ProxyURL:    proxy.URL,
		InsecureTLS: true,
		Host:        "target.example.com",
		Port:        22,
		Timeout:     5 * time.Second,
	}, Stdio{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if got := connectCount.Load(); got < 2 {
		t.Fatalf("HTTP CONNECT requests=%d, want at least 2 for login and WebSocket", got)
	}
	if stdout.String() != "tunnel-via-proxy" {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunTunnelClosesBlockingInputWhenRemoteEnds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "token", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/ws/tunnel":
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			connection, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
			connected, _ := ws.EncodeMessage("tunnel-connected", ws.TunnelConnectedData{Host: "target.example.com", Port: 22})
			if err := connection.WriteMessage(websocket.TextMessage, connected); err != nil {
				return
			}
			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				time.Now().Add(time.Second),
			)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	input := &blockingReadCloser{closed: make(chan struct{})}
	err := RunTunnel(context.Background(), Options{
		ServerURL:   server.URL,
		WebUser:     "admin",
		WebPassword: "secret",
		Host:        "target.example.com",
		Port:        22,
		NoProxy:     true,
		Timeout:     3 * time.Second,
	}, Stdio{Stdin: input, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-input.closed:
	default:
		t.Fatal("blocking tunnel input was not closed")
	}
}

type blockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *blockingReadCloser) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func TestRunTunnelNeverReadsPasswordFromSSHStream(t *testing.T) {
	err := RunTunnel(context.Background(), Options{
		ServerURL: "https://webssh.example.com",
		WebUser:   "admin",
		Host:      "target.example.com",
		Port:      22,
	}, Stdio{Stdin: failOnRead{}, Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "GOWEBSSH_PASSWORD") {
		t.Fatalf("err=%v", err)
	}
}

type failOnRead struct{}

func (failOnRead) Read([]byte) (int, error) {
	return 0, errors.New("stdin must not be read")
}
