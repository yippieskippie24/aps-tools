package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
	"github.com/yippieskippie24/aps-tools/internal/progress"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// This file is everything you can do with a member, the same wherever
// they're listed (Members, Check a member): the details pane on the right
// of the list, and the popup that Enter opens. Every action that changes a
// system confirms first in the popup, runs as a task (with its steps on
// the bottom line), records itself in the activity log, and then checks
// the member again so the details are current. The work itself is done by
// the shared packages (onboard, welcome, the connectors).

// memberView is what the app knows about one member.
type memberView struct {
	person connectors.Person
	// sheet is the membership sheet's verdict (the onboarding gate).
	sheet connectors.MemberStatus
	// systems are the other systems' results; nil means not checked yet.
	systems []check.SystemResult
}

// states is each system's state by machine name, for suggestions.
func (mv memberView) states() map[string]connectors.State {
	out := map[string]connectors.State{}
	for _, s := range mv.systems {
		out[s.ID] = s.State
	}
	return out
}

// checked reports whether the other systems have been checked.
func (mv memberView) checked() bool { return mv.systems != nil }

// who is "Name <email>", or just the email.
func (mv memberView) who() string {
	if n := mv.person.Name(); n != "" {
		return n + " <" + mv.person.Email + ">"
	}
	return mv.person.Email
}

// name is their name, or their email if the sheet has no name.
func (mv memberView) name() string {
	if n := mv.person.Name(); n != "" {
		return n
	}
	return mv.person.Email
}

// memberActions is the list of everything that can be done for mv, the
// suggested action first among those available.
func (a App) memberActions(mv memberView) []popItem {
	var items []popItem
	email := mv.person.Email

	// 1. Onboarding (or overriding the sheet first).
	switch {
	case mv.sheet.State == connectors.StatePending || mv.sheet.State == connectors.StateMissing:
		items = append(items, popItem{label: "Override the sheet and onboard…",
			run: func(a App) (App, tea.Cmd) { return a.openPopup(a.reasonPopup(mv)) }})
	case !mv.checked():
		items = append(items, popItem{label: "Onboard (checks every system first)",
			run: func(a App) (App, tea.Cmd) { return a.planFor(mv, "") }})
	default:
		var missing []string
		for _, s := range mv.systems {
			if s.State == connectors.StateMissing || s.State == connectors.StateProblem {
				missing = append(missing, s.Short())
			}
		}
		it := popItem{label: "Onboard", run: func(a App) (App, tea.Cmd) { return a.planFor(mv, "") }}
		if len(missing) == 0 {
			it.off = "set up everywhere"
		} else {
			it.label = "Onboard: set up " + connectors.JoinPlain(missing)
		}
		items = append(items, it)
	}

	// 2. Emails.
	items = append(items, popItem{label: "Email them…",
		run: func(a App) (App, tea.Cmd) { return a.openPopup(a.emailsPopup(mv)) }})

	// 3. Cloudflare re-send.
	resend := popItem{label: "Re-send Cloudflare's verification email",
		run: func(a App) (App, tea.Cmd) { return a.openPopup(a.resendPopup(mv)) }}
	if mv.checked() {
		switch mv.states()["cloudflare"] {
		case connectors.StatePending:
		case connectors.StateOK:
			resend.off = "already verified"
		default:
			resend.off = "not added yet"
		}
	}
	items = append(items, resend)

	// 4. Check again, 5. their activity.
	checkLabel := "Check every system again"
	if !mv.checked() {
		checkLabel = "Check every system"
	}
	items = append(items,
		popItem{label: checkLabel, run: func(a App) (App, tea.Cmd) { return a.recheck(mv.person) }},
		popItem{label: "Their activity log", run: func(a App) (App, tea.Cmd) {
			a = a.openLog()
			a.log.filter.SetValue(email)
			return a, nil
		}},
	)

	// The suggestion: onboarding when it's available (or not yet known),
	// otherwise their emails (the popup suggests which).
	if items[0].off == "" {
		items[0].suggested = true
	} else {
		items[1].suggested = true
	}
	return items
}

