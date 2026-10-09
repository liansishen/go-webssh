package cli

// Target hostnames are resolved exclusively through HTTPS DNS providers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	dohAccept        = "application/dns-json"
	dohMaxBodyBytes  = 64 * 1024
	dohQueryTimeout  = 10 * time.Second
	dohTypeA         = 1
	dohTypeAAAA      = 28
	dohStatusNoError = 0
)

var defaultDoHEndpoints = []string{
	"https://doh.pub/dns-query",
	"https://dns.alidns.com/resolve",
}

type dohResolver struct {
	client    *http.Client
	endpoints []string
}

func newDoHResolver(client *http.Client) *dohResolver {
	return &dohResolver{
		client:    client,
		endpoints: append([]string(nil), defaultDoHEndpoints...),
	}
}

type dohAnswer struct {
	Name string `json:"name"`
	Type int    `json:"type"`
	Data string `json:"data"`
}

// Question differs between providers and is not needed for address lookup.
type dohResponse struct {
	Status *int        `json:"Status"`
	Answer []dohAnswer `json:"Answer"`
}

func (r *dohResolver) lookup(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("doh: empty host")
	}
	if ip := parseLiteralIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if r == nil {
		return nil, errors.New("doh: nil resolver")
	}
	if r.client == nil {
		return nil, errors.New("doh: nil http client")
	}
	if len(r.endpoints) == 0 {
		return nil, errors.New("doh: no endpoints configured")
	}

	ctx, cancel := withDoHTimeout(ctx)
	defer cancel()

	var errs []error
	for index, endpoint := range r.endpoints {
		deadline, _ := ctx.Deadline()
		budget := time.Until(deadline) / time.Duration(len(r.endpoints)-index)
		if budget > 2*dohQueryTimeout {
			budget = 2 * dohQueryTimeout
		}
		// Reserve time for the remaining provider when the primary stalls.
		endpointContext, cancelEndpoint := context.WithTimeout(ctx, budget)
		ips, err := r.lookupEndpoint(endpointContext, endpoint, host)
		cancelEndpoint()
		if err != nil {
			errs = append(errs, err)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if len(ips) > 0 {
			return ips, nil
		}
		errs = append(errs, fmt.Errorf("doh: %s: no address records", endpointHost(endpoint)))
		if ctx.Err() != nil {
			break
		}
	}
	if len(errs) == 0 {
		return nil, errors.New("doh: no address records")
	}
	return nil, errors.Join(errs...)
}

func (r *dohResolver) lookupEndpoint(ctx context.Context, endpoint, host string) ([]net.IP, error) {
	ipv4, err4 := r.query(ctx, endpoint, host, dohTypeA)
	if len(ipv4) > 0 {
		return ipv4, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	ipv6, err6 := r.query(ctx, endpoint, host, dohTypeAAAA)
	if len(ipv6) > 0 {
		return ipv6, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	switch {
	case err4 != nil && err6 != nil:
		return nil, errors.Join(err4, err6)
	case err4 != nil:
		return nil, err4
	case err6 != nil:
		return nil, err6
	default:
		return nil, nil
	}
}

// Provider responses and target query URLs are excluded from error messages.
func (r *dohResolver) query(ctx context.Context, endpoint, host string, rrType int) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, dohQueryTimeout)
	defer cancel()
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("doh: invalid endpoint")
	}
	q := u.Query()
	q.Set("name", host)
	q.Set("type", dohTypeName(rrType))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("doh: build request")
	}
	req.Header.Set("Accept", dohAccept)

	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("doh: %s: request failed", endpointHost(endpoint))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, dohMaxBodyBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("doh: %s: read response", endpointHost(endpoint))
	}
	if len(body) > dohMaxBodyBytes {
		return nil, fmt.Errorf("doh: %s: response too large", endpointHost(endpoint))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh: %s: unexpected http status %d", endpointHost(endpoint), resp.StatusCode)
	}

	var parsed dohResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("doh: %s: invalid json response", endpointHost(endpoint))
	}
	if parsed.Status == nil {
		return nil, fmt.Errorf("doh: %s: missing dns status", endpointHost(endpoint))
	}
	if *parsed.Status != dohStatusNoError {
		return nil, fmt.Errorf("doh: %s: dns status %d", endpointHost(endpoint), *parsed.Status)
	}
	return filterAddresses(parsed.Answer, rrType), nil
}

// Providers include final address records alongside CNAME records.
func filterAddresses(answers []dohAnswer, rrType int) []net.IP {
	var ips []net.IP
	for _, answer := range answers {
		if answer.Type != rrType {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(answer.Data))
		if ip == nil {
			continue
		}
		if rrType == dohTypeA {
			if v4 := ip.To4(); v4 != nil {
				ips = append(ips, v4)
			}
			continue
		}
		if ip.To4() == nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

func dohTypeName(rrType int) string {
	if rrType == dohTypeAAAA {
		return "AAAA"
	}
	return "A"
}

func parseLiteralIP(host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		if ip := net.ParseIP(host[1 : len(host)-1]); ip != nil {
			return ip
		}
	}
	return nil
}

func withDoHTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 4*dohQueryTimeout)
}

func endpointHost(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "endpoint"
	}
	return u.Host
}
