package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/followup"
	"github.com/yippieskippie24/aps-tools/internal/progress"
	"github.com/yippieskippie24/aps-tools/internal/roster"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// This file is the "Follow-ups" screen: members who are only part-way set
// up (haven't verified their email, need setting up, haven't used their
// forum invite), so the webmaster can follow up on many at once. Which
// members are in each list, and the bulk work itself, live in
// internal/followup; this file only draws it and asks.
//
// The flow for each follow-up:
//  1. Enter opens a popup listing its members as tick boxes, none ticked:
//     the webmaster deliberately ticks who's included (space), then
//     presses enter to move on to the actions. Nothing has changed yet.
//  2. Choose the action (e.g. "Re-send … to 9 members"). It runs one
//     member at a time, showing whose turn it is, then everyone is checked
//     again.
//  3. A popup shows what happened for each member, and offers to email
//     them one by one: each email opens in the browser, then "Did you send
//     it?", then the next.
//
// It uses the same snapshot of everyone as the Members screen.

// followupsView holds the Follow-ups screen's state.
type followupsView struct {
	cursor int // the highlighted follow-up
}

// openFollowups shows the screen, checking everyone first if the app
// hasn't yet.
func (a App) openFollowups() (App, tea.Cmd) {
	a.screen = screenFollowups
	if a.members.roster == nil && !a.members.loading {
		return a.loadRoster()
	}
	return a, nil
}

// followupLists is every follow-up, from the current snapshot.
func (a App) followupLists() []followup.List {
	if a.members.roster == nil {
		return nil
	}
	return followup.Lists(*a.members.roster)
}

func (a App) updateFollowups(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	lists := a.followupLists()
	fv := &a.followups
	switch msg.String() {
	case "esc", "q":
		a.screen = screenHome
	case "up", "k":
		if len(lists) > 0 {
			fv.cursor = (fv.cursor - 1 + len(lists)) % len(lists)
		}
	case "down", "j":
		if len(lists) > 0 {
			fv.cursor = (fv.cursor + 1) % len(lists)
		}
	case "r":
		if !a.members.loading {
			return a.loadRoster()
		}
	case "enter":
		if fv.cursor < len(lists) {
			l := lists[fv.cursor]
			if len(l.Members) == 0 {
				a.flash = "✓ Nobody needs this follow-up right now."
				return a, nil
			}
			return a.openPopup(a.followupPopup(l))
		}
	}
	return a, nil
}

func (a App) followupKeys() string {
	if a.members.loading || a.members.roster == nil {
		return a.keyHelp("r", "check again", "esc", "back")
	}
	return a.keyHelp("↑/↓", "follow-up", "enter", "choose who and follow up", "r", "check everyone again", "esc", "back")
}

func (a App) viewFollowups(w int) string {
	s := a.styles
	inner := innerWidth(w)
	var b strings.Builder
	b.WriteString(s.heading.Render("Follow-ups") + "\n")
	switch {
	case a.members.loading:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		b.WriteString(spinner + " " + s.subtle.Render("Checking everyone on the sheet in every system… (read-only)"))
		return b.String()
	case a.members.err != "":
		b.WriteString(lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(a.members.err))
		b.WriteString("\n\n" + s.subtle.Render("Press r to try again."))
		return b.String()
	case a.members.roster == nil:
		return b.String()
	}
	r := *a.members.roster
	lists := a.followupLists()
	b.WriteString(s.subtle.Render(fitWidth(fmt.Sprintf("From the check of everyone %s · r to check again", ago(r.CheckedAt)), inner)) + "\n")
	b.WriteString(s.subtle.Render(fitWidth("Members who are only part-way set up. Choose one to pick who's included and follow up on them all.", inner)) + "\n\n")

	// The follow-ups on the left, the highlighted one's members on the right.
	leftW := min(max(inner*2/5, 34), 44)
	rightW := max(inner-leftW-3, 20)
	h := max(a.cardInnerHeight()-5, 4)
	var left []string
	for i, l := range lists {
		sel := i == a.followups.cursor
		bg := lipgloss.NewStyle()
		marker := "  "
		if sel {
			bg, marker = bg.Background(s.p.selected), "▸ "
		}
		count := fmt.Sprint(len(l.Members))
		countStyle := lipgloss.NewStyle().Foreground(s.p.warn).Bold(true)
		if len(l.Members) == 0 {
			countStyle, count = lipgloss.NewStyle().Foreground(s.p.ok), "✓"
		}
		nameW := leftW - 2 - 4
		left = append(left, bg.Foreground(s.p.accent).Bold(true).Render(marker)+
			s.text.Inherit(bg).Width(nameW).Render(fitWidth(l.Kind.Title(), nameW-1))+
			countStyle.Inherit(bg).Width(4).Align(lipgloss.Right).Render(count))
	}
	right := ""
	if a.followups.cursor < len(lists) {
		right = a.viewFollowupDetail(lists[a.followups.cursor], rightW, h)
	}
	b.WriteString(a.splitPanes(strings.Join(left, "\n"), right, leftW, rightW, h))
	return strings.TrimRight(b.String(), "\n")
}

