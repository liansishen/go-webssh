package security

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestKnownHostsCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	callback, err := NewHostKeyCallback("known-hosts", path, logger)
	if err != nil {
		t.Fatal(err)
	}
	if callback == nil {
		t.Fatal("expected callback")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}
