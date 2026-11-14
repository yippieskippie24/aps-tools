package tui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// testDeps never touches your real settings or Keychain.
func testDeps() doctor.Deps {
	return doctor.Deps{
		ConfigPath:       func() (string, error) { return "/tmp/example/config.toml", nil },
		LoadConfig:       func() (*config.Config, error) { return nil, config.ErrNotFound },
		Secrets:          func(*config.Config) secrets.Store { return secrets.NewMemory() },
		CheckOnePassword: func() error { return nil },
		Connectors:       func(*config.Config, secrets.Store) []connectors.Connector { return nil },
	}
}

// tall gives the app a big window, for tests that read the whole screen.
func tall(t *testing.T, a App) App {
	t.Helper()
	m, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 80})
	return m.(App)
}

// press sends one key to the app and returns the new state and command.
func press(t *testing.T, a App, key string) (App, tea.Cmd) {
	t.Helper()
	var msg tea.KeyPressMsg
	switch key {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	default:
		msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
	m, cmd := a.Update(msg)
	return m.(App), cmd
}

func TestMenuWrapsAround(t *testing.T) {
	a := New(testDeps())
	a, _ = press(t, a, "up")
	if a.cursor != len(menu)-1 {
		t.Errorf("up from the top: cursor = %d, want last item", a.cursor)
	}
	a, _ = press(t, a, "down")
	if a.cursor != 0 {
		t.Errorf("down from the bottom: cursor = %d, want 0", a.cursor)
	}
}

func TestComingSoonShowsAMessage(t *testing.T) {
	// Every current item is built, so add a pretend future one.
	saved := menu
	t.Cleanup(func() { menu = saved })
	menu = append(slices.Clone(saved), menuItem{key: "9", title: "Board changes", action: actionComingSoon, phase: "Phase 3"})
	a := New(testDeps())
	a, _ = press(t, a, "9")
	if !strings.Contains(a.status, "Phase 3") {
		t.Errorf("status = %q, want it to mention Phase 3", a.status)
	}
	if a.screen != screenHome {
		t.Error("an unbuilt feature should stay on the home screen")
	}
}

func TestTestConnectionsRunsDoctorAndShowsResults(t *testing.T) {
	a := New(testDeps())
	a, cmd := press(t, a, "4")
	if a.screen != screenDoctor || !a.checking || cmd == nil {
		t.Fatalf("pressing 4 should start a connection test (screen=%v checking=%v)", a.screen, a.checking)
	}

	// Simulate the background check finishing.
	results := doctor.Run(context.Background(), testDeps())
	m, _ := a.Update(doctorDoneMsg{results: results})
	a = m.(App)

	view := a.View().Content
	for _, want := range []string{"Settings file", "aps-tools config init", "Keychain"} {
		if !strings.Contains(view, want) {
			t.Errorf("doctor screen is missing %q", want)
		}
	}

	a, _ = press(t, a, "esc")
	if a.screen != screenHome {
		t.Error("esc should return to the home screen")
	}
}

func TestHelpOpensAndCloses(t *testing.T) {
	a := tall(t, New(testDeps()))
	a, _ = press(t, a, "?")
	if a.screen != screenHelp {
		t.Fatal("? should open help")
	}
	if !strings.Contains(a.View().Content, "handover.md") {
		t.Error("help should point new webmasters at the handover guide")
	}
	a, _ = press(t, a, "esc")
	if a.screen != screenHome {
		t.Error("esc should close help")
	}
}

func TestQuit(t *testing.T) {
	a := New(testDeps())
	_, cmd := press(t, a, "q")
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q on the home screen should quit")
	}
}

func TestRendersAtSmallAndLargeSizes(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 20}, {Width: 200, Height: 60}} {
		a := New(testDeps())
		m, _ := a.Update(size)
		if v := m.(App).View().Content; !strings.Contains(v, "APS Webmaster") {
			t.Errorf("at %dx%d the header is missing", size.Width, size.Height)
		}
	}
}

// fakeSystem is a pretend connector for check-screen tests.
type fakeSystem struct{ status connectors.MemberStatus }

func (fakeSystem) ID() string                           { return "fake" }
func (fakeSystem) Name() string                         { return "Fake Desk" }
func (fakeSystem) Missing() []string                    { return nil }
func (fakeSystem) TestConnection(context.Context) error { return nil }
func (f fakeSystem) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	return f.status, nil
}

