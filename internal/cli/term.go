//go:build linux || darwin || freebsd || netbsd || openbsd

package cli

import (
	"context"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	return err == nil
}

func makeRaw(fd int) (restore func(), err error) {
	old, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	raw := *old
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return nil, err
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, old)
	}, nil
}

func windowSize(fd int) (cols, rows int, ok bool) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0, false
	}
	return int(ws.Col), int(ws.Row), true
}

func readPassword(fd int) (string, error) {
	old, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return "", err
	}
	hidden := *old
	hidden.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &hidden); err != nil {
		return "", err
	}
	defer unix.IoctlSetTermios(fd, ioctlWriteTermios, old)

	var buf []byte
	tmp := make([]byte, 1)
	for {
		n, err := unix.Read(fd, tmp)
		if n > 0 {
			switch tmp[0] {
			case '\n', '\r':
				return string(buf), nil
			case 127, 8:
				if len(buf) > 0 {
					buf = buf[:len(buf)-1]
				}
			default:
				buf = append(buf, tmp[0])
			}
		}
		if err != nil {
			return string(buf), err
		}
	}
}

func notifyResize(ctx context.Context, fn func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGWINCH)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				fn()
			}
		}
	}()
}