// viewFollowupDetail explains a follow-up and lists its members.
func (a App) viewFollowupDetail(l followup.List, width, h int) string {
	s := a.styles
	lines := []string{s.text.Bold(true).Render(l.Kind.Title())}
	lines = append(lines, strings.Split(s.subtle.Width(width).Render(l.Kind.Explain()), "\n")...)
	lines = append(lines, "")
	if len(l.Members) == 0 {
		return strings.Join(append(lines, lipgloss.NewStyle().Foreground(s.p.ok).Render("✓ Nobody right now.")), "\n")
	}
	room := h - len(lines) - 2
	for i, e := range l.Members {
		if i == room && len(l.Members) > room+1 {
			lines = append(lines, s.subtle.Render(fmt.Sprintf("…and %d more", len(l.Members)-i)))
			break
		}
		name := e.Name()
		if name == "" {
			name = e.Email
		}
		line := s.text.Render(name)
		if d := l.Kind.Detail(e); d != "" {
			line += s.subtle.Render(" · " + d)
		}
		lines = append(lines, fitWidth(line, width))
	}
	lines = append(lines, "", s.subtle.Render("Enter: choose who and follow up"))
	return strings.Join(lines, "\n")
}

// ---- The tick-box popup ----

// followupPopup lists a follow-up's members as tick boxes (none ticked, so
// every member included is a deliberate choice), then the actions for
// those ticked.
func (a App) followupPopup(l followup.List) *popup {
	k := l.Kind
	byEmail := map[string]connectors.Person{}
	pop := &popup{
		title: k.Title(),
		sub:   "Tick who to include with space (a ticks everyone), then press enter. Nothing changes until you choose an action.",
		lines: []string{a.styles.subtle.Render(k.Explain())},
	}
	for _, e := range l.Members {
		name := e.Name()
		if name == "" {
			name = e.Email
		}
		byEmail[e.Email] = e.Person
		detail := k.Detail(e)
		if w := e.Warning(); w != "" { // e.g. a status that isn't Active
			detail = "! " + w + " · " + detail
		}
		pop.checks = append(pop.checks, checkRow{label: name, detail: detail, key: e.Email})
	}
	chosen := func(p *popup) []connectors.Person {
		var out []connectors.Person
		for _, email := range p.ticked() {
			out = append(out, byEmail[email])
		}
		return out
	}
	pop.refresh = func(p *popup) {
		n := len(p.ticked())
		none := ""
		if n == 0 {
			none = "tick someone first"
		}
		p.items = nil
		if k.Action() != "" {
			p.items = append(p.items, popItem{label: actionLabel(k, n), off: none, suggested: true,
				run: func(a App) (App, tea.Cmd) { return a.runFollowup(k, chosen(p)) }})
		}
		p.items = append(p.items, a.emailItems(k, n, none, k.Action() == "", func() []connectors.Person { return chosen(p) })...)
		p.items = append(p.items, popItem{label: "Close", run: func(a App) (App, tea.Cmd) { return a, nil }})
	}
	return pop
}

