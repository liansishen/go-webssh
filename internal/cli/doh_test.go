package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// dohTestClient trusts the self-signed certificates of the local httptest TLS
// servers. Production clients still verify certificates against the real
// provider certificates.
func dohTestClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}
}

// newDoHServer starts a local HTTPS DoH server whose handler delegates to
// respond. respond returns the HTTP status, the DNS Status field, and the
// Answer records.
func newDoHServer(t *testing.T, respond func(name, qtype string) (int, int, []dohAnswer)) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method=%q want GET", r.Method)
		}
		if got := r.Header.Get("Accept"); got != dohAccept {
			t.Errorf("Accept=%q want %q", got, dohAccept)
		}
		name := r.URL.Query().Get("name")
		qtype := r.URL.Query().Get("type")
		httpStatus, dnsStatus, answers := respond(name, qtype)
		if httpStatus != http.StatusOK {
			w.WriteHeader(httpStatus)
			_, _ = io.WriteString(w, "provider error body")
			return
		}
		w.Header().Set("Content-Type", dohAccept)
		_ = json.NewEncoder(w).Encode(dohResponse{Status: &dnsStatus, Answer: answers})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

type countingTransport struct {
	requests atomic.Int32
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.requests.Add(1)
	return nil, errors.New("network access disabled")
}

func TestNewDoHResolverDefaultEndpoints(t *testing.T) {
	t.Parallel()
	r := newDoHResolver(&http.Client{})
	if r == nil {
		t.Fatal("newDoHResolver returned nil")
	}
	want := []string{"https://doh.pub/dns-query", "https://dns.alidns.com/resolve"}
	if !reflect.DeepEqual(r.endpoints, want) {
		t.Fatalf("endpoints=%v want=%v", r.endpoints, want)
	}
	r.endpoints[0] = "https://evil.example/"
	if defaultDoHEndpoints[0] != want[0] {
		t.Fatalf("package defaults mutated: %v", defaultDoHEndpoints)
	}
}

func TestDoHResolverLiteralIPSkipsNetwork(t *testing.T) {
	t.Parallel()
	transport := &countingTransport{}
	r := &dohResolver{
		client:    &http.Client{Transport: transport},
		endpoints: []string{"https://doh.pub/dns-query"},
	}
	cases := []struct {
		host string
		want string
	}{
		{"192.0.2.1", "192.0.2.1"},
		{"2001:db8::1", "2001:db8::1"},
		{"[2001:db8::1]", "2001:db8::1"},
	}
	for _, tc := range cases {
		ips, err := r.lookup(context.Background(), tc.host)
		if err != nil {
			t.Fatalf("lookup(%q) err=%v", tc.host, err)
		}
		if got := ipStrings(ips); !reflect.DeepEqual(got, []string{tc.want}) {
			t.Fatalf("lookup(%q) ips=%v want=%v", tc.host, got, tc.want)
		}
	}
	if got := transport.requests.Load(); got != 0 {
		t.Fatalf("literal IP triggered %d network requests", got)
	}
}

func TestDoHResolverPrefersIPv4OverAAAA(t *testing.T) {
	t.Parallel()
	var aaaaHits atomic.Int32
	srv := newDoHServer(t, func(name, qtype string) (int, int, []dohAnswer) {
		if name != "example.com" {
			t.Errorf("name=%q want example.com", name)
		}
		switch qtype {
		case "A":
			return http.StatusOK, 0, []dohAnswer{
				{Type: dohTypeA, Data: "192.0.2.10"},
				{Type: dohTypeA, Data: "192.0.2.11"},
			}
		case "AAAA":
			aaaaHits.Add(1)
			return http.StatusOK, 0, []dohAnswer{{Type: dohTypeAAAA, Data: "2001:db8::1"}}
		default:
			t.Errorf("unexpected qtype %q", qtype)
			return http.StatusOK, 0, nil
		}
	})
	r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}

	ips, err := r.lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := ipStrings(ips); !reflect.DeepEqual(got, []string{"192.0.2.10", "192.0.2.11"}) {
		t.Fatalf("ips=%v", got)
	}
	if aaaaHits.Load() != 0 {
		t.Fatalf("AAAA queried %d times despite usable A records", aaaaHits.Load())
	}
}

