package ws

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/auth"
	"github.com/liansishen/go-webssh/internal/config"
	"github.com/liansishen/go-webssh/internal/security"
	"github.com/liansishen/go-webssh/internal/sshclient"
	"golang.org/x/crypto/ssh"
)

const (
	maxConnectMsgBytes = 256 * 1024
	maxInputMsgBytes   = 64 * 1024
	maxMessageBytes    = 256 * 1024
)

type Handler struct {
	Cfg            *config.Config
	Auth           *auth.Authenticator
	NetworkPolicy  *security.NetworkPolicy
	HostKeyCB      ssh.HostKeyCallback
	Logger         *slog.Logger
	activeSessions atomic.Int64
	activeTunnels  atomic.Int64
	upgrader       websocket.Upgrader
}

func NewHandler(cfg *config.Config, authenticator *auth.Authenticator, policy *security.NetworkPolicy, hostKeyCB ssh.HostKeyCallback, logger *slog.Logger) *Handler {
	h := &Handler{
		Cfg:           cfg,
		Auth:          authenticator,
		NetworkPolicy: policy,
		HostKeyCB:     hostKeyCB,
		Logger:        logger,
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     h.checkOrigin,
	}
	return h
}

func (h *Handler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if isSameOrigin(r, origin) {
		return true
	}
	for _, allowed := range h.Cfg.Server.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	h.Logger.Warn("websocket origin denied", "origin", origin, "host", r.Host)
	return false
}

func isSameOrigin(r *http.Request, origin string) bool {
	reqHost := r.Host
	if strings.HasPrefix(origin, "https://") {
		host := strings.TrimPrefix(origin, "https://")
		return host == reqHost || host == strings.Split(reqHost, ":")[0]
	}
	if strings.HasPrefix(origin, "http://") {
		host := strings.TrimPrefix(origin, "http://")
		return host == reqHost
	}
	return false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.Auth.SessionFromRequest(r); !ok {
		http.Error(w, `{"ok":false,"error":{"code":"AUTH_REQUIRED","message":"authentication required"}}`, http.StatusUnauthorized)
		return
	}
	if !h.checkOrigin(r) {
		http.Error(w, `{"ok":false,"error":{"code":"WS_ORIGIN_DENIED","message":"origin not allowed"}}`, http.StatusForbidden)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.Logger.Warn("websocket upgrade failed", "err", err)
		return
	}

	session := &wsSession{
		h:        h,
		conn:     conn,
		remoteIP: clientIP(r),
		logger:   h.Logger,
	}
	session.run()
}

type wsSession struct {
	h         *Handler
	conn      *websocket.Conn
	remoteIP  string
	logger    *slog.Logger
	writeMu   sync.Mutex
	sshSess   *sshclient.Session
	sessionID string
	startTime time.Time
	host      string
	port      int
	username  string
	connected atomic.Bool
}

