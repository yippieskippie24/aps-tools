package tui

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/clipboard"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// This file is the "Check a member" screen: type an email, see the member's
// status in every APS system. The actual checking happens in internal/check.

// memberCheck holds the state of the Check a member screen.
type memberCheck struct {
	input   textinput.Model
	running bool
	err     string        // problem with the input or the check itself
	report  *check.Report // nil until a check has finished
	notice  string        // short confirmation, e.g. "Copied"
}

func newMemberCheck(s styles) memberCheck {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "name@example.com"
	in.CharLimit = 254 // longest possible email address
	mc := memberCheck{input: in}
	mc.restyle(s)
	return mc
}

// restyle applies the current theme to the text box.
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

// copiedMsg reports whether copying to the clipboard worked.
type copiedMsg struct{ err error }

// openCheck shows the screen with an empty, focused email box.
func (a App) openCheck() (App, tea.Cmd) {
	a.screen = screenCheck
	a.member = newMemberCheck(a.styles)
	return a, a.member.input.Focus()
}

// startCheck validates the email and runs the check in the background.
func (a App) startCheck(email string) (App, tea.Cmd) {
	if _, err := check.NormalizeEmail(email); err != nil {
		a.member.err = "That doesn't look like an email address — try name@example.com."
		return a, nil
	}
	a.member.err, a.member.notice, a.member.report = "", "", nil
	a.member.running = true
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

func (a App) updateCheck(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mc := &a.member
	if mc.running {
		if msg.String() == "esc" {
			a.screen = screenHome // the check finishes quietly in the background
		}
		return a, nil
	}

	// Typing an email.
	if mc.report == nil {
		switch msg.String() {
		case "esc":
			a.screen = screenHome
			return a, nil
		case "enter":
			return a.startCheck(mc.input.Value())
		}
		var cmd tea.Cmd
		mc.input, cmd = mc.input.Update(msg)
		mc.err = ""
		return a, cmd
	}

	// Looking at a report.
	switch msg.String() {
	case "esc", "q":
		a.screen = screenHome
	case "n", "/":
		return a.openCheck()
	case "r":
		return a.startCheck(mc.report.Email)
	case "c":
		text := mc.report.PlainText()
		return a, func() tea.Msg { return copiedMsg{err: clipboard.Copy(text)} }
	case "?":
		a.screen = screenHelp
	}
	return a, nil
}

// checkKeys is the footer for the check screen in its current phase.
func (a App) checkKeys() string {
	switch {
	case a.member.running:
		return a.keyHelp("esc", "back")
	case a.member.report == nil:
		return a.keyHelp("enter", "check", "esc", "back")
	default:
		return a.keyHelp("n", "new check", "r", "re-check", "c", "copy report", "esc", "back")
	}
}

func (a App) viewCheck(w int) string {
	s := a.styles
	mc := a.member
	inner := w - 6 // card border + padding

	var b strings.Builder
	b.WriteString(s.heading.Render("Check a member"))
	b.WriteString("\n")

	switch {
	case mc.report == nil && !mc.running:
		b.WriteString(s.subtle.Render("Enter the member's email address. Nothing is changed in any system.") + "\n\n")
		mc.input.SetWidth(inner - 4)
		b.WriteString(mc.input.View())
		if mc.err != "" {
			b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(s.p.fail).Render(mc.err))
		}

	case mc.running:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		b.WriteString(spinner + " " + s.subtle.Render("Checking "+strings.TrimSpace(mc.input.Value())+" in every system…"))

	default:
		b.WriteString(a.viewReport(*mc.report, inner))
		if mc.notice != "" {
			b.WriteString("\n\n" + s.statusLine.Render(mc.notice))
		}
	}
	return s.card.Width(w).Render(b.String())
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
		if res.Fix != "" {
			rows = append(rows, indent.Render(s.fixArrow.Render("→ ")+s.text.Width(inner-4).Render(res.Fix)))
		}
		rows = append(rows, "")
	}
	if len(r.NotYetChecked) > 0 {
		rows = append(rows, s.subtle.Width(inner).Render("Not checked by the tool yet: "+strings.Join(r.NotYetChecked, ", ")))
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}
