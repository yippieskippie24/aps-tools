package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/activity"
)

// This file is the "Activity log" screen: every change the tool has made
// (and every attempt that failed), newest first, grouped by day. Press / to
// filter it (e.g. a member's name or email, or "Cloudflare"). The log
// itself is internal/activity's plain text file next to the settings.

// logLimit is how many recent entries the screen reads.
const logLimit = 2000

// logView holds the state of the Activity log screen.
type logView struct {
	entries []activity.Entry // newest first
	path    string
	err     string
	filter  textinput.Model
	editing bool // the filter box has the keyboard
}

func newLogView(s styles) logView {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "filter: a name, email, system or word"
	in.CharLimit = 100
	lv := logView{filter: in}
	lv.restyle(s)
	return lv
}

// restyle applies the current theme to the filter box.
func (lv *logView) restyle(s styles) {
	st := textinput.DefaultStyles(true)
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(s.p.accent).Bold(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(s.p.text)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(s.p.subtle)
	st.Blurred = st.Focused
	lv.filter.SetStyles(st)
}

// openLog shows the screen, reading the log fresh (it's a small local file).
func (a App) openLog() App {
	a.screen = screenLog
	filter := a.log.filter.Value()
	a.log = newLogView(a.styles)
	a.log.filter.SetValue(filter) // keep a filter between visits
	log, err := a.activityLog()
	if err != nil {
		a.log.err = err.Error()
		return a
	}
	a.log.path = log.Path()
	entries, err := log.Entries(logLimit)
	if err != nil {
		a.log.err = "Couldn't read the activity log: " + err.Error()
	}
	for i := len(entries) - 1; i >= 0; i-- {
		a.log.entries = append(a.log.entries, entries[i])
	}
	return a
}

// logRows is the entries matching the filter: every word must appear in the
// entry's member, system or text.
func (a App) logRows() []activity.Entry {
	words := strings.Fields(strings.ToLower(a.log.filter.Value()))
	var out []activity.Entry
	for _, e := range a.log.entries {
		hay := strings.ToLower(e.Member + " " + e.System + " " + e.What)
		match := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if match {
			out = append(out, e)
		}
	}
	return out
}

func (a App) updateLog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	lv := &a.log
	key := msg.String()
	if lv.editing {
		switch key {
		case "enter":
			lv.editing = false
			lv.filter.Blur()
			return a, nil
		case "esc":
			lv.editing = false
			lv.filter.Blur()
			lv.filter.SetValue("")
			return a, nil
		}
		var cmd tea.Cmd
		lv.filter, cmd = lv.filter.Update(msg)
		a.scroll = 0
		return a, cmd
	}
	switch key {
	case "/":
		lv.editing = true
		return a, lv.filter.Focus()
	case "r":
		return a.openLog(), nil
	case "esc", "q":
		if lv.filter.Value() != "" {
			lv.filter.SetValue("") // first esc clears the filter
			a.scroll = 0
			return a, nil
		}
		a.screen = screenHome
	case "?":
		a.screen = screenHelp
	}
	return a, nil
}

// logKeys is the footer for the Activity log screen.
func (a App) logKeys() string {
	if a.log.editing {
		return a.keyHelp("type", "filter", "enter", "done", "esc", "clear")
	}
	return a.keyHelp("↑/↓", "scroll", "/", "filter", "r", "reload", "esc", "back")
}

func (a App) viewLog(w int) string {
	s := a.styles
	lv := a.log
	inner := innerWidth(w)

	var b strings.Builder
	b.WriteString(s.heading.Render("Activity log"))
	b.WriteString("\n")
	b.WriteString(s.subtle.Width(inner).Render("Every change the tool has made, and every attempt that failed, newest first.") + "\n")
	if lv.path != "" {
		b.WriteString(s.subtle.Render(fitWidth("Full log: "+lv.path, inner)) + "\n")
	}
	if lv.editing || lv.filter.Value() != "" {
		lv.filter.SetWidth(inner - 4)
		b.WriteString(lv.filter.View() + "\n")
	}
	b.WriteString("\n")
	if lv.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(lv.err))
		return b.String()
	}
	rows := a.logRows()
	switch {
	case len(lv.entries) == 0:
		b.WriteString(s.subtle.Width(inner).Render("Nothing yet. Changes appear here when onboarding, emails or re-sends make them."))
		return b.String()
	case len(rows) == 0:
		b.WriteString(s.subtle.Render("Nothing matches the filter."))
		return b.String()
	}

	timeW, whoW, sysW := 7, 30, 22
	showWho := inner >= timeW+2+whoW+sysW+20
	whatW := inner - timeW - 2 - sysW
	if showWho {
		whatW -= whoW
	}
	day := ""
	for _, e := range rows {
		if d := e.Time.Format("Monday 2 January 2006"); d != day {
			if day != "" {
				b.WriteString("\n")
			}
			day = d
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(s.p.accent).Render(d) + "\n")
		}
		mark := lipgloss.NewStyle().Foreground(s.p.ok).Bold(true).Render("✓ ")
		whatStyle := s.text
		if !e.OK {
			mark = lipgloss.NewStyle().Foreground(s.p.fail).Bold(true).Render("✗ ")
			whatStyle = lipgloss.NewStyle().Foreground(s.p.fail)
		}
		cols := []string{s.subtle.Width(timeW).Render(e.Time.Format("3:04pm")), mark}
		who := e.Member
		if who == "" {
			who = "—"
		}
		if showWho {
			cols = append(cols, s.text.Width(whoW).Render(fitWidth(who, whoW-1)))
		}
		cols = append(cols, s.subtle.Width(sysW).Render(fitWidth(e.System, sysW-1)))
		what := e.What
		if !showWho && e.Member != "" {
			what = e.Member + ": " + what
		}
		cols = append(cols, whatStyle.Width(max(whatW, 10)).Render(what))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cols...) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
