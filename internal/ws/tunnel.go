package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/security"
)

const tunnelMaxMessageBytes = 256 * 1024

// ServeTunnelHTTP exposes an authenticated raw TCP relay for OpenSSH ProxyCommand.
func (h *Handler) ServeTunnelHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.Auth.SessionFromRequest(r); !ok {
		writeTunnelHTTPError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	if !h.Cfg.Tunnel.Enabled {
		writeTunnelHTTPError(w, http.StatusForbidden, "TUNNEL_DISABLED", "TCP tunnel is disabled")
		return
	}
	if !h.checkOrigin(r) {
		writeTunnelHTTPError(w, http.StatusForbidden, "WS_ORIGIN_DENIED", "origin not allowed")
		return
	}
	if !h.reserveTunnel() {
		writeTunnelHTTPError(w, http.StatusTooManyRequests, "TUNNEL_LIMIT_EXCEEDED", "maximum concurrent tunnels exceeded")
		return
	}
	defer h.activeTunnels.Add(-1)

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Warn("tunnel websocket upgrade failed", "err", err)
		}
		return
	}
	defer conn.Close()

	s := &tunnelSession{
		h:        h,
		conn:     conn,
		remoteIP: clientIP(r),
		id:       newSessionID(),
	}
	s.run()
}

type tunnelSession struct {
	h        *Handler
	conn     *websocket.Conn
	remoteIP string
	id       string
	host     string
	port     int
	target   net.Conn
	bytesIn  atomic.Uint64
	bytesOut atomic.Uint64
}

func (s *tunnelSession) run() {
	started := time.Now()
	if err := s.connect(); err != nil {
		return
	}
	defer s.target.Close()

	if s.h.Logger != nil {
		s.h.Logger.Info("TCP tunnel connected", "tunnel_id", s.id, "remote_ip", s.remoteIP, "host", s.host, "port", s.port)
		defer func() {
			s.h.Logger.Info("TCP tunnel closed",
				"tunnel_id", s.id,
				"remote_ip", s.remoteIP,
				"host", s.host,
				"port", s.port,
				"duration", time.Since(started),
				"bytes_to_target", s.bytesIn.Load(),
				"bytes_from_target", s.bytesOut.Load(),
			)
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 2)
	go func() { errCh <- s.copyTargetToWebSocket() }()
	go func() { errCh <- s.copyWebSocketToTarget() }()
	go s.keepAlive(ctx)

	<-errCh
	cancel()
	_ = s.target.Close()
	_ = s.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "tunnel closed"),
		time.Now().Add(5*time.Second),
	)
}

func (s *tunnelSession) connect() error {
	s.conn.SetReadLimit(tunnelMaxMessageBytes)
	_ = s.conn.SetReadDeadline(time.Now().Add(s.h.Cfg.Tunnel.ConnectTimeout))
	messageType, payload, err := s.conn.ReadMessage()
	if err != nil {
		return err
	}
	if messageType != websocket.TextMessage {
		_ = s.sendError("BAD_REQUEST", "first tunnel message must be tunnel-connect")
		return errors.New("invalid first tunnel message")
	}
	message, err := DecodeMessage(payload)
	if err != nil || message.Type != "tunnel-connect" {
		_ = s.sendError("BAD_REQUEST", "first tunnel message must be tunnel-connect")
		return errors.New("invalid first tunnel message")
	}
	var request TunnelConnectData
	if err := json.Unmarshal(message.Data, &request); err != nil {
		_ = s.sendError("BAD_REQUEST", "invalid tunnel connect payload")
		return err
	}
	request.Host = strings.TrimSpace(request.Host)
	if request.Port == 0 {
		request.Port = 22
	}
	if request.Host == "" || len(request.Host) > 253 {
		_ = s.sendError("INVALID_HOST", "host is required and must not exceed 253 characters")
		return errors.New("invalid host")
	}
	if !s.h.tunnelPortAllowed(request.Port) {
		_ = s.sendError("TUNNEL_PORT_DENIED", "target port is not allowed for TCP tunnels")
		return errors.New("tunnel port denied")
	}

	address, err := s.h.NetworkPolicy.ResolveAllowedHostPort(request.Host, request.Port)
	if err != nil {
		code, message := "NETWORK_DENIED", err.Error()
		var policyErr *security.PolicyError
		if errors.As(err, &policyErr) {
			code, message = policyErr.Code, policyErr.Message
		}
		_ = s.sendError(code, message)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.h.Cfg.Tunnel.ConnectTimeout)
	defer cancel()
	target, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		_ = s.sendError("TUNNEL_CONNECT_FAILED", "failed to connect to tunnel target")
		return fmt.Errorf("connect tunnel target: %w", err)
	}

	s.host, s.port, s.target = request.Host, request.Port, target
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(s.h.Cfg.Tunnel.IdleTimeout))
	})
	_ = s.conn.SetReadDeadline(time.Now().Add(s.h.Cfg.Tunnel.IdleTimeout))
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.h.Cfg.Tunnel.WriteTimeout))
	return s.conn.WriteJSON(Message{
		Type: "tunnel-connected",
		Data: mustMarshalTunnelData(TunnelConnectedData{Host: s.host, Port: s.port}),
	})
}

