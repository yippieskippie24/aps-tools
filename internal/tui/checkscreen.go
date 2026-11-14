package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/clipboard"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/roster"
)

// This file is the "Check a member" screen. It opens on a quick list of
// everyone on the membership sheet (only the sheet is read, so it's fast):
// type to narrow it by name or email, Tab to switch between Everyone /
// Ready / Not ready, and Enter to check the highlighted member in every
// system. A full email address can be typed for someone who isn't on the
// sheet. The checking happens in internal/check; the list comes from
// roster.Directory.

// memberCheck holds the state of the Check a member screen.
type memberCheck struct {
	input   textinput.Model // the search box (or an email address)
	tab     int             // index into checkTabs
	cursor  int             // highlighted row in the filtered list
	top     int             // first row shown
	running bool
	who     string        // whose check is running or shown, for headings
	err     string        // problem with the input or the check itself
	report  *check.Report // nil until a check has finished
	notice  string        // short confirmation, e.g. "Copied"
}

// people is the quick member list, kept while the app is open.
type people struct {
	loading bool
	err     string
	entries []connectors.DirectoryEntry
	at      time.Time
}

// checkTabs are the list's groups, by the sheet's verdict.
var checkTabs = []string{"Everyone", "Ready", "Not ready"}

// checkSorts are the ways the list can be ordered (← and → switch).
var checkSorts = []string{"Name", "Last name", "Email"}

// sortKey is what a member is ordered by for sort mode i.
func sortKey(p connectors.Person, i int) string {
	switch checkSorts[i] {
	case "Last name":
		return strings.ToLower(p.LastName + " " + p.FirstName + " " + p.Email)
	case "Email":
		return strings.ToLower(p.Email)
	}
	name := p.Name()
	if name == "" {
		name = "~" // people with no name go last
	}
	return strings.ToLower(name + " " + p.Email)
}

func newMemberCheck(s styles) memberCheck {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "type a name or email…"
	in.CharLimit = 254 // longest possible email address
	mc := memberCheck{input: in}
	mc.restyle(s)
	return mc
}

// restyle applies the current theme to the search box.
func (mc *memberCheck) restyle(s styles) {
	st := textinput.DefaultStyles(true)
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(s.p.accent).Bold(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(s.p.text)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(s.p.subtle)
	st.Blurred = st.Focused
	mc.input.SetStyles(st)
}

// checkDoneMsg carries a finished report (or why it failed).
type checkDoneMsg struct {
	report check.Report
	err    error
}

// peopleDoneMsg carries the quick member list (or why it failed).
type peopleDoneMsg struct {
	entries []connectors.DirectoryEntry
	err     error
}

// copiedMsg reports whether copying to the clipboard worked.
type copiedMsg struct{ err error }

// openCheck shows the screen with the member list and an empty, focused
// search box, loading the list the first time.
func (a App) openCheck() (App, tea.Cmd) {
	a.screen = screenCheck
	a.member = newMemberCheck(a.styles)
	focus := a.member.input.Focus()
	if a.people.entries == nil && !a.people.loading {
		a, load := a.loadPeople()
		return a, tea.Batch(focus, load)
	}
	return a, focus
}

// loadPeople reads the member list from the sheet in the background.
func (a App) loadPeople() (App, tea.Cmd) {
	a.people.loading, a.people.err = true, ""
	deps := a.deps
	run := func() tea.Msg {
		cfg, err := deps.LoadConfig()
		if errors.Is(err, config.ErrNotFound) {
			cfg, err = &config.Config{}, nil
		}
		if err != nil {
			return peopleDoneMsg{err: err}
		}
		entries, err := roster.Directory(context.Background(), deps.Connectors(cfg, deps.Secrets(cfg)))
		return peopleDoneMsg{entries: entries, err: err}
	}
	return a, tea.Batch(run, spinTick())
}

// peopleMsg stores the loaded member list.
func (a App) peopleMsg(msg peopleDoneMsg) App {
	a.people.loading = false
	if msg.err != nil {
		a.people.err = msg.err.Error()
		return a
	}
	a.people.entries, a.people.at, a.people.err = msg.entries, time.Now(), ""
	if a.people.entries == nil {
		a.people.entries = []connectors.DirectoryEntry{} // loaded, but empty
	}
	return a
}

// checkRows is the list after the tab and the search box are applied.
func (a App) checkRows() []connectors.DirectoryEntry {
	var out []connectors.DirectoryEntry
	query := a.member.input.Value()
	for _, e := range a.people.entries {
		ready := e.Status.State == connectors.StateOK
		switch checkTabs[a.member.tab] {
		case "Ready":
			if !ready {
				continue
			}
		case "Not ready":
			if ready {
				continue
			}
		}
		if roster.Matches(e.Person, query) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return sortKey(out[i].Person, a.peopleSort) < sortKey(out[j].Person, a.peopleSort)
	})
	return out
}