// actionLabel is a follow-up's action for n members, e.g. "Onboard 3
// members".
func actionLabel(k followup.Kind, n int) string {
	switch k {
	case followup.Unverified:
		return "Re-send Cloudflare's verification email to " + memberCount(n)
	case followup.NeedsSetup:
		return "Onboard " + memberCount(n)
	}
	return k.Action() + ": " + memberCount(n)
}

func memberCount(n int) string {
	if n == 1 {
		return "1 member"
	}
	return fmt.Sprintf("%d members", n)
}

// ---- Running it ----

// followupDoneMsg carries what happened, and everyone checked again.
type followupDoneMsg struct {
	kind      followup.Kind
	results   []followup.Result
	roster    roster.Roster
	rosterErr error
	err       error // nothing was done
}

// runFollowup does k's action for each person, one at a time, then checks
// everyone again so every screen is up to date.
func (a App) runFollowup(k followup.Kind, people []connectors.Person) (App, tea.Cmd) {
	label := actionLabel(k, len(people))
	ctx, watch, ok := a.begin(strings.Replace(label, "Re-send", "Re-sending", 1))
	if !ok {
		return a, nil
	}
	if k == followup.NeedsSetup {
		a.task.label = "Onboarding " + memberCount(len(people))
	}
	log, logErr := a.activityLog()
	return a, tea.Batch(watch, func() tea.Msg {
		if logErr != nil {
			return followupDoneMsg{kind: k, err: fmt.Errorf("the activity log can't be written, so nothing was changed: %w", logErr)}
		}
		_, conns, err := a.connect(ctx)
		if err != nil {
			return followupDoneMsg{kind: k, err: err}
		}
		results := followup.Run(ctx, k, conns, people, log)
		r, rerr := roster.Load(ctx, conns)
		return followupDoneMsg{kind: k, results: results, roster: r, rosterErr: rerr}
	})
}

// followupMsg handles the result: it updates the snapshot and shows what
// happened.
func (a App) followupMsg(msg followupDoneMsg) (App, tea.Cmd) {
	if msg.err != nil {
		a.flash = "✗ " + msg.err.Error()
		return a, nil
	}
	if msg.rosterErr == nil {
		r := msg.roster
		a.members.roster = &r
		a.members.clamp()
	}
	return a.openPopup(a.resultsPopup(msg))
}

// resultsPopup shows what happened for each member (problems first), and
// offers to email those it worked for.
func (a App) resultsPopup(msg followupDoneMsg) *popup {
	s := a.styles
	okN, failed := followup.Count(msg.results)
	verb := "Re-sent to"
	if msg.kind == followup.NeedsSetup {
		verb = "Onboarded"
	}
	pop := &popup{title: fmt.Sprintf("%s %d of %s", verb, okN, memberCount(len(msg.results))),
		sub: "Everything is in the activity log."}
	if failed > 0 {
		pop.sub = fmt.Sprintf("%d didn't work: see below. Everything is in the activity log.", failed)
	}
	var worked []connectors.Person
	var good, bad []string
	for _, r := range msg.results {
		who := r.Person.Name()
		if who == "" {
			who = r.Person.Email
		}
		if r.OK {
			worked = append(worked, r.Person)
			good = append(good, lipgloss.NewStyle().Foreground(s.p.ok).Render("✓ ")+s.text.Render(who)+s.subtle.Render(": "+r.What))
		} else {
			bad = append(bad, lipgloss.NewStyle().Foreground(s.p.fail).Render("✗ ")+s.text.Render(who)+s.subtle.Render(": "+r.What))
		}
	}
	all := append(bad, good...)
	room := max(a.cardHeight()-14, 4)
	if len(all) > room {
		all = append(all[:room-1], s.subtle.Render(fmt.Sprintf("…and %d more", len(all)-room+1)))
	}
	pop.lines = all
	if msg.rosterErr != nil {
		pop.lines = append(pop.lines, "", lipgloss.NewStyle().Foreground(s.p.warn).Render("! Couldn't check everyone again: "+msg.rosterErr.Error()))
	}
	k := msg.kind
	if len(worked) > 0 {
		pop.items = append(pop.items, a.emailItems(k, len(worked), "", true, func() []connectors.Person { return worked })...)
	}
	pop.items = append(pop.items, popItem{label: "Close", run: func(a App) (App, tea.Cmd) { return a, nil }})
	return pop
}