// suggestedLabel is the suggested action, for the details pane.
func (a App) suggestedLabel(mv memberView) string {
	for _, it := range a.memberActions(mv) {
		if it.suggested {
			if it.label == "Email them…" {
				return "Email them: " + welcome.Suggest(mv.sheet.State, mv.states()).Name
			}
			return it.label
		}
	}
	return "Nothing to do"
}

// actionsPopup is the popup Enter opens on a member.
func (a App) actionsPopup(mv memberView) *popup {
	return &popup{title: mv.name(), sub: mv.person.Email + rowNote(mv.person), lines: []string{a.statusLine(mv)}, items: a.memberActions(mv)}
}

func rowNote(p connectors.Person) string {
	if p.Row > 0 {
		return fmt.Sprintf(" · sheet row %d", p.Row)
	}
	return ""
}

// statusLine is "✓ Sheet  … Mail  ✗ Jira  ✗ Forum".
func (a App) statusLine(mv memberView) string {
	parts := []string{a.stateColor(mv.sheet.State).Render(check.Icon(mv.sheet.State) + " Sheet")}
	for _, s := range mv.systems {
		parts = append(parts, a.stateColor(s.State).Render(check.Icon(s.State)+" "+shortSystemName(s.System)))
	}
	if !mv.checked() {
		parts = append(parts, a.styles.subtle.Render("other systems not checked yet"))
	}
	return strings.Join(parts, "  ")
}

// viewMemberDetails is the details pane: who they are, where they stand in
// each system, and the suggested next step.
func (a App) viewMemberDetails(mv memberView, width int) string {
	s := a.styles
	var rows []string
	rows = append(rows, s.text.Bold(true).Render(fitWidth(mv.name(), width)))
	rows = append(rows, s.subtle.Render(fitWidth(mv.person.Email+rowNote(mv.person), width)), "")
	line := func(st connectors.MemberStatus, label string) {
		head := a.stateColor(st.State).Render(check.Icon(st.State)) + " " + s.text.Bold(true).Render(label)
		rows = append(rows, head)
		if st.Summary != "" {
			rows = append(rows, s.subtle.PaddingLeft(2).Width(width).Render(st.Summary))
		}
		if st.Warning != "" {
			rows = append(rows, lipgloss.NewStyle().Foreground(s.p.warn).PaddingLeft(2).Width(width).Render("! "+st.Warning))
		}
	}
	line(mv.sheet, "Membership sheet")
	if mv.checked() {
		for _, sys := range mv.systems {
			line(sys.MemberStatus, sys.System)
		}
	} else {
		rows = append(rows, s.subtle.Width(width).Render("Other systems not checked yet: Enter → Check every system."))
	}
	rows = append(rows, "", s.subtle.Render("Suggested next step"),
		lipgloss.NewStyle().Foreground(s.p.accent).Bold(true).Width(width).Render(a.suggestedLabel(mv)),
		"", s.subtle.Render("Enter: everything you can do"))
	return strings.Join(rows, "\n")
}

// ---- Onboarding from the popup ----

// popPlanMsg carries an onboarding plan made from the popup.
type popPlanMsg struct {
	plan onboard.Plan
	err  error
}

// popRunMsg carries the plan after its changes were made.
type popRunMsg struct {
	plan onboard.Plan
	err  error
}

// planFor checks the member everywhere and plans their onboarding (with
// an override reason, if given), then shows the plan in a popup. Planning
// changes nothing; an override is recorded once planned.
func (a App) planFor(mv memberView, reason string) (App, tea.Cmd) {
	ctx, watch, ok := a.begin("Planning onboarding for " + mv.name())
	if !ok {
		return a, nil
	}
	log, logErr := a.activityLog()
	email := mv.person.Email
	return a, tea.Batch(watch, func() tea.Msg {
		_, conns, err := a.connect(ctx)
		if err != nil {
			return popPlanMsg{err: err}
		}
		if reason == "" {
			p, err := onboard.Prepare(ctx, conns, email)
			return popPlanMsg{plan: p, err: err}
		}
		if logErr != nil {
			return popPlanMsg{err: logErr} // an override must be recorded
		}
		p, err := onboard.PrepareOverride(ctx, conns, email, reason)
		if err == nil && p.Override != "" {
			progress.Report(ctx, "Recording the override in the activity log")
			err = p.LogOverride(log)
		}
		return popPlanMsg{plan: p, err: err}
	})
}

