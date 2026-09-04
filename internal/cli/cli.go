// Package cli is the terminal client for an existing Go WebSSH server.
//
// It logs in over HTTP, upgrades to /api/ws/ssh, and relays a local tty to
// the server's interactive PTY session. Outbound HTTPS uses an HTTP proxy
// (CONNECT) when configured, matching a LAN that intercepts raw SSH.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/ws"
	"golang.org/x/crypto/ssh"
)

const (
	maxKeyBytes    = 128 * 1024
	maxInputBytes  = 32 * 1024
	defaultCols    = 80
	defaultRows    = 24
	defaultTimeout = 45 * time.Second
)

// Options configure a CLI session.
type Options struct {
	ServerURL    string
	WebUser      string
	WebPassword  string
	ProxyURL     string
	NoProxy      bool
	InsecureTLS  bool
	IdentityFile string
	Passphrase   string
	Saved        string
	ListSaved    bool
	Host         string
	Port         int
	SSHUser      string
	Term         string
	UseTmux      bool
	TmuxSession  string
	Timeout      time.Duration
	UserAgent    string
}

// Stdio is the local terminal and log streams.
type Stdio struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	StdinFile  *os.File
	StdoutFile *os.File
}

// DefaultStdio uses the process standard streams.
func DefaultStdio() Stdio {
	return Stdio{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		StdinFile:  os.Stdin,
		StdoutFile: os.Stdout,
	}
}

// RemoteExit is returned when the remote SSH session ends with a status.
type RemoteExit struct {
	Code    int
	Message string
}

func (e *RemoteExit) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("remote closed with status %d", e.Code)
}

