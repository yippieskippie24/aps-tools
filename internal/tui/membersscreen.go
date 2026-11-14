package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/roster"
)

// This file is the "Members" screen: everyone on the membership sheet,
// checked in every system and grouped by what they need. The checking and
// grouping happen in internal/roster; this file only draws it.
//
// The roster is kept while the app is open (a snapshot, with its age shown)
// so moving around is instant; r checks everyone again. Opening a member's
// onboarding always checks them live first, because that can change things.

// tabAll is the extra tab after the groups that shows everyone.
const tabAll = -1

// membersView holds the state of the Members screen.
type membersView struct {
	loading bool
	err     string
	roster  *roster.Roster
	tab     int // index into memberTabs
	cursor  int // highlighted row in the current tab
	top     int // first row shown (the list scrolls under the headings)
	notice  string
}

// memberTabs are the tabs, in order: each group, then everyone.
var memberTabs = append(append([]roster.Group{}, roster.Groups...), roster.Group(tabAll))

// rosterDoneMsg carries a freshly loaded roster (or why it failed).
type rosterDoneMsg struct {
	roster roster.Roster
	err    error
}

// memberRefreshedMsg carries the roster after one member was re-checked.
type memberRefreshedMsg struct {
	roster roster.Roster
	err    error
}

// openMembers shows the screen, loading the roster the first time.
func (a App) openMembers() (App, tea.Cmd) {
	a.screen = screenMembers
	if a.members.roster == nil && !a.members.loading {
		return a.loadRoster()
	}
	return a, nil
}

// loadRoster checks everyone in the background. Read-only.
func (a App) loadRoster() (App, tea.Cmd) {
	ctx, watch, ok := a.begin("Checking everyone on the sheet in every system")
	if !ok {
		return a, nil
	}
	a.members.loading, a.members.err, a.members.notice = true, "", ""
	run := func() tea.Msg {
		_, conns, err := a.connect(ctx)
		if err != nil {
			return rosterDoneMsg{err: err}
		}
		r, err := roster.Load(ctx, conns)
		return rosterDoneMsg{roster: r, err: err}
	}
	return a, tea.Batch(watch, run)
}

// refreshMember re-checks one member (after onboarding them) in the
// background. The roster is copied so the screen never reads it mid-change.
func (a App) refreshMember(email string) (App, tea.Cmd) {
	if a.members.roster == nil {
		return a, nil
	}
	ctx, watch, ok := a.begin("Re-checking " + email + " after onboarding")
	if !ok {
		return a, nil
	}
	r := *a.members.roster
	r.Entries = append([]roster.Entry(nil), r.Entries...)
	return a, tea.Batch(watch, func() tea.Msg {
		_, conns, err := a.connect(ctx)
		if err != nil {
			return memberRefreshedMsg{roster: r, err: err}
		}
		err = r.Refresh(ctx, conns, email)
		return memberRefreshedMsg{roster: r, err: err}
	})
}

// membersMsg handles this screen's background results.
func (a App) membersMsg(msg tea.Msg) App {
	mv := &a.members
	switch msg := msg.(type) {
	case rosterDoneMsg:
		mv.loading = false
		if msg.err != nil {
			mv.err = msg.err.Error()
			return a
		}
		mv.roster, mv.err = &msg.roster, ""
		mv.clamp()
	case memberRefreshedMsg:
		if msg.err != nil {
			a.flash = "Couldn't re-check them: " + msg.err.Error()
			return a
		}
		mv.roster = &msg.roster
		a.flash = "✓ Re-checked after onboarding."
		mv.clamp()
	}
	return a
}

// rows is the current tab's entries.
func (mv membersView) rows() []roster.Entry {
	if mv.roster == nil {
		return nil
	}
	if memberTabs[mv.tab] == tabAll {
		return mv.roster.Entries
	}
	return mv.roster.In(memberTabs[mv.tab])
}

// selected is the highlighted member, if any.
func (mv membersView) selected() (roster.Entry, bool) {
	rows := mv.rows()
	if mv.cursor < 0 || mv.cursor >= len(rows) {
		return roster.Entry{}, false
	}
	return rows[mv.cursor], true
}

// clamp keeps the cursor on a row after the list changes.
func (mv *membersView) clamp() {
	mv.cursor = min(max(mv.cursor, 0), max(len(mv.rows())-1, 0))
}

