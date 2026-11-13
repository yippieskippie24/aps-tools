package secrets

import (
	"errors"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/config"
)

func TestOnePasswordReadsTheReference(t *testing.T) {
	var gotArgs []string
	s := onePasswordStore{
		refs: map[string]string{JiraAPIToken: "op://APS/Jira API token/credential"},
		run: func(args ...string) (string, error) {
			gotArgs = args
			return "secret-value", nil
		},
	}
	v, err := s.Get(JiraAPIToken)
	if err != nil || v != "secret-value" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	if strings.Join(gotArgs, " ") != "read --no-newline op://APS/Jira API token/credential" {
		t.Errorf("op called with %q", gotArgs)
	}
}

func TestOnePasswordUnsetReferenceIsNotFound(t *testing.T) {
	s := onePasswordStore{refs: map[string]string{}, run: func(...string) (string, error) {
		t.Error("op must not be called without a reference")
		return "", nil
	}}
	if _, err := s.Get(JiraAPIToken); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if err := s.Set(JiraAPIToken, "x"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Set err = %v, want ErrReadOnly", err)
	}
}

func TestExplainOpError(t *testing.T) {
	cases := map[string]string{
		"[ERROR] 2026/11/13 10:00:00 no accounts configured for use with 1Password CLI": "Integrate with 1Password CLI",
		"[ERROR] 2026/11/13 10:00:00 authorization prompt dismissed, please try again":  "wasn't approved",
		`[ERROR] 2026/11/13 10:00:00 "Jira" isn't an item in the "APS" vault`:           "couldn't find that secret reference",
	}
	for in, want := range cases {
		if err := explainOpError(in); !strings.Contains(err.Error(), want) {
			t.Errorf("explainOpError(%q) = %q, want it to mention %q", in, err, want)
		}
	}
}

func TestForConfig(t *testing.T) {
	if _, ok := ForConfig(&config.Config{}).(keychainStore); !ok {
		t.Error("default should be the Keychain")
	}
	cfg := &config.Config{Secrets: config.Secrets{Store: config.StoreOnePassword}}
	if _, ok := ForConfig(cfg).(onePasswordStore); !ok {
		t.Error(`store = "1password" should use 1Password`)
	}
}

func TestLooksLikeReference(t *testing.T) {
	if !LooksLikeReference(" op://APS/Jira API token/credential ") {
		t.Error("a real reference was rejected")
	}
	for _, bad := range []string{"", "my-token", "op://APS", "https://example.com/a/b/c"} {
		if LooksLikeReference(bad) {
			t.Errorf("%q should not look like a reference", bad)
		}
	}
}