// startCheck validates the email and runs the check in the background.
// who is how to name them while it runs (their name, if known).
func (a App) startCheck(email string) (App, tea.Cmd) {
	return a.startCheckFor(email, email)
}

func (a App) startCheckFor(email, who string) (App, tea.Cmd) {
	if _, err := check.NormalizeEmail(email); err != nil {
		a.member.err = "No one on the sheet matches that. To check someone who isn't on it, type their full email address."
		return a, nil
	}
	a.member.err, a.member.notice, a.member.report = "", "", nil
	a.member.running, a.member.who = true, who
	a.member.input.Blur()
	deps := a.deps
	run := func() tea.Msg {
		cfg, err := deps.LoadConfig()
		if errors.Is(err, config.ErrNotFound) {
			cfg, err = &config.Config{}, nil
		}
		if err != nil {
			return checkDoneMsg{err: err}
		}
		r, err := check.Run(context.Background(), deps.Connectors(cfg, deps.Secrets(cfg)), email, connectors.NotYetBuilt())
		return checkDoneMsg{report: r, err: err}
	}
	return a, tea.Batch(run, spinTick())
}

// checkListHeight is how many member rows fit under the headings.
func (a App) checkListHeight() int {
	return max(a.cardInnerHeight()-8, 1)
}

func (a App) updateCheck(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mc := &a.member
	key := msg.String()
	if mc.running {
		if key == "esc" {
			a.screen = a.back // the check finishes quietly in the background
		}
		return a, nil
	}

	// Looking at a report.
	if mc.report != nil {
		switch key {
		case "esc", "q":
			if a.back == screenMembers {
				a.screen = a.back
				return a, nil
			}
			mc.report, mc.notice = nil, "" // back to the list, search kept
			return a, mc.input.Focus()
		case "n", "/":
			mc.report, mc.notice = nil, ""
			mc.input.SetValue("")
			mc.cursor, mc.top = 0, 0
			return a, mc.input.Focus()
		case "r":
			return a.startCheckFor(mc.report.Email, mc.who)
		case "e":
			sheet := connectors.StateOK
			if st, ok := mc.report.States()["sheet"]; ok {
				sheet = st
			}
			return a.openEmail(connectors.Person{Email: mc.report.Email}, sheet, mc.report.States()), nil
		case "c":
			text := mc.report.PlainText()
			return a, func() tea.Msg { return copiedMsg{err: clipboard.Copy(text)} }
		case "?":
			a.screen = screenHelp
		}
		return a, nil
	}

	// Choosing someone from the list (the search box has the keyboard).
	rows := a.checkRows()
	page := max(a.checkListHeight()-1, 1)
	switch key {
	case "esc":
		if mc.input.Value() != "" {
			mc.input.SetValue("") // first esc clears the search
			mc.cursor, mc.top, mc.err = 0, 0, ""
			return a, nil
		}
		a.screen = a.back
		return a, nil
	case "enter":
		if mc.cursor < len(rows) {
			e := rows[mc.cursor]
			who := e.Person.Name()
			if who == "" {
				who = e.Person.Email
			}
			return a.startCheckFor(e.Person.Email, who)
		}
		return a.startCheck(strings.TrimSpace(mc.input.Value()))
	case "tab":
		mc.tab, mc.cursor, mc.top = (mc.tab+1)%len(checkTabs), 0, 0
		return a, nil
	case "shift+tab":
		mc.tab, mc.cursor, mc.top = (mc.tab-1+len(checkTabs))%len(checkTabs), 0, 0
		return a, nil
	case "ctrl+r":
		return a.loadPeople()
	case "right":
		a.peopleSort = (a.peopleSort + 1) % len(checkSorts)
		mc.cursor, mc.top = 0, 0
		return a, nil
	case "left":
		a.peopleSort = (a.peopleSort - 1 + len(checkSorts)) % len(checkSorts)
		mc.cursor, mc.top = 0, 0
		return a, nil
	case "up":
		mc.cursor--
	case "down":
		mc.cursor++
	case "pgup":
		mc.cursor -= page
	case "pgdown":
		mc.cursor += page
	default:
		var cmd tea.Cmd
		before := mc.input.Value()
		mc.input, cmd = mc.input.Update(msg)
		if mc.input.Value() != before {
			mc.cursor, mc.top, mc.err = 0, 0, ""
		}
		return a, cmd
	}
	mc.cursor = min(max(mc.cursor, 0), max(len(rows)-1, 0))
	if h := a.checkListHeight(); mc.cursor < mc.top {
		mc.top = mc.cursor
	} else if mc.cursor >= mc.top+h {
		mc.top = mc.cursor - h + 1
	}
	return a, nil
}