func (s *tunnelSession) copyTargetToWebSocket() error {
	buffer := make([]byte, 32*1024)
	for {
		count, err := s.target.Read(buffer)
		if count > 0 {
			_ = s.conn.SetWriteDeadline(time.Now().Add(s.h.Cfg.Tunnel.WriteTimeout))
			if writeErr := s.conn.WriteMessage(websocket.BinaryMessage, buffer[:count]); writeErr != nil {
				return writeErr
			}
			s.bytesOut.Add(uint64(count))
		}
		if err != nil {
			return err
		}
	}
}

func (s *tunnelSession) copyWebSocketToTarget() error {
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(s.h.Cfg.Tunnel.IdleTimeout))
		messageType, payload, err := s.conn.ReadMessage()
		if err != nil {
			return err
		}
		switch messageType {
		case websocket.BinaryMessage:
			_ = s.target.SetWriteDeadline(time.Now().Add(s.h.Cfg.Tunnel.WriteTimeout))
			if err := writeAll(s.target, payload); err != nil {
				return err
			}
			s.bytesIn.Add(uint64(len(payload)))
		case websocket.TextMessage:
			message, decodeErr := DecodeMessage(payload)
			if decodeErr != nil || message.Type != "tunnel-eof" {
				return errors.New("unexpected tunnel control message")
			}
			if tcp, ok := s.target.(*net.TCPConn); ok {
				if err := tcp.CloseWrite(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected tunnel message")
		}
	}
}

func (s *tunnelSession) keepAlive(ctx context.Context) {
	interval := tunnelPingInterval(s.h.Cfg.Tunnel.IdleTimeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

func (s *tunnelSession) sendError(code, message string) error {
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.h.Cfg.Tunnel.WriteTimeout))
	return s.conn.WriteJSON(Message{Type: "error", Data: mustMarshalTunnelData(ErrorData{Code: code, Message: message})})
}

func (h *Handler) reserveTunnel() bool {
	limit := int64(h.Cfg.Tunnel.MaxConnections)
	for {
		current := h.activeTunnels.Load()
		if current >= limit {
			return false
		}
		if h.activeTunnels.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (h *Handler) tunnelPortAllowed(port int) bool {
	for _, allowed := range h.Cfg.Tunnel.AllowedPorts {
		if port == allowed {
			return true
		}
	}
	return false
}

func tunnelPingInterval(timeout time.Duration) time.Duration {
	interval := 20 * time.Second
	if timeout > 0 && timeout/2 < interval {
		interval = timeout / 2
	}
	if interval < time.Second {
		return time.Second
	}
	return interval
}

func writeAll(w io.Writer, payload []byte) error {
	for len(payload) > 0 {
		count, err := w.Write(payload)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		payload = payload[count:]
	}
	return nil
}

func mustMarshalTunnelData(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}

func writeTunnelHTTPError(w http.ResponseWriter, status int, code, message string) {
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
