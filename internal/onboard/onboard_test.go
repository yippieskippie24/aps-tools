package onboard

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// fake is a system that answers the member check with a fixed status.
type fake struct {
	name    string
	missing []string
	status  connectors.MemberStatus
}

func (f *fake) ID() string                           { return strings.ToLower(f.name) }
func (f *fake) Name() string                         { return f.name }
func (f *fake) Missing() []string                    { return f.missing }
func (f *fake) TestConnection(context.Context) error { return nil }
func (f *fake) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	return f.status, nil
}

// fakeSheet is the membership list.
type fakeSheet struct {
	fake
	person connectors.Person
}

func (f *fakeSheet) LookupMember(_ context.Context, email string) (connectors.Person, connectors.MemberStatus, error) {
	p := f.person
	p.Email = email
	return p, f.status, nil
}

// fakeOnboarder can set members up, and counts how often it's asked to.
type fakeOnboarder struct {
	fake
	plan  string
	fail  error
	calls int
}

func (f *fakeOnboarder) PlanOnboard(context.Context, connectors.Person, connectors.MemberStatus) (string, error) {
	return f.plan, nil
}

func (f *fakeOnboarder) Onboard(_ context.Context, p connectors.Person) (string, error) {
	f.calls++
	if f.fail != nil {
		return "", f.fail
	}
	return "Added " + p.Email, nil
}

func vettedSheet() *fakeSheet {
	return &fakeSheet{
		fake:   fake{name: "Membership sheet", status: connectors.MemberStatus{State: connectors.StateOK, Summary: "On the membership list, and their application is vetted."}},
		person: connectors.Person{FirstName: "Alex", LastName: "Example", Row: 42},
	}
}

func TestNotVettedStopsAtTheGate(t *testing.T) {
	sheet := vettedSheet()
	sheet.status = connectors.MemberStatus{State: connectors.StatePending,
		Summary: "On the list as New: their application is still in progress.", Fix: "Wait for the chair."}
	other := &fakeOnboarder{fake: fake{name: "Forum"}, plan: "Invite them"}

	p, err := Prepare(context.Background(), []connectors.Connector{sheet, other}, " Alex@Example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Ready || len(p.Steps) != 0 || p.Person.Email != "alex@example.com" {
		t.Fatalf("plan = %+v", p)
	}
	if want := "Not ready to onboard: on the list as New: their application is still in progress."; p.Summary() != want {
		t.Errorf("Summary() = %q", p.Summary())
	}
	if err := p.Run(context.Background(), activity.Open(filepath.Join(t.TempDir(), "a.log")), nil); !errors.Is(err, ErrNotReady) {
		t.Errorf("Run on an unvetted member: %v", err)
	}
	if other.calls != 0 {
		t.Error("an unvetted member was set up")
	}
	if !strings.Contains(p.PlainText(), "→ Wait for the chair.") {
		t.Errorf("checklist doesn't say what to do:\n%s", p.PlainText())
	}
}

func TestGateWithoutTheSheet(t *testing.T) {
	p, _ := Prepare(context.Background(), []connectors.Connector{&fake{name: "Forum"}}, "alex@example.com")
	if p.Ready || p.Gate.Fix != "Run: aps-tools setup sheet" {
		t.Errorf("no sheet: %+v", p.Gate)
	}
	sheet := vettedSheet()
	sheet.missing = []string{"a Google service account key"}
	p, _ = Prepare(context.Background(), []connectors.Connector{sheet}, "alex@example.com")
	if p.Ready || !strings.Contains(p.Gate.Summary, "isn't connected") {
		t.Errorf("sheet not connected: %+v", p.Gate)
	}
}

func TestBadEmail(t *testing.T) {
	if _, err := Prepare(context.Background(), nil, "alex"); err == nil {
		t.Error("no error for a bad email address")
	}
}