// checkKeys is the footer for the check screen in its current phase.
func (a App) checkKeys() string {
	switch {
	case a.member.running:
		return a.keyHelp("esc", "back")
	case a.member.report == nil:
		return a.keyHelp("↑/↓", "highlight", "enter", "check", "type", "search", "←/→", "sort", "tab", "group", "ctrl+r", "reload list", "esc", "back")
	default:
		return a.keyHelp("e", "email them", "n", "new check", "r", "re-check", "c", "copy report", "esc", "back")
	}
}

func (a App) viewCheck(w int) string {
	s := a.styles
	mc := a.member
	inner := innerWidth(w)

	var b strings.Builder
	b.WriteString(s.heading.Render("Check a member"))
	b.WriteString("\n")

	switch {
	case mc.running:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		b.WriteString(spinner + " " + s.subtle.Render("Checking "+mc.who+" in every system… (read-only)"))
	case mc.report != nil:
		b.WriteString(a.viewReport(*mc.report, inner))
		if mc.notice != "" {
			b.WriteString("\n\n" + s.statusLine.Render(mc.notice))
		}
	default:
		b.WriteString(a.viewPeople(inner))
	}
	return b.String()
}

// viewPeople draws the search box and the member list.
func (a App) viewPeople(inner int) string {
	s := a.styles
	mc := a.member
	var b strings.Builder
	mc.input.SetWidth(inner - 4)
	b.WriteString(mc.input.View() + "\n")

	switch {
	case a.people.loading:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		b.WriteString(spinner + " " + s.subtle.Render("Reading the membership sheet…"))
		return b.String()
	case a.people.err != "":
		b.WriteString(lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(a.people.err) + "\n")
		b.WriteString(s.subtle.Width(inner).Render("You can still type a full email address and press enter to check it. ctrl+r tries the list again."))
		if mc.err != "" {
			b.WriteString("\n" + lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(mc.err))
		}
		return b.String()
	}

	// Tabs with counts (counts follow the search too).
	var tabs []string
	saved := mc.tab
	for i, name := range checkTabs {
		a.member.tab = i
		text := fmt.Sprintf(" %s %d ", name, len(a.checkRows()))
		if i == saved {
			tabs = append(tabs, lipgloss.NewStyle().Bold(true).Foreground(s.p.text).Background(s.p.selected).Render(text))
		} else {
			tabs = append(tabs, s.subtle.Render(text))
		}
	}
	a.member.tab = saved
	b.WriteString(strings.Join(tabs, " ") + "\n")
	b.WriteString(s.subtle.Render(fitWidth(fmt.Sprintf("Sorted by %s (←/→ to change) · from the sheet, read %s (ctrl+r to reload)",
		strings.ToLower(checkSorts[a.peopleSort]), ago(a.people.at)), inner)) + "\n")

	rows := a.checkRows()
	nameW, emailW := 26, 34
	showEmail := inner >= 2+nameW+emailW+20 || checkSorts[a.peopleSort] == "Email"
	if showEmail && inner < 2+nameW+emailW+20 {
		nameW, emailW = 20, 28 // sorted by email: make room for it
	}
	statusW := inner - 2 - nameW
	if showEmail {
		statusW -= emailW
	}

	// Column headings; the sorted column is marked ▲.
	head := lipgloss.NewStyle().Foreground(s.p.subtle).Bold(true)
	sorted := lipgloss.NewStyle().Foreground(s.p.accent).Bold(true)
	nameHead, emailHead := head.Width(nameW).Render("Name"), head.Width(emailW).Render("Email")
	switch checkSorts[a.peopleSort] {
	case "Name":
		nameHead = sorted.Width(nameW).Render("Name ▲")
	case "Last name":
		nameHead = sorted.Width(nameW).Render("Name (by last name) ▲")
	case "Email":
		emailHead = sorted.Width(emailW).Render("Email ▲")
	}
	headRow := "  " + nameHead
	if showEmail {
		headRow += emailHead
	}
	b.WriteString(headRow + head.Render("Membership sheet") + "\n")
	if len(rows) == 0 {
		b.WriteString(s.subtle.Render("  No one on the sheet matches. To check someone who isn't on it, type their full email address and press enter."))
	}
	h := a.checkListHeight()
	end := min(mc.top+h, len(rows))
	for i := mc.top; i < end; i++ {
		e := rows[i]
		bg := lipgloss.NewStyle()
		marker := "  "
		if i == mc.cursor {
			bg = bg.Background(s.p.selected)
			marker = bg.Foreground(s.p.accent).Bold(true).Render("▸ ")
		} else {
			marker = bg.Render(marker)
		}
		name := e.Person.Name()
		if name == "" {
			name = "(no name)"
		}
		row := marker + s.text.Inherit(bg).Width(nameW).Render(fitWidth(name, nameW-1))
		if showEmail {
			row += s.subtle.Inherit(bg).Width(emailW).Render(fitWidth(e.Person.Email, emailW-1))
		}
		status := check.Icon(e.Status.State) + " " + e.Status.Summary
		row += a.stateColor(e.Status.State).UnsetBold().Inherit(bg).Width(max(statusW, 0)).Render(fitWidth(status, max(statusW, 0)))
		b.WriteString(row + "\n")
	}
	if len(rows) > h {
		b.WriteString(s.subtle.Render(fmt.Sprintf("  %d–%d of %d", mc.top+1, end, len(rows))) + "\n")
	}
	if mc.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(mc.err))
	}
	return strings.TrimRight(b.String(), "\n")
}

