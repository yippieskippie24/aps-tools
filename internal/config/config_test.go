package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempConfigDir points the tool at a throwaway folder for one test,
// so tests never touch your real settings.
func useTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestPathHonoursXDG(t *testing.T) {
	dir := useTempConfigDir(t)
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "aps-tools", "config.toml")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	useTempConfigDir(t)
	if _, err := Load(); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load() error = %v, want ErrNotFound", err)
	}
}

func TestTemplateLoadsAndRefusesOverwrite(t *testing.T) {
	useTempConfigDir(t)

	path, err := WriteTemplate()
	if err != nil {
		t.Fatalf("WriteTemplate() error: %v", err)
	}
	// The template must be valid TOML that matches the Config struct.
	if _, err := Load(); err != nil {
		t.Fatalf("template does not load cleanly: %v", err)
	}
	// Settings may contain personal details, so only the owner may read it.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file permissions = %o, want 600", perm)
	}
	// A second init must not clobber the user's edits.
	if _, err := WriteTemplate(); err == nil {
		t.Error("WriteTemplate() overwrote an existing file")
	}
}

func TestLoadRejectsUnknownSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[webmaster]\nemial = \"x@example.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFrom(path)
	if err == nil || !strings.Contains(err.Error(), "emial") {
		t.Errorf("LoadFrom() error = %v, want it to name the misspelled setting", err)
	}
}
