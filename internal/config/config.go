package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Auth          AuthConfig          `yaml:"auth"`
	SSH           SSHConfig           `yaml:"ssh"`
	NetworkPolicy NetworkPolicyConfig `yaml:"network_policy"`
	Logging       LoggingConfig       `yaml:"logging"`
	Credentials   CredentialsConfig   `yaml:"credentials"`
	UI            UIConfig            `yaml:"ui"`
}

type ServerConfig struct {
	Listen         string   `yaml:"listen"`
	SessionSecret  string   `yaml:"session_secret"`
	SecureCookie   bool     `yaml:"secure_cookie"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type AuthConfig struct {
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password"`
	PasswordHash string        `yaml:"password_hash"`
	SessionTTL   time.Duration `yaml:"session_ttl"`
}

type SSHConfig struct {
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
	IdleTimeout    time.Duration `yaml:"idle_timeout"`
	MaxSessions    int           `yaml:"max_sessions"`
	HostKeyPolicy  string        `yaml:"host_key_policy"`
	KnownHostsFile string        `yaml:"known_hosts_file"`
}

type NetworkPolicyConfig struct {
	AllowPrivateRanges bool     `yaml:"allow_private_ranges"`
	Deny               []string `yaml:"deny"`
	Allow              []string `yaml:"allow"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type CredentialsConfig struct {
	Enabled bool   `yaml:"enabled"`
	DBFile  string `yaml:"db_file"`
	KeyFile string `yaml:"key_file"`
	KeyHex  string `yaml:"-"`
}

type UIConfig struct {
	// ThemesDir is a local directory of *.json xterm themes.
	// Directory files overlay embedded defaults and can add new themes.
	ThemesDir string `yaml:"themes_dir"`
}

// duration is a helper for YAML duration strings like "15s", "30m".
type duration struct {
	time.Duration
}

func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a string")
	}
	if value.Value == "" {
		d.Duration = 0
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		// try integer seconds
		if sec, err2 := strconv.Atoi(value.Value); err2 == nil {
			d.Duration = time.Duration(sec) * time.Second
			return nil
		}
		return err
	}
	d.Duration = parsed
	return nil
}

// rawConfig supports YAML duration strings.
type rawConfig struct {
	Server struct {
		Listen         string   `yaml:"listen"`
		SessionSecret  string   `yaml:"session_secret"`
		SecureCookie   bool     `yaml:"secure_cookie"`
		AllowedOrigins []string `yaml:"allowed_origins"`
	} `yaml:"server"`
	Auth struct {
		Username     string   `yaml:"username"`
		Password     string   `yaml:"password"`
		PasswordHash string   `yaml:"password_hash"`
		SessionTTL   duration `yaml:"session_ttl"`
	} `yaml:"auth"`
	SSH struct {
		ConnectTimeout duration `yaml:"connect_timeout"`
		IdleTimeout    duration `yaml:"idle_timeout"`
		MaxSessions    int      `yaml:"max_sessions"`
		HostKeyPolicy  string   `yaml:"host_key_policy"`
		KnownHostsFile string   `yaml:"known_hosts_file"`
	} `yaml:"ssh"`
	NetworkPolicy NetworkPolicyConfig `yaml:"network_policy"`
	Logging       LoggingConfig       `yaml:"logging"`
	Credentials   struct {
		Enabled *bool  `yaml:"enabled"`
		DBFile  string `yaml:"db_file"`
		KeyFile string `yaml:"key_file"`
	} `yaml:"credentials"`
	UI struct {
		ThemesDir string `yaml:"themes_dir"`
	} `yaml:"ui"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Listen:         "127.0.0.1:8080",
			SessionSecret:  "",
			SecureCookie:   false,
			AllowedOrigins: nil,
		},
		Auth: AuthConfig{
			Username:     "admin",
			Password:     "",
			PasswordHash: "",
			SessionTTL:   12 * time.Hour,
		},
		SSH: SSHConfig{
			ConnectTimeout: 15 * time.Second,
			IdleTimeout:    30 * time.Minute,
			MaxSessions:    5,
			HostKeyPolicy:  "known-hosts",
			KnownHostsFile: "./known_hosts",
		},
		NetworkPolicy: NetworkPolicyConfig{
			AllowPrivateRanges: false,
			Deny: []string{
				"127.0.0.0/8",
				"169.254.0.0/16",
				"::1/128",
				"0.0.0.0/8",
			},
			Allow: nil,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
		Credentials: CredentialsConfig{Enabled: true, DBFile: "./credentials.db"},
		UI:          UIConfig{ThemesDir: "./themes"},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyRaw(cfg, &raw)
	return cfg, nil
}

