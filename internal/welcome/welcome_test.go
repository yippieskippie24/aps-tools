package welcome

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

func exampleValues() map[string]string {
	cfg := &config.Config{
		Webmaster:  config.Webmaster{Name: "Pat Webmaster"},
		Jira:       config.Jira{ServiceDeskName: "APS Help"},
		Cloudflare: config.Cloudflare{ZoneName: "example.org"},
		Discourse: config.Discourse{URL: "https://forum.example.org", GettingStartedURL: "https://forum.example.org/tag/getting-started",
			ElevatorAccessURL: "https://forum.example.org/t/elevator/25"},
	}
	return Values(cfg, connectors.Person{Email: "alex@example.com", FirstName: "Alex", LastName: "Example"})
}

func TestRenderExample(t *testing.T) {
	e, err := Render(Example, "alex@example.com", exampleValues())
	if err != nil {
		t.Fatal(err)
	}
	if e.To != "alex@example.com" || e.Subject != "Welcome to APS! A few quick steps to get connected" {
		t.Errorf("to %q, subject %q", e.To, e.Subject)
	}
	// Only the invite link is unknown until the tool creates invites.
	if strings.Join(e.Missing, ",") != "FORUM_INVITE_LINK" {
		t.Errorf("missing = %v", e.Missing)
	}
	for _, want := range []string{
		`<p style="margin:0 0 12px;">Hello Alex,</p>`,
		`news@example.org`,
		`<div style="border:2px solid #1a73e8;`, // the short-version box
		`<ol style="margin:0 0 12px;padding-left:24px;"><li style="margin-bottom:4px;"><strong>Verify your email address.</strong>`,
		`<ul style="margin:0 0 12px;padding-left:24px;">`,
		`<a href="https://forum.example.org/tag/getting-started" style="color:#1a5fb4;">https://forum.example.org/tag/getting-started</a>`,
		`<span style="background:#fff3a3;">{{FORUM_INVITE_LINK}}</span>`,
		`Welcome aboard!<br>Pat Webmaster<br>APS Webmaster`,
	} {
		if !strings.Contains(e.HTML, want) {
			t.Errorf("HTML is missing %q", want)
		}
	}
	for _, unwanted := range []string{"This is your welcome email template", "Subject:", "**", "{{FIRST_NAME}}", "<h1", "<table"} {
		if strings.Contains(e.HTML, unwanted) {
			t.Errorf("HTML contains %q", unwanted)
		}
	}
	if !strings.Contains(e.Text, "Hello Alex,") || !strings.Contains(e.Text, "1. Verify your email address.") || strings.Contains(e.Text, "**") {
		t.Errorf("plain text:\n%s", e.Text)
	}
}