func (a App) updateMembers(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mv := &a.members
	key := msg.String()
	if key == "esc" || key == "q" {
		a.screen = screenHome
		return a, nil
	}
	if mv.loading {
		return a, nil
	}
	if key == "r" {
		return a.loadRoster()
	}
	if mv.roster == nil {
		return a, nil
	}
	n := len(mv.rows())
	page := max(a.memberListHeight()-1, 1)
	switch key {
	case "right", "l", "tab":
		mv.tab, mv.cursor, mv.top, mv.notice = (mv.tab+1)%len(memberTabs), 0, 0, ""
	case "left", "h", "shift+tab":
		mv.tab, mv.cursor, mv.top, mv.notice = (mv.tab-1+len(memberTabs))%len(memberTabs), 0, 0, ""
	case "up", "k":
		mv.cursor--
	case "down", "j":
		mv.cursor++
	case "pgup":
		mv.cursor -= page
	case "pgdown", "space":
		mv.cursor += page
	case "home":
		mv.cursor = 0
	case "end":
		mv.cursor = n - 1
	case "enter":
		if e, ok := mv.selected(); ok {
			return a.openPopup(a.actionsPopup(entryView(e)))
		}
	case "?":
		a.screen = screenHelp
	}
	mv.clamp()
	// Keep the highlighted row in view.
	h := a.memberListHeight()
	if mv.cursor < mv.top {
		mv.top = mv.cursor
	}
	if mv.cursor >= mv.top+h {
		mv.top = mv.cursor - h + 1
	}
	return a, nil
}

// entryView is what the details pane and popup need from a roster entry.
func entryView(e roster.Entry) memberView {
	systems := e.Systems
	if systems == nil {
		systems = []check.SystemResult{}
	}
	return memberView{person: e.Person, sheet: e.Sheet, systems: systems}
}

// membersKeys is the footer for the Members screen.
func (a App) membersKeys() string {
	mv := a.members
	if mv.loading || mv.roster == nil {
		return a.keyHelp("r", "check again", "esc", "back")
	}
	return a.keyHelp("↑/↓", "member", "enter", "what you can do", "←/→", "group", "r", "check everyone again", "esc", "back")
}

// membersHeader is how many lines come above the list (heading, age line,
// tabs, explanation, column headings), so the list knows how much room it
// has.
const membersHeader = 6

// memberListHeight is how many member rows fit in the card.
func (a App) memberListHeight() int {
	extra := 1 // the "rows x–y of n" line under the list
	if a.members.roster != nil {
		extra += len(a.members.roster.Problems)
	}
	return max(a.cardInnerHeight()-membersHeader-extra, 1)
}

func (a App) viewMembers(w int) string {
	s := a.styles
	mv := a.members
	inner := innerWidth(w)

	var b strings.Builder
	b.WriteString(s.heading.Render("Members"))
	b.WriteString("\n")

	switch {
	case mv.loading:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		b.WriteString(spinner + " " + s.subtle.Render("Checking everyone on the sheet in every system… (read-only)"))
		return b.String()
	case mv.err != "":
		b.WriteString(lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(mv.err))
		b.WriteString("\n\n" + s.subtle.Render("Press r to try again."))
		return b.String()
	case mv.roster == nil:
		return b.String()
	}
	r := *mv.roster

	b.WriteString(s.subtle.Render(fitWidth(fmt.Sprintf("%d on the sheet · checked %s · r to check everyone again", len(r.Entries), ago(r.CheckedAt)), inner)))
	b.WriteString("\n")
	b.WriteString(a.viewMemberTabs(r, inner) + "\n")
	explain := "Everyone on the membership sheet."
	if g := memberTabs[mv.tab]; g != tabAll {
		explain = g.Explain()
	}
	b.WriteString(s.subtle.Render(fitWidth(explain, inner)) + "\n")
	for _, p := range r.Problems {
		b.WriteString(lipgloss.NewStyle().Foreground(s.p.warn).Render(fitWidth("! "+p, inner)) + "\n")
	}

	// The list on the left, the highlighted member's details on the right.
	leftW := min(max(inner*2/5, 30), 48)
	rightW := max(inner-leftW-3, 20)
	h := a.memberListHeight()
	rows := mv.rows()
	var left []string
	head := lipgloss.NewStyle().Foreground(s.p.subtle).Bold(true)
	nameW := leftW - 2 - 12
	left = append(left, "  "+head.Width(nameW).Render("Name")+head.Render("Sh Ma Ji Fo"))
	if len(rows) == 0 {
		left = append(left, s.subtle.Render("  Nobody here."))
	}
	end := min(mv.top+h, len(rows))
	for i := mv.top; i < end; i++ {
		left = append(left, a.memberListRow(rows[i], nameW, i == mv.cursor))
	}
	if len(rows) > h {
		left = append(left, s.subtle.Render(fmt.Sprintf("  %d–%d of %d", mv.top+1, end, len(rows))))
	}
	right := ""
	if e, ok := mv.selected(); ok {
		right = a.viewMemberDetails(entryView(e), rightW)
	}
	b.WriteString(a.splitPanes(strings.Join(left, "\n"), right, leftW, rightW, h+1))
	return strings.TrimRight(b.String(), "\n")
}

