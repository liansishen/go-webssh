package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/ws"
)

func relay(ctx context.Context, conn *websocket.Conn, stdio Stdio) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var restore func()
	if stdio.StdinFile != nil && isTerminal(int(stdio.StdinFile.Fd())) {
		fn, err := makeRaw(int(stdio.StdinFile.Fd()))
		if err != nil {
			return fmt.Errorf("set raw terminal: %w", err)
		}
		restore = fn
		defer restore()
	}

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
	go func() { errc <- s.copyStdin(ctx) }()
	go func() { errc <- s.keepAlive(ctx) }()

	readErr := s.readLoop(ctx)
	cancel()
	if restore != nil {
		restore()
		restore = nil
	}

	var re *RemoteExit
	if errors.As(readErr, &re) {
		if re.Code == 0 {
			return nil
		}
		return re
	}
	if websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			return err
		}
	default:
	}
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
			s.writeMu.Lock()
			_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			err := s.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second))
			s.writeMu.Unlock()
			if err != nil {
				return err
			}
		}
	}
}

func (s *relaySession) readLoop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		mt, data, err := s.conn.ReadMessage()
		if err != nil {
			return err
		}
		switch mt {
		case websocket.BinaryMessage:
			if _, werr := s.stdio.Stdout.Write(data); werr != nil {
				return werr
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
			return err
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