func typeText(t *testing.T, a App, text string) App {
	t.Helper()
	for _, r := range text {
		m, _ := a.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		a = m.(App)
	}
	return a
}

func TestCheckScreenRejectsBadEmail(t *testing.T) {
	a := New(testDeps())
	a, _ = press(t, a, "2")
	if a.screen != screenCheck {
		t.Fatal("2 should open Check a member")
	}
	a = typeText(t, a, "not-an-email")
	a, cmd := press(t, a, "enter")
	if a.member.running || cmd != nil || a.member.err == "" {
		t.Errorf("a bad email should show an error and not run a check (running=%v err=%q)", a.member.running, a.member.err)
	}
}

func TestCheckScreenRunsAndShowsReport(t *testing.T) {
	deps := testDeps()
	deps.Connectors = func(*config.Config, secrets.Store) []connectors.Connector {
		return []connectors.Connector{fakeSystem{connectors.MemberStatus{
			State: connectors.StateMissing, Summary: "Not a customer.", Fix: "Add them.",
		}}}
	}
	a := New(deps)
	a, _ = press(t, a, "2")
	a = typeText(t, a, "Alex@Example.com")
	a, cmd := press(t, a, "enter")
	if !a.member.running || cmd == nil {
		t.Fatal("enter should start the check")
	}

	// Run the background work the way Bubble Tea would, and feed the
	// finished report back in.
	var done checkDoneMsg
	for _, msg := range runBatch(cmd) {
		if d, ok := msg.(checkDoneMsg); ok {
			done = d
		}
	}
	m, _ := a.Update(done)
	a = m.(App)

	view := a.View().Content
	for _, want := range []string{"alex@example.com", "Fake Desk", "Not set up", "Add them."} {
		if !strings.Contains(view, want) {
			t.Errorf("report screen is missing %q", want)
		}
	}

	a, _ = press(t, a, "n")
	if a.member.report != nil || a.member.input.Value() != "" {
		t.Error("n should start a fresh check")
	}
}

// runBatch runs a command and, if it is a batch, each command inside it,
// skipping timers so the test doesn't wait.
func runBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			if c == nil {
				continue
			}
			if m := c(); m != nil {
				if _, tick := m.(spinTickMsg); !tick {
					out = append(out, m)
				}
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

// fakeSheet is a pretend membership sheet for onboarding tests.
type fakeSheet struct{ status connectors.MemberStatus }

func (fakeSheet) ID() string                           { return "sheet" }
func (fakeSheet) Name() string                         { return "Membership sheet" }
func (fakeSheet) Missing() []string                    { return nil }
func (fakeSheet) TestConnection(context.Context) error { return nil }
func (f fakeSheet) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	return f.status, nil
}
func (f fakeSheet) LookupMember(_ context.Context, email string) (connectors.Person, connectors.MemberStatus, error) {
	return connectors.Person{Email: email, FirstName: "Alex", LastName: "Example", Row: 42}, f.status, nil
}

// fakeList is a pretend system the tool can set members up in.
type fakeList struct{ calls *int }

func (fakeList) ID() string                           { return "list" }
func (fakeList) Name() string                         { return "Mailing list" }
func (fakeList) Missing() []string                    { return nil }
func (fakeList) TestConnection(context.Context) error { return nil }
func (fakeList) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	return connectors.MemberStatus{State: connectors.StateMissing, Summary: "Not on the list."}, nil
}
func (fakeList) PlanOnboard(context.Context, connectors.Person, connectors.MemberStatus) (string, error) {
	return "Add them to news@", nil
}
func (f fakeList) Onboard(context.Context, connectors.Person) (string, error) {
	*f.calls++
	return "Added to news@", nil
}

// onboardDeps connects a vetted (or not) member to a list the tool can
// change, with the activity log in a temporary folder.
func onboardDeps(t *testing.T, vetted bool, calls *int) doctor.Deps {
	deps := testDeps()
	dir := t.TempDir()
	deps.ConfigPath = func() (string, error) { return filepath.Join(dir, "config.toml"), nil }
	st := connectors.MemberStatus{State: connectors.StateOK, Summary: "Vetted."}
	if !vetted {
		st = connectors.MemberStatus{State: connectors.StatePending, Summary: "On the list as New.", Fix: "Wait for the chair."}
	}
	deps.Connectors = func(*config.Config, secrets.Store) []connectors.Connector {
		return []connectors.Connector{fakeSheet{st}, fakeList{calls}, fakeSystem{connectors.MemberStatus{
			State: connectors.StateMissing, Summary: "Not a customer.", Fix: "Add them by hand."}}}
	}
	return deps
}

