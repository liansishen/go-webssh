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
		t.Fatalf("validate: %v", err)
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
	ApplyEnv(cfg)
	ApplyFlags(cfg, "127.0.0.1:19090")
	if cfg.Server.Listen != "127.0.0.1:19090" {
		t.Fatalf("flag should override env, got %s", cfg.Server.Listen)
	}
	if cfg.Auth.Username != "bob" {
		t.Fatalf("username=%s", cfg.Auth.Username)
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