func TestPlanSteps(t *testing.T) {
	auto := &fakeOnboarder{fake: fake{name: "Mailing list", status: connectors.MemberStatus{State: connectors.StateMissing, Summary: "Not on the list."}}, plan: "Add them to news@"}
	alreadyDone := &fakeOnboarder{fake: fake{name: "Jira", status: connectors.MemberStatus{State: connectors.StateOK, Summary: "Customer."}}}
	conns := []connectors.Connector{
		vettedSheet(),
		auto,
		alreadyDone,
		&fake{name: "Forum", status: connectors.MemberStatus{State: connectors.StateMissing, Summary: "No account.", Fix: "Invite them."}},
		&fake{name: "Waiting", status: connectors.MemberStatus{State: connectors.StatePending, Summary: "Invited.", Fix: "Remind them."}},
		&fake{name: "Offline", missing: []string{"an API key"}},
	}
	p, err := Prepare(context.Background(), conns, "alex@example.com")
	if err != nil || !p.Ready {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	want := []struct {
		system string
		kind   Kind
		text   string // in Now or Action
	}{
		{"Membership sheet", Done, "Row 42: on the membership list"},
		{"Mailing list", Auto, "Add them to news@"},
		{"Jira", Done, "Customer."},
		{"Forum", Manual, "Invite them."},
		{"Waiting", Waiting, "Remind them."},
		{"Offline", Blocked, "aps-tools setup offline"},
		{"Welcome email", Manual, "aps-tools email welcome alex@example.com"},
		{"Membership sheet record", Later, "later version"},
	}
	if len(p.Steps) != len(want) {
		t.Fatalf("got %d steps: %+v", len(p.Steps), p.Steps)
	}
	for i, w := range want {
		s := p.Steps[i]
		if s.System != w.system || s.Kind != w.kind || !strings.Contains(s.Now+" "+s.Action, w.text) {
			t.Errorf("step %d = %+v, want %s %v %q", i, s, w.system, w.kind, w.text)
		}
	}
	if p.Who() != "Alex Example <alex@example.com>" {
		t.Errorf("Who() = %q", p.Who())
	}
	if want := "1 change the tool can make, 2 steps to do by hand and 1 system couldn't be checked."; p.Summary() != want {
		t.Errorf("Summary() = %q", p.Summary())
	}
}

func TestRun(t *testing.T) {
	ok := &fakeOnboarder{fake: fake{name: "Mailing list", status: connectors.MemberStatus{State: connectors.StateMissing}}, plan: "Add them"}
	bad := &fakeOnboarder{fake: fake{name: "Forum", status: connectors.MemberStatus{State: connectors.StateMissing}}, plan: "Invite them", fail: errors.New("the forum said no")}
	manual := &fake{name: "Jira", status: connectors.MemberStatus{State: connectors.StateMissing, Fix: "Add them by hand."}}
	p, _ := Prepare(context.Background(), []connectors.Connector{vettedSheet(), ok, bad, manual}, "alex@example.com")

	log := activity.Open(filepath.Join(t.TempDir(), "activity.log"))
	var seen []string
	if err := p.Run(context.Background(), log, func(s Step) { seen = append(seen, s.System) }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, ",") != "Mailing list,Forum" {
		t.Errorf("progress for %v; only the tool's own steps should run", seen)
	}
	if p.Steps[1].Outcome != Succeeded || p.Steps[1].Result != "Added alex@example.com" {
		t.Errorf("good step: %+v", p.Steps[1])
	}
	if p.Steps[2].Outcome != Failed || !strings.Contains(p.Steps[2].OutcomeText(), "the forum said no") {
		t.Errorf("failing step: %+v", p.Steps[2])
	}

	if want := "Made 1 change, 1 change failed and 2 steps to do by hand."; p.Summary() != want {
		t.Errorf("Summary() after running = %q", p.Summary())
	}

	// Running again doesn't repeat anything.
	if err := p.Run(context.Background(), log, nil); err != nil {
		t.Fatal(err)
	}
	if ok.calls != 1 || bad.calls != 1 {
		t.Errorf("steps ran %d and %d times; each should run once", ok.calls, bad.calls)
	}

	lines, _ := log.Recent(10)
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"alex@example.com\tOnboarding\tok\tStarted: 2 changes to make",
		"alex@example.com\tMailing list\tok\tAdded alex@example.com",
		"alex@example.com\tForum\tFAILED\tInvite them: the forum said no",
		"alex@example.com\tOnboarding\tFAILED\tFinished with 1 failure",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("activity log is missing %q:\n%s", want, text)
		}
	}
}

func TestNothingToDo(t *testing.T) {
	done := &fake{name: "Jira", status: connectors.MemberStatus{State: connectors.StateOK}}
	p, _ := Prepare(context.Background(), []connectors.Connector{vettedSheet(), done}, "alex@example.com")
	// An existing member set up everywhere doesn't need a welcome email.
	if p.Summary() != "Nothing to do: already set up everywhere." {
		t.Errorf("Summary() = %q", p.Summary())
	}
	if w := p.Steps[2]; w.System != "Welcome email" || w.Kind != Done || !strings.Contains(w.Now, "Not needed") {
		t.Errorf("welcome email step = %+v", w)
	}
}

