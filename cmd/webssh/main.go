package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/liansishen/go-webssh/internal/auth"
	"github.com/liansishen/go-webssh/internal/config"
	"github.com/liansishen/go-webssh/internal/httpserver"
	"github.com/liansishen/go-webssh/internal/logging"
	"github.com/liansishen/go-webssh/internal/security"
	"github.com/liansishen/go-webssh/internal/vault"
	"github.com/liansishen/go-webssh/internal/ws"
	"github.com/liansishen/go-webssh/themes"
	"github.com/liansishen/go-webssh/web"
)

var version = "0.5.10"

func main() {
	var (
		configPath string
		listen     string
		showVer    bool
	)
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.StringVar(&listen, "listen", "", "listen address (overrides config)")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.Parse()

	if showVer {
		fmt.Println(version)
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	config.ApplyEnv(cfg)
	config.ApplyFlags(cfg, listen)

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Logging.Level)
	var credentialStore *vault.Store
	if cfg.Credentials.Enabled {
		credentialStore, err = vault.Open(cfg.Credentials.DBFile)
		if err != nil {
			logger.Error("credential database error", "err", err)
			os.Exit(1)
		}
		defer credentialStore.Close()
	}

	if cfg.UsesPlaintextPassword() {
		logger.Warn("SECURITY WARNING: auth.password is configured in plaintext; prefer auth.password_hash (bcrypt)")
	}
	if cfg.SSH.HostKeyPolicy == "insecure-ignore" {
		logger.Warn("SECURITY WARNING: ssh.host_key_policy=insecure-ignore disables host key verification")
	}

	store := auth.NewStore(cfg.Auth.SessionTTL)
	authenticator := &auth.Authenticator{
		Username:      cfg.Auth.Username,
		Password:      cfg.Auth.Password,
		PasswordHash:  cfg.Auth.PasswordHash,
		Store:         store,
		SecureCookie:  cfg.Server.SecureCookie,
		TTL:           cfg.Auth.SessionTTL,
		SessionSecret: cfg.Server.SessionSecret,
	}
	if credentialStore != nil {
		authenticator.VaultSalt = credentialStore.Salt()
	}

	policy, err := security.NewNetworkPolicy(cfg.NetworkPolicy.AllowPrivateRanges, cfg.NetworkPolicy.Deny, cfg.NetworkPolicy.Allow)
	if err != nil {
		logger.Error("network policy error", "err", err)
		os.Exit(1)
	}

	hostKeyCB, err := security.NewHostKeyCallback(cfg.SSH.HostKeyPolicy, cfg.SSH.KnownHostsFile, logger)
	if err != nil {
		logger.Error("host key callback error", "err", err)
		os.Exit(1)
	}

	wsHandler := ws.NewHandler(cfg, authenticator, policy, hostKeyCB, logger)
	themeCatalog := &themes.Catalog{Dir: cfg.UI.ThemesDir}
	if names, err := themeCatalog.Names(); err != nil {
		logger.Error("theme catalog error", "err", err)
		os.Exit(1)
	} else {
		logger.Info("loaded terminal themes", "count", len(names), "dir", cfg.UI.ThemesDir)
	}
	srv := httpserver.New(cfg, authenticator, wsHandler, credentialStore, http.FS(web.FS), themeCatalog, logger)

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	logger.Info("go-webssh started", "version", version)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
