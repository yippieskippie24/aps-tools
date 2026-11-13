package check

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// fakeConnector lets tests decide what a system says about a member.
type fakeConnector struct {
	name    string
	missing []string
	status  connectors.MemberStatus
	err     error
	gotMail string
}

func (f *fakeConnector) ID() string                           { return strings.ToLower(f.name) }
func (f *fakeConnector) Name() string                         { return f.name }
func (f *fakeConnector) Missing() []string                    { return f.missing }
func (f *fakeConnector) TestConnection(context.Context) error { return nil }
func (f *fakeConnector) CheckMember(_ context.Context, email string) (connectors.MemberStatus, error) {
	f.gotMail = email
	return f.status, f.err
}

func TestNormalizeEmail(t *testing.T) {
	good := map[string]string{
		"alex@example.com":       "alex@example.com",
		"  Alex@Example.COM  ":   "alex@example.com",
		"first.last@example.org": "first.last@example.org",
	}
	for in, want := range good {
		got, err := NormalizeEmail(in)
		if err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "alex", "alex@", "Alex <alex@example.com>", "alex@localhost", "a b@example.com"} {
		if _, err := NormalizeEmail(bad); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) should be rejected", bad)
		}
	}
}

func TestRunGathersResultsInOrder(t *testing.T) {
	ok := &fakeConnector{name: "Alpha", status: connectors.MemberStatus{State: connectors.StateOK, Summary: "fine"}}
	missing := &fakeConnector{name: "Beta", status: connectors.MemberStatus{State: connectors.StateMissing, Summary: "not there", Fix: "add them"}}
	unset := &fakeConnector{name: "Gamma", missing: []string{"the owner's approval", "an API key (run: aps-tools setup gamma)"}}
	broken := &fakeConnector{name: "Delta", err: errors.New("site didn't answer")}

	r, err := Run(context.Background(), []connectors.Connector{ok, missing, unset, broken}, " Alex@Example.com ", []string{"Later"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Email != "alex@example.com" || ok.gotMail != "alex@example.com" {
		t.Errorf("email not normalised before checking: report=%q connector got=%q", r.Email, ok.gotMail)
	}
	wantStates := []connectors.State{connectors.StateOK, connectors.StateMissing, connectors.StateUnknown, connectors.StateUnknown}
	for i, want := range wantStates {
		if r.Results[i].State != want {
			t.Errorf("result %d (%s) = %v, want %v", i, r.Results[i].System, r.Results[i].State, want)
		}
	}
	if d := strings.Join(r.Results[2].Details, " "); d != "Still needed: the owner's approval and an API key." {
		t.Errorf("unconfigured details = %q", d)
	}
	if !strings.Contains(r.Results[2].Fix, "aps-tools setup gamma") {
		t.Errorf("unconfigured system should say how to set it up, got %q", r.Results[2].Fix)
	}
	if unset.gotMail != "" {
		t.Error("an unconfigured connector must not be called")
	}
	if r.Worst() != connectors.StateMissing {
		t.Errorf("Worst() = %v, want Missing", r.Worst())
	}
	if r.Summary() != "3 systems need attention." {
		t.Errorf("Summary() = %q", r.Summary())
	}
	text := r.PlainText()
	for _, want := range []string{"alex@example.com", "Beta: Not set up — not there", "→ add them", "Not checked by the tool yet: Later"} {
		if !strings.Contains(text, want) {
			t.Errorf("plain-text report missing %q:\n%s", want, text)
		}
	}
}

func TestRunRejectsBadEmail(t *testing.T) {
	if _, err := Run(context.Background(), nil, "not-an-email", nil); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("err = %v, want ErrInvalidEmail", err)
	}
}

func TestSummaryWording(t *testing.T) {
	ok := SystemResult{System: "A", MemberStatus: connectors.MemberStatus{State: connectors.StateOK}}
	if s := (Report{Results: []SystemResult{ok}}).Summary(); s != "Fully set up everywhere." {
		t.Errorf("all OK: %q", s)
	}
	if s := (Report{Results: []SystemResult{ok}, NotYetChecked: []string{"B"}}).Summary(); s != "Set up in every system checked so far." {
		t.Errorf("all OK with unchecked systems: %q", s)
	}
	if s := (Report{}).Summary(); !strings.Contains(s, "No systems") {
		t.Errorf("no systems: %q", s)
	}
}