func (s *wsSession) run() {
	defer s.cleanup()

	s.conn.SetReadLimit(maxMessageBytes)
	_ = s.conn.SetReadDeadline(time.Now().Add(s.h.Cfg.SSH.IdleTimeout))
	s.conn.SetPongHandler(func(string) error {
		_ = s.conn.SetReadDeadline(time.Now().Add(s.h.Cfg.SSH.IdleTimeout))
		return nil
	})

	// First business message must be connect.
	for {
		mt, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
			continue
		}
		if len(data) > maxConnectMsgBytes {
			_ = s.sendError("BAD_REQUEST", "message too large")
			return
		}
		msg, err := DecodeMessage(data)
		if err != nil {
			_ = s.sendError("BAD_REQUEST", "invalid message")
			return
		}
		if msg.Type != "connect" {
			_ = s.sendError("BAD_REQUEST", "first message must be connect")
			return
		}
		if err := s.handleConnect(msg.Data); err != nil {
			return
		}
		break
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.copyOutput(ctx, s.sshSess.Stdout)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.streamMetrics(ctx)
	}()
	go func() {
		defer wg.Done()
		s.copyOutput(ctx, s.sshSess.Stderr)
	}()

	// Detect SSH shell exit.
	sshDone := make(chan error, 1)
	go func() {
		sshDone <- s.sshSess.Session.Wait()
	}()

	// WebSocket read loop.
	wsDone := make(chan error, 1)
	go func() {
		for {
			_ = s.conn.SetReadDeadline(time.Now().Add(s.h.Cfg.SSH.IdleTimeout))
			mt, data, err := s.conn.ReadMessage()
			if err != nil {
				wsDone <- err
				return
			}
			if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
				continue
			}
			if len(data) > maxInputMsgBytes+2048 {
				_ = s.sendError("BAD_REQUEST", "message too large")
				wsDone <- errors.New("message too large")
				return
			}
			msg, err := DecodeMessage(data)
			if err != nil {
				continue
			}
			switch msg.Type {
			case "input":
				var input string
				if err := json.Unmarshal(msg.Data, &input); err != nil {
					continue
				}
				if len(input) > maxInputMsgBytes {
					_ = s.sendError("BAD_REQUEST", "input too large")
					continue
				}
				if s.sshSess != nil && s.sshSess.Stdin != nil {
					if _, err := s.sshSess.Stdin.Write([]byte(input)); err != nil {
						wsDone <- err
						return
					}
				}
			case "resize":
				var rd ResizeData
				if err := json.Unmarshal(msg.Data, &rd); err != nil {
					continue
				}
				if rd.Rows < 5 || rd.Rows > 200 || rd.Cols < 20 || rd.Cols > 500 {
					continue
				}
				_ = s.sshSess.WindowChange(rd.Rows, rd.Cols)
			case "ping":
				var sentAt float64
				if err := json.Unmarshal(msg.Data, &sentAt); err == nil {
					_ = s.writeJSON(map[string]any{"type": "pong", "data": sentAt})
				}
			case "disconnect":
				wsDone <- io.EOF
				return
			}
		}
	}()

	select {
	case err := <-sshDone:
		var exitStatus *int
		if err == nil {
			z := 0
			exitStatus = &z
		} else if ee, ok := err.(*ssh.ExitError); ok {
			code := ee.ExitStatus()
			exitStatus = &code
		}
		_ = s.sendClosed("ssh session closed", exitStatus)
	case <-wsDone:
		_ = s.sendClosed("client disconnected", nil)
	}

	cancel()
	if s.sshSess != nil {
		s.sshSess.Close()
	}
	wg.Wait()
}

func (s *wsSession) streamMetrics(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	var previous sshclient.RawMetrics
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, err := s.sshSess.CollectMetrics()
			if err != nil {
				continue
			}
			data := MetricsData{
				MemoryTotal:    current.MemoryTotal,
				DiskUsed:       current.DiskUsed,
				DiskTotal:      current.DiskTotal,
				NetworkRXBytes: current.NetworkRX,
				NetworkTXBytes: current.NetworkTX,
				Load1:          current.Load1,
				UptimeSeconds:  current.Uptime,
			}
			if current.MemoryTotal > 0 {
				data.MemoryUsed = current.MemoryTotal - current.MemoryAvail
				data.MemoryPercent = float64(data.MemoryUsed) * 100 / float64(current.MemoryTotal)
			}
			if current.DiskTotal > 0 {
				data.DiskPercent = float64(current.DiskUsed) * 100 / float64(current.DiskTotal)
			}
			if !previous.CollectedAt.IsZero() {
				totalDelta := current.CPUTotal - previous.CPUTotal
				idleDelta := current.CPUIdle - previous.CPUIdle
				if totalDelta > 0 && idleDelta <= totalDelta {
					data.CPUPercent = float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
				}
				seconds := current.CollectedAt.Sub(previous.CollectedAt).Seconds()
				if seconds > 0 {
					data.NetworkRXPerSec = float64(current.NetworkRX-previous.NetworkRX) / seconds
					data.NetworkTXPerSec = float64(current.NetworkTX-previous.NetworkTX) / seconds
				}
			}
			previous = current
			_ = s.writeJSON(map[string]any{"type": "metrics", "data": data})
		}
	}
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