// planPopup shows a plan and asks before making its changes.
func (a App) planPopup(p onboard.Plan) *popup {
	s := a.styles
	pop := &popup{title: "Onboard " + p.Who(), sub: p.Summary()}
	if !p.Ready {
		pop.lines = append(pop.lines, lipgloss.NewStyle().Foreground(s.p.fail).Render(p.Gate.Summary))
		if p.Gate.Fix != "" {
			pop.lines = append(pop.lines, s.subtle.Render("→ "+p.Gate.Fix))
		}
		if p.CanOverride() {
			mv := memberView{person: p.Person, sheet: p.Gate}
			pop.items = append(pop.items, popItem{label: "Override the sheet…", suggested: true,
				run: func(a App) (App, tea.Cmd) { return a.openPopup(a.reasonPopup(mv)) }})
		}
		pop.items = append(pop.items, popItem{label: "Close", run: func(a App) (App, tea.Cmd) { return a, nil }})
		return pop
	}
	for _, st := range p.Steps {
		text := st.Action
		if text == "" {
			text = st.Now
		}
		icon := a.kindColor(st.Kind).Render(st.Kind.Icon())
		pop.lines = append(pop.lines, icon+" "+s.text.Bold(true).Render(st.System)+s.subtle.Render(": "+text))
		if st.Warning != "" {
			pop.lines = append(pop.lines, lipgloss.NewStyle().Foreground(s.p.warn).Render("  ! "+st.Warning))
		}
	}
	if n := p.Count(onboard.Auto); n > 0 {
		pop.items = append(pop.items, popItem{label: fmt.Sprintf("Make %s now", changeCount(n)), suggested: true,
			run: func(a App) (App, tea.Cmd) { return a.runPlan(p) }})
	}
	mv := memberView{person: p.Person, sheet: p.Gate}
	pop.items = append(pop.items,
		popItem{label: "Email them…", run: func(a App) (App, tea.Cmd) { return a.openPopup(a.emailsPopup(mv)) }},
		popItem{label: "Close", run: func(a App) (App, tea.Cmd) { return a, nil }},
	)
	pop.footerHint = "+ the tool does it · □ by hand · ✓ done · … waiting on them    enter choose · esc back"
	return pop
}

// runPlan makes the plan's changes (the webmaster has just said yes).
func (a App) runPlan(p onboard.Plan) (App, tea.Cmd) {
	ctx, watch, ok := a.begin("Onboarding " + p.Who())
	if !ok {
		return a, nil
	}
	p.Steps = slices.Clone(p.Steps)
	log, logErr := a.activityLog()
	return a, tea.Batch(watch, func() tea.Msg {
		if logErr != nil {
			return popRunMsg{plan: p, err: logErr} // never change what can't be recorded
		}
		err := p.Run(ctx, log, nil)
		return popRunMsg{plan: p, err: err}
	})
}

// ---- Emails from the popup ----

// popEmailSentMsg reports that "sent" was recorded (or why not).
type popEmailSentMsg struct {
	kind welcome.Kind
	who  string
	err  error
}

// emailsPopup lists every email, the one that fits them best first.
func (a App) emailsPopup(mv memberView) *popup {
	suggested := welcome.Suggest(mv.sheet.State, mv.states())
	kinds := []welcome.Kind{suggested}
	for _, k := range welcome.Kinds {
		if k.ID != suggested.ID {
			kinds = append(kinds, k)
		}
	}
	pop := &popup{title: "Email " + mv.name(), sub: "Opens in your browser, ready to copy into Mail"}
	for i, k := range kinds {
		k := k
		pop.items = append(pop.items, popItem{label: k.Name, suggested: i == 0, run: func(a App) (App, tea.Cmd) {
			if k.ID == welcome.VerifyEmail.ID && mv.states()["cloudflare"] == connectors.StatePending {
				return a.openPopup(a.verifyResendPopup(mv))
			}
			return a.openEmailFor(mv, k, false)
		}})
	}
	return pop
}