// settle runs a command's background work and feeds the results back in,
// repeatedly, until nothing is left to do.
func settle(t *testing.T, a App, cmd tea.Cmd) App {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		var next []tea.Cmd
		for _, msg := range runBatch(cmd) {
			m, c := a.Update(msg)
			a = m.(App)
			next = append(next, c)
		}
		cmd = tea.Batch(next...)
	}
	return a
}

func TestOnboardScreen(t *testing.T) {
	calls := 0
	deps := onboardDeps(t, true, &calls)
	a := tall(t, New(deps))
	a, _ = press(t, a, "3")
	if a.screen != screenOnboard {
		t.Fatal("3 should open Onboard a new member")
	}
	a = typeText(t, a, "alex@example.com")
	a, cmd := press(t, a, "enter")
	a = settle(t, a, cmd)
	if a.onboard.phase != obPlan {
		t.Fatalf("phase = %v, want the plan", a.onboard.phase)
	}
	view := a.View().Content
	for _, want := range []string{"Alex Example <alex@example.com>", "Row 42", "Add them to news@", "Add them by hand.", "copy checklist"} {
		if !strings.Contains(view, want) {
			t.Errorf("plan screen is missing %q", want)
		}
	}

	// g asks first; n cancels without changing anything.
	a, _ = press(t, a, "g")
	if a.onboard.phase != obConfirm || !strings.Contains(a.View().Content, "Make 1 change for Alex Example") {
		t.Fatalf("g should ask before changing anything (phase %v)", a.onboard.phase)
	}
	a, _ = press(t, a, "n")
	if a.onboard.phase != obPlan || calls != 0 {
		t.Fatalf("cancel: phase %v, %d changes made", a.onboard.phase, calls)
	}

	// g then y runs the step.
	a, _ = press(t, a, "g")
	a, cmd = press(t, a, "y")
	a = settle(t, a, cmd)
	if a.onboard.phase != obDone || calls != 1 {
		t.Fatalf("after go: phase %v, %d changes made", a.onboard.phase, calls)
	}
	if view := a.View().Content; !strings.Contains(view, "Added to news@") || !strings.Contains(view, "activity log") {
		t.Errorf("done screen doesn't show the result:\n%s", view)
	}
	path, _ := deps.ConfigPath()
	lines, _ := activity.Open(filepath.Join(filepath.Dir(path), activity.FileName)).Recent(10)
	if len(lines) != 3 || !strings.Contains(lines[1], "Added to news@") {
		t.Errorf("activity log = %v", lines)
	}

	// Going again does nothing more.
	a, _ = press(t, a, "g")
	if a.onboard.phase != obDone || calls != 1 {
		t.Errorf("g after finishing: phase %v, %d changes", a.onboard.phase, calls)
	}
}

func TestOnboardScreenStopsWhenNotVetted(t *testing.T) {
	calls := 0
	a := tall(t, New(onboardDeps(t, false, &calls)))
	a, _ = a.openOnboard()
	a = typeText(t, a, "alex@example.com")
	a, cmd := press(t, a, "enter")
	a = settle(t, a, cmd)
	view := a.View().Content
	if !strings.Contains(view, "Not ready to onboard") || !strings.Contains(view, "Wait for the chair.") {
		t.Errorf("not-vetted screen:\n%s", view)
	}
	a, _ = press(t, a, "g")
	if a.onboard.phase != obPlan || calls != 0 {
		t.Errorf("an unvetted member must never be set up (phase %v, %d changes)", a.onboard.phase, calls)
	}
}

