package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
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
	a := New(testDeps())
	a, _ = press(t, a, "2") // "Onboard a new member" arrives in Phase 2
	if !strings.Contains(a.status, "Phase 2") {
		t.Errorf("status = %q, want it to mention Phase 2", a.status)
	}
	if a.screen != screenHome {
		t.Error("an unbuilt feature should stay on the home screen")
	}
}

func TestTestConnectionsRunsDoctorAndShowsResults(t *testing.T) {
	a := New(testDeps())
	a, cmd := press(t, a, "3")
	if a.screen != screenDoctor || !a.checking || cmd == nil {
		t.Fatalf("pressing 3 should start a connection test (screen=%v checking=%v)", a.screen, a.checking)
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
	a := New(testDeps())
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
	a, _ = press(t, a, "1")
	if a.screen != screenCheck {
		t.Fatal("1 should open Check a member")
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
	a, _ = press(t, a, "1")
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
	for _, want := range []string{"alex@example.com", "Fake Desk", "Not set up", "Add them.", "Not checked by the tool yet"} {
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
