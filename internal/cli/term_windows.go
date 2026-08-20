//go:build windows

package cli

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const utf8CodePage uint32 = 65001

func isTerminal(fd int) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(fd), &mode) == nil
}

func makeRaw(fd int) (restore func(), err error) {
	in := windows.Handle(fd)
	var inMode uint32
	if err := windows.GetConsoleMode(in, &inMode); err != nil {
		return nil, err
	}
	rawIn := inMode
	rawIn &^= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_MOUSE_INPUT
	rawIn |= windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(in, rawIn); err != nil {
		rawIn &^= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
		if err := windows.SetConsoleMode(in, rawIn); err != nil {
			return nil, err
		}
	}

	out := windows.Stdout
	var outMode uint32
	outOK := windows.GetConsoleMode(out, &outMode) == nil
	if outOK {
		rawOut := outMode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_WRAP_AT_EOL_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		if err := windows.SetConsoleMode(out, rawOut); err != nil {
			_ = windows.SetConsoleMode(out, outMode|windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_WRAP_AT_EOL_OUTPUT)
		}
	}

	oldOutCP, _ := windows.GetConsoleOutputCP()
	oldInCP, _ := windows.GetConsoleCP()
	_ = windows.SetConsoleOutputCP(utf8CodePage)
	_ = windows.SetConsoleCP(utf8CodePage)

	return func() {
		_ = windows.SetConsoleMode(in, inMode)
		if outOK {
			_ = windows.SetConsoleMode(out, outMode)
		}
		if oldOutCP != 0 {
			_ = windows.SetConsoleOutputCP(oldOutCP)
		}
		if oldInCP != 0 {
			_ = windows.SetConsoleCP(oldInCP)
		}
	}, nil
}

func windowSize(fd int) (cols, rows int, ok bool) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(fd), &info); err != nil {
		if err := windows.GetConsoleScreenBufferInfo(windows.Stdout, &info); err != nil {
			return 0, 0, false
		}
	}
	cols = int(info.Window.Right - info.Window.Left + 1)
	rows = int(info.Window.Bottom - info.Window.Top + 1)
	if cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return cols, rows, true
}

func readPassword(fd int) (string, error) {
	h := windows.Handle(fd)
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return "", err
	}
	hidden := mode
	hidden &^= windows.ENABLE_ECHO_INPUT
	if err := windows.SetConsoleMode(h, hidden); err != nil {
		return "", err
	}
	defer windows.SetConsoleMode(h, mode)

	buf := make([]byte, 512)
	var n uint32
	if err := windows.ReadFile(h, buf, &n, nil); err != nil {
		return "", err
	}
	s := string(buf[:n])
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s, nil
}

func notifyResize(ctx context.Context, fn func()) {
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		lastCols, lastRows := 0, 0
		if c, r, ok := windowSize(int(os.Stdout.Fd())); ok {
			lastCols, lastRows = c, r
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c, r, ok := windowSize(int(os.Stdout.Fd()))
				if !ok || (c == lastCols && r == lastRows) {
					continue
				}
				lastCols, lastRows = c, r
				fn()
			}
		}
	}()
}