// emailItems are the ways to email n members: one email to them all (in
// Bcc) when the follow-up's email has nothing personal in it, and one by
// one. off, if set, makes them unavailable; suggest marks the first one.
func (a App) emailItems(k followup.Kind, n int, off string, suggest bool, people func() []connectors.Person) []popItem {
	var items []popItem
	if wk, ok := k.GroupEmail(); ok && n > 1 {
		items = append(items, popItem{label: "Email " + memberCount(n) + " together (one email, everyone in Bcc)", off: off, suggested: suggest,
			run: func(a App) (App, tea.Cmd) { return a.openGroupEmail(wk, people()) }})
		suggest = false
	}
	items = append(items, popItem{label: "Email " + memberCount(n) + " one by one", off: off, suggested: suggest,
		run: func(a App) (App, tea.Cmd) { return a.startQueue(k, people()) }})
	return items
}

// ---- One email to several members ----

// showGroupEmail builds one email of kind k for people (in Bcc) and opens
// its copy page. Tests replace it so they don't open a browser.
var showGroupEmail = func(k welcome.Kind, cfg *config.Config, people []connectors.Person) (welcome.Email, error) {
	e, _, err := k.ForGroup(cfg, people)
	if err != nil {
		return welcome.Email{}, err
	}
	_, err = welcome.Open(e, memberCount(len(people)))
	return e, err
}

// groupOpenedMsg reports that a group email opened (or why not).
type groupOpenedMsg struct {
	kind   welcome.Kind
	people []connectors.Person
	notes  []string
	err    error
}

// groupSentMsg reports that a group email was recorded as sent.
type groupSentMsg struct {
	kind welcome.Kind
	n    int
	err  error
}

// openGroupEmail opens one email of kind k to everyone in people.
func (a App) openGroupEmail(k welcome.Kind, people []connectors.Person) (App, tea.Cmd) {
	ctx, watch, ok := a.begin("Opening one " + strings.ToLower(k.Name) + " for " + memberCount(len(people)))
	if !ok {
		return a, nil
	}
	return a, tea.Batch(watch, func() tea.Msg {
		cfg, _, err := a.connect(ctx)
		if err != nil {
			return groupOpenedMsg{kind: k, people: people, err: err}
		}
		progress.Report(ctx, "Filling in the template and opening it in your browser")
		e, err := showGroupEmail(k, cfg, people)
		return groupOpenedMsg{kind: k, people: people, notes: e.Notes, err: err}
	})
}

// groupMsg handles a group email's results.
func (a App) groupMsg(msg tea.Msg) (App, tea.Cmd) {
	switch msg := msg.(type) {
	case groupOpenedMsg:
		if msg.err != nil {
			a.flash = "✗ Couldn't open the " + strings.ToLower(msg.kind.Name) + ": " + msg.err.Error()
			return a, nil
		}
		k, people := msg.kind, msg.people
		pop := &popup{
			title: "The " + strings.ToLower(k.Name) + " for " + memberCount(len(people)) + " is open in your browser.",
			sub:   "Copy it into Mail with everyone in Bcc, and send it. Did you send it?",
			items: []popItem{
				{label: "Yes, I sent it", suggested: true, run: func(a App) (App, tea.Cmd) { return a.recordGroupSent(k, people) }},
				{label: "Not yet", run: func(a App) (App, tea.Cmd) {
					a.flash = "Not recorded as sent."
					return a, nil
				}},
			},
		}
		for _, n := range msg.notes {
			pop.lines = append(pop.lines, a.styles.subtle.Render("ℹ "+n))
		}
		return a.openPopup(pop)
	case groupSentMsg:
		if msg.err != nil {
			a.flash = "✗ Couldn't record it: " + msg.err.Error()
		} else {
			a.flash = fmt.Sprintf("✓ %s to %s recorded as sent in the activity log.", msg.kind.Name, memberCount(msg.n))
		}
	}
	return a, nil
}