func TestOnboardScreenOverride(t *testing.T) {
	calls := 0
	deps := onboardDeps(t, false, &calls)
	a := tall(t, New(deps))
	a, _ = a.openOnboard()
	a = typeText(t, a, "alex@example.com")
	a, cmd := press(t, a, "enter")
	a = settle(t, a, cmd)
	if !strings.Contains(a.View().Content, "override") {
		t.Fatal("a not-vetted member should offer an override")
	}

	a, _ = press(t, a, "o")
	if a.onboard.phase != obReason {
		t.Fatalf("o should ask for a reason (phase %v)", a.onboard.phase)
	}
	a = typeText(t, a, "ok")
	a, cmd = press(t, a, "enter")
	if cmd != nil || a.onboard.err == "" {
		t.Fatal("a too-short reason must be refused")
	}
	a = typeText(t, a, " - paid at the meeting")
	a, cmd = press(t, a, "enter")
	a = settle(t, a, cmd)
	if !a.onboard.plan.Ready || a.onboard.plan.Override != "ok - paid at the meeting" {
		t.Fatalf("override plan = %+v", a.onboard.plan)
	}
	view := a.View().Content
	for _, want := range []string{"Sheet overridden", "Override reason: ok - paid at the meeting", "Override recorded"} {
		if !strings.Contains(view, want) {
			t.Errorf("override screen is missing %q", want)
		}
	}
	if calls != 0 {
		t.Error("an override must not change anything by itself")
	}
	path, _ := deps.ConfigPath()
	lines, _ := activity.Open(filepath.Join(filepath.Dir(path), activity.FileName)).Recent(5)
	if len(lines) != 1 || !strings.Contains(lines[0], "Override by the webmaster") || !strings.Contains(lines[0], "Reason: ok - paid at the meeting") {
		t.Errorf("activity log = %v", lines)
	}
}

func TestFillsTheWindow(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 20}, {Width: 100, Height: 30}, {Width: 220, Height: 60}} {
		a := New(testDeps())
		m, _ := a.Update(size)
		a = m.(App)
		lines := strings.Split(a.View().Content, "\n")
		if len(lines) != size.Height {
			t.Errorf("%dx%d: drew %d lines, want the full height", size.Width, size.Height, len(lines))
		}
		widest := 0
		for _, l := range lines {
			widest = max(widest, lipgloss.Width(l))
		}
		if widest != size.Width-1 {
			t.Errorf("%dx%d: widest line is %d, want the window less the right margin", size.Width, size.Height, widest)
		}
	}
}

func TestLongContentScrolls(t *testing.T) {
	a := New(testDeps())
	m, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	a = m.(App)
	a, _ = press(t, a, "?")
	if a.maxScroll() == 0 {
		t.Fatal("help should be taller than a 16-line window")
	}
	if !strings.Contains(a.View().Content, "more below") {
		t.Error("no 'more below' marker")
	}
	a, _ = press(t, a, "down")
	if a.scroll != 1 {
		t.Errorf("down: scroll = %d", a.scroll)
	}
	for i := 0; i < 100; i++ {
		a, _ = press(t, a, "down")
	}
	if a.scroll != a.maxScroll() || !strings.Contains(a.View().Content, "more above") {
		t.Errorf("scrolled past the end: %d of %d", a.scroll, a.maxScroll())
	}
	a, _ = press(t, a, "esc")
	a, _ = press(t, a, "?")
	if a.scroll != 0 {
		t.Error("a screen should open at the top")
	}
}

// rosterSheet is a pretend membership sheet that can list everyone.
type rosterSheet struct{ fakeSheet }

func (rosterSheet) AllMembers(context.Context) ([]connectors.DirectoryEntry, error) {
	ok := connectors.MemberStatus{State: connectors.StateOK, Summary: "Vetted."}
	return []connectors.DirectoryEntry{
		{Person: connectors.Person{Email: "amy@example.com", FirstName: "Amy", LastName: "Able", Row: 2}, Status: ok},
		{Person: connectors.Person{Email: "ned@example.com", FirstName: "Ned", LastName: "New", Row: 3},
			Status: connectors.MemberStatus{State: connectors.StatePending, Summary: "On the list as New."}},
	}, nil
}

