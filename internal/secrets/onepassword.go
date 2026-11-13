package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
)

// This file lets the tool read API keys from 1Password instead of the
// Keychain. See docs/setup/1password.md for how to turn it on.
//
// How it works: the settings file holds a "secret reference" for each key
// (like op://APS/Jira API token/credential). To get the key, the tool runs
// 1Password's own command-line tool: `op read <reference>`. 1Password asks you
// to approve with Touch ID; the key itself is never written to disk.

// ForConfig returns the secret store the settings ask for: the Keychain by
// default, or 1Password when [secrets] store = "1password".
func ForConfig(cfg *config.Config) Store {
	if cfg == nil || !cfg.Secrets.UsesOnePassword() {
		return Keychain()
	}
	return OnePassword(map[string]string{
		JiraAPIToken:       cfg.Secrets.JiraAPITokenRef,
		CloudflareAPIToken: cfg.Secrets.CloudflareAPITokenRef,
		DiscourseAPIKey:    cfg.Secrets.DiscourseAPIKeyRef,
	})
}

// OnePassword returns a read-only Store that looks up each key name in refs
// and reads that secret reference from 1Password.
func OnePassword(refs map[string]string) Store {
	return onePasswordStore{refs: refs, run: runOp}
}

// ErrReadOnly means the store can't save keys: with 1Password, you save the
// key in the 1Password app and give the tool its secret reference instead.
var ErrReadOnly = errors.New("keys can't be saved from here when using 1Password — save it in 1Password and use its secret reference")

type onePasswordStore struct {
	refs map[string]string
	// run executes the op command; tests replace it with a fake.
	run func(args ...string) (string, error)
}

func (s onePasswordStore) Get(key string) (string, error) {
	ref := CleanReference(s.refs[key])
	if ref == "" {
		return "", ErrNotFound
	}
	v, err := s.run("read", "--no-newline", ref)
	if err != nil {
		return "", err
	}
	return v, nil
}

func (s onePasswordStore) Set(key, value string) error { return ErrReadOnly }

// Delete does nothing: the tool never removes items from your 1Password.
func (s onePasswordStore) Delete(key string) error { return nil }

// ReadReference reads one secret reference directly. Setup uses it to test a
// reference before saving it.
func ReadReference(ref string) (string, error) {
	return runOp("read", "--no-newline", CleanReference(ref))
}

// CheckOnePassword confirms the op tool is installed and connected to the
// 1Password app, by listing your accounts. Returns a fix-it message if not.
func CheckOnePassword() error {
	if _, err := exec.LookPath("op"); err != nil {
		return errors.New("1Password's command-line tool isn't installed — run: brew install --cask 1password-cli")
	}
	out, err := runOp("account", "list")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return errors.New("1Password's command-line tool isn't connected to the app — in 1Password, open Settings → Developer and turn on \"Integrate with 1Password CLI\"")
	}
	return nil
}

// CleanReference tidies a pasted secret reference. 1Password wraps references
// that contain spaces in quotes when you copy them (e.g. "op://Private/My
// Item/credential"), so surrounding quotes — straight or curly — and spaces
// are removed.
func CleanReference(s string) string {
	s = strings.TrimSpace(s)
	for _, q := range [][2]string{{`"`, `"`}, {`'`, `'`}, {"\u201c", "\u201d"}, {"\u2018", "\u2019"}} {
		if len(s) >= 2 && strings.HasPrefix(s, q[0]) && strings.HasSuffix(s, q[1]) {
			s = strings.TrimSpace(s[len(q[0]) : len(s)-len(q[1])])
			break
		}
	}
	return s
}

// LooksLikeReference reports whether s is shaped like a secret reference.
// Surrounding quotes are allowed (see CleanReference).
func LooksLikeReference(s string) bool {
	s = CleanReference(s)
	return strings.HasPrefix(s, "op://") && strings.Count(s, "/") >= 4
}

// opTimeout leaves time to approve the Touch ID prompt.
const opTimeout = 60 * time.Second

// runOp runs the 1Password CLI and returns its output, turning its errors
// into plain-English messages.
func runOp(args ...string) (string, error) {
	path, err := exec.LookPath("op")
	if err != nil {
		return "", errors.New("1Password's command-line tool isn't installed — run: brew install --cask 1password-cli")
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	// No keyboard input: op must never stop to ask questions in the
	// terminal. Approval happens in the 1Password app (Touch ID) instead.
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", errors.New("1Password didn't answer in time — was the approval prompt dismissed? Try again")
		}
		return "", explainOpError(stderr.String())
	}
	return stdout.String(), nil
}

// explainOpError translates the op tool's error output into advice.
func explainOpError(stderr string) error {
	msg := strings.TrimSpace(stderr)
	// op prefixes errors with "[ERROR] 2026/01/02 15:04:05 "; drop that.
	if i := strings.Index(msg, "[ERROR]"); i >= 0 {
		msg = strings.TrimSpace(msg[i+len("[ERROR]"):])
		if f := strings.Fields(msg); len(f) > 2 && strings.Count(f[0], "/") == 2 {
			msg = strings.Join(f[2:], " ")
		}
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "no accounts configured"),
		strings.Contains(lower, "not currently signed in"),
		strings.Contains(lower, "connecting to desktop app"):
		return errors.New("1Password's command-line tool isn't connected to the app — in 1Password, open Settings → Developer and turn on \"Integrate with 1Password CLI\", and make sure 1Password is unlocked")
	case strings.Contains(lower, "authorization prompt dismissed"), strings.Contains(lower, "authorization denied"):
		return errors.New("1Password access wasn't approved — run the command again and approve the prompt")
	case strings.Contains(lower, "isn't an item"), strings.Contains(lower, "could not find item"),
		strings.Contains(lower, "isn't a vault"), strings.Contains(lower, "isn't a field"),
		strings.Contains(lower, "could not find"):
		return fmt.Errorf("1Password couldn't find that secret reference (%s) — copy it again from the item in 1Password", msg)
	case msg == "":
		return errors.New("1Password's command-line tool failed without saying why")
	default:
		return fmt.Errorf("1Password: %s", msg)
	}
}