// recordGroupSent records a group email as sent to each member.
func (a App) recordGroupSent(k welcome.Kind, people []connectors.Person) (App, tea.Cmd) {
	_, watch, ok := a.begin("Recording the " + strings.ToLower(k.Name) + " in the activity log")
	if !ok {
		return a, nil
	}
	log, logErr := a.activityLog()
	return a, tea.Batch(watch, func() tea.Msg {
		if logErr != nil {
			return groupSentMsg{kind: k, n: len(people), err: logErr}
		}
		return groupSentMsg{kind: k, n: len(people), err: followup.LogGroupSent(log, k, people)}
	})
}

// ---- Emailing them one by one ----

// emailQueue steps through emailing several members, one at a time.
type emailQueue struct {
	kind          followup.Kind
	people        []connectors.Person
	i             int // whose turn it is
	sent, skipped int
	err           string // why the last email couldn't open
}

// startQueue starts emailing people one by one.
func (a App) startQueue(k followup.Kind, people []connectors.Person) (App, tea.Cmd) {
	a.queue = &emailQueue{kind: k, people: people}
	return a.queueNext()
}

// queueNext asks about the current member's email, or finishes.
func (a App) queueNext() (App, tea.Cmd) {
	q := a.queue
	if q == nil {
		return a, nil
	}
	if q.i >= len(q.people) {
		a.queue = nil
		a.flash = fmt.Sprintf("✓ Finished: %d sent, %d skipped. Sent emails are in the activity log.", q.sent, q.skipped)
		return a, nil
	}
	mv := a.memberViewFor(q.people[q.i])
	k := q.kind.Email(mv.sheet.State, mv.states())
	pop := &popup{
		title:      fmt.Sprintf("Email %d of %d: %s", q.i+1, len(q.people), mv.name()),
		sub:        mv.person.Email + " · " + k.Name,
		onEsc:      stopQueue,
		footerHint: "↑/↓ choose · enter do it · esc stop",
		items: []popItem{
			{label: "Open the " + strings.ToLower(k.Name), suggested: true,
				run: func(a App) (App, tea.Cmd) { return a.openEmailFor(mv, k, false) }},
			{label: "Skip them", run: func(a App) (App, tea.Cmd) {
				a.queue.i++
				a.queue.skipped++
				a.queue.err = ""
				return a.queueNext()
			}},
			{label: "Stop here", run: stopQueue},
		},
	}
	if q.err != "" {
		pop.lines = []string{lipgloss.NewStyle().Foreground(a.styles.p.fail).Render("✗ " + q.err)}
	}
	a.pop = nil
	return a.openPopup(pop)
}

// stopQueue stops emailing, saying how far it got.
func stopQueue(a App) (App, tea.Cmd) {
	q := a.queue
	a.queue, a.pop = nil, nil
	if q != nil {
		a.flash = fmt.Sprintf("Stopped: %d sent, %d skipped, %d not reached.", q.sent, q.skipped, len(q.people)-q.i)
	}
	return a, nil
}

// queueAdvance moves to the next member after the current one's email was
// sent (or not).
func (a App) queueAdvance(sent bool) (App, tea.Cmd) {
	q := a.queue
	q.i++
	q.err = ""
	if sent {
		q.sent++
	} else {
		q.skipped++
	}
	return a.queueNext()
}

// memberViewFor is what the snapshot knows about a person.
func (a App) memberViewFor(p connectors.Person) memberView {
	if a.members.roster != nil {
		for _, e := range a.members.roster.Entries {
			if strings.EqualFold(e.Email, p.Email) {
				return entryView(e)
			}
		}
	}
	return memberView{person: p}
}
