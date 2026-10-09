package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

type privateConnection struct {
	resolver *dohResolver
	address  string
}

func (c *client) preparePrivateConnection(ctx context.Context) error {
	if c.private == nil || c.private.address != "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.opt.Timeout)
	defer cancel()
	addresses, err := c.private.resolver.lookup(ctx, c.base.Hostname())
	if err != nil {
		return fmt.Errorf("resolve WebSSH server using DoH: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("DoH returned no WebSSH server addresses")
	}
	address := addresses[0].String()
	tlsCfg := privateTLSConfig(c.dialer.TLSClientConfig, c.base.Hostname(), address)
	transport := c.http.Transport.(*http.Transport)
	var proxyURL *url.URL
	if transport.Proxy != nil {
		proxyURL, err = transport.Proxy(&http.Request{URL: c.base})
		if err != nil {
			return errors.New("cannot select HTTP proxy for private connection")
		}
		if proxyURL != nil && proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
			return errors.New("private connection supports HTTP and HTTPS proxies")
		}
	}
	transport.TLSClientConfig = tlsCfg
	transport.Proxy = nil
	port := c.base.Port()
	if port == "" {
		port = "443"
	}
	dial := privateTLSDialer(net.JoinHostPort(address, port), proxyURL, tlsCfg, c.opt.Timeout)
	transport.DialTLSContext = dial
	c.http.Transport = &privateTransport{transport: transport, host: c.base.Host, address: address}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("HTTP redirects are disabled in private connection mode; configure the final WebSSH URL")
	}
	c.dialer.TLSClientConfig = tlsCfg
	c.dialer.Proxy = nil
	c.dialer.NetDialTLSContext = dial
	c.private.address = address
	return nil
}

func privateTLSConfig(base *tls.Config, hostname, address string) *tls.Config {
	cfg := base.Clone()
	cfg.MinVersion = tls.VersionTLS13
	cfg.ServerName = address
	// A literal IP suppresses SNI; VerifyConnection validates the original hostname.
	cfg.InsecureSkipVerify = true
	cfg.VerifyConnection = func(state tls.ConnectionState) error {
		if state.Version < tls.VersionTLS13 || len(state.PeerCertificates) == 0 {
			return errors.New("private connection requires TLS 1.3 and a server certificate")
		}
		intermediates := x509.NewCertPool()
		for _, certificate := range state.PeerCertificates[1:] {
			intermediates.AddCert(certificate)
		}
		options := x509.VerifyOptions{
			DNSName:       hostname,
			Roots:         cfg.RootCAs,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if cfg.Time != nil {
			options.CurrentTime = cfg.Time()
		}
		_, err := state.PeerCertificates[0].Verify(options)
		return err
	}
	return cfg
}

type privateTransport struct {
	transport *http.Transport
	host      string
	address   string
}

func (t *privateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.Host != t.host {
		return nil, errors.New("private connection only permits the configured HTTPS origin")
	}
	out := req.Clone(req.Context())
	out.Host = t.host
	out.URL.Host = hostWithIP(req.URL, t.address)
	return t.transport.RoundTrip(out)
}

func (t *privateTransport) CloseIdleConnections() {
	t.transport.CloseIdleConnections()
}

func hostWithIP(u *url.URL, address string) string {
	if u.Port() != "" {
		return net.JoinHostPort(address, u.Port())
	}
	if net.ParseIP(address).To4() == nil {
		return "[" + address + "]"
	}
	return address
}

func (c *client) privateWebSocketURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if u.Scheme != "wss" || u.Host != c.base.Host || c.private.address == "" {
		return "", errors.New("private WebSocket requires the configured WSS origin and a resolved address")
	}
	u.Host = hostWithIP(u, c.private.address)
	return u.String(), nil
}

func (c *client) closeIdleConnections() {
	c.http.CloseIdleConnections()
	if c.private != nil {
		c.private.resolver.client.CloseIdleConnections()
	}
}

func newPrivateResolver(proxy func(*http.Request) (*url.URL, error), timeout time.Duration) *dohResolver {
	return newDoHResolver(&http.Client{
		Transport: &http.Transport{
			Proxy:               proxy,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: timeout,
		},
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("DoH redirects are disabled")
		},
	})
}
