package main

import (
	"errors"
	"flag"
	"strconv"
	"strings"

	"github.com/liansishen/go-webssh/internal/cli"
)

func resolveTunnelOptions(fs *flag.FlagSet, opt cli.Options, stdio bool, getenv func(string) string) (cli.Options, bool, error) {
	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if !explicit["stdio"] {
		if value := strings.TrimSpace(getenv("GOWEBSSH_STDIO")); value != "" {
			var err error
			stdio, err = strconv.ParseBool(value)
			if err != nil {
				return opt, false, errors.New("GOWEBSSH_STDIO must be a boolean (true/false)")
			}
		}
	}
	if !stdio {
		return opt, false, nil
	}
	if opt.ListSaved || opt.Saved != "" {
		return opt, true, errors.New("--stdio cannot be combined with --list or --saved")
	}

	args := fs.Args()
	portProvided := explicit["p"] || explicit["port"]
	switch len(args) {
	case 0:
		opt.Host = strings.TrimSpace(getenv("GOWEBSSH_TARGET_HOST"))
	case 1:
		_, host, port, err := cli.ParseDestination(args[0])
		if err != nil {
			return opt, true, err
		}
		opt.Host = host
		if port != 0 {
			opt.Port = port
			portProvided = true
		}
	case 2:
		opt.Host = strings.TrimSpace(args[0])
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return opt, true, errors.New("tunnel port must be between 1 and 65535")
		}
		opt.Port = port
		portProvided = true
	default:
		return opt, true, errors.New("tunnel mode accepts a host and optional port")
	}
	if opt.Host == "" {
		return opt, true, errors.New("tunnel destination host is required (host argument or GOWEBSSH_TARGET_HOST)")
	}
	if !portProvided {
		if value := strings.TrimSpace(getenv("GOWEBSSH_TARGET_PORT")); value != "" {
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				return opt, true, errors.New("GOWEBSSH_TARGET_PORT must be between 1 and 65535")
			}
			opt.Port = port
		}
	}
	if opt.Port == 0 {
		opt.Port = 22
	}
	if opt.Port < 1 || opt.Port > 65535 {
		return opt, true, errors.New("tunnel destination port must be between 1 and 65535")
	}
	return opt, true, nil
}

func resolvePrivateConnect(fs *flag.FlagSet, value bool, getenv func(string) string) (bool, error) {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "private-connect" {
			explicit = true
		}
	})
	if explicit {
		return value, nil
	}
	raw := strings.TrimSpace(getenv("GOWEBSSH_PRIVATE_CONNECT"))
	if raw == "" {
		return value, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("GOWEBSSH_PRIVATE_CONNECT must be a boolean (true/false)")
	}
	return value, nil
}