func applyRaw(cfg *Config, raw *rawConfig) {
	if raw.Server.Listen != "" {
		cfg.Server.Listen = raw.Server.Listen
	}
	if raw.Server.SessionSecret != "" {
		cfg.Server.SessionSecret = raw.Server.SessionSecret
	}
	cfg.Server.SecureCookie = raw.Server.SecureCookie
	if raw.Server.AllowedOrigins != nil {
		cfg.Server.AllowedOrigins = raw.Server.AllowedOrigins
	}

	if raw.Auth.Username != "" {
		cfg.Auth.Username = raw.Auth.Username
	}
	if raw.Auth.Password != "" {
		cfg.Auth.Password = raw.Auth.Password
	}
	if raw.Auth.PasswordHash != "" {
		cfg.Auth.PasswordHash = raw.Auth.PasswordHash
	}
	if raw.Auth.SessionTTL.Duration > 0 {
		cfg.Auth.SessionTTL = raw.Auth.SessionTTL.Duration
	}

	if raw.SSH.ConnectTimeout.Duration > 0 {
		cfg.SSH.ConnectTimeout = raw.SSH.ConnectTimeout.Duration
	}
	if raw.SSH.IdleTimeout.Duration > 0 {
		cfg.SSH.IdleTimeout = raw.SSH.IdleTimeout.Duration
	}
	if raw.SSH.MaxSessions > 0 {
		cfg.SSH.MaxSessions = raw.SSH.MaxSessions
	}
	if raw.SSH.HostKeyPolicy != "" {
		cfg.SSH.HostKeyPolicy = raw.SSH.HostKeyPolicy
	}
	if raw.SSH.KnownHostsFile != "" {
		cfg.SSH.KnownHostsFile = raw.SSH.KnownHostsFile
	}

	// Network policy: only override when explicitly set in YAML.
	// YAML zero value for bool is false; we merge deny/allow if provided.
	if raw.NetworkPolicy.Deny != nil || raw.NetworkPolicy.Allow != nil || raw.NetworkPolicy.AllowPrivateRanges {
		cfg.NetworkPolicy.AllowPrivateRanges = raw.NetworkPolicy.AllowPrivateRanges
		if raw.NetworkPolicy.Deny != nil {
			cfg.NetworkPolicy.Deny = raw.NetworkPolicy.Deny
		}
		if raw.NetworkPolicy.Allow != nil {
			cfg.NetworkPolicy.Allow = raw.NetworkPolicy.Allow
		}
	}

	if raw.Logging.Level != "" {
		cfg.Logging.Level = raw.Logging.Level
	}
	if raw.Credentials.DBFile != "" {
		cfg.Credentials.DBFile = raw.Credentials.DBFile
	}
	if raw.Credentials.KeyFile != "" {
		cfg.Credentials.KeyFile = raw.Credentials.KeyFile
	}
	if raw.Credentials.Enabled != nil {
		cfg.Credentials.Enabled = *raw.Credentials.Enabled
	}
	if raw.UI.ThemesDir != "" {
		cfg.UI.ThemesDir = raw.UI.ThemesDir
	}
}

