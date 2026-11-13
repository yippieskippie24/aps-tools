// Package secrets stores and retrieves API keys and tokens in the macOS Keychain.
//
// Why the Keychain? Secrets never touch a plain-text file, so they can't be
// accidentally shared or committed. See docs/decisions/0003-secrets-in-keychain.md.
//
// Every entry is stored under the Keychain "service" name ServiceName. You can
// see (and delete) them in the Keychain Access app by searching for "aps-tools".
package secrets

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// ServiceName groups all of this tool's Keychain entries together.
// Changing it would make the tool "forget" every saved key, so don't.
const ServiceName = "aps-tools"

// Key names for each secret the tool uses. Defining them all here means
// there is one place to see every secret the tool can store.
//
// Add a constant here when a new connector needs a secret, and document it
// in that connector's setup guide in docs/setup/.
const (
	// JiraAPIToken is the Atlassian API token used for Jira Service
	// Management. See docs/setup/jira.md.
	JiraAPIToken = "jira-api-token"

	// CloudflareAPIToken is the read-only Cloudflare API token used for
	// Email Routing and the mailing list. See docs/setup/cloudflare.md.
	CloudflareAPIToken = "cloudflare-api-token"

	// DiscourseAPIKey is the read-only Discourse key (a user API key
	// approved in the browser, or an admin API key). See docs/setup/discourse.md.
	DiscourseAPIKey = "discourse-api-key"

	// GoogleServiceAccountKey is the key of the Google "service account" that
	// reads the membership Google Sheet (a small JSON document).
	// See docs/setup/google-sheet.md.
	GoogleServiceAccountKey = "google-service-account-key"

	// probeKey is never actually stored; Doctor looks it up only to prove
	// the Keychain can be reached.
	probeKey = "doctor-probe"
)

// ErrNotFound means no secret is saved under that name yet.
var ErrNotFound = errors.New("not saved in the Keychain yet")

// Store reads and writes secrets. The real implementation uses the macOS
// Keychain; tests can use an in-memory fake instead (see NewMemory).
type Store interface {
	// Get returns the secret saved under key, or ErrNotFound.
	Get(key string) (string, error)
	// Set saves (or replaces) the secret under key.
	Set(key, value string) error
	// Delete removes the secret. Deleting a missing secret is not an error.
	Delete(key string) error
}

// Keychain returns a Store backed by the macOS Keychain.
func Keychain() Store { return keychainStore{} }

type keychainStore struct{}

func (keychainStore) Get(key string) (string, error) {
	v, err := keyring.Get(ServiceName, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("could not read %q from the Keychain: %w", key, err)
	}
	return v, nil
}

func (keychainStore) Set(key, value string) error {
	if err := keyring.Set(ServiceName, key, value); err != nil {
		return fmt.Errorf("could not save %q to the Keychain: %w", key, err)
	}
	return nil
}

func (keychainStore) Delete(key string) error {
	err := keyring.Delete(ServiceName, key)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("could not delete %q from the Keychain: %w", key, err)
	}
	return nil
}

// CheckAccess confirms the store can be read. It looks up a key that never
// exists: "not found" is the healthy answer, anything else is a real problem
// (for example, the Keychain is locked or access was denied).
func CheckAccess(s Store) error {
	_, err := s.Get(probeKey)
	if err == nil || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// NewMemory returns an in-memory Store for tests. Nothing is persisted.
func NewMemory() Store { return memoryStore{} }

type memoryStore map[string]string

func (m memoryStore) Get(key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m memoryStore) Set(key, value string) error { m[key] = value; return nil }

func (m memoryStore) Delete(key string) error { delete(m, key); return nil }
