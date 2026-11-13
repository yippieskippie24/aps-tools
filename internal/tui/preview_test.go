package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
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

		chk := a
		chk.screen = screenCheck
		chk.member.report = &check.Report{
			Email: "alex@example.com",
			Results: []check.SystemResult{
				{System: "Jira Service Management", MemberStatus: connectors.MemberStatus{
					State: connectors.StateOK, Summary: "Customer on the APS Help service desk.",
					Details: []string{"Name in Jira: Alex Example"}}},
				{System: "Discourse", MemberStatus: connectors.MemberStatus{
					State: connectors.StatePending, Summary: "Invited 12 days ago; invite not accepted yet.",
					Fix: "Resend the invite, or remind them to check their spam folder."}},
				{System: "Mailing list (Cloudflare)", MemberStatus: connectors.MemberStatus{
					State: connectors.StateMissing, Summary: "Not on the mailing list.",
					Fix: "Add them to the list."}},
			},
		}
		write("check-report", chk)

		chkIn := a
		chkIn, _ = chkIn.openCheck()
		chkIn = typeText(t, chkIn, "alex@exam")
		write("check-input", chkIn)

		help := a
		help.screen = screenHelp
		m, _ = help.Update(tea.WindowSizeMsg{Width: 84, Height: 34})
		write("help", m.(App))
	}
}