// verifyResendPopup asks whether to re-send Cloudflare's email along with
// the "Verify your email" reminder.
func (a App) verifyResendPopup(mv memberView) *popup {
	return &popup{
		title: "Also have Cloudflare send them a new verification email?",
		lines: []string{a.styles.subtle.Render("Cloudflare can't re-send by itself, so the tool removes their unverified address and adds it again. Their mail isn't affected. It's recorded in the activity log.")},
		items: []popItem{
			{label: "Yes, re-send it and open the email", suggested: true, run: func(a App) (App, tea.Cmd) { return a.openEmailFor(mv, welcome.VerifyEmail, true) }},
			{label: "No, just open the email", run: func(a App) (App, tea.Cmd) { return a.openEmailFor(mv, welcome.VerifyEmail, false) }},
		},
	}
}

// openEmailFor opens a member's email in the browser, then asks whether
// it was sent.
func (a App) openEmailFor(mv memberView, k welcome.Kind, resend bool) (App, tea.Cmd) {
	ctx, watch, ok := a.begin("Opening the " + strings.ToLower(k.Name) + " for " + mv.name())
	if !ok {
		return a, nil
	}
	log, logErr := a.activityLog()
	return a, tea.Batch(watch, func() tea.Msg {
		m := a.emailWork(ctx, k, mv.person, resend, log, logErr)
		m.viaPopup = true
		return m
	})
}

// sentPopup asks "Did you send it?" after an email opened.
func (a App) sentPopup(m emailOpenedMsg) *popup {
	who := m.person.Name()
	if who == "" {
		who = m.person.Email
	}
	pop := &popup{title: "The " + strings.ToLower(m.kind.Name) + " for " + who + " is open in your browser.",
		sub: "Copy it into Mail and send it. Did you send it?"}
	if m.resent != "" {
		pop.lines = append(pop.lines, m.resent)
	}
	for _, n := range m.notes {
		pop.lines = append(pop.lines, lipgloss.NewStyle().Foreground(a.styles.p.warn).Render("! "+n))
	}
	if len(m.missing) > 0 {
		pop.lines = append(pop.lines, lipgloss.NewStyle().Foreground(a.styles.p.warn).Render("! Fill in after pasting: {{"+strings.Join(m.missing, "}}, {{")+"}}"))
	}
	email, kind := m.person.Email, m.kind
	if a.queue != nil { // emailing a list, one by one
		pop.title = fmt.Sprintf("%d of %d: ", a.queue.i+1, len(a.queue.people)) + pop.title
		pop.onEsc = stopQueue
		pop.footerHint = "↑/↓ choose · enter do it · esc stop"
		pop.items = []popItem{
			{label: "Yes, I sent it: next", suggested: true, run: func(a App) (App, tea.Cmd) { return a.recordSent(kind, email, who) }},
			{label: "Not sent: skip to the next", run: func(a App) (App, tea.Cmd) { return a.queueAdvance(false) }},
			{label: "Stop here", run: stopQueue},
		}
		return pop
	}
	pop.items = []popItem{
		{label: "Yes, I sent it", suggested: true, run: func(a App) (App, tea.Cmd) { return a.recordSent(kind, email, who) }},
		{label: "Not yet", run: func(a App) (App, tea.Cmd) {
			a.flash = "Not recorded as sent. Open it again from their actions when you're ready."
			return a, nil
		}},
	}
	return pop
}

// recordSent records the webmaster's "yes, I sent it".
func (a App) recordSent(k welcome.Kind, email, who string) (App, tea.Cmd) {
	_, watch, ok := a.begin("Recording the " + strings.ToLower(k.Name) + " in the activity log")
	if !ok {
		return a, nil
	}
	log, logErr := a.activityLog()
	return a, tea.Batch(watch, func() tea.Msg {
		if logErr != nil {
			return popEmailSentMsg{kind: k, who: who, err: logErr}
		}
		return popEmailSentMsg{kind: k, who: who, err: onboard.LogEmailSent(log, email, k.Name)}
	})
}

