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

	// Secrets says where API keys are kept (Keychain or 1Password).
	// See docs/setup/1password.md.
	Secrets Secrets `toml:"secrets"`

	// Jira holds the settings for Jira Service Management.
	// See docs/setup/jira.md.
	Jira Jira `toml:"jira"`
}

// Where API keys can be stored. See internal/secrets.
const (
	// StoreKeychain keeps keys in the macOS Keychain. This is the default.
	StoreKeychain = "keychain"
	// StoreOnePassword reads keys from 1Password via its "op" command-line tool.
	StoreOnePassword = "1password"
)

// Secrets says where API keys are kept.
type Secrets struct {
	// Store is "keychain" (default when empty) or "1password".
	Store string `toml:"store"`
	// JiraAPITokenRef is the 1Password secret reference (op://...) for the
	// Jira API token. Only used when Store is "1password".
	JiraAPITokenRef string `toml:"jira_api_token_ref"`
}

// UsesOnePassword reports whether keys should be read from 1Password.
func (s Secrets) UsesOnePassword() bool { return s.Store == StoreOnePassword }

// Webmaster holds details about the current APS Webmaster.
type Webmaster struct {
	// Name is how you sign emails, e.g. "Alex Example".
	Name string `toml:"name"`
	// Email is the address members should reply to.
	Email string `toml:"email"`
}

// Jira holds the settings for Jira Service Management (Cloud).
type Jira struct {
	// Site is the Jira Cloud address without https://, e.g. "example.atlassian.net".
	Site string `toml:"site"`
	// Email is the webmaster's own Atlassian account email (the API token's owner).
	Email string `toml:"email"`
	// ServiceDeskID is the numeric ID of the service desk members belong to.
	ServiceDeskID string `toml:"service_desk_id"`
	// ServiceDeskName is that service desk's name, shown in reports.
	ServiceDeskName string `toml:"service_desk_name"`
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
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("settings file already exists at %s — edit it instead", path)
	}
	return path, SaveTo(path, &Config{})
}

// LoadOrEmpty reads the settings file, or returns empty settings if it
// doesn't exist yet. Setup commands use this so they work on a fresh install.
func LoadOrEmpty() (*Config, error) {
	cfg, err := Load()
	if errors.Is(err, ErrNotFound) {
		return &Config{}, nil
	}
	return cfg, err
}

// Save writes the settings to the standard location, keeping all the
// explanatory comments (see template.go).
func Save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SaveTo(path, cfg)
}

// SaveTo writes the settings to a specific path.
//
// It writes to a temporary file first and then renames it into place, so a
// crash half-way through can never leave you with a half-written file.
func SaveTo(path string, cfg *Config) error {
	text, err := render(cfg)
	if err != nil {
		return err
	}
	// 0o700 / 0o600: only your user account can read the folder and file.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("could not create folder for settings: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o600); err != nil {
		return fmt.Errorf("could not write settings file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("could not save settings file: %w", err)
	}
	return nil
}
