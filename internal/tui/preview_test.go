package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/yippieskippie24/aps-tools/internal/doctor"
)

// TestWritePreviews saves each screen as a text file with colours, for
// reviewing the design without opening a terminal. It only runs when asked:
//
//	APS_PREVIEW_DIR=/tmp/aps-previews go test ./internal/tui -run Preview
//
// Turn a preview into an image with Charm's "freeze" tool:
//
//	freeze /tmp/aps-previews/home-dark.ans -o home.png
func TestWritePreviews(t *testing.T) {
	dir := os.Getenv("APS_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set APS_PREVIEW_DIR to write screen previews")
	}
	for _, dark := range []bool{true, false} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		a := New(testDeps())
		a.styles = newStyles(dark)
		m, _ := a.Update(tea.WindowSizeMsg{Width: 84, Height: 28})
		a = m.(App)

		write := func(name string, a App) {
			path := filepath.Join(dir, name+"-"+theme+".ans")
			if err := os.WriteFile(path, []byte(a.View().Content), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		home := a
		home.cursor = 2
		write("home", home)

		soon := a
		soon, _ = press(t, soon, "enter")
		write("home-soon", soon)

		doc := a
		doc.screen = screenDoctor
		doc.results = doctor.Run(t.Context(), testDeps())
		write("doctor", doc)

		help := a
		help.screen = screenHelp
		m, _ = help.Update(tea.WindowSizeMsg{Width: 84, Height: 34})
		write("help", m.(App))
	}
}
