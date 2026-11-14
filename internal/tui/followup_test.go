package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// trioSheet lists three vetted members.
type trioSheet struct{ fakeSheet }

func (trioSheet) AllMembers(context.Context) ([]connectors.DirectoryEntry, error) {
	ok := connectors.MemberStatus{State: connectors.StateOK, Summary: "Vetted."}
	var out []connectors.DirectoryEntry
	for i, n := range []string{"Amy", "Bo", "Cy"} {
		out = append(out, connectors.DirectoryEntry{Status: ok, Person: connectors.Person{
			Email: strings.ToLower(n) + "@example.com", FirstName: n, LastName: "Test", Row: i + 2}})
	}
	return out, nil
}

// unverifiedMail is Cloudflare with everyone unverified; it records
// re-sends.
type unverifiedMail struct{ resent *[]string }

func (unverifiedMail) ID() string                           { return "cloudflare" }
func (unverifiedMail) Name() string                         { return "Mailing list (Cloudflare)" }
func (unverifiedMail) Missing() []string                    { return nil }
func (unverifiedMail) TestConnection(context.Context) error { return nil }
func (unverifiedMail) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	return connectors.MemberStatus{State: connectors.StatePending, Summary: "Hasn't clicked Cloudflare's email."}, nil
}
func (u unverifiedMail) ResendVerification(_ context.Context, email string) (string, error) {
	*u.resent = append(*u.resent, email)
	return "Cloudflare is sending them a new verification email.", nil
}

func followupDeps(t *testing.T, resent *[]string) doctor.Deps {
	deps := onboardDeps(t, true, new(int))
	deps.Connectors = func(*config.Config, secrets.Store) []connectors.Connector {
		return []connectors.Connector{trioSheet{fakeSheet{connectors.MemberStatus{State: connectors.StateOK, Summary: "Vetted."}}},
			unverifiedMail{resent}}
	}
	return deps
}

// The whole flow: the list, unticking someone, re-sending to the rest,
// the results, then emailing them one by one.
func TestFollowUpResendAndEmail(t *testing.T) {
	var opened []string
	saved := showEmail
	t.Cleanup(func() { showEmail = saved })
	showEmail = func(k welcome.Kind, _ *config.Config, p connectors.Person, _ string, _ connectors.ForumLink) (welcome.Email, error) {
		opened = append(opened, k.ID+" "+p.Email)
		return welcome.Email{}, nil
	}
	var resent []string
	deps := followupDeps(t, &resent)
	a := tall(t, New(deps))
	a, cmd := press(t, a, "2")
	if a.screen != screenFollowups {
		t.Fatal("2 should open Follow-ups")
	}
	a = settle(t, a, cmd)
	view := a.View().Content
	for _, want := range []string{"Haven't verified their email", "Amy Test", "Hasn't clicked"} {
		if !strings.Contains(view, want) {
			t.Errorf("follow-ups screen is missing %q:\n%s", want, view)
		}
	}

	// Nobody starts ticked, so nothing can be done until someone is.
	a, _ = press(t, a, "enter")
	if a.pop == nil || len(a.pop.checks) != 3 || len(a.pop.ticked()) != 0 {
		t.Fatalf("the popup should list all three, unticked: %+v", a.pop)
	}
	if a.pop.items[0].off == "" {
		t.Fatal("with nobody ticked, the action should be unavailable")
	}
	// a ticks everyone; then untick Bo.
	a, _ = press(t, a, "a")
	a, _ = press(t, a, "down")
	a, _ = press(t, a, "space")
	if got := a.pop.ticked(); len(got) != 2 || !strings.Contains(a.View().Content, "to 2 members") {
		t.Fatalf("ticked %v:\n%s", got, a.View().Content)
	}
	if len(resent) != 0 {
		t.Fatal("nothing should change before choosing the action")
	}

	// Choose the re-send.
	for range 2 { // past Cy, to the first item
		a, _ = press(t, a, "down")
	}
	a, cmd = press(t, a, "enter")
	a = settle(t, a, cmd)
	if strings.Join(resent, ",") != "amy@example.com,cy@example.com" {
		t.Fatalf("re-sent to %v:\n%s", resent, a.View().Content)
	}
	if a.pop == nil || !strings.Contains(a.View().Content, "Re-sent to 2 of 2 members") {
		t.Fatalf("results:\n%s", a.View().Content)
	}
	path, _ := deps.ConfigPath()
	logf := activity.Open(filepath.Join(filepath.Dir(path), activity.FileName))
	if lines, _ := logf.Recent(10); len(lines) != 2 {
		t.Errorf("log %v", lines)
	}

	// Email them one by one (the second choice; the first is one email
	// to both): Amy sent, Cy skipped.
	a, _ = press(t, a, "2")
	if a.queue == nil || !strings.Contains(a.View().Content, "Email 1 of 2: Amy Test") {
		t.Fatalf("queue:\n%s", a.View().Content)
	}
	a, cmd = press(t, a, "enter") // open it
	a = settle(t, a, cmd)
	if len(opened) != 1 || opened[0] != "verify-email amy@example.com" {
		t.Fatalf("opened %v", opened)
	}
	a, cmd = press(t, a, "enter") // yes, sent: next
	a = settle(t, a, cmd)
	if !strings.Contains(a.View().Content, "Email 2 of 2: Cy Test") {
		t.Fatalf("should move on to Cy:\n%s", a.View().Content)
	}
	a, _ = press(t, a, "2") // skip them
	if a.queue != nil || a.pop != nil || !strings.Contains(a.View().Content, "1 sent, 1 skipped") {
		t.Errorf("should finish:\n%s", a.View().Content)
	}
	if lines, _ := logf.Recent(10); len(lines) != 3 || !strings.Contains(lines[2], "Verify your email") {
		t.Errorf("the sent email should be logged: %v", lines)
	}
}

