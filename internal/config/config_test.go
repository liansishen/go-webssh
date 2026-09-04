package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultAndValidate(t *testing.T) {
	cfg := Default()
	cfg.Auth.Password = "secret"
	cfg.Server.SessionSecret = "1234567890123456"
	cfg.SSH.HostKeyPolicy = "insecure-ignore"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Credentials.KeyFile != cfg.Credentials.DBFile+".key" {
		t.Fatalf("key_file=%q", cfg.Credentials.KeyFile)
	}
}

func TestLoadYAMLDurations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen: "0.0.0.0:9090"
  session_secret: "abcdefghijklmnopqrstuv"
auth:
  username: "alice"
  password: "pw"
  session_ttl: "2h"
ssh:
  connect_timeout: "5s"
  idle_timeout: "10m"
  host_key_policy: "insecure-ignore"
  max_sessions: 3
tunnel:
  enabled: true
  connect_timeout: "4s"
  write_timeout: "6s"
  idle_timeout: "8m"
  max_connections: 7
  allowed_ports: [22, 2222]
logging:
  level: "debug"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "0.0.0.0:9090" {
		t.Fatalf("listen=%s", cfg.Server.Listen)
	}
	if cfg.Auth.SessionTTL != 2*time.Hour {
		t.Fatalf("ttl=%v", cfg.Auth.SessionTTL)
	}
	if cfg.SSH.ConnectTimeout != 5*time.Second {
		t.Fatalf("connect_timeout=%v", cfg.SSH.ConnectTimeout)
	}
	if cfg.SSH.MaxSessions != 3 {
		t.Fatalf("max_sessions=%d", cfg.SSH.MaxSessions)
	}
	if !cfg.Tunnel.Enabled || cfg.Tunnel.ConnectTimeout != 4*time.Second || cfg.Tunnel.WriteTimeout != 6*time.Second || cfg.Tunnel.IdleTimeout != 8*time.Minute {
		t.Fatalf("tunnel timing config=%+v", cfg.Tunnel)
	}
	if cfg.Tunnel.MaxConnections != 7 || len(cfg.Tunnel.AllowedPorts) != 2 || cfg.Tunnel.AllowedPorts[1] != 2222 {
		t.Fatalf("tunnel limits config=%+v", cfg.Tunnel)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyEnvAndFlags(t *testing.T) {
	cfg := Default()
	t.Setenv("GOWEBSSH_LISTEN", "127.0.0.1:18080")
	t.Setenv("GOWEBSSH_USERNAME", "bob")
	t.Setenv("GOWEBSSH_PASSWORD", "pw")
	t.Setenv("GOWEBSSH_SESSION_SECRET", "secret-secret-secret")
	t.Setenv("GOWEBSSH_CREDENTIALS_KEY_FILE", "/tmp/credentials.key")
	t.Setenv("GOWEBSSH_CREDENTIALS_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("GOWEBSSH_TUNNEL_ENABLED", "true")
	ApplyEnv(cfg)
	ApplyFlags(cfg, "127.0.0.1:19090")
	if cfg.Server.Listen != "127.0.0.1:19090" {
		t.Fatalf("flag should override env, got %s", cfg.Server.Listen)
	}
	if cfg.Auth.Username != "bob" {
		t.Fatalf("username=%s", cfg.Auth.Username)
	}
	if cfg.Credentials.KeyFile != "/tmp/credentials.key" || cfg.Credentials.KeyHex == "" {
		t.Fatalf("credential key overrides were not applied: %+v", cfg.Credentials)
	}
	if !cfg.Tunnel.Enabled {
		t.Fatal("tunnel environment override was not applied")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRequiresAuth(t *testing.T) {
	cfg := Default()
	cfg.Server.SessionSecret = "1234567890123456"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing password")
	}
}

func TestCredentialStorageConfig(t *testing.T) {
	if !Default().Credentials.Enabled {
		t.Fatal("credential storage should default enabled")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("credentials:\n  enabled: false\n  db_file: /tmp/ignored.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Credentials.Enabled {
		t.Fatal("explicit disabled setting was ignored")
	}
}

func TestTunnelConfigIsSecureByDefault(t *testing.T) {
	cfg := Default()
	if cfg.Tunnel.Enabled {
		t.Fatal("TCP tunnel must be disabled by default")
	}
	if len(cfg.Tunnel.AllowedPorts) != 1 || cfg.Tunnel.AllowedPorts[0] != 22 {
		t.Fatalf("default tunnel ports=%v", cfg.Tunnel.AllowedPorts)
	}
	if cfg.Tunnel.WriteTimeout != 30*time.Second {
		t.Fatalf("default tunnel write timeout=%v", cfg.Tunnel.WriteTimeout)
	}

	cfg.Auth.Password = "secret"
	cfg.Server.SessionSecret = "1234567890123456"
	cfg.SSH.HostKeyPolicy = "insecure-ignore"
	cfg.Tunnel.AllowedPorts = []int{22, 22}
	if err := cfg.Validate(); err == nil {
		t.Fatal("duplicate tunnel ports must be rejected")
	}
}

func TestThemesDirConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen: "127.0.0.1:8080"
  session_secret: "abcdefghijklmnopqrstuv"
auth:
  username: "admin"
  password: "pw"
ssh:
  host_key_policy: "insecure-ignore"
ui:
  themes_dir: "/var/lib/go-webssh/themes"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ThemesDir != "/var/lib/go-webssh/themes" {
		t.Fatalf("themes_dir=%q", cfg.UI.ThemesDir)
	}
}