type apiError struct {
	OK    bool `json:"ok"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type credentialSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Term     string `json:"term"`
	UseTmux  bool   `json:"useTmux"`
}

type credentialList struct {
	Credentials []credentialSummary `json:"credentials"`
}

type savedCredential struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
	Term       string `json:"term"`
	UseTmux    bool   `json:"useTmux"`
	UseHerdr   bool   `json:"useHerdr"`
}

type client struct {
	opt    Options
	stdio  Stdio
	base   *url.URL
	http   *http.Client
	dialer *websocket.Dialer
	origin string
}

// Run logs in to the WebSSH server and either lists saved credentials or
// opens an interactive PTY session.
func Run(ctx context.Context, opt Options, stdio Stdio) error {
	if stdio.Stderr == nil {
		stdio.Stderr = io.Discard
	}
	if stdio.Stdout == nil {
		stdio.Stdout = io.Discard
	}
	if stdio.Stdin == nil {
		stdio.Stdin = bytes.NewReader(nil)
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

	c, err := newClient(opt, stdio)
	if err != nil {
		return err
	}

	if strings.TrimSpace(opt.WebPassword) == "" {
		pw, err := promptSecret(stdio, "Web password: ")
		if err != nil {
			return err
		}
		opt.WebPassword = pw
		c.opt.WebPassword = pw
	}

	if err := c.login(ctx); err != nil {
		return err
	}

	if opt.ListSaved {
		return c.listSaved(ctx)
	}

	host := strings.TrimSpace(opt.Host)
	sshUser := strings.TrimSpace(opt.SSHUser)
	port := opt.Port
	term := strings.TrimSpace(opt.Term)
	key := ""
	passphrase := opt.Passphrase
	useTmux := opt.UseTmux
	tmuxSession := opt.TmuxSession

	if opt.Saved != "" {
		saved, err := c.fetchSaved(ctx, opt.Saved)
		if err != nil {
			return err
		}
		if host == "" {
			host = saved.Host
		}
		if sshUser == "" {
			sshUser = saved.Username
		}
		if port == 0 {
			port = saved.Port
		}
		if term == "" {
			term = saved.Term
		}
		key = saved.PrivateKey
		if passphrase == "" {
			passphrase = saved.Passphrase
		}
		if !opt.UseTmux {
			useTmux = saved.UseTmux || saved.UseHerdr
		}
	}

	if opt.IdentityFile != "" {
		loaded, err := readIdentityFile(opt.IdentityFile)
		if err != nil {
			return err
		}
		key = loaded
	}

	if key == "" {
		loaded, path, err := readDefaultIdentity()
		if err != nil {
			return err
		}
		key = loaded
		if path != "" {
			fmt.Fprintf(stdio.Stderr, "Using identity file %s\n", path)
		}
	}

	if host == "" || sshUser == "" {
		return errors.New("destination [user@]host[:port] is required (or --saved)")
	}
	if port == 0 {
		port = 22
	}
	if term == "" {
		term = strings.TrimSpace(os.Getenv("TERM"))
	}
	if term == "" {
		term = "xterm-256color"
	}

	if passphrase == "" {
		if _, err := ssh.ParsePrivateKey([]byte(key)); err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				passphrase, err = promptSecret(stdio, "Key passphrase: ")
				if err != nil {
					return err
				}
			}
		}
	}

	cols, rows := defaultCols, defaultRows
	if stdio.StdoutFile != nil {
		if c, r, ok := windowSize(int(stdio.StdoutFile.Fd())); ok {
			cols, rows = c, r
		}
	}
	cols, rows = clampSize(cols, rows)

	conn, err := c.dialWS(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	connect := ws.ConnectData{
		Host:          host,
		Port:          port,
		Username:      sshUser,
		PrivateKey:    key,
		Passphrase:    passphrase,
		Term:          term,
		Cols:          cols,
		Rows:          rows,
		UseHerdr:      useTmux,
		LegacyUseTmux: useTmux,
		HerdrSession:  tmuxSession,
		TmuxSession:   tmuxSession,
	}
	payload, err := ws.EncodeMessage("connect", connect)
	connect.PrivateKey = ""
	connect.Passphrase = ""
	key = ""
	passphrase = ""
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("send connect: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(opt.Timeout + 20*time.Second))
	if err := waitConnected(conn); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})

	fmt.Fprintf(stdio.Stderr, "Connected to %s@%s:%d via %s\n", sshUser, host, port, c.base.Host)
	return relay(ctx, conn, stdio)
}

func newClient(opt Options, stdio Stdio) (*client, error) {
	base, err := url.Parse(strings.TrimSpace(opt.ServerURL))
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("invalid WebSSH URL %q", opt.ServerURL)
	}
	switch base.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return nil, fmt.Errorf("unsupported WebSSH URL scheme %q", base.Scheme)
	}
	proxyFn, err := proxyFunc(opt)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if opt.InsecureTLS {
		tlsCfg.InsecureSkipVerify = true
	}
	transport := &http.Transport{
		Proxy:               proxyFn,
		TLSClientConfig:     tlsCfg,
		TLSHandshakeTimeout: opt.Timeout,
		ForceAttemptHTTP2:   false,
	}
	return &client{
		opt:    opt,
		stdio:  stdio,
		base:   base,
		origin: originFromURL(base),
		http: &http.Client{
			Jar:       jar,
			Timeout:   opt.Timeout,
			Transport: transport,
		},
		dialer: &websocket.Dialer{
			Proxy:            proxyFn,
			TLSClientConfig:  tlsCfg,
			HandshakeTimeout: opt.Timeout,
		},
	}, nil
}

func proxyFunc(opt Options) (func(*http.Request) (*url.URL, error), error) {
	if opt.NoProxy {
		return func(*http.Request) (*url.URL, error) { return nil, nil }, nil
	}
	raw := strings.TrimSpace(opt.ProxyURL)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("GOWEBSSH_PROXY"))
	}
	if raw == "" {
		return http.ProxyFromEnvironment, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid HTTP proxy URL %q", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (want http or https)", u.Scheme)
	}
	return http.ProxyURL(u), nil
}

func originFromURL(u *url.URL) string {
	scheme := u.Scheme
	switch scheme {
	case "wss":
		scheme = "https"
	case "ws":
		scheme = "http"
	}
	host := u.Host
	if (scheme == "https" && u.Port() == "443") || (scheme == "http" && u.Port() == "80") {
		host = u.Hostname()
	}
	return (&url.URL{Scheme: scheme, Host: host}).String()
}

func (c *client) apiURL(path string) string {
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (c *client) wsURL() string {
	return c.websocketURL("/api/ws/ssh")
}

func (c *client) tunnelWSURL() string {
	return c.websocketURL("/api/ws/tunnel")
}

func (c *client) websocketURL(path string) string {
	u := *c.base
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (c *client) login(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{
		"username": c.opt.WebUser,
		"password": c.opt.WebPassword,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL("/api/login"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.opt.UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login: %s", readAPIError(resp))
	}
	return nil
}

func (c *client) listSaved(ctx context.Context) error {
	items, err := c.listCredentials(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(c.stdio.Stdout, "No saved credentials.")
		return nil
	}
	fmt.Fprintf(c.stdio.Stdout, "%-20s  %-24s  %s\n", "ID", "NAME", "TARGET")
	for _, item := range items {
		port := item.Port
		if port == 0 {
			port = 22
		}
		fmt.Fprintf(c.stdio.Stdout, "%-20s  %-24s  %s@%s:%d\n", item.ID, item.Name, item.Username, item.Host, port)
	}
	return nil
}

func (c *client) listCredentials(ctx context.Context) ([]credentialSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL("/api/credentials"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.opt.UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list credentials: %s", readAPIError(resp))
	}
	var out credentialList
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("list credentials: invalid response")
	}
	return out.Credentials, nil
}

func (c *client) fetchSaved(ctx context.Context, nameOrID string) (savedCredential, error) {
	items, err := c.listCredentials(ctx)
	if err != nil {
		return savedCredential{}, err
	}
	id, err := matchSaved(items, nameOrID)
	if err != nil {
		return savedCredential{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL("/api/credentials/"+url.PathEscape(id)), nil)
	if err != nil {
		return savedCredential{}, err
	}
	req.Header.Set("User-Agent", c.opt.UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return savedCredential{}, fmt.Errorf("saved credential: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return savedCredential{}, fmt.Errorf("saved credential: %s", readAPIError(resp))
	}
	var item savedCredential
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return savedCredential{}, fmt.Errorf("saved credential: invalid response")
	}
	if strings.TrimSpace(item.PrivateKey) == "" {
		return savedCredential{}, fmt.Errorf("saved credential %q has no private key", nameOrID)
	}
	return item, nil
}

func (c *client) dialWS(ctx context.Context) (*websocket.Conn, error) {
	return c.dialWSURL(ctx, c.wsURL())
}

func (c *client) dialTunnelWS(ctx context.Context) (*websocket.Conn, error) {
	return c.dialWSURL(ctx, c.tunnelWSURL())
}

func (c *client) dialWSURL(ctx context.Context, endpoint string) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set("Origin", c.origin)
	header.Set("User-Agent", c.opt.UserAgent)
	c.attachCookies(header)
	conn, resp, err := c.dialer.DialContext(ctx, endpoint, header)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			return nil, fmt.Errorf("websocket: %s: %w", readAPIError(resp), err)
		}
		return nil, fmt.Errorf("websocket: %w", err)
	}
	return conn, nil
}

func (c *client) attachCookies(header http.Header) {
	// cookiejar ignores ws:// and wss:// URLs, so copy the login cookie from the HTTP origin.
	if c.http.Jar == nil {
		return
	}
	u := *c.base
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	cookies := c.http.Jar.Cookies(&u)
	if len(cookies) == 0 {
		return
	}
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	header.Set("Cookie", strings.Join(parts, "; "))
}

func waitConnected(conn *websocket.Conn) error {
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("waiting for connected: %w", err)
		}
		if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
			continue
		}
		if mt == websocket.BinaryMessage {
			continue
		}
		msg, err := ws.DecodeMessage(data)
		if err != nil {
			continue
		}
		switch msg.Type {
		case "connected":
			return nil
		case "error":
			var ed ws.ErrorData
			_ = json.Unmarshal(msg.Data, &ed)
			if ed.Message == "" {
				ed.Message = "connect failed"
			}
			if ed.Code != "" {
				return fmt.Errorf("%s: %s", ed.Code, ed.Message)
			}
			return errors.New(ed.Message)
		case "closed":
			var cd ws.ClosedData
			_ = json.Unmarshal(msg.Data, &cd)
			if cd.Reason == "" {
				cd.Reason = "connection closed before connected"
			}
			return errors.New(cd.Reason)
		}
	}
}

func matchSaved(items []credentialSummary, nameOrID string) (string, error) {
	nameOrID = strings.TrimSpace(nameOrID)
	if nameOrID == "" {
		return "", errors.New("saved credential id or name is required")
	}
	var matches []credentialSummary
	lower := strings.ToLower(nameOrID)
	for _, item := range items {
		if item.ID == nameOrID || strings.EqualFold(item.Name, nameOrID) {
			return item.ID, nil
		}
		if strings.HasPrefix(item.ID, nameOrID) || strings.HasPrefix(strings.ToLower(item.Name), lower) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("saved credential %q is ambiguous", nameOrID)
	}
	return "", fmt.Errorf("saved credential %q not found", nameOrID)
}

func readAPIError(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var ae apiError
	if json.Unmarshal(body, &ae) == nil && ae.Error.Message != "" {
		if ae.Error.Code != "" {
			return ae.Error.Code + ": " + ae.Error.Message
		}
		return ae.Error.Message
	}
	if len(bytes.TrimSpace(body)) > 0 && !bytes.Contains(body, []byte{'<'}) {
		return strings.TrimSpace(string(body))
	}
	return resp.Status
}

func promptSecret(stdio Stdio, prompt string) (string, error) {
	fmt.Fprint(stdio.Stderr, prompt)
	if stdio.StdinFile != nil && isTerminal(int(stdio.StdinFile.Fd())) {
		s, err := readPassword(int(stdio.StdinFile.Fd()))
		fmt.Fprintln(stdio.Stderr)
		if err != nil {
			return "", err
		}
		return s, nil
	}
	sc := bufio.NewScanner(stdio.Stdin)
	if !sc.Scan() {
		if sc.Err() != nil {
			return "", sc.Err()
		}
		return "", errors.New("password is required")
	}
	return sc.Text(), nil
}

func readIdentityFile(path string) (string, error) {
	path = expandPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("identity file: %w", err)
	}
	if len(data) > maxKeyBytes {
		return "", errors.New("identity file is too large")
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("identity file %s is empty", path)
	}
	return string(data), nil
}

func readDefaultIdentity() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", errors.New("private key is required (-i or --saved)")
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) == 0 || len(data) > maxKeyBytes {
			continue
		}
		return string(data), path, nil
	}
	return "", "", errors.New("private key is required (-i or --saved)")
}

func expandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func clampSize(cols, rows int) (int, int) {
	if cols < 20 {
		cols = 20
	}
	if cols > 500 {
		cols = 500
	}
	if rows < 5 {
		rows = 5
	}
	if rows > 200 {
		rows = 200
	}
	return cols, rows
}

// ParseDestination parses [user@]host[:port] or [user@][ipv6]:port.
func ParseDestination(s string) (user, host string, port int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", 0, errors.New("missing destination")
	}
	if strings.Contains(s, "://") {
		return "", "", 0, errors.New("destination must be [user@]host[:port], not a URL")
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		user = s[:i]
		s = s[i+1:]
		if strings.TrimSpace(user) == "" {
			return "", "", 0, errors.New("missing username before @")
		}
	}
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", "", 0, errors.New("invalid IPv6 destination")
		}
		host = s[1:end]
		rest := s[end+1:]
		if host == "" {
			return "", "", 0, errors.New("missing host")
		}
		if rest == "" {
			return user, host, 0, nil
		}
		if !strings.HasPrefix(rest, ":") {
			return "", "", 0, errors.New("invalid destination")
		}
		port, err = parsePort(rest[1:])
		if err != nil {
			return "", "", 0, err
		}
		return user, host, port, nil
	}
	if h, p, splitErr := net.SplitHostPort(s); splitErr == nil {
		port, err = parsePort(p)
		if err != nil {
			return "", "", 0, err
		}
		if h == "" {
			return "", "", 0, errors.New("missing host")
		}
		return user, h, port, nil
	}
	if s == "" {
		return "", "", 0, errors.New("missing host")
	}
	return user, s, 0, nil
}

func parsePort(s string) (int, error) {
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return !unicode.IsDigit(r) }) >= 0 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	port, err := strconv.Atoi(s)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return port, nil
}

// OriginFromServerURL is exported for tests.
func OriginFromServerURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid url")
	}
	return originFromURL(u), nil
}

// WebsocketURL is exported for tests.
func WebsocketURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid url")
	}
	c := &client{base: u}
	return c.wsURL(), nil
}