// Esc while emailing a list stops it, saying how far it got.
func TestFollowUpQueueStops(t *testing.T) {
	var resent []string
	a := tall(t, New(followupDeps(t, &resent)))
	a, cmd := press(t, a, "2")
	a = settle(t, a, cmd)
	a, _ = press(t, a, "enter")
	a, _ = press(t, a, "a")
	a, _ = press(t, a, "3") // Email 3 members one by one
	if a.queue == nil {
		t.Fatalf("queue didn't start:\n%s", a.View().Content)
	}
	a, _ = press(t, a, "esc")
	if a.queue != nil || a.pop != nil || !strings.Contains(a.View().Content, "3 not reached") {
		t.Errorf("esc should stop:\n%s", a.View().Content)
	}
	if len(resent) != 0 {
		t.Error("emailing must not re-send anything")
	}
}

// "Verify your email" can go to several members as one email, in Bcc.
func TestFollowUpGroupEmail(t *testing.T) {
	var groups [][]string
	saved := showGroupEmail
	t.Cleanup(func() { showGroupEmail = saved })
	showGroupEmail = func(k welcome.Kind, _ *config.Config, people []connectors.Person) (welcome.Email, error) {
		var emails []string
		for _, p := range people {
			emails = append(emails, p.Email)
		}
		groups = append(groups, append([]string{k.ID}, emails...))
		return welcome.Email{}, nil
	}
	var resent []string
	deps := followupDeps(t, &resent)
	a := tall(t, New(deps))
	a, cmd := press(t, a, "2")
	a = settle(t, a, cmd)
	a, _ = press(t, a, "enter")
	a, _ = press(t, a, "space") // tick Amy
	a, _ = press(t, a, "down")
	a, _ = press(t, a, "space") // tick Bo
	a, cmd = press(t, a, "2")   // Email 2 members together
	a = settle(t, a, cmd)
	if len(groups) != 1 || strings.Join(groups[0], " ") != "verify-email amy@example.com bo@example.com" {
		t.Fatalf("opened %v", groups)
	}
	a, cmd = press(t, a, "enter") // Yes, I sent it
	a = settle(t, a, cmd)
	path, _ := deps.ConfigPath()
	lines, _ := activity.Open(filepath.Join(filepath.Dir(path), activity.FileName)).Recent(10)
	if len(lines) != 2 || !strings.Contains(lines[0], "in Bcc") || len(resent) != 0 {
		t.Errorf("log %v, resent %v", lines, resent)
	}
	if !strings.Contains(a.View().Content, "to 2 members recorded as sent") {
		t.Errorf("no confirmation:\n%s", a.View().Content)
	}
}
