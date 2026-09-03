package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/ws"
)

const (
	cliReadTimeout     = 60 * time.Second
	terminalMouseReset = "\x1b[?9l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1005l\x1b[?1006l\x1b[?1015l\x1b[?1016l"
)

func resetTerminalMouseModes(stdio Stdio) {
	if stdio.StdoutFile == nil || !isTerminal(int(stdio.StdoutFile.Fd())) {
		return
	}
	_, _ = io.WriteString(stdio.Stdout, terminalMouseReset)
}

func writeDisconnectNotice(stdio Stdio, reason string) {
	if reason == "" {
		reason = "connection closed"
	}
	fmt.Fprintf(stdio.Stderr, "go-webssh-cli: disconnected: %s\n", reason)
}

func describeDisconnect(err error) string {
	if err == nil {
		return "connection closed"
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		if closeErr.Text != "" {
			return fmt.Sprintf("WebSocket closed (code %d): %s", closeErr.Code, closeErr.Text)
		}
		return fmt.Sprintf("WebSocket closed (code %d)", closeErr.Code)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "WebSocket read timeout"
	}
	if errors.Is(err, io.EOF) {
		return "peer closed the connection"
	}
	if errors.Is(err, context.Canceled) {
		return "connection canceled"
	}
	return err.Error()
}

func describeRemoteExit(exit *RemoteExit) string {
	if exit == nil {
		return "remote session closed"
	}
	if exit.Message == "" {
		return fmt.Sprintf("remote session exited with status %d", exit.Code)
	}
	if exit.Code != 0 {
		return fmt.Sprintf("%s (exit status %d)", exit.Message, exit.Code)
	}
	return exit.Message
}


func relay(ctx context.Context, conn *websocket.Conn, stdio Stdio) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resetTerminalMouseModes(stdio)

	var restore func()
	if stdio.StdinFile != nil && isTerminal(int(stdio.StdinFile.Fd())) {
		fn, err := makeRaw(int(stdio.StdinFile.Fd()))
		if err != nil {
			return fmt.Errorf("set raw terminal: %w", err)
		}
		restore = fn
	}
	defer func() {
		resetTerminalMouseModes(stdio)
		if restore != nil {
			restore()
		}
	}()

	s := &relaySession{
		conn:  conn,
		stdio: stdio,
	}

	notifyResize(ctx, func() {
		if stdio.StdoutFile == nil {
			return
		}
		c, r, ok := windowSize(int(stdio.StdoutFile.Fd()))
		if !ok {
			return
		}
		c, r = clampSize(c, r)
		_ = s.sendJSON("resize", ws.ResizeData{Cols: c, Rows: r})
	})

	errc := make(chan error, 3)
	go func() {
		err := s.copyStdin(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			err = fmt.Errorf("stdin relay: %w", err)
		}
		errc <- err
	}()
	go func() {
		err := s.keepAlive(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			err = fmt.Errorf("keepalive: %w", err)
		}
		errc <- err
	}()

	readErr := s.readLoop(ctx)
	cancel()

	var re *RemoteExit
	if errors.As(readErr, &re) {
		if re.Code == 0 {
			writeDisconnectNotice(stdio, describeRemoteExit(re))
			return nil
		}
		return re
	}
	if readErr != nil {
		if websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) ||
			errors.Is(readErr, io.EOF) || errors.Is(readErr, context.Canceled) {
			writeDisconnectNotice(stdio, describeDisconnect(readErr))
			return nil
		}
		return fmt.Errorf("disconnected: %s", describeDisconnect(readErr))
	}
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			return err
		}
	default:
	}
	writeDisconnectNotice(stdio, "connection closed")
	return nil
}

type relaySession struct {
	conn    *websocket.Conn
	stdio   Stdio
	writeMu sync.Mutex
}

func (s *relaySession) sendJSON(typ string, data any) error {
	payload, err := ws.EncodeMessage(typ, data)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return s.conn.WriteMessage(websocket.TextMessage, payload)
}

func (s *relaySession) copyStdin(ctx context.Context) error {
	buf := make([]byte, maxInputBytes)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := s.stdio.Stdin.Read(buf)
		if n > 0 {
			if sendErr := s.sendJSON("input", string(buf[:n])); sendErr != nil {
				return sendErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (s *relaySession) keepAlive(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Use an application ping so the server's read deadline is refreshed.
			if err := s.sendJSON("ping", float64(time.Now().UnixMilli())); err != nil {
				return err
			}
		}
	}
}

func (s *relaySession) readLoop(ctx context.Context) error {
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(cliReadTimeout))
	})
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(cliReadTimeout))
		mt, data, err := s.conn.ReadMessage()
		if err != nil {
			return err
		}
		switch mt {
		case websocket.BinaryMessage:
			if _, werr := s.stdio.Stdout.Write(data); werr != nil {
				return fmt.Errorf("stdout: %w", werr)
			}
		case websocket.TextMessage:
			if err := s.handleText(data); err != nil {
				return err
			}
		}
	}
}

func (s *relaySession) handleText(data []byte) error {
	msg, err := ws.DecodeMessage(data)
	if err != nil {
		return nil
	}
	switch msg.Type {
	case "output":
		var text string
		if json.Unmarshal(msg.Data, &text) == nil && text != "" {
			_, err := s.stdio.Stdout.Write([]byte(text))
			if err != nil {
				return fmt.Errorf("stdout: %w", err)
			}
			return nil
		}
	case "error":
		var ed ws.ErrorData
		_ = json.Unmarshal(msg.Data, &ed)
		if ed.Message == "" {
			ed.Message = "session error"
		}
		if ed.Code != "" {
			return fmt.Errorf("%s: %s", ed.Code, ed.Message)
		}
		return errors.New(ed.Message)
	case "closed":
		var cd ws.ClosedData
		_ = json.Unmarshal(msg.Data, &cd)
		code := 0
		if cd.ExitStatus != nil {
			code = *cd.ExitStatus
		}
		return &RemoteExit{Code: code, Message: cd.Reason}
	case "metrics", "pong", "connected":
	}
	return nil
}