func TestMembersScreen(t *testing.T) {
	calls := 0
	deps := onboardDeps(t, true, &calls)
	deps.Connectors = func(*config.Config, secrets.Store) []connectors.Connector {
		return []connectors.Connector{rosterSheet{fakeSheet{connectors.MemberStatus{State: connectors.StateOK, Summary: "Vetted."}}},
			fakeList{&calls}}
	}
	a := tall(t, New(deps))
	a, cmd := press(t, a, "1")
	if a.screen != screenMembers || !a.members.loading {
		t.Fatal("1 should open Members and start checking everyone")
	}
	a = settle(t, a, cmd)
	view := a.View().Content
	for _, want := range []string{"Needs setup 1", "Not ready 1", "Everyone 2", "Amy Able", "Mail: Not on the list."} {
		if !strings.Contains(view, want) {
			t.Errorf("members screen is missing %q", want)
		}
	}
	if strings.Contains(view, "Ned New") {
		t.Error("Ned isn't vetted, so he shouldn't be under Needs setup")
	}

	// → moves through the groups; Not ready has Ned.
	for i := 0; i < 3; i++ {
		a, _ = press(t, a, "right")
	}
	if !strings.Contains(a.View().Content, "Ned New") {
		t.Errorf("Not ready tab should list Ned:\n%s", a.View().Content)
	}

	// enter opens onboarding for the highlighted member; esc comes back.
	a, _ = press(t, a, "left")
	a, _ = press(t, a, "left")
	a, _ = press(t, a, "left")
	a, cmd = press(t, a, "enter")
	if a.screen != screenOnboard || a.onboard.phase != obPlanning {
		t.Fatalf("enter should plan Amy's onboarding (screen %v phase %v)", a.screen, a.onboard.phase)
	}
	a = settle(t, a, cmd)
	if a.onboard.plan == nil || a.onboard.plan.Person.Email != "amy@example.com" {
		t.Fatalf("plan = %+v", a.onboard.plan)
	}
	a, _ = press(t, a, "esc")
	if a.screen != screenMembers {
		t.Errorf("esc from onboarding should return to Members, got %v", a.screen)
	}
	if calls != 0 {
		t.Error("opening members and plans must not change anything")
	}
}

func TestOnboardWelcomeEmail(t *testing.T) {
	var opened []string
	saved := showEmail
	t.Cleanup(func() { showEmail = saved })
	showEmail = func(_ welcome.Kind, _ *config.Config, p connectors.Person, who string, _ connectors.ForumLink) (welcome.Email, error) {
		opened = append(opened, who)
		return welcome.Email{Missing: []string{"FORUM_INVITE_LINK"}}, nil
	}

	calls := 0
	deps := onboardDeps(t, true, &calls)
	a := tall(t, New(deps))
	a, _ = a.openOnboard()
	a = typeText(t, a, "alex@example.com")
	a, cmd := press(t, a, "enter")
	a = settle(t, a, cmd)
	if !strings.Contains(a.View().Content, "welcome email") {
		t.Fatal("the plan should offer w for the welcome email")
	}

	a, cmd = press(t, a, "w")
	a = settle(t, a, cmd)
	if len(opened) != 1 || opened[0] != "Alex Example <alex@example.com>" || a.onboard.phase != obWelcome {
		t.Fatalf("opened %v, phase %v", opened, a.onboard.phase)
	}
	if view := a.View().Content; !strings.Contains(view, "Did you send it?") || !strings.Contains(view, "{{FORUM_INVITE_LINK}}") {
		t.Errorf("welcome prompt:\n%s", view)
	}

	// "not yet" records nothing.
	a, _ = press(t, a, "n")
	path, _ := deps.ConfigPath()
	log := activity.Open(filepath.Join(filepath.Dir(path), activity.FileName))
	if lines, _ := log.Recent(5); len(lines) != 0 {
		t.Errorf("not yet: log = %v", lines)
	}

	// Open it again and say yes.
	a, cmd = press(t, a, "w")
	a = settle(t, a, cmd)
	a, cmd = press(t, a, "y")
	a = settle(t, a, cmd)
	lines, _ := log.Recent(5)
	if len(lines) != 1 || !strings.Contains(lines[0], "Welcome email\tok\tSent by the webmaster") {
		t.Errorf("log = %v", lines)
	}
	if a.welcomeAvailable() {
		t.Error("after it's sent, w shouldn't offer it again")
	}
	if calls != 0 {
		t.Error("the welcome email must not change any system")
	}
}

// fakeForum is a pretend forum: no invite until onboarding creates one.
type fakeForum struct{ created *bool }

