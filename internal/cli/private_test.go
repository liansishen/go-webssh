package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func privateTestCertificate(t *testing.T, hostname string, expired bool) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: hostname},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true, BasicConstraintsValid: true,
	}
	if expired {
		template.NotAfter = now.Add(-time.Minute)
	}
	if ip := net.ParseIP(hostname); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{hostname}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

func privateTestClient(t *testing.T, server *httptest.Server, hostname, prefix, proxy string, roots *x509.CertPool) *client {
	t.Helper()
	address, _ := url.Parse(server.URL)
	opt := Options{ServerURL: "https://" + net.JoinHostPort(hostname, address.Port()) + prefix,
		WebUser: "admin", WebPassword: "test-password", PrivateConnect: true,
		NoProxy: proxy == "", ProxyURL: proxy, Timeout: 3 * time.Second}
	c, err := newClient(opt, Stdio{Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	c.dialer.TLSClientConfig.RootCAs = roots
	var queries atomic.Int32
	doh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries.Add(1)
		if r.URL.Query().Get("name") != hostname {
			t.Errorf("DoH hostname=%q", r.URL.Query().Get("name"))
		}
		_, _ = w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"127.0.0.1"}]}`))
	}))
	t.Cleanup(doh.Close)
	c.private.resolver = &dohResolver{client: doh.Client(), endpoints: []string{doh.URL}}
	t.Cleanup(c.closeIdleConnections)
	return c
}

func TestPrivateConnectionHTTPAndWebSockets(t *testing.T) {
	for _, mode := range []string{"direct", "HTTP proxy", "HTTPS proxy", "HTTP proxy with auth", "HTTPS proxy with auth"} {
		useProxy := mode != "direct"
		t.Run(mode, func(t *testing.T) {
			const hostname = "webssh.example.test"
			certificate, roots := privateTestCertificate(t, hostname, false)
			var requests atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if !strings.HasPrefix(r.Host, hostname+":") {
					t.Errorf("HTTP Host=%q", r.Host)
				}
				if r.TLS == nil || r.TLS.ServerName != "" || r.TLS.Version != tls.VersionTLS13 {
					t.Errorf("TLS=%+v", r.TLS)
				}
				if r.URL.Path == "/prefix/api/login" {
					http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "token", Path: "/", Domain: hostname, Secure: true, HttpOnly: true})
					_, _ = w.Write([]byte(`{"ok":true}`))
					return
				}
				cookie, err := r.Cookie("gowebssh_session")
				if err != nil || cookie.Value != "token" {
					http.Error(w, "missing login cookie", 401)
					return
				}
				switch r.URL.Path {
				case "/prefix/api/credentials":
					_ = json.NewEncoder(w).Encode(credentialList{Credentials: []credentialSummary{{ID: "prod", Name: "prod"}}})
				case "/prefix/api/credentials/prod":
					_ = json.NewEncoder(w).Encode(savedCredential{ID: "prod", Host: "target.example.test", PrivateKey: "test-key"})
				case "/prefix/api/ws/ssh", "/prefix/api/ws/tunnel":
					if r.Header.Get("Origin") != "https://"+r.Host {
						t.Errorf("Origin=%q Host=%q", r.Header.Get("Origin"), r.Host)
					}
					upgrader := websocket.Upgrader{}
					conn, err := upgrader.Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer conn.Close()
					kind, payload, err := conn.ReadMessage()
					if err == nil {
						_ = conn.WriteMessage(kind, payload)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
			server.StartTLS()
			t.Cleanup(server.Close)
			proxyURL := ""
			connectTargets := make(chan string, 16)
			if useProxy {
				proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(mode, "with auth") && r.Header.Get("Proxy-Authorization") != "Basic cHJveHktdXNlcjpwcm94eS1wYXNzd29yZA==" {
						http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
						return
					}
					if r.Method != http.MethodConnect {
						http.Error(w, "CONNECT required", 405)
						return
					}
					connectTargets <- r.Host
					upstream, err := net.DialTimeout("tcp", r.Host, time.Second)
					if err != nil {
						http.Error(w, "dial failed", 502)
						return
					}
					defer upstream.Close()
					downstream, buffered, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer downstream.Close()
					_ = downstream.SetDeadline(time.Now().Add(5 * time.Second))
					_ = upstream.SetDeadline(time.Now().Add(5 * time.Second))
					_, _ = buffered.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
					_ = buffered.Flush()
					done := make(chan struct{})
					go func() { _, _ = io.Copy(upstream, buffered); _ = upstream.Close(); close(done) }()
					_, _ = io.Copy(downstream, upstream)
					_ = downstream.Close()
					<-done
				}))
				if strings.HasPrefix(mode, "HTTPS proxy") {
					proxyCertificate, _ := privateTestCertificate(t, "127.0.0.1", false)
					proxy.TLS = &tls.Config{Certificates: []tls.Certificate{proxyCertificate}}
					roots.AddCert(proxyRootsCertificate(t, proxyCertificate))
					proxy.StartTLS()
				} else {
					proxy.Start()
				}
				t.Cleanup(proxy.Close)
				proxyURL = proxy.URL
				if strings.HasSuffix(mode, "with auth") {
					u, _ := url.Parse(proxyURL)
					u.User = url.UserPassword("proxy-user", "proxy-password")
					proxyURL = u.String()
				}
			}
			c := privateTestClient(t, server, hostname, "/prefix", proxyURL, roots)
			if err := c.login(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := c.listSaved(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := c.fetchSaved(context.Background(), "prod"); err != nil {
				t.Fatal(err)
			}
			for _, dial := range []func(context.Context) (*websocket.Conn, error){c.dialWS, c.dialTunnelWS} {
				conn, err := dial(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				payload := []byte{0, 255, 'S', 'S', 'H'}
				if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
					t.Fatal(err)
				}
				kind, got, err := conn.ReadMessage()
				_ = conn.Close()
				if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(got, payload) {
					t.Fatalf("echo=%v kind=%d err=%v", got, kind, err)
				}
			}
			if requests.Load() != 6 {
				t.Fatalf("requests=%d", requests.Load())
			}
			if useProxy {
				if len(connectTargets) < 3 {
					t.Fatalf("CONNECT requests=%d", len(connectTargets))
				}
				for len(connectTargets) > 0 {
					got := <-connectTargets
					host, _, err := net.SplitHostPort(got)
					if err != nil || net.ParseIP(host) == nil || host != "127.0.0.1" {
						t.Fatalf("CONNECT destination=%q", got)
					}
				}
			}
		})
	}
}

func TestPrivateConnectionRejectsInvalidTLS(t *testing.T) {
	for _, test := range []struct {
		name, certName          string
		expired, trusted, tls12 bool
	}{
		{"wrong hostname", "other.example.test", false, true, false},
		{"unknown authority", "webssh.example.test", false, false, false},
		{"expired certificate", "webssh.example.test", true, true, false},
		{"TLS 1.2 only", "webssh.example.test", false, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			certificate, roots := privateTestCertificate(t, test.certName, test.expired)
			var requests atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
			if test.tls12 {
				server.TLS.MaxVersion = tls.VersionTLS12
			}
			server.StartTLS()
			defer server.Close()
			if !test.trusted {
				roots = x509.NewCertPool()
			}
			c := privateTestClient(t, server, "webssh.example.test", "", "", roots)
			if err := c.login(context.Background()); err == nil {
				t.Fatal("login accepted invalid TLS")
			}
			if requests.Load() != 0 {
				t.Fatal("credentials were sent before valid TLS")
			}
		})
	}
}

func TestPrivateConnectionRejectsRedirects(t *testing.T) {
	certificate, roots := privateTestCertificate(t, "webssh.example.test", false)
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "https://other.example.test/login", http.StatusTemporaryRedirect)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	defer server.Close()
	c := privateTestClient(t, server, "webssh.example.test", "", "", roots)
	if err := c.login(context.Background()); err == nil || !strings.Contains(err.Error(), "redirects are disabled") {
		t.Fatalf("error=%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d", requests.Load())
	}
	if _, err := c.http.Get("https://other.example.test/"); err == nil {
		t.Fatal("accepted another origin")
	}
	if _, err := c.privateWebSocketURL("wss://other.example.test/"); err == nil {
		t.Fatal("accepted another WebSocket origin")
	}
}

func TestPrivateConnectionOptions(t *testing.T) {
	for _, test := range []struct {
		url      string
		insecure bool
	}{
		{"http://example.com", false}, {"ws://example.com", false}, {"https://example.com", true}, {"https://user:secret@example.com", false},
	} {
		if _, err := newClient(Options{ServerURL: test.url, PrivateConnect: true, InsecureTLS: test.insecure}, Stdio{}); err == nil {
			t.Errorf("accepted URL=%q insecure=%v", test.url, test.insecure)
		}
	}
	c, err := newClient(Options{ServerURL: "wss://example.com", PrivateConnect: true}, Stdio{})
	if err != nil || c.base.Scheme != "https" {
		t.Fatalf("WSS client=%v error=%v", c, err)
	}
}

func TestPrivateAddressFormatting(t *testing.T) {
	for _, test := range []struct{ url, address, want string }{
		{"https://example.com", "192.0.2.1", "192.0.2.1"},
		{"https://example.com:8443", "192.0.2.1", "192.0.2.1:8443"},
		{"https://example.com", "2001:db8::1", "[2001:db8::1]"},
		{"https://example.com:8443", "2001:db8::1", "[2001:db8::1]:8443"},
	} {
		u, _ := url.Parse(test.url)
		if got := hostWithIP(u, test.address); got != test.want {
			t.Errorf("host=%q want=%q", got, test.want)
		}
	}
}

func proxyRootsCertificate(t *testing.T, certificate tls.Certificate) *x509.Certificate {
	t.Helper()
	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestPrivateConnectionProxyFailureAndCancellation(t *testing.T) {
	certificate, roots := privateTestCertificate(t, "webssh.example.test", false)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected target request") }))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	defer server.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Host, "webssh.example.test") {
			t.Error("domain leaked in CONNECT")
		}
		http.Error(w, "blocked", http.StatusBadRequest)
	}))
	defer proxy.Close()
	c := privateTestClient(t, server, "webssh.example.test", "", proxy.URL, roots)
	if err := c.login(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("error=%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := privateTLSDialer("127.0.0.1:443", nil, c.dialer.TLSClientConfig, time.Second)(ctx, "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("canceled dial succeeded")
	}
	if _, err := privateTLSDialer("127.0.0.1:443", nil, c.dialer.TLSClientConfig, time.Second)(context.Background(), "tcp", "webssh.example.test:443"); err == nil {
		t.Fatal("dialed hostname")
	}
}