// ---- Cloudflare re-send from the popup ----

// popResendMsg reports how a re-send went.
type popResendMsg struct {
	email  string
	result string
}

// resendPopup confirms a re-send of Cloudflare's verification email.
func (a App) resendPopup(mv memberView) *popup {
	return &popup{
		title: "Have Cloudflare send " + mv.name() + " a new verification email?",
		lines: []string{a.styles.subtle.Render("Cloudflare can't re-send by itself, so the tool removes their unverified address and adds it again (after checking it's safe). Their mail isn't affected. It's recorded in the activity log.")},
		items: []popItem{
			{label: "Yes, re-send it", suggested: true, run: func(a App) (App, tea.Cmd) { return a.resendFor(mv) }},
			{label: "Cancel", run: func(a App) (App, tea.Cmd) { return a, nil }},
		},
	}
}

// resendFor has Cloudflare send a new verification email.
func (a App) resendFor(mv memberView) (App, tea.Cmd) {
	ctx, watch, ok := a.begin("Re-sending Cloudflare's verification email to " + mv.name())
	if !ok {
		return a, nil
	}
	log, logErr := a.activityLog()
	email := mv.person.Email
	return a, tea.Batch(watch, func() tea.Msg {
		_, conns, err := a.connect(ctx)
		if err != nil {
			return popResendMsg{email: email, result: "✗ " + err.Error()}
		}
		progress.Report(ctx, "Checking it's safe, then removing and re-adding their address")
		return popResendMsg{email: email, result: resendVerification(ctx, conns, email, log, logErr)}
	})
}

// ---- Override ----