func TestDoHResolverFallsBackToAAAA(t *testing.T) {
	t.Parallel()
	srv := newDoHServer(t, func(name, qtype string) (int, int, []dohAnswer) {
		switch qtype {
		case "A":
			return http.StatusOK, 0, nil
		case "AAAA":
			return http.StatusOK, 0, []dohAnswer{{Type: dohTypeAAAA, Data: "2001:db8::1"}}
		default:
			t.Errorf("unexpected qtype %q", qtype)
			return http.StatusOK, 0, nil
		}
	})
	r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}

	ips, err := r.lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := ipStrings(ips); !reflect.DeepEqual(got, []string{"2001:db8::1"}) {
		t.Fatalf("ips=%v", got)
	}
}

func TestDoHResolverFallbackToBackup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		primary func(name, qtype string) (int, int, []dohAnswer)
	}{
		{"http error", func(string, string) (int, int, []dohAnswer) {
			return http.StatusInternalServerError, 0, nil
		}},
		{"no addresses", func(string, string) (int, int, []dohAnswer) {
			return http.StatusOK, 0, nil
		}},
		{"nxdomain", func(string, string) (int, int, []dohAnswer) {
			return http.StatusOK, 3, nil
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var primaryHits, backupHits atomic.Int32
			primary := newDoHServer(t, func(name, qtype string) (int, int, []dohAnswer) {
				primaryHits.Add(1)
				return tc.primary(name, qtype)
			})
			backup := newDoHServer(t, func(name, qtype string) (int, int, []dohAnswer) {
				backupHits.Add(1)
				if qtype == "A" {
					return http.StatusOK, 0, []dohAnswer{{Type: dohTypeA, Data: "192.0.2.53"}}
				}
				return http.StatusOK, 0, nil
			})
			r := &dohResolver{client: dohTestClient(), endpoints: []string{primary.URL, backup.URL}}

			ips, err := r.lookup(context.Background(), "example.com")
			if err != nil {
				t.Fatalf("lookup err=%v", err)
			}
			if got := ipStrings(ips); !reflect.DeepEqual(got, []string{"192.0.2.53"}) {
				t.Fatalf("ips=%v", got)
			}
			if primaryHits.Load() == 0 {
				t.Fatal("primary endpoint was not contacted")
			}
			if backupHits.Load() == 0 {
				t.Fatal("backup endpoint was not contacted")
			}
		})
	}
}

func TestDoHResolverRejectsInvalidRecords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		respond func(name, qtype string) (int, int, []dohAnswer)
	}{
		{"A data is not an IP", func(string, string) (int, int, []dohAnswer) {
			return http.StatusOK, 0, []dohAnswer{{Type: dohTypeA, Data: "not-an-ip"}}
		}},
		{"A record carries IPv6", func(string, string) (int, int, []dohAnswer) {
			return http.StatusOK, 0, []dohAnswer{{Type: dohTypeA, Data: "2001:db8::1"}}
		}},
		{"AAAA record carries IPv4", func(string, string) (int, int, []dohAnswer) {
			return http.StatusOK, 0, []dohAnswer{{Type: dohTypeAAAA, Data: "192.0.2.1"}}
		}},
		{"only CNAME", func(string, string) (int, int, []dohAnswer) {
			return http.StatusOK, 0, []dohAnswer{{Type: 5, Data: "alias.example."}}
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newDoHServer(t, tc.respond)
			r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}
			ips, err := r.lookup(context.Background(), "example.com")
			if err == nil {
				t.Fatalf("lookup ips=%v want error", ipStrings(ips))
			}
			if len(ips) != 0 {
				t.Fatalf("lookup returned ips=%v with error", ipStrings(ips))
			}
		})
	}
}