func (s *wsSession) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return s.conn.WriteJSON(v)
}

func (s *wsSession) sendError(code, message string) error {
	return s.writeJSON(map[string]any{
		"type": "error",
		"data": ErrorData{Code: code, Message: message},
	})
}

func (s *wsSession) sendClosed(reason string, exitStatus *int) error {
	return s.writeJSON(map[string]any{
		"type": "closed",
		"data": ClosedData{Reason: reason, ExitStatus: exitStatus},
	})
}

func (s *wsSession) copyOutput(ctx context.Context, r io.Reader) {
	buf := make([]byte, 16*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := r.Read(buf)
		if n > 0 {
			s.writeMu.Lock()
			_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			_ = s.conn.WriteMessage(websocket.BinaryMessage, buf[:n])
			s.writeMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *wsSession) handleConnect(raw json.RawMessage) error {
	var cd ConnectData
	if err := json.Unmarshal(raw, &cd); err != nil {
		_ = s.sendError("BAD_REQUEST", "invalid connect payload")
		return err
	}

	cd.Host = strings.TrimSpace(cd.Host)
	cd.Username = strings.TrimSpace(cd.Username)
	if cd.Port == 0 {
		cd.Port = 22
	}
	if cd.Host == "" {
		_ = s.sendError("INVALID_HOST", "host is required")
		return errors.New("invalid host")
	}
	if len(cd.Host) > 253 {
		_ = s.sendError("INVALID_HOST", "host is too long")
		return errors.New("invalid host")
	}
	if cd.Port < 1 || cd.Port > 65535 {
		_ = s.sendError("INVALID_PORT", "port must be between 1 and 65535")
		return errors.New("invalid port")
	}
	if cd.Username == "" || len(cd.Username) > 128 {
		_ = s.sendError("BAD_REQUEST", "username is required")
		return errors.New("invalid username")
	}
	if strings.TrimSpace(cd.PrivateKey) == "" {
		_ = s.sendError("PRIVATE_KEY_REQUIRED", "private key is required")
		return errors.New("private key required")
	}
	if len(cd.PrivateKey) > 128*1024 {
		_ = s.sendError("PRIVATE_KEY_TOO_LARGE", "private key is too large")
		return errors.New("private key too large")
	}
	if len(cd.Passphrase) > 8*1024 {
		_ = s.sendError("BAD_REQUEST", "passphrase is too large")
		return errors.New("passphrase too large")
	}
	if cd.Term == "" {
		cd.Term = "xterm-256color"
	}
	if len(cd.Term) > 64 {
		_ = s.sendError("BAD_REQUEST", "term is invalid")
		return errors.New("invalid term")
	}
	if !cd.UseHerdr && cd.LegacyUseTmux {
		cd.UseHerdr = true
	}
	if cd.HerdrSession == "" {
		cd.HerdrSession = cd.TmuxSession
	}
	if cd.UseHerdr {
		if cd.HerdrSession == "" {
			cd.HerdrSession = "gowebssh-" + newSessionID()
		}
		if len(cd.HerdrSession) > 64 || strings.ContainsFunc(cd.HerdrSession, func(r rune) bool {
			return !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
		}) {
			_ = s.sendError("BAD_REQUEST", "Herdr session name is invalid")
			return errors.New("invalid Herdr session")
		}
	}
	if cd.Rows < 5 || cd.Rows > 200 {
		cd.Rows = 24
	}
	if cd.Cols < 20 || cd.Cols > 500 {
		cd.Cols = 80
	}

	if err := s.h.NetworkPolicy.ValidateHostPort(cd.Host, cd.Port); err != nil {
		code, msg := "NETWORK_DENIED", err.Error()
		var pe *security.PolicyError
		if errors.As(err, &pe) {
			code, msg = pe.Code, pe.Message
		}
		_ = s.sendError(code, msg)
		s.logger.Info("ssh connect denied", "remote_ip", s.remoteIP, "host", cd.Host, "port", cd.Port, "ssh_user", cd.Username, "code", code)
		return err
	}

	s.logger.Info("ssh connect requested", "remote_ip", s.remoteIP, "host", cd.Host, "port", cd.Port, "ssh_user", cd.Username)
	s.startTime = time.Now()
	s.host = cd.Host
	s.port = cd.Port
	s.username = cd.Username

	if !s.h.reserveSession() {
		_ = s.sendError("SESSION_LIMIT_EXCEEDED", "maximum concurrent sessions exceeded")
		return errors.New("session limit")
	}
	reserved := true
	defer func() {
		if reserved {
			s.h.activeSessions.Add(-1)
		}
	}()

	req := sshclient.ConnectRequest{
		Host:         cd.Host,
		Port:         cd.Port,
		Username:     cd.Username,
		PrivateKey:   cd.PrivateKey,
		Passphrase:   cd.Passphrase,
		Term:         cd.Term,
		Cols:         cd.Cols,
		Rows:         cd.Rows,
		UseHerdr:     cd.UseHerdr,
		HerdrSession: cd.HerdrSession,
	}
	cd.PrivateKey = ""
	cd.Passphrase = ""

	sshSess, err := sshclient.Connect(req, sshclient.DialConfig{
		HostKeyCallback: s.h.HostKeyCB,
		Timeout:         s.h.Cfg.SSH.ConnectTimeout,
	})
	req.PrivateKey = ""
	req.Passphrase = ""

	if err != nil {
		code, msg := classifySSHError(err)
		_ = s.sendError(code, msg)
		s.logger.Warn("ssh connect failed", "remote_ip", s.remoteIP, "host", s.host, "port", s.port, "ssh_user", s.username, "code", code)
		return err
	}

	s.sshSess = sshSess
	if !sshSess.HerdrActive {
		cd.HerdrSession = ""
	}
	s.sessionID = newSessionID()
	s.connected.Store(true)
	reserved = false

	if err := s.writeJSON(map[string]any{
		"type": "connected",
		"data": ConnectedData{
			SessionID:    s.sessionID,
			Host:         s.host,
			Port:         s.port,
			Username:     s.username,
			HerdrSession: cd.HerdrSession,
			TmuxSession:  cd.HerdrSession,
		},
	}); err != nil {
		return err
	}
	s.logger.Info("ssh connected", "session_id", s.sessionID, "remote_ip", s.remoteIP, "host", s.host, "port", s.port, "ssh_user", s.username)
	return nil
}

func (h *Handler) reserveSession() bool {
	limit := int64(h.Cfg.SSH.MaxSessions)
	for {
		current := h.activeSessions.Load()
		if current >= limit {
			return false
		}
		if h.activeSessions.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func classifySSHError(err error) (string, string) {
	var ce *sshclient.CodedError
	if errors.As(err, &ce) {
		return ce.Code, ce.Message
	}
	var he *security.HostKeyError
	if errors.As(err, &he) {
		return he.Code, he.Message
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "host key") {
		return "HOST_KEY_UNTRUSTED", "host key verification failed"
	}
	return "SSH_CONNECT_FAILED", "SSH connection failed"
}

func (s *wsSession) cleanup() {
	if s.connected.Swap(false) {
		s.h.activeSessions.Add(-1)
	}
	if s.sshSess != nil {
		s.sshSess.Close()
		s.sshSess = nil
	}
	if s.sessionID != "" && s.host != "" {
		dur := time.Since(s.startTime)
		s.logger.Info("ssh session closed",
			"session_id", s.sessionID,
			"remote_ip", s.remoteIP,
			"host", s.host,
			"port", s.port,
			"ssh_user", s.username,
			"duration", dur.String(),
		)
	}
	_ = s.conn.Close()
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