// reasonPopup asks why the sheet should be overridden.
func (a App) reasonPopup(mv memberView) *popup {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "e.g. Paid at the meeting; the chair will update the sheet"
	in.CharLimit = 300
	st := textinput.DefaultStyles(true)
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(a.styles.p.accent).Bold(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(a.styles.p.text)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(a.styles.p.subtle)
	st.Blurred = st.Focused
	in.SetStyles(st)
	return &popup{
		title: "Override the membership sheet for " + mv.name() + "?",
		lines: []string{
			lipgloss.NewStyle().Foreground(a.styles.p.warn).Render("The sheet says: " + mv.sheet.Summary),
			a.styles.subtle.Render("Only if you know they should be set up anyway. Your reason is recorded in the activity log, then their plan opens."),
		},
		input: &in,
		onInput: func(a App, value string) (App, tea.Cmd) {
			if err := onboard.CheckReason(value); err != nil {
				a.pop.inputErr = "Please give a reason of at least a few words: it's the record of this override."
				return a, nil
			}
			a.pop = nil
			return a.planFor(mv, value)
		},
		footerHint: "enter override and record it · esc back",
	}
}

// ---- Checking them again ----

// popCheckedMsg carries a fresh check of one member in every system.
type popCheckedMsg struct {
	report check.Report
	err    error
	quiet  bool // a follow-up check after an action: don't announce it
}

// recheck checks a member in every system (read-only) and updates what
// the screens show about them.
func (a App) recheck(p connectors.Person) (App, tea.Cmd) {
	return a.recheckAs(p, false)
}

func (a App) recheckAs(p connectors.Person, quiet bool) (App, tea.Cmd) {
	who := p.Name()
	if who == "" {
		who = p.Email
	}
	ctx, watch, ok := a.begin("Checking " + who + " in every system")
	if !ok {
		return a, nil
	}
	return a, tea.Batch(watch, func() tea.Msg {
		_, conns, err := a.connect(ctx)
		if err != nil {
			return popCheckedMsg{err: err, quiet: quiet}
		}
		rep, err := check.Run(ctx, conns, p.Email, nil)
		return popCheckedMsg{report: rep, err: err, quiet: quiet}
	})
}

// applyCheck stores a fresh check where the screens look for it.
func (a App) applyCheck(rep check.Report) App {
	if a.checked == nil {
		a.checked = map[string]check.Report{}
	}
	a.checked[rep.Email] = rep
	if a.members.roster != nil {
		r := *a.members.roster
		r.Entries = slices.Clone(r.Entries)
		for i := range r.Entries {
			r.Entries[i].Systems = slices.Clone(r.Entries[i].Systems)
		}
		r.Apply(rep)
		a.members.roster = &r
		a.members.clamp()
	}
	for i, e := range a.people.entries {
		if strings.EqualFold(e.Person.Email, rep.Email) {
			if sheet, ok := sheetResult(rep); ok {
				a.people.entries[i].Status = sheet
			}
		}
	}
	return a
}

// sheetResult finds the membership sheet's part of a check.
func sheetResult(rep check.Report) (connectors.MemberStatus, bool) {
	for _, r := range rep.Results {
		if r.ID == "sheet" {
			return r.MemberStatus, true
		}
	}
	return connectors.MemberStatus{}, false
}

// otherResults is a check without the membership sheet's part.
func otherResults(rep check.Report) []check.SystemResult {
	out := []check.SystemResult{}
	for _, r := range rep.Results {
		if r.ID != "sheet" {
			out = append(out, r)
		}
	}
	return out
}

// memberMsg handles the results of the popup's actions.
func (a App) memberMsg(msg tea.Msg) (App, tea.Cmd) {
	switch msg := msg.(type) {
	case popPlanMsg:
		if msg.err != nil {
			a.flash = "Couldn't plan their onboarding: " + msg.err.Error()
			return a, nil
		}
		a.pop = nil
		if msg.plan.Override != "" {
			a.flash = "✓ Override recorded in the activity log. Ask the membership chair to update the sheet."
		}
		return a.openPopup(a.planPopup(msg.plan))
	case popRunMsg:
		made, failed := 0, 0
		for _, s := range msg.plan.Steps {
			switch s.Outcome {
			case onboard.Succeeded:
				made++
			case onboard.Failed:
				failed++
			}
		}
		switch {
		case msg.err != nil:
			a.flash = "Activity log problem: " + msg.err.Error()
		case failed > 0:
			var why []string
			for _, s := range msg.plan.Steps {
				if s.Outcome == onboard.Failed {
					why = append(why, s.System+": "+s.Result)
				}
			}
			a.flash = fmt.Sprintf("%s made, %d failed: %s", changeCount(made), failed, strings.Join(why, " · "))
		default:
			a.flash = fmt.Sprintf("✓ Onboarded %s: %s made. Next: Email them → Welcome email.", msg.plan.Who(), changeCount(made))
		}
		return a.recheckAs(msg.plan.Person, true)
	case emailOpenedMsg:
		if msg.err != nil && a.queue != nil {
			a.queue.err = "Couldn't open it: " + msg.err.Error()
			return a.queueNext()
		}
		if msg.err != nil {
			a.flash = strings.TrimSpace(msg.resent + " Couldn't open the " + strings.ToLower(msg.kind.Name) + ": " + msg.err.Error())
			return a, nil
		}
		return a.openPopup(a.sentPopup(msg))
	case popEmailSentMsg:
		if a.queue != nil {
			if msg.err != nil {
				a.queue.err = "Couldn't record it as sent: " + msg.err.Error()
				return a.queueNext()
			}
			return a.queueAdvance(true)
		}
		if msg.err != nil {
			a.flash = "Couldn't record it: " + msg.err.Error()
		} else {
			a.flash = "✓ " + msg.kind.Name + " to " + msg.who + " recorded as sent in the activity log."
		}
	case popResendMsg:
		a.flash = msg.result
		return a.recheckAs(connectors.Person{Email: msg.email}, true)
	case popCheckedMsg:
		if msg.err != nil {
			a.flash = "Couldn't check them: " + msg.err.Error()
			return a, nil
		}
		a = a.applyCheck(msg.report)
		if !msg.quiet {
			a.flash = "✓ Checked " + msg.report.Email + " in every system at " + time.Now().Format("3:04 PM") + "."
		}
	}
	return a, nil
}
