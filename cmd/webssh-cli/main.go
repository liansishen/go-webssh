package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/liansishen/go-webssh/internal/cli"
)

var version = "0.5.21"

func main() {
	fs := flag.NewFlagSet("go-webssh-cli", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: go-webssh-cli [options] [user@]host[:port]
       go-webssh-cli --stdio [options] host [port]

Open an interactive SSH shell through a Go WebSSH server, or expose a raw
stream for OpenSSH ProxyCommand with --stdio. Traffic to the WebSSH server is
HTTPS/WebSocket and uses HTTP_PROXY/HTTPS_PROXY (CONNECT).

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  export https_proxy=http://lan-proxy:8080
  export GOWEBSSH_URL=https://webssh.example.com
  export GOWEBSSH_USERNAME=admin
  go-webssh-cli -i ~/.ssh/id_ed25519 user@target.example.com
  ssh -o 'ProxyCommand=go-webssh-cli --stdio %%h %%p' user@target.example.com

  go-webssh-cli --url https://webssh.example.com --list
  go-webssh-cli --url https://webssh.example.com --saved prod
`)
	}

	var (
		serverURL   string
		webUser     string
		webPassword string
		proxyURL    string
		noProxy     bool
		insecure    bool
		identity    string
		passphrase  string
		saved       string
		listSaved   bool
		port        int
		login       string
		term        string
		useTmux     bool
		tmuxSession string
		timeout     time.Duration
		showVersion bool
		stdioTunnel bool
	)

	fs.StringVar(&serverURL, "url", os.Getenv("GOWEBSSH_URL"), "WebSSH base URL (or GOWEBSSH_URL)")
	fs.StringVar(&webUser, "web-user", os.Getenv("GOWEBSSH_USERNAME"), "WebSSH login username (or GOWEBSSH_USERNAME)")
	fs.StringVar(&webPassword, "web-password", "", "WebSSH login password (or GOWEBSSH_PASSWORD)")
	fs.StringVar(&proxyURL, "proxy", "", "HTTP proxy URL (overrides HTTP_PROXY/HTTPS_PROXY and GOWEBSSH_PROXY)")
	fs.BoolVar(&noProxy, "no-proxy", false, "do not use an HTTP proxy")
	fs.BoolVar(&insecure, "insecure", false, "skip TLS verification for the WebSSH server")
	fs.StringVar(&identity, "i", "", "SSH private key file")
	fs.StringVar(&identity, "identity", "", "SSH private key file")
	fs.StringVar(&passphrase, "passphrase", "", "private key passphrase")
	fs.StringVar(&saved, "saved", "", "use a saved credential (id or name)")
	fs.BoolVar(&listSaved, "list", false, "list saved credentials and exit")
	fs.IntVar(&port, "p", 0, "SSH port (default 22)")
	fs.IntVar(&port, "port", 0, "SSH port (default 22)")
	fs.StringVar(&login, "l", "", "SSH username")
	fs.StringVar(&login, "login", "", "SSH username")
	fs.StringVar(&term, "term", "", "TERM value (default $TERM or xterm-256color)")
	fs.BoolVar(&useTmux, "herdr", false, "request Herdr session recovery")
	fs.BoolVar(&useTmux, "tmux", false, "alias for --herdr")
	fs.StringVar(&tmuxSession, "herdr-session", "", "Herdr session name")
	fs.StringVar(&tmuxSession, "tmux-session", "", "alias for --herdr-session")
	fs.DurationVar(&timeout, "timeout", 45*time.Second, "HTTP and SSH connect timeout")
	fs.BoolVar(&stdioTunnel, "stdio", false, "relay raw target TCP over stdin/stdout for OpenSSH ProxyCommand")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.BoolVar(&showVersion, "v", false, "print version and exit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if showVersion {
		fmt.Println(version)
		return
	}

	if webPassword == "" {
		webPassword = os.Getenv("GOWEBSSH_PASSWORD")
	}

	opt := cli.Options{
		ServerURL:    serverURL,
		WebUser:      webUser,
		WebPassword:  webPassword,
		ProxyURL:     proxyURL,
		NoProxy:      noProxy,
		InsecureTLS:  insecure,
		IdentityFile: identity,
		Passphrase:   passphrase,
		Saved:        saved,
		ListSaved:    listSaved,
		Port:         port,
		SSHUser:      login,
		Term:         term,
		UseTmux:      useTmux,
		TmuxSession:  tmuxSession,
		Timeout:      timeout,
		UserAgent:    "go-webssh-cli/" + version,
	}

	args := fs.Args()
	switch {
	case stdioTunnel && (listSaved || saved != ""):
		fmt.Fprintln(os.Stderr, "go-webssh-cli: --stdio cannot be combined with --list or --saved")
		os.Exit(2)
	case stdioTunnel && len(args) == 2:
		opt.Host = strings.TrimSpace(args[0])
		parsedPort, err := strconv.Atoi(args[1])
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			fmt.Fprintln(os.Stderr, "go-webssh-cli: tunnel port must be between 1 and 65535")
			os.Exit(2)
		}
		opt.Port = parsedPort
	case stdioTunnel && len(args) == 1:
		_, host, destPort, err := cli.ParseDestination(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-webssh-cli: %v\n", err)
			os.Exit(2)
		}
		opt.Host = host
		if destPort != 0 {
			opt.Port = destPort
		}
	case stdioTunnel:
		fs.Usage()
		os.Exit(2)
	case listSaved:
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "go-webssh-cli: --list does not take a destination")
			os.Exit(2)
		}
	case len(args) == 1:
		user, host, destPort, err := cli.ParseDestination(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-webssh-cli: %v\n", err)
			os.Exit(2)
		}
		opt.Host = host
		if opt.SSHUser == "" {
			opt.SSHUser = user
		}
		if destPort != 0 {
			opt.Port = destPort
		}
	case saved != "" && len(args) == 0:
		// host/user come from the saved credential
	default:
		fs.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	if stdioTunnel {
		err = cli.RunTunnel(ctx, opt, cli.DefaultStdio())
	} else {
		err = cli.Run(ctx, opt, cli.DefaultStdio())
	}
	if err == nil {
		return
	}
	var re *cli.RemoteExit
	if errors.As(err, &re) {
		if re.Message != "" && re.Code != 0 {
			fmt.Fprintf(os.Stderr, "go-webssh-cli: %s\n", re.Message)
		}
		os.Exit(re.Code)
	}
	fmt.Fprintf(os.Stderr, "go-webssh-cli: %v\n", err)
	os.Exit(1)
}
