package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/followup"
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
		ob := membersPreview(a)
		obPlanned, _ := ob.openPopup(ob.planPopup(plan))
		write("onboard-plan", obPlanned)

		ran := plan
		ran.States = map[string]connectors.State{"cloudflare": connectors.StateMissing, "jira": connectors.StateOK, "discourse": connectors.StateMissing}
		ran.Steps = slices.Clone(plan.Steps)
		ran.Steps[1].Outcome, ran.Steps[1].Result = onboard.Succeeded, "Added a destination address and added them to news@"
		done, _ := ob.openPopup(ob.donePopup(popRunMsg{plan: ran}))
		write("onboard-done", done)

		notReady := ob
		nr := onboard.Plan{
			Person: connectors.Person{Email: "sam@example.com", FirstName: "Sam", LastName: "Sample"},
			Gate: connectors.MemberStatus{State: connectors.StatePending, Summary: "On the list as New: their application is still in progress.",
				Fix: "Wait until the membership chair marks \"App vetted\" as Y before setting them up anywhere else."},
		}
		notReady, _ = notReady.openPopup(notReady.planPopup(nr))
		write("onboard-not-ready", notReady)

		why, _ := notReady.openPopup(notReady.reasonPopup(memberView{person: nr.Person, sheet: nr.Gate}))
		why.pop.input.SetValue("Paid at the meeting; the chair will update the sheet")
		write("onboard-override", why)

		write("members", membersPreview(a))
		big := a
		m, _ = big.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
		write("members-wide", membersPreview(m.(App)))

		// The popups, over the wide Members screen.
		wide := membersPreview(m.(App))
		mv := entryView(wide.members.roster.Entries[0])
		actions, _ := wide.openPopup(wide.actionsPopup(mv))
		write("popup-actions", actions)
		emails, _ := actions.openPopup(actions.emailsPopup(mv))
		write("popup-emails", emails)
		planned, _ := wide.openPopup(wide.planPopup(onboard.Plan{Person: mv.person, Ready: true, Steps: plan.Steps}))
		write("popup-plan", planned)
		reasonMV := mv
		reasonMV.sheet = connectors.MemberStatus{State: connectors.StatePending, Summary: "On the list, but \"App vetted\" isn't marked yet."}
		reason, _ := wide.openPopup(wide.reasonPopup(reasonMV))
		reason.pop.input.SetValue("Paid at the meeting; the chair will update the sheet")
		write("popup-override", reason)
		// Follow-ups: the screen, the tick boxes (one unticked), the results
		// and emailing them one by one.
		fu := wide
		fu.screen = screenFollowups
		write("followups", fu)
		lists := fu.followupLists()
		ticks, _ := fu.openPopup(fu.followupPopup(lists[1]))
		ticks.pop.checks[0].on, ticks.pop.checks[2].on = true, true // Blair left out
		ticks.pop.refresh(ticks.pop)
		ticks.pop.pick = 1
		write("followups-ticks", ticks)
		resent, _ := fu.openPopup(fu.resultsPopup(followupDoneMsg{kind: followup.Unverified, results: []followup.Result{
			{Person: lists[0].Members[0].Person, OK: true, What: "Cloudflare is sending them a new verification email."},
			{Person: lists[0].Members[1].Person, OK: true, What: "Cloudflare is sending them a new verification email."},
		}}))
		write("followups-resent", resent)
		results, _ := fu.openPopup(fu.resultsPopup(followupDoneMsg{kind: followup.NeedsSetup, results: []followup.Result{
			{Person: lists[1].Members[0].Person, OK: true, What: "Added them as a Jira customer; created a personal forum invite (expires Feb 11, 2027)."},
			{Person: lists[1].Members[2].Person, What: "Failed: Discourse forum: the forum refused to make the change."},
		}}))
		write("followups-results", results)
		queued, _ := fu.startQueue(followup.NeedsSetup, []connectors.Person{lists[1].Members[0].Person, lists[1].Members[2].Person})
		write("followups-queue", queued)

		flashed := wide
		flashed.flash = "✓ Onboarded Alex Example: 1 change made. Next: Email them → Welcome email."
		write("members-flash", flashed)

		em := membersPreview(a)
		em, _ = em.openPopup(em.emailsPopup(memberView{person: connectors.Person{Email: "jordan.sample@example.com", FirstName: "Jordan", LastName: "Sample"},
			sheet:   connectors.MemberStatus{State: connectors.StateOK},
			systems: []check.SystemResult{{ID: "cloudflare", MemberStatus: connectors.MemberStatus{State: connectors.StatePending}}, {ID: "discourse", MemberStatus: connectors.MemberStatus{State: connectors.StateOK}}}}))
		write("email", em)

		people := a
		people.screen = screenCheck
		people.member = newMemberCheck(people.styles)
		people.member.input.Focus()
		vetted := connectors.MemberStatus{State: connectors.StateOK, Summary: "On the membership list, and their application is vetted."}
		before := connectors.MemberStatus{State: connectors.StateOK, Summary: "On the membership list. They joined before \"App vetted\" was tracked."}
		notVetted := connectors.MemberStatus{State: connectors.StatePending, Summary: "On the list, but \"App vetted\" isn't marked yet."}
		person := func(first, last string) connectors.Person {
			return connectors.Person{FirstName: first, LastName: last, Email: strings.ToLower(first+"."+last) + "@example.com"}
		}
		people.people = struct {
			loading bool
			err     string
			entries []connectors.DirectoryEntry
			at      time.Time
		}{entries: []connectors.DirectoryEntry{
			{Person: person("Alex", "Example"), Status: vetted},
			{Person: person("Blair", "Sample"), Status: before},
			{Person: person("Casey", "Test"), Status: notVetted},
			{Person: person("Drew", "Demo"), Status: vetted},
		}, at: time.Now().Add(-2 * time.Minute)}
		write("check-list", people)
		m, _ = people.Update(tea.WindowSizeMsg{Width: 130, Height: 28})
		write("check-list-wide", m.(App))

		lg := a
		lg.screen = screenLog
		lg.log = newLogView(lg.styles)
		lg.log.path = "~/.config/aps-tools/activity.log"
		day := time.Date(2026, 11, 13, 15, 4, 0, 0, time.Local)
		lg.log.entries = []activity.Entry{
			{Time: day.Add(9 * time.Minute), Member: "drew.demo@example.com", System: "Verify your email", OK: true, What: "Sent by the webmaster (confirmed in the tool)"},
			{Time: day.Add(8 * time.Minute), Member: "drew.demo@example.com", System: "Mailing list (Cloudflare)", OK: true, What: "Cloudflare is sending them a new verification email (their unverified address was removed and added again)."},
			{Time: day.Add(2 * time.Minute), Member: "alex.example@example.com", System: "Discourse forum", OK: true, What: "Created a personal invite link (no email from the forum); it expires Feb 11, 2027."},
			{Time: day.Add(-26 * time.Hour), Member: "casey.test@example.com", System: "Mailing list (Cloudflare)", OK: false, What: "Re-send verification email: their address is already verified, so there's nothing to re-send"},
		}
		write("log", lg)
		m, _ = lg.Update(tea.WindowSizeMsg{Width: 130, Height: 28})
		write("log-wide", m.(App))

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
			{System: "Mailing list (Cloudflare)", ID: "cloudflare", MemberStatus: mail},
			{System: "Jira Service Management", ID: "jira", MemberStatus: jira},
			{System: "Discourse forum", ID: "discourse", MemberStatus: forum},
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
		SystemIDs: []string{"cloudflare", "jira", "discourse"},
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
