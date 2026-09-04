package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/ws"
)

// RunTunnel opens a raw TCP-over-WebSocket stream for OpenSSH ProxyCommand.
// Stdout is reserved exclusively for bytes received from the tunnel target.
func RunTunnel(ctx context.Context, opt Options, stdio Stdio) error {
	if stdio.Stdin == nil {
		stdio.Stdin = strings.NewReader("")
	}
	if stdio.Stdout == nil {
		stdio.Stdout = io.Discard
	}
	if stdio.Stderr == nil {
		stdio.Stderr = io.Discard
	}
	if opt.Timeout <= 0 {
		opt.Timeout = defaultTimeout
	}
	if opt.UserAgent == "" {
		opt.UserAgent = "go-webssh-cli"
	}
	if strings.TrimSpace(opt.ServerURL) == "" {
		return errors.New("WebSSH URL is required (--url or GOWEBSSH_URL)")
	}
	if strings.TrimSpace(opt.WebUser) == "" {
		return errors.New("WebSSH username is required (--web-user or GOWEBSSH_USERNAME)")
	}
	if opt.WebPassword == "" {
		return errors.New("WebSSH password is required in --stdio mode (set GOWEBSSH_PASSWORD or --web-password)")
	}
	opt.Host = strings.TrimSpace(opt.Host)
	if opt.Host == "" {
		return errors.New("tunnel destination host is required")
	}
	if opt.Port == 0 {
		opt.Port = 22
	}
	if opt.Port < 1 || opt.Port > 65535 {
		return errors.New("tunnel destination port must be between 1 and 65535")
	}

	client, err := newClient(opt, stdio)
	if err != nil {
		return err
	}
	if err := client.login(ctx); err != nil {
		return err
	}
	conn, err := client.dialTunnelWS(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(256 * 1024)

	payload, err := ws.EncodeMessage("tunnel-connect", ws.TunnelConnectData{Host: opt.Host, Port: opt.Port})
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(opt.Timeout))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("send tunnel connect: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(opt.Timeout + 20*time.Second))
	if err := waitTunnelConnected(conn); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})
	return relayTunnel(ctx, conn, stdio, opt.Timeout)
}

func waitTunnelConnected(conn *websocket.Conn) error {
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("waiting for tunnel: %w", err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		message, err := ws.DecodeMessage(payload)
		if err != nil {
			continue
		}
		switch message.Type {
		case "tunnel-connected":
			return nil
		case "error":
			var errorData ws.ErrorData
			_ = json.Unmarshal(message.Data, &errorData)
			if errorData.Message == "" {
				errorData.Message = "tunnel connection failed"
			}
			if errorData.Code != "" {
				return fmt.Errorf("%s: %s", errorData.Code, errorData.Message)
			}
			return errors.New(errorData.Message)
		}
	}
}

func relayTunnel(ctx context.Context, conn *websocket.Conn, stdio Stdio, writeTimeout time.Duration) error {
	relayCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)

	go func() {
		if err := copyTunnelInput(stdio.Stdin, conn, writeTimeout); err != nil {
			errCh <- err
		}
	}()
	go func() { errCh <- copyTunnelOutput(conn, stdio.Stdout) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-relayCtx.Done():
		}
	}()

	err := <-errCh
	cancel()
	_ = conn.Close()
	closeTunnelInput(stdio)
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return nil
	}
	return fmt.Errorf("tunnel relay: %w", err)
}

func copyTunnelInput(input io.Reader, conn *websocket.Conn, writeTimeout time.Duration) error {
	buffer := make([]byte, 32*1024)
	for {
		count, err := input.Read(buffer)
		if count > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if writeErr := conn.WriteMessage(websocket.BinaryMessage, buffer[:count]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				payload, encodeErr := ws.EncodeMessage("tunnel-eof", nil)
				if encodeErr != nil {
					return encodeErr
				}
				_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
				return conn.WriteMessage(websocket.TextMessage, payload)
			}
			return err
		}
	}
}

func closeTunnelInput(stdio Stdio) {
	if closer, ok := stdio.Stdin.(io.Closer); ok {
		_ = closer.Close()
		return
	}
	if stdio.StdinFile != nil {
		_ = stdio.StdinFile.Close()
	}
}

func copyTunnelOutput(conn *websocket.Conn, output io.Writer) error {
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		switch messageType {
		case websocket.BinaryMessage:
			if err := writeFull(output, payload); err != nil {
				return err
			}
		case websocket.TextMessage:
			message, decodeErr := ws.DecodeMessage(payload)
			if decodeErr != nil {
				return decodeErr
			}
			if message.Type == "error" {
				var errorData ws.ErrorData
				_ = json.Unmarshal(message.Data, &errorData)
				return fmt.Errorf("%s: %s", errorData.Code, errorData.Message)
			}
		}
	}
}

func writeFull(output io.Writer, payload []byte) error {
	for len(payload) > 0 {
		count, err := output.Write(payload)
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
