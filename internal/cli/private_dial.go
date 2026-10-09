package cli

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

func privateTLSDialer(destination string, proxy *url.URL, cfg *tls.Config, timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != destination {
			return nil, errors.New("private connection dial requires the resolved server address")
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		firstHop := destination
		if proxy != nil {
			port := proxy.Port()
			if port == "" {
				port = "80"
				if proxy.Scheme == "https" {
					port = "443"
				}
			}
			firstHop = net.JoinHostPort(proxy.Hostname(), port)
		}
		raw, err := (&net.Dialer{}).DialContext(ctx, network, firstHop)
		if err != nil {
			return nil, err
		}
		ok := false
		defer func() {
			if !ok {
				_ = raw.Close()
			}
		}()
		stopCancel := context.AfterFunc(ctx, func() { _ = raw.SetDeadline(time.Now()) })
		defer stopCancel()
		if deadline, exists := ctx.Deadline(); exists {
			_ = raw.SetDeadline(deadline)
		}
		var conn net.Conn = raw
		if proxy != nil {
			if proxy.Scheme == "https" {
				proxyConn := tls.Client(conn, &tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: proxy.Hostname(),
					RootCAs:    cfg.RootCAs,
				})
				if err := proxyConn.HandshakeContext(ctx); err != nil {
					return nil, fmt.Errorf("HTTPS proxy TLS: %w", err)
				}
				conn = proxyConn
			}
			request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: destination}, Host: destination, Header: make(http.Header)}
			if proxy.User != nil {
				password, _ := proxy.User.Password()
				credential := base64.StdEncoding.EncodeToString([]byte(proxy.User.Username() + ":" + password))
				request.Header.Set("Proxy-Authorization", "Basic "+credential)
			}
			if err := request.Write(conn); err != nil {
				return nil, fmt.Errorf("write proxy CONNECT: %w", err)
			}
			reader := bufio.NewReader(conn)
			response, err := http.ReadResponse(reader, request)
			if err != nil {
				return nil, fmt.Errorf("read proxy CONNECT: %w", err)
			}
			if response.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("proxy CONNECT to IP failed (HTTP %d)", response.StatusCode)
			}
			conn = &bufferedConnection{Conn: conn, reader: reader}
		}
		target := tls.Client(conn, cfg)
		if err := target.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("private WebSSH TLS: %w", err)
		}
		if !stopCancel() {
			return nil, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := raw.SetDeadline(time.Time{}); err != nil {
			return nil, err
		}
		ok = true
		return target, nil
	}
}

type bufferedConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConnection) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
