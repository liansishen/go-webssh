package ws

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/config"
)

func TestReserveSessionHonorsConcurrentLimit(t *testing.T) {
	h := &Handler{Cfg: &config.Config{SSH: config.SSHConfig{MaxSessions: 3}}}
	var successes atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.reserveSession() {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 3 || h.activeSessions.Load() != 3 {
		t.Fatalf("successes=%d active=%d", successes.Load(), h.activeSessions.Load())
	}
}

func TestCopyOutputPreservesRawBytesInBinaryFrame(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		serverConn <- conn
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	conn := <-serverConn
	defer conn.Close()
	output := []byte{0xe4, 0xb8, 0xad, 0xff, 0x00, 0x1b, '[', 'm'}
	session := &wsSession{conn: conn}
	done := make(chan struct{})
	go func() {
		session.copyOutput(context.Background(), bytes.NewReader(output))
		close(done)
	}()

	messageType, payload, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("message type=%d, want binary", messageType)
	}
	if string(payload) != string(output) {
		t.Fatalf("payload=% x, want % x", payload, output)
	}
	<-done
}
