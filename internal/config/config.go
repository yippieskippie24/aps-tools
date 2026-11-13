// Package config loads and saves the tool's settings file.
//
// The settings file holds NON-SECRET values only (site URLs, account IDs,
// your name). API keys and tokens live in the macOS Keychain instead — see
// internal/secrets and docs/decisions/0003-secrets-in-keychain.md.
//
// Location: $XDG_CONFIG_HOME/aps-tools/config.toml, which is normally
// ~/.config/aps-tools/config.toml. Run `aps-tools config path` to print it.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FileName is the name of the settings file inside the config directory.
const FileName = "config.toml"

// Config is everything stored in config.toml.
//
// Each outside system (connector) gets its own section here as it is built.
// When you add a section, also add a commented example to template.go so
// that `aps-tools config init` produces a self-explanatory file.
type Config struct {
	// Webmaster describes the person running the tool. Used later to sign
	// the welcome email.
	Webmaster Webmaster `toml:"webmaster"`
}

// Webmaster holds details about the current APS Webmaster.
type Webmaster struct {
	// Name is how you sign emails, e.g. "Alex Example".
	Name string `toml:"name"`
	// Email is the address members should reply to.
	Email string `toml:"email"`
}

// Dir returns the folder that holds config.toml.
//
// It honours XDG_CONFIG_HOME (handy for testing with a throwaway folder)
// and otherwise uses ~/.config/aps-tools. We deliberately use ~/.config rather
// than macOS's ~/Library/Application Support because it is easier to find
// and to explain in documentation.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "aps-tools"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find your home folder: %w", err)
	}
	return filepath.Join(home, ".config", "aps-tools"), nil
}

// Path returns the full path to config.toml.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// ErrNotFound means the settings file doesn't exist yet. Run `aps-tools config init`.
var ErrNotFound = errors.New("settings file not found (run `aps-tools config init` to create it)")

// Load reads config.toml from its standard location.
// It returns ErrNotFound if the file doesn't exist yet.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads a settings file from a specific path.
func LoadFrom(path string) (*Config, error) {
	var cfg Config
	meta, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("settings file %s has a mistake in it: %w", path, err)
	}
	// Catch typos like "emial = ..." which would otherwise be silently ignored.
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("settings file %s has unknown setting %q — check the spelling", path, undecoded[0].String())
	}
	return &cfg, nil
}

// WriteTemplate creates a new, commented config.toml at the standard location.
// It refuses to overwrite an existing file, and returns the path it wrote.
func WriteTemplate() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	return path, writeTemplateTo(path)
}

func writeTemplateTo(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("settings file already exists at %s — edit it instead", path)
	}
	// 0o700 / 0o600: only your user account can read the folder and file.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("could not create folder for settings: %w", err)
	}
	if err := os.WriteFile(path, []byte(Template), 0o600); err != nil {
		return fmt.Errorf("could not write settings file: %w", err)
	}
	return nil
}