func TestDoHResolverSupportsCNAMEAnswers(t *testing.T) {
	t.Parallel()
	srv := newDoHServer(t, func(name, qtype string) (int, int, []dohAnswer) {
		if qtype != "A" {
			t.Errorf("unexpected qtype %q", qtype)
		}
		return http.StatusOK, 0, []dohAnswer{
			{Name: "example.com.", Type: 5, Data: "alias.example.com."},
			{Name: "alias.example.com.", Type: dohTypeAAAA, Data: "2001:db8::9"},
			{Name: "alias.example.com.", Type: dohTypeA, Data: "192.0.2.7"},
		}
	})
	r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}

	ips, err := r.lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := ipStrings(ips); !reflect.DeepEqual(got, []string{"192.0.2.7"}) {
		t.Fatalf("ips=%v", got)
	}
}

func TestDoHResolverRejectsNXDOMAIN(t *testing.T) {
	t.Parallel()
	srv := newDoHServer(t, func(string, string) (int, int, []dohAnswer) {
		return http.StatusOK, 3, nil
	})
	r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}

	_, err := r.lookup(context.Background(), "example.com")
	if err == nil {
		t.Fatal("NXDOMAIN did not produce an error")
	}
	if !strings.Contains(err.Error(), "dns status 3") {
		t.Fatalf("err=%v", err)
	}
}

func TestDoHResolverHidesHTTPErrorBody(t *testing.T) {
	t.Parallel()
	const secret = "topsecret-token-value"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, secret)
	}))
	t.Cleanup(srv.Close)
	r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}

	_, err := r.lookup(context.Background(), "example.com")
	if err == nil {
		t.Fatal("HTTP error did not produce an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked response body: %v", err)
	}
}

func TestDoHResolverResponseSizeLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", dohAccept)
		_, _ = w.Write(bytes.Repeat([]byte("a"), dohMaxBodyBytes+32))
	}))
	t.Cleanup(srv.Close)
	r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}

	_, err := r.lookup(context.Background(), "example.com")
	if err == nil {
		t.Fatal("oversized response did not produce an error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err=%v", err)
	}
}

func TestDoHResolverContextCancel(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	r := &dohResolver{client: dohTestClient(), endpoints: []string{srv.URL}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.lookup(ctx, "example.com")
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not receive the DoH request")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lookup did not return after context cancellation")
	}
}

func TestDoHResolverNoAddressesAcrossEndpoints(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	empty := func(string, string) (int, int, []dohAnswer) {
		hits.Add(1)
		return http.StatusOK, 0, nil
	}
	first := newDoHServer(t, empty)
	second := newDoHServer(t, empty)
	r := &dohResolver{client: dohTestClient(), endpoints: []string{first.URL, second.URL}}

	_, err := r.lookup(context.Background(), "example.com")
	if err == nil {
		t.Fatal("empty answers did not produce an error")
	}
	if hits.Load() < 4 {
		t.Fatalf("expected A and AAAA queries against both endpoints, got %d requests", hits.Load())
	}
}

func TestDoHResolverEmptyHost(t *testing.T) {
	t.Parallel()
	r := newDoHResolver(dohTestClient())
	if _, err := r.lookup(context.Background(), "   "); err == nil {
		t.Fatal("empty host did not produce an error")
	}
}

func TestDoHMissingStatus(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Answer":[{"type":1,"data":"192.0.2.1"}]}`))
	}))
	defer server.Close()
	resolver := &dohResolver{client: server.Client(), endpoints: []string{server.URL}}
	if _, err := resolver.lookup(context.Background(), "example.test"); err == nil || !strings.Contains(err.Error(), "missing dns status") {
		t.Fatalf("error=%v", err)
	}
}

func TestDoHTimeoutReservesFallbackBudget(t *testing.T) {
	primary := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer primary.Close()
	fallback := newDoHServer(t, func(name, qtype string) (int, int, []dohAnswer) {
		return http.StatusOK, 0, []dohAnswer{{Type: dohTypeA, Data: "192.0.2.10"}}
	})
	client := dohTestClient()
	defer client.CloseIdleConnections()
	resolver := &dohResolver{client: client, endpoints: []string{primary.URL, fallback.URL}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	addresses, err := resolver.lookup(ctx, "example.test")
	if err != nil || len(addresses) != 1 || addresses[0].String() != "192.0.2.10" {
		t.Fatalf("addresses=%v error=%v", addresses, err)
	}
}
