package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
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

		plan := onboard.Plan{
			Person: connectors.Person{Email: "alex@example.com", FirstName: "Alex", LastName: "Example", Row: 42},
			Ready:  true,
			Steps: []onboard.Step{
				{System: "Membership Google Sheet", Kind: onboard.Done, Now: "On the membership list, and their application is vetted (row 42)."},
				{System: "Mailing list (Cloudflare)", Kind: onboard.Auto, Now: "Not on the mailing list.", Action: "Add a destination address, then add them to news@"},
				{System: "Jira Service Management", Kind: onboard.Done, Now: "Customer on the APS Help service desk."},
				{System: "Discourse forum", Kind: onboard.Manual, Now: "No forum account or invite.", Action: "Invite them to the forum."},
				{System: "Welcome email", Kind: onboard.Manual, Action: "Send it from your own mail for now. (The tool will draft it in a later version.)"},
				{System: "Membership sheet record", Kind: onboard.Later, Now: "The tool will record onboarding on the sheet in a later version."},
			},
		}
		ob := a
		ob, _ = ob.openOnboard()
		write("onboard-input", ob)
		ob.onboard.input.Blur()
		ob.onboard.plan, ob.onboard.phase = &plan, obPlan
		write("onboard-plan", ob)
		ob.onboard.phase = obConfirm
		write("onboard-confirm", ob)

		ran := plan
		ran.Steps = slices.Clone(plan.Steps)
		ran.Steps[1].Outcome, ran.Steps[1].Result = onboard.Succeeded, "Added a destination address and added them to news@"
		done := ob
		done.onboard.plan, done.onboard.phase = &ran, obDone
		done.onboard.notice = "Recorded in the activity log: ~/.config/aps-tools/activity.log"
		write("onboard-done", done)

		notReady := ob
		notReady.onboard.phase = obPlan
		notReady.onboard.plan = &onboard.Plan{
			Person: connectors.Person{Email: "sam@example.com", FirstName: "Sam", LastName: "Sample"},
			Gate: connectors.MemberStatus{State: connectors.StatePending, Summary: "On the list as New: their application is still in progress.",
				Fix: "Wait until the membership chair marks \"App vetted\" as Y before setting them up anywhere else."},
		}
		write("onboard-not-ready", notReady)

		help := a
		help.screen = screenHelp
		m, _ = help.Update(tea.WindowSizeMsg{Width: 84, Height: 34})
		write("help", m.(App))
	}
}