func (fakeForum) ID() string                           { return "discourse" }
func (fakeForum) Name() string                         { return "Discourse forum" }
func (fakeForum) Missing() []string                    { return nil }
func (fakeForum) TestConnection(context.Context) error { return nil }
func (f fakeForum) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	if *f.created {
		return connectors.MemberStatus{State: connectors.StatePending, Summary: "Invited."}, nil
	}
	return connectors.MemberStatus{State: connectors.StateMissing, Summary: "No forum account."}, nil
}
func (fakeForum) PlanOnboard(context.Context, connectors.Person, connectors.MemberStatus) (string, error) {
	return "Create a personal invite link", nil
}
func (f fakeForum) Onboard(context.Context, connectors.Person) (string, error) {
	*f.created = true
	return "Created a personal invite link.", nil
}
func (f fakeForum) InviteLink(context.Context, string) (connectors.ForumLink, error) {
	if *f.created {
		return connectors.ForumLink{URL: "https://forum.example.org/invites/abc"}, nil
	}
	return connectors.ForumLink{}, nil
}

func TestWelcomeEmailGetsTheInviteLink(t *testing.T) {
	var links []string
	saved := showEmail
	t.Cleanup(func() { showEmail = saved })
	showEmail = func(_ welcome.Kind, _ *config.Config, _ connectors.Person, _ string, link connectors.ForumLink) (welcome.Email, error) {
		links = append(links, link.URL)
		return welcome.Email{}, nil
	}
	created := false
	deps := onboardDeps(t, true, new(int))
	deps.Connectors = func(*config.Config, secrets.Store) []connectors.Connector {
		return []connectors.Connector{fakeSheet{connectors.MemberStatus{State: connectors.StateOK, Summary: "Vetted."}}, fakeForum{&created}}
	}
	a := tall(t, New(deps))
	a, _ = a.openOnboard()
	a = typeText(t, a, "alex@example.com")
	a, cmd := press(t, a, "enter")
	a = settle(t, a, cmd)

	// Before Go there's no invite: w explains instead of opening.
	a, cmd = press(t, a, "w")
	a = settle(t, a, cmd)
	if len(links) != 0 || !strings.Contains(a.onboard.notice, "Press g first") {
		t.Fatalf("w before go: opened %v, notice %q", links, a.onboard.notice)
	}

	// Go creates the invite; then w fills in its link.
	a, _ = press(t, a, "g")
	a, cmd = press(t, a, "y")
	a = settle(t, a, cmd)
	if !created || a.onboard.phase != obDone {
		t.Fatalf("go: created %v, phase %v", created, a.onboard.phase)
	}
	a, cmd = press(t, a, "w")
	a = settle(t, a, cmd)
	if len(links) != 1 || links[0] != "https://forum.example.org/invites/abc" {
		t.Errorf("welcome email links = %v", links)
	}
}

func TestEmailFromMembersScreen(t *testing.T) {
	var opened []string
	saved := showEmail
	t.Cleanup(func() { showEmail = saved })
	showEmail = func(k welcome.Kind, _ *config.Config, p connectors.Person, _ string, _ connectors.ForumLink) (welcome.Email, error) {
		opened = append(opened, k.ID+" "+p.Email)
		return welcome.Email{}, nil
	}
	calls := 0
	deps := onboardDeps(t, true, &calls)
	deps.Connectors = func(*config.Config, secrets.Store) []connectors.Connector {
		return []connectors.Connector{rosterSheet{fakeSheet{connectors.MemberStatus{State: connectors.StateOK, Summary: "Vetted."}}}, fakeList{&calls}}
	}
	a := tall(t, New(deps))
	a, cmd := press(t, a, "1")
	a = settle(t, a, cmd)

	// Ned isn't vetted: the suggestion is "Application in progress".
	for i := 0; i < 3; i++ {
		a, _ = press(t, a, "right")
	}
	a, _ = press(t, a, "e")
	if a.screen != screenEmail || a.email.choices[0].ID != welcome.ApplicationInProgress.ID {
		t.Fatalf("e should open the Email screen with the suggestion first: screen %v, %+v", a.screen, a.email.choices)
	}
	view := a.View().Content
	if !strings.Contains(view, "Application in progress") || !strings.Contains(view, "suggested") || !strings.Contains(view, "Changing your email address") {
		t.Errorf("picker view:\n%s", view)
	}

	a, cmd = press(t, a, "enter")
	a = settle(t, a, cmd)
	if len(opened) != 1 || opened[0] != "application-in-progress ned@example.com" || a.email.sending == nil {
		t.Fatalf("opened %v, sending %v", opened, a.email.sending)
	}
	a, cmd = press(t, a, "y")
	a = settle(t, a, cmd)
	path, _ := deps.ConfigPath()
	lines, _ := activity.Open(filepath.Join(filepath.Dir(path), activity.FileName)).Recent(5)
	if len(lines) != 1 || !strings.Contains(lines[0], "ned@example.com\tApplication in progress\tok\tSent by the webmaster") {
		t.Errorf("log = %v", lines)
	}
	if !strings.Contains(a.View().Content, "recorded as sent") {
		t.Error("no confirmation shown")
	}
	a, _ = press(t, a, "esc")
	if a.screen != screenMembers {
		t.Errorf("esc should go back to Members, got %v", a.screen)
	}
}