// stateColor picks the colour for a member state.
func (a App) stateColor(st connectors.State) lipgloss.Style {
	p := a.styles.p
	switch st {
	case connectors.StateOK:
		return lipgloss.NewStyle().Foreground(p.ok).Bold(true)
	case connectors.StatePending, connectors.StateUnknown:
		return lipgloss.NewStyle().Foreground(p.warn).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(p.fail).Bold(true)
	}
}

func (a App) viewReport(r check.Report, inner int) string {
	s := a.styles
	var rows []string

	rows = append(rows, s.text.Bold(true).Render(r.Email))
	rows = append(rows, a.stateColor(r.Worst()).Render(r.Summary()))
	rows = append(rows, "")

	indent := lipgloss.NewStyle().PaddingLeft(2)
	for _, res := range r.Results {
		col := a.stateColor(res.State)
		left := col.Render(check.Icon(res.State)) + " " + s.text.Bold(true).Render(res.System)
		right := col.Render(res.State.String())
		gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 2 {
			gap = 2
		}
		rows = append(rows, left+strings.Repeat(" ", gap)+right)
		rows = append(rows, indent.Render(s.text.Width(inner-2).Render(res.Summary)))
		for _, d := range res.Details {
			rows = append(rows, indent.Render(s.subtle.Width(inner-2).Render(d)))
		}
		if res.Warning != "" {
			rows = append(rows, indent.Render(a.warningLine(res.Warning, inner-2)))
		}
		if res.Fix != "" {
			rows = append(rows, indent.Render(lipgloss.JoinHorizontal(lipgloss.Top, s.fixArrow.Render("→ "), s.text.Width(inner-4).Render(res.Fix))))
		}
		rows = append(rows, "")
	}
	if len(r.NotYetChecked) > 0 {
		rows = append(rows, s.subtle.Width(inner).Render("Not checked by the tool yet: "+strings.Join(r.NotYetChecked, ", ")))
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

// warningLine draws "! something to double-check" in the warning colour,
// wrapped to width.
func (a App) warningLine(text string, width int) string {
	warn := lipgloss.NewStyle().Foreground(a.styles.p.warn)
	return lipgloss.JoinHorizontal(lipgloss.Top, warn.Bold(true).Render("! "), warn.Width(width-2).Render(text))
}
