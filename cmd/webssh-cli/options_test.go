package main

import (
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/liansishen/go-webssh/internal/cli"
)

func TestResolveTunnelOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		env       map[string]string
		wantStdio bool
		wantHost  string
		wantPort  int
		wantError string
	}{
		{name: "default interactive"},
		{name: "environment only", env: map[string]string{"GOWEBSSH_STDIO": "true", "GOWEBSSH_TARGET_HOST": "992319.xyz", "GOWEBSSH_TARGET_PORT": "8822"}, wantStdio: true, wantHost: "992319.xyz", wantPort: 8822},
		{name: "default port", env: map[string]string{"GOWEBSSH_STDIO": "1", "GOWEBSSH_TARGET_HOST": "target.example.com"}, wantStdio: true, wantHost: "target.example.com", wantPort: 22},
		{name: "trim environment", env: map[string]string{"GOWEBSSH_STDIO": " true ", "GOWEBSSH_TARGET_HOST": " target.example.com ", "GOWEBSSH_TARGET_PORT": " 2222 "}, wantStdio: true, wantHost: "target.example.com", wantPort: 2222},
		{name: "legacy arguments", args: []string{"--stdio", "target.example.com", "2222"}, wantStdio: true, wantHost: "target.example.com", wantPort: 2222},
		{name: "destination overrides environment", args: []string{"--stdio", "other.example.com", "2200"}, env: map[string]string{"GOWEBSSH_STDIO": "invalid", "GOWEBSSH_TARGET_HOST": "target.example.com", "GOWEBSSH_TARGET_PORT": "invalid"}, wantStdio: true, wantHost: "other.example.com", wantPort: 2200},
		{name: "combined destination overrides port flag", args: []string{"--stdio", "-p", "2200", "other.example.com:2222"}, env: map[string]string{"GOWEBSSH_TARGET_PORT": "invalid"}, wantStdio: true, wantHost: "other.example.com", wantPort: 2222},
		{name: "host argument with environment port", args: []string{"--stdio", "other.example.com"}, env: map[string]string{"GOWEBSSH_TARGET_HOST": "target.example.com", "GOWEBSSH_TARGET_PORT": "8822"}, wantStdio: true, wantHost: "other.example.com", wantPort: 8822},
		{name: "short port flag overrides environment", args: []string{"--stdio", "-p", "2200"}, env: map[string]string{"GOWEBSSH_TARGET_HOST": "target.example.com", "GOWEBSSH_TARGET_PORT": "invalid"}, wantStdio: true, wantHost: "target.example.com", wantPort: 2200},
		{name: "long port flag overrides environment", args: []string{"--stdio", "--port", "2200"}, env: map[string]string{"GOWEBSSH_TARGET_HOST": "target.example.com", "GOWEBSSH_TARGET_PORT": "8822"}, wantStdio: true, wantHost: "target.example.com", wantPort: 2200},
		{name: "zero flag preserves default", args: []string{"--stdio", "--port", "0", "target.example.com"}, env: map[string]string{"GOWEBSSH_TARGET_PORT": "8822"}, wantStdio: true, wantHost: "target.example.com", wantPort: 22},
		{name: "explicit interactive overrides environment", args: []string{"--stdio=false"}, env: map[string]string{"GOWEBSSH_STDIO": "true", "GOWEBSSH_TARGET_HOST": "target.example.com", "GOWEBSSH_TARGET_PORT": "invalid"}},
		{name: "explicit interactive ignores invalid mode", args: []string{"--stdio=false"}, env: map[string]string{"GOWEBSSH_STDIO": "invalid"}},
		{name: "interactive ignores target variables", env: map[string]string{"GOWEBSSH_STDIO": "false", "GOWEBSSH_TARGET_HOST": "target.example.com", "GOWEBSSH_TARGET_PORT": "invalid"}},
		{name: "missing host", env: map[string]string{"GOWEBSSH_STDIO": "true"}, wantError: "GOWEBSSH_TARGET_HOST"},
		{name: "empty host", args: []string{"--stdio"}, env: map[string]string{"GOWEBSSH_TARGET_HOST": " "}, wantError: "destination host is required"},
		{name: "invalid boolean", env: map[string]string{"GOWEBSSH_STDIO": "yes"}, wantError: "GOWEBSSH_STDIO must be a boolean"},
		{name: "invalid environment port", args: []string{"--stdio", "target.example.com"}, env: map[string]string{"GOWEBSSH_TARGET_PORT": "abc"}, wantError: "GOWEBSSH_TARGET_PORT"},
		{name: "zero environment port", args: []string{"--stdio", "target.example.com"}, env: map[string]string{"GOWEBSSH_TARGET_PORT": "0"}, wantError: "GOWEBSSH_TARGET_PORT"},
		{name: "large environment port", args: []string{"--stdio", "target.example.com"}, env: map[string]string{"GOWEBSSH_TARGET_PORT": "65536"}, wantError: "GOWEBSSH_TARGET_PORT"},
		{name: "negative environment port", args: []string{"--stdio", "target.example.com"}, env: map[string]string{"GOWEBSSH_TARGET_PORT": "-1"}, wantError: "GOWEBSSH_TARGET_PORT"},
		{name: "invalid positional port", args: []string{"--stdio", "target.example.com", "0"}, wantError: "tunnel port must be between"},
		{name: "invalid flag port", args: []string{"--stdio", "-p", "65536", "target.example.com"}, wantError: "tunnel destination port must be between"},
		{name: "too many arguments", args: []string{"--stdio", "target.example.com", "22", "extra"}, wantError: "accepts a host and optional port"},
		{name: "saved conflict", args: []string{"--saved", "prod"}, env: map[string]string{"GOWEBSSH_STDIO": "true"}, wantError: "cannot be combined"},
		{name: "list conflict", args: []string{"--list"}, env: map[string]string{"GOWEBSSH_STDIO": "true"}, wantError: "cannot be combined"},
		{name: "IPv6 environment host", env: map[string]string{"GOWEBSSH_STDIO": "true", "GOWEBSSH_TARGET_HOST": "2001:db8::1", "GOWEBSSH_TARGET_PORT": "65535"}, wantStdio: true, wantHost: "2001:db8::1", wantPort: 65535},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			var opt cli.Options
			var stdio bool
			fs.BoolVar(&stdio, "stdio", false, "")
			fs.IntVar(&opt.Port, "p", 0, "")
			fs.IntVar(&opt.Port, "port", 0, "")
			fs.BoolVar(&opt.ListSaved, "list", false, "")
			fs.StringVar(&opt.Saved, "saved", "", "")
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			opt, stdio, err := resolveTunnelOptions(fs, opt, stdio, func(key string) string { return tt.env[key] })
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error=%v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if stdio != tt.wantStdio || opt.Host != tt.wantHost || opt.Port != tt.wantPort {
				t.Fatalf("stdio=%v host=%q port=%d, want %v %q %d", stdio, opt.Host, opt.Port, tt.wantStdio, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestResolvePrivateConnect(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		env       string
		want      bool
		wantError bool
	}{
		{name: "default"},
		{name: "environment", env: "true", want: true},
		{name: "trimmed environment", env: " 1 ", want: true},
		{name: "disabled", env: "false"},
		{name: "invalid", env: "invalid", wantError: true},
		{name: "flag overrides", args: []string{"--private-connect"}, env: "invalid", want: true},
		{name: "disable overrides", args: []string{"--private-connect=false"}, env: "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			var value bool
			fs.BoolVar(&value, "private-connect", false, "")
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			got, err := resolvePrivateConnect(fs, value, func(string) string { return tt.env })
			if got != tt.want || (err != nil) != tt.wantError {
				t.Fatalf("value=%v error=%v", got, err)
			}
		})
	}
}