func TestEmailFromOnboarding(t *testing.T) {
	calls := 0
	a := tall(t, New(onboardDeps(t, true, &calls)))
	a, _ = a.openOnboard()
	a = typeText(t, a, "alex@example.com")
	a, cmd := press(t, a, "enter")
	a = settle(t, a, cmd)
	a, _ = press(t, a, "e")
	if a.screen != screenEmail || a.email.person.Email != "alex@example.com" || len(a.email.choices) != len(welcome.Kinds) {
		t.Fatalf("e from onboarding: screen %v, %+v", a.screen, a.email)
	}
	if !strings.Contains(a.View().Content, "Forum access help") {
		t.Error("every email should be offered")
	}
	a, _ = press(t, a, "esc")
	if a.screen != screenOnboard {
		t.Errorf("esc should go back to onboarding, got %v", a.screen)
	}
}

// fakeMail is a pretend mailing list that can re-send verification emails.
type fakeMail struct{ resent *int }

func (fakeMail) ID() string                           { return "cloudflare" }
func (fakeMail) Name() string                         { return "Mailing list (Cloudflare)" }
func (fakeMail) Missing() []string                    { return nil }
func (fakeMail) TestConnection(context.Context) error { return nil }
func (fakeMail) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	return connectors.MemberStatus{State: connectors.StatePending}, nil
}
func (f fakeMail) ResendVerification(context.Context, string) (string, error) {
	*f.resent++
	return "Cloudflare is sending them a new verification email.", nil
}

func TestVerifyEmailOffersToResend(t *testing.T) {
	saved := showEmail
	t.Cleanup(func() { showEmail = saved })
	showEmail = func(welcome.Kind, *config.Config, connectors.Person, string, connectors.ForumLink) (welcome.Email, error) {
		return welcome.Email{}, nil
	}
	for _, answer := range []string{"n", "y"} {
		resent := 0
		deps := onboardDeps(t, true, new(int))
		deps.Connectors = func(*config.Config, secrets.Store) []connectors.Connector {
			return []connectors.Connector{fakeMail{&resent}}
		}
		a := tall(t, New(deps))
		a = a.openEmail(connectors.Person{Email: "pat@example.com", FirstName: "Pat"}, connectors.StateOK,
			map[string]connectors.State{"cloudflare": connectors.StatePending, "discourse": connectors.StateOK})
		if a.email.choices[0].ID != welcome.VerifyEmail.ID {
			t.Fatalf("suggested %s", a.email.choices[0].ID)
		}
		a, _ = press(t, a, "enter")
		if !a.email.askResend || !strings.Contains(a.View().Content, "new verification email?") {
			t.Fatalf("verify email for an unverified address should ask about re-sending first")
		}
		a, cmd := press(t, a, answer)
		a = settle(t, a, cmd)
		path, _ := deps.ConfigPath()
		lines, _ := activity.Open(filepath.Join(filepath.Dir(path), activity.FileName)).Recent(5)
		switch answer {
		case "n":
			if resent != 0 || len(lines) != 0 || a.email.sending == nil {
				t.Errorf("n: resent %d, log %v, sending %v", resent, lines, a.email.sending)
			}
		case "y":
			if resent != 1 || len(lines) != 1 || !strings.Contains(lines[0], "Mailing list (Cloudflare)\tok\tCloudflare is sending") {
				t.Errorf("y: resent %d, log %v", resent, lines)
			}
			if !strings.Contains(a.email.notice, "✓ Cloudflare is sending") || a.email.sending == nil {
				t.Errorf("y: notice %q", a.email.notice)
			}
		}
	}
}
