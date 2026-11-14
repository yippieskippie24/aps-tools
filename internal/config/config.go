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

	// Cloudflare holds the settings for APS email and the mailing list.
	// See docs/setup/cloudflare.md.
	Cloudflare Cloudflare `toml:"cloudflare"`

	// Discourse holds the settings for the APS forum.
	// See docs/setup/discourse.md.
	Discourse Discourse `toml:"discourse"`
	// Sheet holds the settings for the membership Google Sheet.
	// See docs/setup/google-sheet.md.
	Sheet Sheet `toml:"google_sheet"`
}

// Sheet holds the settings for the APS membership Google Sheet.
type Sheet struct {
	// SpreadsheetID is the long ID in the sheet's link
	// (docs.google.com/spreadsheets/d/<ID>/edit).
	SpreadsheetID string `toml:"spreadsheet_id"`
	// SpreadsheetTitle is the sheet's name, for display only.
	SpreadsheetTitle string `toml:"spreadsheet_title"`
	// Tab is the name of the tab (worksheet) that lists members.
	Tab string `toml:"tab"`
	// ServiceAccount is the email address of the Google service account the
	// tool reads as. The sheet must be shared with it. Not a secret.
	ServiceAccount string `toml:"service_account"`
	// AccessRequested is true while a request to share the sheet with the
	// service account is waiting for the sheet owner's approval. Setup sets
	// it when you choose "I've requested access", and clears it once the
	// sheet can be read.
	AccessRequested bool `toml:"access_requested"`
	// VettingStarted is the date (YYYY-MM-DD) the membership chair began
	// marking "App vetted". Members who started before it, and aren't
	// marked New, count as vetted even with a blank "App vetted": they
	// joined before it was tracked. Blank means every member needs
	// "App vetted". Setup suggests a date from the sheet.
	VettingStarted string `toml:"vetting_started"`
}

// Discourse holds the settings for the APS Discourse forum.
type Discourse struct {
	// URL is the forum's address, e.g. "https://forum.example.org".
	URL string `toml:"url"`
	// Username is the webmaster's own Discourse username (the key's owner).
	Username string `toml:"username"`
	// MemberGroup is the group every member should be in. Empty = don't check.
	MemberGroup string `toml:"member_group"`
	// KeyType says which kind of key is stored: "user" (approved in the
	// browser; works on every Discourse plan) or "admin" (an admin API key,
	// Pro plans and higher). Empty means "user".
	KeyType string `toml:"key_type"`
	// ClientID names this computer's copy of the tool to the forum when a
	// user key is used. Not a secret: it's useless without the key.
	ClientID string `toml:"client_id"`
	// GettingStartedURL is the link to the forum's "Getting Started"
	// topics, for the welcome email ({{GETTING_STARTED_LINK}}).
	// "aps-tools setup email" asks for it.
	GettingStartedURL string `toml:"getting_started_url"`
	// ElevatorAccessURL is the forum topic explaining how to request access
	// to Elevator (where members share photos with the Arboretum), for the
	// welcome email ({{ELEVATOR_ACCESS_LINK}}). "aps-tools setup email" asks.
	ElevatorAccessURL string `toml:"elevator_access_url"`
	// CreateInvites lets onboarding create each new member's personal forum
	// invite. It needs a key with write access ("aps-tools setup discourse"
	// asks). Off means the tool only reads the forum.
	CreateInvites bool `toml:"create_invites"`
	// WelcomeTopicURL is the topic new members land on after accepting
	// their invite. Optional.
	WelcomeTopicURL string `toml:"welcome_topic_url"`
}

// Discourse key types; see Discourse.KeyType.
const (
	DiscourseUserKey  = "user"
	DiscourseAdminKey = "admin"
)

// UsesAdminKey reports whether an admin API key (rather than a user API
// key) is stored for the forum.
func (d Discourse) UsesAdminKey() bool { return d.KeyType == DiscourseAdminKey }

// Cloudflare holds the settings for Cloudflare Email Routing.
type Cloudflare struct {
	// AccountID and AccountName identify the Cloudflare account APS email is in.
	AccountID   string `toml:"account_id"`
	AccountName string `toml:"account_name"`
	// ZoneID and ZoneName identify the APS domain, e.g. "example.org".
	ZoneID   string `toml:"zone_id"`
	ZoneName string `toml:"zone_name"`
	// MembersList is the group address that goes to ALL members, given as
	// the part before the @ (e.g. "news"). Empty means "news".
	MembersList string `toml:"members_list"`
}

// DefaultMembersList is used when MembersList isn't set.
const DefaultMembersList = "news"

// MembersListName returns the all-members list name, or the default.
func (c Cloudflare) MembersListName() string {
	if c.MembersList == "" {
		return DefaultMembersList
	}
	return c.MembersList
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
	// CloudflareAPITokenRef is the 1Password secret reference for the
	// Cloudflare API token. Only used when Store is "1password".
	CloudflareAPITokenRef string `toml:"cloudflare_api_token_ref"`
	// DiscourseAPIKeyRef is the 1Password secret reference for the
	// Discourse API key. Only used when Store is "1password".
	DiscourseAPIKeyRef string `toml:"discourse_api_key_ref"`
	// GoogleServiceAccountKeyRef is the 1Password secret reference for the
	// Google service account key. Only used when Store is "1password".
	GoogleServiceAccountKeyRef string `toml:"google_service_account_key_ref"`
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
