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
		ConfigPath: func() (string, error) { return "/tmp/example/config.toml", nil },
		LoadConfig: func() (*config.Config, error) { return nil, config.ErrNotFound },
		Store:      secrets.NewMemory(),
		Connectors: func(*config.Config, secrets.Store) []connectors.Connector { return nil },
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
	a, _ = press(t, a, "enter") // "Check a member" is first
	if !strings.Contains(a.status, "Phase 1") {
		t.Errorf("status = %q, want it to mention Phase 1", a.status)
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
