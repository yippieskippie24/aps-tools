package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/config"
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
	a.members.loading, a.members.err, a.members.notice = true, "", ""
	deps := a.deps
	run := func() tea.Msg {
		cfg, err := deps.LoadConfig()
		if errors.Is(err, config.ErrNotFound) {
			cfg, err = &config.Config{}, nil
		}
		if err != nil {
			return rosterDoneMsg{err: err}
		}
		r, err := roster.Load(context.Background(), deps.Connectors(cfg, deps.Secrets(cfg)))
		return rosterDoneMsg{roster: r, err: err}
	}
	return a, tea.Batch(run, spinTick())
}

// refreshMember re-checks one member (after onboarding them) in the
// background. The roster is copied so the screen never reads it mid-change.
func (a App) refreshMember(email string) tea.Cmd {
	if a.members.roster == nil {
		return nil
	}
	r := *a.members.roster
	r.Entries = append([]roster.Entry(nil), r.Entries...)
	deps := a.deps
	return func() tea.Msg {
		cfg, err := deps.LoadConfig()
		if err != nil {
			return memberRefreshedMsg{roster: r, err: err}
		}
		err = r.Refresh(context.Background(), deps.Connectors(cfg, deps.Secrets(cfg)), email)
		return memberRefreshedMsg{roster: r, err: err}
	}
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
			mv.notice = "Couldn't re-check them: " + msg.err.Error()
			return a
		}
		mv.roster = &msg.roster
		mv.notice = "Re-checked after onboarding."
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
	case "enter", "o":
		if e, ok := mv.selected(); ok {
			return a.onboardFromMembers(e.Email)
		}
	case "c":
		if e, ok := mv.selected(); ok {
			return a.checkFromMembers(e)
		}
	case "e":
		if e, ok := mv.selected(); ok {
			return a.openEmail(e.Person, e.Sheet.State, mv.roster.States(e)), nil
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

// onboardFromMembers opens onboarding for a member, planned live, and comes
// back to this list afterwards.
func (a App) onboardFromMembers(email string) (tea.Model, tea.Cmd) {
	a, _ = a.openOnboard()
	a.back = screenMembers
	a.onboard.input.SetValue(email)
	return a.startPlan(email)
}

// checkFromMembers opens the member check for a member, and comes back to
// this list afterwards.
func (a App) checkFromMembers(e roster.Entry) (tea.Model, tea.Cmd) {
	a.screen, a.member, a.back = screenCheck, newMemberCheck(a.styles), screenMembers
	who := e.Name()
	if who == "" {
		who = e.Email
	}
	return a.startCheckFor(e.Email, who)
}

// membersKeys is the footer for the Members screen.
func (a App) membersKeys() string {
	mv := a.members
	switch {
	case mv.loading || mv.roster == nil:
		return a.keyHelp("r", "check again", "esc", "back")
	}
	return a.keyHelp("←/→", "group", "↑/↓", "move", "enter", "onboard", "e", "email", "c", "check", "r", "refresh", "esc", "back")
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
	if a.members.notice != "" {
		extra++
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

	b.WriteString(s.subtle.Render(fmt.Sprintf("%d on the sheet · checked %s · ", len(r.Entries), ago(r.CheckedAt)) + "r to refresh"))
	b.WriteString("\n")

	// Tabs, with counts (short labels when the window is narrow).
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
		text := labels[i]
		if i == mv.tab {
			tabs = append(tabs, lipgloss.NewStyle().Bold(true).Foreground(s.p.text).Background(s.p.selected).Render(text))
		} else {
			tabs = append(tabs, s.subtle.Render(text))
		}
	}
	b.WriteString(strings.Join(tabs, " "))
	b.WriteString("\n")
	explain := "Everyone on the membership sheet."
	if g := memberTabs[mv.tab]; g != tabAll {
		explain = g.Explain()
	}
	b.WriteString(s.subtle.Render(fitWidth(explain, inner)))
	b.WriteString("\n")

	for _, p := range r.Problems {
		b.WriteString(lipgloss.NewStyle().Foreground(s.p.warn).Render(fitWidth("! "+p, inner)) + "\n")
	}

	// Columns: name, (email when there's room), one icon per system, then
	// why they're in this group.
	cols := memberColumns(inner, r)
	b.WriteString(a.memberHeaderRow(cols, r) + "\n")

	rows := mv.rows()
	if len(rows) == 0 {
		b.WriteString(s.subtle.Render("  Nobody here."))
	}
	h := a.memberListHeight()
	end := min(mv.top+h, len(rows))
	for i := mv.top; i < end; i++ {
		b.WriteString(a.memberRow(rows[i], cols, i == mv.cursor) + "\n")
	}
	if len(rows) > h {
		b.WriteString(s.subtle.Render(fmt.Sprintf("  %d–%d of %d", mv.top+1, end, len(rows))) + "\n")
	}
	if mv.notice != "" {
		b.WriteString(s.statusLine.Render(fitWidth(mv.notice, inner)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// columns is the member list's layout for the current width.
type columns struct {
	name, email, icon, reason int
	showEmail                 bool
}

func memberColumns(inner int, r roster.Roster) columns {
	c := columns{name: 24, email: 32, icon: 6}
	icons := c.icon * (len(r.Systems) + 1)
	c.showEmail = inner >= 2+c.name+c.email+icons+20
	used := 2 + c.name + icons
	if c.showEmail {
		used += c.email
	}
	c.reason = max(inner-used, 0)
	return c
}

func (a App) memberHeaderRow(c columns, r roster.Roster) string {
	s := a.styles
	h := lipgloss.NewStyle().Foreground(s.p.subtle).Bold(true)
	row := "  " + h.Width(c.name).Render("Name")
	if c.showEmail {
		row += h.Width(c.email).Render("Email")
	}
	for _, name := range append([]string{r.Directory}, r.Systems...) {
		row += h.Width(c.icon).Render(shortSystemName(name))
	}
	if c.reason > 0 {
		row += h.Render("Why")
	}
	return row
}

func (a App) memberRow(e roster.Entry, c columns, selected bool) string {
	s := a.styles
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(s.p.selected)
	}
	text := s.text.Inherit(bg)
	marker := bg.Render("  ")
	if selected {
		marker = bg.Foreground(s.p.accent).Bold(true).Render("▸ ")
	}
	name := e.Name()
	if name == "" {
		name = e.Email
	}
	row := marker + text.Width(c.name).Render(fitWidth(name, c.name-1))
	if c.showEmail {
		row += s.subtle.Inherit(bg).Width(c.email).Render(fitWidth(e.Email, c.email-1))
	}
	row += a.stateColor(e.Sheet.State).Inherit(bg).Width(c.icon).Render(check.Icon(e.Sheet.State))
	for _, sys := range e.Systems {
		row += a.stateColor(sys.State).Inherit(bg).Width(c.icon).Render(check.Icon(sys.State))
	}
	if c.reason > 0 {
		var parts []string
		if e.Group == roster.NotReady {
			parts = append(parts, e.Sheet.Summary)
		}
		for _, sys := range e.Why() {
			parts = append(parts, shortSystemName(sys.System)+": "+sys.Summary)
		}
		why := fitWidth(strings.Join(parts, " · "), c.reason)
		if w := e.Warning(); w != "" {
			// The warning first, in its colour, then the reason.
			warn := fitWidth("! "+w, c.reason)
			rest := ""
			if room := c.reason - lipgloss.Width(warn) - 3; room > 8 && why != "" {
				rest = s.subtle.Inherit(bg).Render(" · " + fitWidth(why, room))
			}
			row += bg.Width(c.reason).Render(lipgloss.NewStyle().Foreground(s.p.warn).Inherit(bg).Render(warn) + rest)
		} else {
			row += s.subtle.Inherit(bg).Width(c.reason).Render(why)
		}
	}
	return row
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
