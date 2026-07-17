package themes

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEmbeddedDefaults(t *testing.T) {
	c := &Catalog{}
	all, err := c.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 8 {
		t.Fatalf("expected embedded defaults, got %d", len(all))
	}
	theme, ok := all["catppuccin-mocha"]
	if !ok {
		t.Fatal("missing catppuccin-mocha")
	}
	if theme["background"] == "" || theme["foreground"] == "" {
		t.Fatalf("incomplete theme: %#v", theme)
	}
	names, err := c.Names()
	if err != nil {
		t.Fatal(err)
	}
	if names[0] != "github-light" {
		t.Fatalf("preferred order broken: %v", names)
	}
}

func TestDirOverlayAndCustomTheme(t *testing.T) {
	dir := t.TempDir()
	custom := `{
  "background": "#000000",
  "foreground": "#ffffff",
  "cursor": "#ffffff",
  "black": "#000000",
  "red": "#ff0000",
  "green": "#00ff00",
  "yellow": "#ffff00",
  "blue": "#0000ff",
  "magenta": "#ff00ff",
  "cyan": "#00ffff",
  "white": "#ffffff",
  "brightBlack": "#555555",
  "brightRed": "#ff5555",
  "brightGreen": "#55ff55",
  "brightYellow": "#ffff55",
  "brightBlue": "#5555ff",
  "brightMagenta": "#ff55ff",
  "brightCyan": "#55ffff",
  "brightWhite": "#ffffff"
}`
	if err := os.WriteFile(filepath.Join(dir, "my-theme.json"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	// Override an embedded theme.
	override := `{"background":"#111111","foreground":"#eeeeee"}`
	if err := os.WriteFile(filepath.Join(dir, "nord.json"), []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &Catalog{Dir: dir}
	all, err := c.Load()
	if err != nil {
		t.Fatal(err)
	}
	if all["my-theme"]["background"] != "#000000" {
		t.Fatalf("custom theme missing: %#v", all["my-theme"])
	}
	if all["nord"]["background"] != "#111111" {
		t.Fatalf("override failed: %#v", all["nord"])
	}
	// Embedded theme still present.
	if all["dracula"]["background"] == "" {
		t.Fatal("embedded theme lost")
	}

	var buf bytes.Buffer
	if err := c.WriteJS(&buf); err != nil {
		t.Fatal(err)
	}
	js := buf.String()
	if !strings.Contains(js, "window.GOWEBSSH_THEMES") || !strings.Contains(js, "my-theme") {
		t.Fatalf("unexpected js: %s", js)
	}
}

func TestMissingDirIsOK(t *testing.T) {
	c := &Catalog{Dir: filepath.Join(t.TempDir(), "missing")}
	all, err := c.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("expected embedded themes when dir missing")
	}
}

func TestInvalidThemeRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"background":"#000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &Catalog{Dir: dir}
	if _, err := c.Load(); err == nil {
		t.Fatal("expected error for missing foreground")
	}
}
