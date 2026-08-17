package sshclient

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type ConnectRequest struct {
	Host         string
	Port         int
	Username     string
	PrivateKey   string
	Passphrase   string
	Term         string
	Cols         int
	Rows         int
	UseHerdr     bool
	HerdrSession string
}

type Session struct {
	Client       *ssh.Client
	Session      *ssh.Session
	Stdin        io.WriteCloser
	Stdout       io.Reader
	Stderr       io.Reader
	HerdrActive  bool
	HerdrSession string
}

type DialConfig struct {
	HostKeyCallback ssh.HostKeyCallback
	Timeout         time.Duration
}

func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func ParseSigner(privateKey, passphrase string) (ssh.Signer, error) {
	keyBytes := []byte(privateKey)
	defer ZeroBytes(keyBytes)

	var signer ssh.Signer
	var err error
	if passphrase != "" {
		passBytes := []byte(passphrase)
		defer ZeroBytes(passBytes)
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, passBytes)
	} else {
		signer, err = ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			if _, ok := err.(*ssh.PassphraseMissingError); ok {
				return nil, &CodedError{Code: "PRIVATE_KEY_PASSPHRASE_REQUIRED", Message: "private key passphrase is required"}
			}
		}
	}
	if err != nil {
		return nil, mapPrivateKeyError(err)
	}
	return signer, nil
}

func mapPrivateKeyError(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "passphrase") || strings.Contains(msg, "decryption") || strings.Contains(msg, "x509: decryption password incorrect") {
		return &CodedError{Code: "PRIVATE_KEY_PASSPHRASE_INVALID", Message: "private key passphrase is incorrect"}
	}
	return &CodedError{Code: "PRIVATE_KEY_INVALID", Message: "private key is invalid or unsupported"}
}

func Connect(req ConnectRequest, dialCfg DialConfig) (*Session, error) {
	if strings.TrimSpace(req.PrivateKey) == "" {
		return nil, &CodedError{Code: "PRIVATE_KEY_REQUIRED", Message: "private key is required"}
	}
	if len(req.PrivateKey) > 256*1024 {
		return nil, &CodedError{Code: "PRIVATE_KEY_TOO_LARGE", Message: "private key is too large"}
	}
	signer, err := ParseSigner(req.PrivateKey, req.Passphrase)
	// Clear passphrase from request ASAP (best effort; string immutability).
	req.Passphrase = ""
	req.PrivateKey = ""
	if err != nil {
		return nil, err
	}

	term := req.Term
	if term == "" {
		term = "xterm-256color"
	}
	rows := req.Rows
	cols := req.Cols
	if rows < 5 {
		rows = 24
	}
	if cols < 20 {
		cols = 80
	}

	timeout := dialCfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	clientConfig := &ssh.ClientConfig{
		User:            req.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: dialCfg.HostKeyCallback,
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		// Preserve host-key errors from callback when possible.
		var hk interface{ Error() string }
		_ = hk
		if he := unwrapHostKey(err); he != nil {
			return nil, he
		}
		return nil, mapDialError(err)
	}
	herdrActive := req.UseHerdr && herdrAvailable(client)

	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, &CodedError{Code: "SSH_SESSION_FAILED", Message: "failed to create SSH session"}
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, &CodedError{Code: "SSH_SESSION_FAILED", Message: "failed to open SSH stdin"}
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, &CodedError{Code: "SSH_SESSION_FAILED", Message: "failed to open SSH stdout"}
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, &CodedError{Code: "SSH_SESSION_FAILED", Message: "failed to open SSH stderr"}
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty(term, rows, cols, modes); err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, &CodedError{Code: "SSH_SESSION_FAILED", Message: "failed to request PTY"}
	}
	if err := sess.Shell(); err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, &CodedError{Code: "SSH_SESSION_FAILED", Message: "failed to start shell"}
	}
	if herdrActive && req.HerdrSession != "" {
		if _, err := stdin.Write([]byte("herdr session attach " + req.HerdrSession + "\r")); err != nil {
			_ = sess.Close()
			_ = client.Close()
			return nil, &CodedError{Code: "SSH_SESSION_FAILED", Message: "failed to start recoverable Herdr session"}
		}
	}

	return &Session{
		Client:       client,
		Session:      sess,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		HerdrActive:  herdrActive,
		HerdrSession: req.HerdrSession,
	}, nil
}

func herdrAvailable(client *ssh.Client) bool {
	probe, err := client.NewSession()
	if err != nil {
		return false
	}
	defer probe.Close()
	return probe.Run("command -v herdr >/dev/null 2>&1") == nil
}

func (s *Session) WindowChange(rows, cols int) error {
	if s == nil || s.Session == nil {
		return fmt.Errorf("session closed")
	}
	return s.Session.WindowChange(rows, cols)
}

func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.Session != nil {
		_ = s.Session.Close()
	}
	if s.Client != nil {
		_ = s.Client.Close()
	}
}

func unwrapHostKey(err error) error {
	type coder interface {
		Error() string
	}
	// Walk unwrap chain looking for security.HostKeyError-like codes via type name/message.
	for e := err; e != nil; e = errors.Unwrap(e) {
		// Avoid importing security to prevent import cycles; match by dynamic type string.
		if strings.Contains(fmt.Sprintf("%T", e), "HostKeyError") {
			return e
		}
		_ = coder(nil)
	}
	return nil
}

func mapDialError(err error) error {
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "unable to authenticate") || strings.Contains(lower, "no supported methods remain") || strings.Contains(lower, "permission denied") {
		return &CodedError{Code: "SSH_AUTH_FAILED", Message: "SSH authentication failed"}
	}
	if strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		return &CodedError{Code: "SSH_CONNECT_TIMEOUT", Message: "SSH connection timed out"}
	}
	// Host key errors bubble up with their own type.
	return &CodedError{Code: "SSH_CONNECT_FAILED", Message: "SSH connection failed"}
}

type CodedError struct {
	Code    string
	Message string
	Cause   error
}

func (e *CodedError) Error() string {
	return e.Message
}

func (e *CodedError) Unwrap() error {
	return e.Cause
}