func TestWarningsAreShownNotBlocking(t *testing.T) {
	sheet := vettedSheet()
	sheet.status.Warning = `Member Status is "Not Active", not Active.`
	list := &fakeOnboarder{fake: fake{name: "Mailing list", status: connectors.MemberStatus{State: connectors.StateMissing}}, plan: "Add them"}
	p, _ := Prepare(context.Background(), []connectors.Connector{sheet, list}, "alex@example.com")
	if !p.Ready || p.Steps[0].Warning == "" || p.Count(Auto) != 1 {
		t.Fatalf("a warning must not stop onboarding: %+v", p)
	}
	if !strings.Contains(p.Summary(), "1 warning to check") || !strings.Contains(p.PlainText(), `! Member Status is "Not Active"`) {
		t.Errorf("warning not shown:\n%s", p.PlainText())
	}
}

func TestOverride(t *testing.T) {
	sheet := vettedSheet()
	sheet.status = connectors.MemberStatus{State: connectors.StatePending, Summary: "On the list, but \"App vetted\" isn't marked yet."}
	list := &fakeOnboarder{fake: fake{name: "Mailing list", status: connectors.MemberStatus{State: connectors.StateMissing}}, plan: "Add them"}
	conns := []connectors.Connector{sheet, list}

	if _, err := PrepareOverride(context.Background(), conns, "alex@example.com", " too short "); !errors.Is(err, ErrNoReason) {
		t.Errorf("short reason: %v", err)
	}
	p, err := PrepareOverride(context.Background(), conns, "alex@example.com", "  Paid at the meeting; chair to update  ")
	if err != nil || !p.Ready || p.Override != "Paid at the meeting; chair to update" {
		t.Fatalf("override: %+v %v", p, err)
	}
	gate := p.Steps[0]
	if gate.Kind != Manual || !strings.Contains(gate.Now, "Overridden by you") || !strings.Contains(gate.Warning, "Paid at the meeting") {
		t.Errorf("gate step = %+v", gate)
	}
	if !strings.HasPrefix(p.Summary(), "Sheet overridden. ") || !strings.Contains(p.PlainText(), "Reason: Paid at the meeting") {
		t.Errorf("override not shown:\n%s", p.PlainText())
	}

	log := activity.Open(filepath.Join(t.TempDir(), "activity.log"))
	if err := p.LogOverride(log); err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), log, nil); err != nil || list.calls != 1 {
		t.Fatalf("run after override: %v, %d calls", err, list.calls)
	}
	lines, _ := log.Recent(10)
	if len(lines) == 0 || !strings.Contains(lines[0], "Membership sheet\tok\tOverride by the webmaster: On the list, but \"App vetted\" isn't marked yet. Reason: Paid at the meeting; chair to update") {
		t.Errorf("override not logged first: %v", lines)
	}

	// A plain Prepare never overrides.
	if p, _ := Prepare(context.Background(), conns, "alex@example.com"); p.Ready || !p.CanOverride() {
		t.Errorf("Prepare without override: ready=%v canOverride=%v", p.Ready, p.CanOverride())
	}
}

func TestNoOverrideWhenTheSheetCantBeRead(t *testing.T) {
	sheet := vettedSheet()
	sheet.missing = []string{"a Google service account key"}
	p, err := PrepareOverride(context.Background(), []connectors.Connector{sheet}, "alex@example.com", "Paid at the meeting")
	if err != nil || p.Ready || p.CanOverride() || p.Override != "" {
		t.Errorf("an unreadable sheet must be fixed, not overridden: %+v %v", p, err)
	}
	// Someone the sheet already passes needs no override.
	p, _ = PrepareOverride(context.Background(), []connectors.Connector{vettedSheet()}, "alex@example.com", "Paid at the meeting")
	if !p.Ready || p.Override != "" {
		t.Errorf("vetted member: %+v", p)
	}
}

func TestWelcomeSent(t *testing.T) {
	missingDesk := &fake{name: "Jira", status: connectors.MemberStatus{State: connectors.StateMissing, Fix: "Add them."}}
	p, _ := Prepare(context.Background(), []connectors.Connector{vettedSheet(), missingDesk}, "alex@example.com")
	log := activity.Open(filepath.Join(t.TempDir(), "activity.log"))
	if err := p.WelcomeSent(log); err != nil {
		t.Fatal(err)
	}
	var welcome Step
	for _, s := range p.Steps {
		if s.System == WelcomeStep {
			welcome = s
		}
	}
	if welcome.Outcome != Succeeded {
		t.Errorf("welcome step = %+v", welcome)
	}
	lines, _ := log.Recent(1)
	if len(lines) != 1 || !strings.Contains(lines[0], "alex@example.com\tWelcome email\tok\tSent by the webmaster") {
		t.Errorf("log = %v", lines)
	}
}