// viewMemberTabs is the group tabs, with counts (short labels when narrow).
func (a App) viewMemberTabs(r roster.Roster, inner int) string {
	s := a.styles
	labels := make([]string, len(memberTabs))
	width := 0
	for i, g := range memberTabs {
		label, count := "Everyone", len(r.Entries)
		if g != tabAll {
			label, count = g.String(), r.Count(g)
		}
		labels[i] = fmt.Sprintf(" %s %d ", label, count)
		width += lipgloss.Width(labels[i]) + 1
	}
	if width > inner {
		short := map[roster.Group]string{roster.NeedsSetup: "Setup", roster.Waiting: "Wait", roster.CantCheck: "Can't check", roster.NotReady: "Not ready", roster.SetUp: "Done", tabAll: "All"}
		for i, g := range memberTabs {
			count := len(r.Entries)
			if g != tabAll {
				count = r.Count(g)
			}
			labels[i] = fmt.Sprintf(" %s %d ", short[g], count)
		}
	}
	var tabs []string
	for i := range memberTabs {
		if i == a.members.tab {
			tabs = append(tabs, lipgloss.NewStyle().Bold(true).Foreground(s.p.text).Background(s.p.selected).Render(labels[i]))
		} else {
			tabs = append(tabs, s.subtle.Render(labels[i]))
		}
	}
	return strings.Join(tabs, " ")
}

// memberListRow is one member in the list: name, then an icon for the
// sheet and each system.
func (a App) memberListRow(e roster.Entry, nameW int, selected bool) string {
	s := a.styles
	bg := lipgloss.NewStyle()
	marker := "  "
	if selected {
		bg = bg.Background(s.p.selected)
		marker = "▸ "
	}
	name := e.Name()
	if name == "" {
		name = e.Email
	}
	row := bg.Foreground(s.p.accent).Bold(true).Render(marker) + s.text.Inherit(bg).Width(nameW).Render(fitWidth(name, nameW-1))
	icons := []connectors.State{e.Sheet.State}
	for _, sys := range e.Systems {
		icons = append(icons, sys.State)
	}
	for _, st := range icons {
		row += a.stateColor(st).Inherit(bg).Width(3).Render(check.Icon(st))
	}
	if e.Warning() != "" {
		row += lipgloss.NewStyle().Foreground(s.p.warn).Inherit(bg).Render("!")
	}
	return row
}

// splitPanes puts left and right side by side, h lines tall, with a thin
// line between them.
func (a App) splitPanes(left, right string, leftW, rightW, h int) string {
	clip := func(text string, width int) string {
		lines := strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
		if len(lines) > h {
			lines = lines[:h]
		}
		return lipgloss.NewStyle().Width(width).Height(h).Render(strings.Join(lines, "\n"))
	}
	sep := lipgloss.NewStyle().Foreground(a.styles.p.faint).Render(strings.TrimRight(strings.Repeat("│\n", h), "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, clip(left, leftW), " "+sep+" ", clip(right, rightW))
}

// shortSystemName is a system's name for a narrow column heading.
func shortSystemName(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.Contains(l, "sheet"):
		return "Sheet"
	case strings.Contains(l, "mail"):
		return "Mail"
	case strings.Contains(l, "jira"):
		return "Jira"
	case strings.Contains(l, "discourse"), strings.Contains(l, "forum"):
		return "Forum"
	}
	if f := strings.Fields(name); len(f) > 0 && len(f[0]) <= 5 {
		return f[0]
	}
	return fitWidth(name, 5)
}

// fitWidth shortens s with "…" so it fits in width columns.
func fitWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// ago says how long ago t was, e.g. "just now" or "5 min ago".
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	default:
		return "at " + t.Format("3:04 PM")
	}
}