// ApplyEnv applies environment variable overrides.
func ApplyEnv(cfg *Config) {
	if v := os.Getenv("GOWEBSSH_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("GOWEBSSH_USERNAME"); v != "" {
		cfg.Auth.Username = v
	}
	if v := os.Getenv("GOWEBSSH_PASSWORD"); v != "" {
		cfg.Auth.Password = v
	}
	if v := os.Getenv("GOWEBSSH_PASSWORD_HASH"); v != "" {
		cfg.Auth.PasswordHash = v
	}
	if v := os.Getenv("GOWEBSSH_SESSION_SECRET"); v != "" {
		cfg.Server.SessionSecret = v
	}
	if v := os.Getenv("GOWEBSSH_SECURE_COOKIE"); v != "" {
		cfg.Server.SecureCookie = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("GOWEBSSH_HOST_KEY_POLICY"); v != "" {
		cfg.SSH.HostKeyPolicy = v
	}
	if v := os.Getenv("GOWEBSSH_KNOWN_HOSTS_FILE"); v != "" {
		cfg.SSH.KnownHostsFile = v
	}
	if v := os.Getenv("GOWEBSSH_ALLOW_PRIVATE_RANGES"); v != "" {
		cfg.NetworkPolicy.AllowPrivateRanges = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("GOWEBSSH_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("GOWEBSSH_CREDENTIALS_KEY_FILE"); v != "" {
		cfg.Credentials.KeyFile = v
	}
	if v := os.Getenv("GOWEBSSH_CREDENTIALS_KEY"); v != "" {
		cfg.Credentials.KeyHex = v
	}
	if v := os.Getenv("GOWEBSSH_THEMES_DIR"); v != "" {
		cfg.UI.ThemesDir = v
	}
}

// ApplyFlags applies high-priority CLI overrides.
func ApplyFlags(cfg *Config, listen string) {
	if listen != "" {
		cfg.Server.Listen = listen
	}
}

func (c *Config) Validate() error {
	if c.Server.Listen == "" {
		return errors.New("server.listen is required")
	}
	if c.Auth.Username == "" {
		return errors.New("auth.username is required")
	}
	if c.Auth.Password == "" && c.Auth.PasswordHash == "" {
		return errors.New("auth.password or auth.password_hash is required")
	}
	if c.Server.SessionSecret == "" {
		return errors.New("server.session_secret is required (set GOWEBSSH_SESSION_SECRET or config)")
	}
	if len(c.Server.SessionSecret) < 16 {
		return errors.New("server.session_secret must be at least 16 characters")
	}
	policy := strings.ToLower(strings.TrimSpace(c.SSH.HostKeyPolicy))
	if policy != "known-hosts" && policy != "insecure-ignore" {
		return fmt.Errorf("ssh.host_key_policy must be known-hosts or insecure-ignore, got %q", c.SSH.HostKeyPolicy)
	}
	c.SSH.HostKeyPolicy = policy
	if policy == "known-hosts" && strings.TrimSpace(c.SSH.KnownHostsFile) == "" {
		return errors.New("ssh.known_hosts_file is required when host_key_policy is known-hosts")
	}
	if c.SSH.MaxSessions <= 0 {
		c.SSH.MaxSessions = 5
	}
	if c.Auth.SessionTTL <= 0 {
		c.Auth.SessionTTL = 12 * time.Hour
	}
	if c.SSH.ConnectTimeout <= 0 {
		c.SSH.ConnectTimeout = 15 * time.Second
	}
	if c.SSH.IdleTimeout <= 0 {
		c.SSH.IdleTimeout = 30 * time.Minute
	}
	if c.Credentials.Enabled {
		if strings.TrimSpace(c.Credentials.DBFile) == "" {
			return errors.New("credentials.db_file is required when credential storage is enabled")
		}
		if strings.TrimSpace(c.Credentials.KeyFile) == "" {
			c.Credentials.KeyFile = c.Credentials.DBFile + ".key"
		}
		if c.Credentials.KeyHex != "" {
			decoded, err := hex.DecodeString(strings.TrimSpace(c.Credentials.KeyHex))
			if err != nil || len(decoded) != 32 {
				return errors.New("GOWEBSSH_CREDENTIALS_KEY must be exactly 64 hexadecimal characters")
			}
		}
	}
	return nil
}

func (c *Config) UsesPlaintextPassword() bool {
	return c.Auth.Password != "" && c.Auth.PasswordHash == ""
}
