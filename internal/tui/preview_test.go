package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
	"github.com/yippieskippie24/aps-tools/internal/roster"
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

		why := notReady
		why.onboard.phase = obReason
		why.onboard.reason.SetValue("Paid at the meeting; the chair will update the sheet")
		why.onboard.reason.Focus()
		write("onboard-override", why)

		write("members", membersPreview(a))
		big := a
		m, _ = big.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
		write("members-wide", membersPreview(m.(App)))

		help := a
		help.screen = screenHelp
		m, _ = help.Update(tea.WindowSizeMsg{Width: 84, Height: 34})
		write("help", m.(App))
	}
}

// membersPreview fills the Members screen with made-up members.
func membersPreview(a App) App {
	st := func(state connectors.State, summary string) connectors.MemberStatus {
		return connectors.MemberStatus{State: state, Summary: summary}
	}
	ok := st(connectors.StateOK, "Fine.")
	sys := func(mail, jira, forum connectors.MemberStatus) []check.SystemResult {
		return []check.SystemResult{
			{System: "Mailing list (Cloudflare)", MemberStatus: mail},
			{System: "Jira Service Management", MemberStatus: jira},
			{System: "Discourse forum", MemberStatus: forum},
		}
	}
	entry := func(first, last string, g roster.Group, sheet connectors.MemberStatus, systems []check.SystemResult) roster.Entry {
		return roster.Entry{Person: connectors.Person{FirstName: first, LastName: last, Email: strings.ToLower(first) + "." + strings.ToLower(last) + "@example.com"},
			Sheet: sheet, Systems: systems, Group: g}
	}
	noForum := st(connectors.StateMissing, "No forum account, and no invite found.")
	unverified := st(connectors.StatePending, "Added 46 days ago, but they haven't clicked Cloudflare's verification email.")
	r := roster.Roster{
		Directory: "Membership Google Sheet",
		Systems:   []string{"Mailing list (Cloudflare)", "Jira Service Management", "Discourse forum"},
		CheckedAt: time.Now().Add(-3 * time.Minute),
		Entries: []roster.Entry{
			entry("Alex", "Example", roster.NeedsSetup, ok, sys(unverified, st(connectors.StateMissing, "Not a customer on the service desk."), noForum)),
			entry("Blair", "Sample", roster.NeedsSetup, ok, sys(ok, ok, noForum)),
			entry("Casey", "Test", roster.NeedsSetup, connectors.MemberStatus{State: connectors.StateOK, Warning: "Member Status is \"Not Active\", not Active."}, sys(st(connectors.StateMissing, "Not a destination address."), ok, noForum)),
			entry("Drew", "Demo", roster.Waiting, ok, sys(unverified, ok, ok)),
			entry("Emery", "Placeholder", roster.NotReady, st(connectors.StatePending, "On the list, but \"App vetted\" isn't marked yet."), sys(ok, ok, ok)),
			entry("Frankie", "Fixture", roster.SetUp, ok, sys(ok, ok, ok)),
		},
	}
	a.screen = screenMembers
	a.members = membersView{roster: &r}
	return a
}
