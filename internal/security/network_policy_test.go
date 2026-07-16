package security

import "testing"

func TestNetworkPolicyDefaults(t *testing.T) {
	p, err := NewNetworkPolicy(false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		host string
		port int
		deny bool
	}{
		{"127.0.0.1", 22, true},
		{"localhost", 22, true},
		{"169.254.169.254", 80, true},
		{"0.0.0.1", 22, true},
		{"10.0.0.1", 22, true},
		{"192.168.1.1", 22, true},
		{"8.8.8.8", 22, false},
		{"example.com", 0, true}, // invalid port
	}
	for _, tc := range cases {
		err := p.ValidateHostPort(tc.host, tc.port)
		if tc.deny && err == nil {
			t.Fatalf("expected deny for %s:%d", tc.host, tc.port)
		}
		if !tc.deny && err != nil {
			t.Fatalf("expected allow for %s:%d: %v", tc.host, tc.port, err)
		}
	}
}

func TestAllowPrivateRanges(t *testing.T) {
	p, err := NewNetworkPolicy(true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateHostPort("10.0.0.5", 22); err != nil {
		t.Fatalf("private should be allowed: %v", err)
	}
	if err := p.ValidateHostPort("127.0.0.1", 22); err == nil {
		t.Fatal("loopback still denied")
	}
	if err := p.ValidateHostPort("169.254.169.254", 80); err == nil {
		t.Fatal("metadata still denied")
	}
}

func TestRejectURLScheme(t *testing.T) {
	p, err := NewNetworkPolicy(true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateHostPort("ssh://example.com", 22); err == nil {
		t.Fatal("scheme should be rejected")
	}
}
