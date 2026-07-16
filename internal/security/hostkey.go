package security

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func NewHostKeyCallback(policy, knownHostsFile string, logger *slog.Logger) (ssh.HostKeyCallback, error) {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "known-hosts":
		if knownHostsFile == "" {
			return nil, fmt.Errorf("known_hosts_file is required for known-hosts policy")
		}
		// Ensure file exists so first-run is not a hard failure; empty known_hosts rejects all hosts.
		if _, err := os.Stat(knownHostsFile); err != nil {
			if os.IsNotExist(err) {
				if mkErr := os.WriteFile(knownHostsFile, []byte("# go-webssh known_hosts\n"), 0o600); mkErr != nil {
					return nil, fmt.Errorf("create known_hosts file: %w", mkErr)
				}
				if logger != nil {
					logger.Warn("known_hosts file missing; created empty file", "path", knownHostsFile)
				}
			} else {
				return nil, fmt.Errorf("stat known_hosts: %w", err)
			}
		}
		cb, err := knownhosts.New(knownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("load known_hosts: %w", err)
		}
		return wrapKnownHosts(cb), nil
	case "insecure-ignore":
		if logger != nil {
			logger.Warn("SECURITY WARNING: ssh.host_key_policy=insecure-ignore disables host key verification; MITM attacks are possible")
		}
		return ssh.InsecureIgnoreHostKey(), nil
	default:
		return nil, fmt.Errorf("unsupported host key policy %q", policy)
	}
}

func wrapKnownHosts(cb ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return err
		}
		// Classify knownhosts errors for better mapping upstream.
		var keyErr *knownhosts.KeyError
		if asKeyError(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				return &HostKeyError{Code: "HOST_KEY_UNTRUSTED", Message: "host key not found in known_hosts", Cause: err}
			}
			return &HostKeyError{Code: "HOST_KEY_CHANGED", Message: "host key verification failed: key has changed", Cause: err}
		}
		return &HostKeyError{Code: "HOST_KEY_UNTRUSTED", Message: "host key verification failed", Cause: err}
	}
}

func asKeyError(err error, target **knownhosts.KeyError) bool {
	if err == nil {
		return false
	}
	if ke, ok := err.(*knownhosts.KeyError); ok {
		*target = ke
		return true
	}
	// knownhosts may wrap
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return asKeyError(u.Unwrap(), target)
	}
	return false
}

type HostKeyError struct {
	Code    string
	Message string
	Cause   error
}

func (e *HostKeyError) Error() string {
	return e.Message
}

func (e *HostKeyError) Unwrap() error {
	return e.Cause
}