func TestRenderEscapesAndLinks(t *testing.T) {
	tmpl := "# a note\nSubject: Hi {{FIRST_NAME}}\n\nHello {{FIRST_NAME}} & friends, see https://example.org/a?b=1&c=2.\n{{MYSTERY}}"
	e, err := Render(tmpl, "x@example.com", map[string]string{"FIRST_NAME": "<Al>"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Subject != "Hi <Al>" {
		t.Errorf("subject = %q", e.Subject)
	}
	if !strings.Contains(e.HTML, "Hello &lt;Al&gt; &amp; friends") {
		t.Errorf("not escaped: %s", e.HTML)
	}
	if !strings.Contains(e.HTML, `<a href="https://example.org/a?b=1&amp;c=2" style="color:#1a5fb4;">https://example.org/a?b=1&amp;c=2</a>.`) {
		t.Errorf("link (with the full stop left out): %s", e.HTML)
	}
	if strings.Join(e.Missing, ",") != "MYSTERY" {
		t.Errorf("missing = %v", e.Missing)
	}
}

func TestTemplateMistakes(t *testing.T) {
	for name, tmpl := range map[string]string{
		"no subject line": "Hello there\n\nBye",
		"no subject":      "# only notes",
		"no message":      "Subject: Hi\n\n",
	} {
		if _, err := Render(tmpl, "x@example.com", nil); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestValuesLeaveUnknownsOut(t *testing.T) {
	v := Values(&config.Config{}, connectors.Person{Email: "x@example.com"})
	if len(v) != 1 || v["MEMBER_EMAIL"] != "x@example.com" {
		t.Errorf("values = %v; only their own address is known, so only it should be filled in", v)
	}
}

func TestPage(t *testing.T) {
	e, _ := Render(Example, "alex@example.com", exampleValues())
	page, err := Page(e, "Alex Example <alex@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	for _, want := range []string{
		"<title>Welcome email: Alex Example &lt;alex@example.com&gt;</title>",
		`id="to">alex@example.com<`,
		"Copy body",
		"Fill these in after pasting",
		"<code>{{FORUM_INVITE_LINK}}</code>",
		`<p style="margin:0 0 12px;">Hello Alex,</p>`, // the body isn't escaped twice
	} {
		if !strings.Contains(s, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

// TestWritePreviewPage saves the example's copy page for a look in a
// browser. It only runs when asked:
//
//	APS_PREVIEW_DIR=/tmp/aps-previews go test ./internal/welcome -run Preview
func TestWritePreviewPage(t *testing.T) {
	dir := os.Getenv("APS_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set APS_PREVIEW_DIR to write the welcome email page")
	}
	e, _ := Render(Example, "alex@example.com", exampleValues())
	page, err := Page(e, "Alex Example <alex@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "welcome-email.html"), page, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestForMemberWithAnAccountSaysSo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{Discourse: config.Discourse{URL: "https://forum.example.org"}}
	e, created, err := Welcome.ForMember(cfg, connectors.Person{Email: "alex@example.com", FirstName: "Alex"},
		connectors.ForumLink{URL: "https://forum.example.org", HasAccount: true})
	if err != nil || !created {
		t.Fatalf("created %v, err %v", created, err)
	}
	if len(e.Notes) != 1 || !strings.Contains(e.Notes[0], "already have a forum account") {
		t.Errorf("notes = %v", e.Notes)
	}
	page, _ := Page(e, "Alex")
	if !strings.Contains(string(page), "already have a forum account") {
		t.Error("the page doesn't show the note")
	}
}

func TestEveryTemplateFillsIn(t *testing.T) {
	values := exampleValues()
	values["FORUM_INVITE_LINK"] = "https://forum.example.org/invites/abc"
	values["FORUM_USERNAME"] = "alex"
	ids, files := map[string]bool{}, map[string]bool{}
	for _, k := range Kinds {
		if ids[k.ID] || files[k.FileName] {
			t.Errorf("%s: duplicate ID or file name", k.ID)
		}
		ids[k.ID], files[k.FileName] = true, true
		e, err := Render(k.Example, "alex@example.com", values)
		if err != nil {
			t.Errorf("%s: %v", k.ID, err)
			continue
		}
		if e.Subject == "" || len(e.Missing) != 0 {
			t.Errorf("%s: subject %q, unfilled %v", k.ID, e.Subject, e.Missing)
		}
		if strings.Contains(strings.ToLower(e.HTML+e.Subject), "club") {
			t.Errorf("%s: APS is a Society, not a club", k.ID)
		}
		if got, ok := KindByID(k.ID); !ok || got.Name != k.Name {
			t.Errorf("KindByID(%q) = %v, %v", k.ID, got, ok)
		}
	}
}

func TestSuggest(t *testing.T) {
	ok, pending, missing := connectors.StateOK, connectors.StatePending, connectors.StateMissing
	cases := []struct {
		name  string
		sheet connectors.State
		mail  connectors.State
		forum connectors.State
		want  Kind
	}{
		{"not vetted", pending, ok, ok, ApplicationInProgress},
		{"brand new", ok, missing, missing, Welcome},
		{"onboarding half done", ok, pending, missing, Welcome},
		{"invite expired", ok, ok, missing, ForumInvite},
		{"invite not used yet", ok, ok, pending, ForumInvite},
		{"just onboarded", ok, pending, pending, Welcome},
		{"hasn't verified", ok, pending, ok, VerifyEmail},
		{"all set, can't get in", ok, ok, ok, ForumAccess},
	}
	for _, c := range cases {
		got := Suggest(c.sheet, map[string]connectors.State{"cloudflare": c.mail, "discourse": c.forum})
		if got.ID != c.want.ID {
			t.Errorf("%s: suggested %s, want %s", c.name, got.ID, c.want.ID)
		}
	}
}

func TestForumAccessForSomeoneWithoutAnAccount(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, _, err := ForumAccess.ForMember(&config.Config{}, connectors.Person{Email: "x@example.com"}, connectors.ForumLink{})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Notes) != 1 || !strings.Contains(e.Notes[0], "don't have an account") || e.Kind.ID != ForumAccess.ID {
		t.Errorf("notes = %v", e.Notes)
	}
}

// Only an email with nothing personal in it can go to several members at
// once; it goes to them in Bcc, greeting "everyone".
func TestForGroup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{Webmaster: config.Webmaster{Name: "Robin Example", Email: "webmaster@example.com"}}
	people := []connectors.Person{{Email: "amy@example.com", FirstName: "Amy"}, {Email: "bo@example.com", FirstName: "Bo"}}

	e, _, err := VerifyEmail.ForGroup(cfg, people)
	if err != nil {
		t.Fatal(err)
	}
	if e.To != "webmaster@example.com" || strings.Join(e.Bcc, ",") != "amy@example.com,bo@example.com" {
		t.Errorf("to %q, bcc %v", e.To, e.Bcc)
	}
	if !strings.Contains(e.Text, "Hello everyone,") || strings.Contains(e.Text, "Amy") {
		t.Errorf("text:\n%s", e.Text)
	}
	page, err := Page(e, "2 members")
	if err != nil || !strings.Contains(string(page), "Copy Bcc") || !strings.Contains(string(page), "amy@example.com, bo@example.com") {
		t.Errorf("page should offer the Bcc list (err %v)", err)
	}

	// Invite links are personal: those emails must go one by one.
	for _, k := range []Kind{Welcome, ForumInvite, ForumAccess, NotGettingEmails} {
		if _, _, err := k.ForGroup(cfg, people); !errors.Is(err, ErrNotGroupable) {
			t.Errorf("%s: err = %v, want ErrNotGroupable", k.ID, err)
		}
	}
}
