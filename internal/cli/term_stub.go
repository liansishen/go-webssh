//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !windows

package cli

import (
	"context"
	"fmt"
)

func isTerminal(fd int) bool { return false }

func makeRaw(fd int) (func(), error) {
	return nil, fmt.Errorf("raw terminal mode is not supported on this platform")
}

func windowSize(fd int) (cols, rows int, ok bool) { return 0, 0, false }

func readPassword(fd int) (string, error) {
	return "", fmt.Errorf("password prompt is not supported on this platform; set GOWEBSSH_PASSWORD")
}

func notifyResize(ctx context.Context, fn func()) {}
