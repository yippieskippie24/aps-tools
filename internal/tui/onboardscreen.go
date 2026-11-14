package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/clipboard"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
)

// This file is the "Onboard a new member" screen. It walks through the same
// stages as `aps-tools onboard`: type an email → see the plan → confirm →
// watch each step run. The work itself happens in internal/onboard.

// obPhase is which stage of onboarding the screen is showing.
type obPhase int

const (
	obInput    obPhase = iota // typing the member's email
	obPlanning                // checking every system
	obPlan                    // showing the plan
	obConfirm                 // "Make these changes?"
	obRunning                 // running the tool's steps, one at a time
	obDone                    // finished running
)

// onboarding holds the state of the Onboard a new member screen.
type onboarding struct {
	input   textinput.Model
	phase   obPhase
	err     string        // problem with the input or with planning
	plan    *onboard.Plan // nil until a plan has been made
	current int           // index of the step running now (obRunning)
	notice  string        // short message, e.g. "Copied" or a log problem
}

func newOnboarding(s styles) onboarding {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "name@example.com"
	in.CharLimit = 254
	ob := onboarding{input: in, current: -1}
	ob.restyle(s)
	return ob
}

// restyle applies the current theme to the text box.
func (ob *onboarding) restyle(s styles) {
	st := textinput.DefaultStyles(true)
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(s.p.accent).Bold(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(s.p.text)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(s.p.subtle)
	st.Blurred = st.Focused
	ob.input.SetStyles(st)
}

// planDoneMsg carries a finished plan (or why planning failed).
type planDoneMsg struct {
	plan onboard.Plan
	err  error
}

// stepDoneMsg carries the plan after one step has run.
type stepDoneMsg struct {
	plan    onboard.Plan
	step    int
	logErr  error // the step ran, but writing the activity log failed
	logPath string
}

// obCopiedMsg reports whether copying the checklist worked.
type obCopiedMsg struct{ err error }

// openOnboard shows the screen with an empty, focused email box.
func (a App) openOnboard() (App, tea.Cmd) {
	a.screen = screenOnboard
	a.onboard = newOnboarding(a.styles)
	return a, a.onboard.input.Focus()
}

// startPlan validates the email and prepares the plan in the background.
// Preparing is read-only.
func (a App) startPlan(email string) (App, tea.Cmd) {
	if _, err := check.NormalizeEmail(email); err != nil {
		a.onboard.err = "That doesn't look like an email address — try name@example.com."
		return a, nil
	}
	a.onboard.err, a.onboard.notice, a.onboard.plan = "", "", nil
	a.onboard.phase = obPlanning
	a.onboard.input.Blur()
	deps := a.deps
	run := func() tea.Msg {
		cfg, err := deps.LoadConfig()
		if errors.Is(err, config.ErrNotFound) {
			cfg, err = &config.Config{}, nil
		}
		if err != nil {
			return planDoneMsg{err: err}
		}
		p, err := onboard.Prepare(context.Background(), deps.Connectors(cfg, deps.Secrets(cfg)), email)
		return planDoneMsg{plan: p, err: err}
	}
	return a, tea.Batch(run, spinTick())
}

// activityLog is the activity log next to the settings file.
func (a App) activityLog() (*activity.Log, error) {
	path, err := a.deps.ConfigPath()
	if err != nil {
		return nil, err
	}
	return activity.Open(filepath.Join(filepath.Dir(path), activity.FileName)), nil
}

// runStep runs step i in the background. The plan is copied first, so the
// screen never reads a step while it's being changed. first and last say
// whether to write the run's start and end lines to the activity log.
func (a App) runStep(i int, first bool) tea.Cmd {
	p := *a.onboard.plan
	p.Steps = slices.Clone(p.Steps)
	log, logErr := a.activityLog()
	return func() tea.Msg {
		if logErr != nil {
			// Never change anything that can't be recorded.
			return stepDoneMsg{plan: p, step: -1, logErr: logErr}
		}
		var errs []error
		if first {
			if err := p.LogStart(log); err != nil {
				return stepDoneMsg{plan: p, step: -1, logErr: err}
			}
		}
		errs = append(errs, p.RunStep(context.Background(), i, log))
		if p.Next() < 0 {
			errs = append(errs, p.LogFinish(log))
		}
		return stepDoneMsg{plan: p, step: i, logErr: errors.Join(errs...), logPath: log.Path()}
	}
}

// onboardMsg handles this screen's background results. ok is false for
// messages that belong to other screens.
func (a App) onboardMsg(msg tea.Msg) (App, tea.Cmd, bool) {
	ob := &a.onboard
	switch msg := msg.(type) {
	case planDoneMsg:
		if msg.err != nil {
			ob.phase, ob.err = obInput, msg.err.Error()
			return a, ob.input.Focus(), true
		}
		ob.plan, ob.phase = &msg.plan, obPlan
		return a, nil, true

	case stepDoneMsg:
		ob.plan = &msg.plan
		if msg.logErr != nil {
			ob.notice = "Activity log problem: " + msg.logErr.Error()
		}
		if msg.step < 0 { // nothing ran: the log couldn't be written
			ob.phase, ob.current = obPlan, -1
			return a, nil, true
		}
		if next := ob.plan.Next(); next >= 0 {
			ob.current = next
			return a, a.runStep(next, false), true
		}
		ob.phase, ob.current = obDone, -1
		if ob.notice == "" {
			ob.notice = "Recorded in the activity log: " + msg.logPath
		}
		return a, nil, true

	case obCopiedMsg:
		if msg.err != nil {
			ob.notice = "Couldn't copy: " + msg.err.Error()
		} else {
			ob.notice = "Checklist copied — paste it into a note."
		}
		return a, nil, true
	}
	return a, nil, false
}

func (a App) updateOnboard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ob := &a.onboard
	key := msg.String()
	switch ob.phase {
	case obInput:
		switch key {
		case "esc":
			a.screen = screenHome
			return a, nil
		case "enter":
			return a.startPlan(ob.input.Value())
		}
		var cmd tea.Cmd
		ob.input, cmd = ob.input.Update(msg)
		ob.err = ""
		return a, cmd

	case obPlanning:
		if key == "esc" {
			a.screen = screenHome // planning is read-only; let it finish quietly
		}
		return a, nil

	case obRunning:
		return a, nil // changes are being made: stay until they finish

	case obConfirm:
		switch key {
		case "y", "g":
			ob.phase, ob.notice = obRunning, ""
			ob.current = ob.plan.Next()
			if ob.current < 0 {
				ob.phase = obDone
				return a, nil
			}
			return a, tea.Batch(a.runStep(ob.current, true), spinTick())
		default:
			ob.phase = obPlan
			ob.notice = "Cancelled: nothing was changed."
		}
		return a, nil
	}

	// Looking at a plan (or the result of running it).
	switch key {
	case "esc", "q":
		a.screen = screenHome
	case "n", "/":
		return a.openOnboard()
	case "r":
		return a.startPlan(ob.plan.Person.Email)
	case "g":
		if ob.phase == obPlan && ob.plan.Ready && ob.plan.Next() >= 0 {
			ob.phase, ob.notice = obConfirm, ""
		}
	case "c":
		text := ob.plan.PlainText()
		return a, func() tea.Msg { return obCopiedMsg{err: clipboard.Copy(text)} }
	case "?":
		a.screen = screenHelp
	}
	return a, nil
}

// onboardKeys is the footer for the onboard screen in its current phase.
func (a App) onboardKeys() string {
	ob := a.onboard
	switch ob.phase {
	case obInput:
		return a.keyHelp("enter", "plan", "esc", "back")
	case obPlanning:
		return a.keyHelp("esc", "back")
	case obRunning:
		return "  " + a.styles.subtle.Render("Making changes — please wait…")
	case obConfirm:
		return a.keyHelp("y", "go", "n", "cancel")
	case obDone:
		return a.keyHelp("r", "check again", "c", "copy", "n", "next member", "esc", "back")
	}
	if ob.plan != nil && ob.plan.Ready && ob.plan.Next() >= 0 {
		return a.keyHelp("g", "go", "c", "copy checklist", "r", "re-check", "n", "new", "esc", "back")
	}
	return a.keyHelp("c", "copy checklist", "r", "re-check", "n", "new", "esc", "back")
}

func (a App) viewOnboard(w int) string {
	s := a.styles
	ob := a.onboard
	inner := w - 6 // card border + padding

	var b strings.Builder
	b.WriteString(s.heading.Render("Onboard a new member"))
	b.WriteString("\n")

	switch ob.phase {
	case obInput:
		b.WriteString(s.subtle.Width(inner).Render("Enter the vetted member's email. The tool checks every system and shows its plan first: nothing changes until you say Go.") + "\n\n")
		ob.input.SetWidth(inner - 4)
		b.WriteString(ob.input.View())
		if ob.err != "" {
			b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(ob.err))
		}
	case obPlanning:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		b.WriteString(spinner + " " + s.subtle.Render("Checking "+strings.TrimSpace(ob.input.Value())+" in every system… (read-only)"))
	default:
		b.WriteString(a.viewPlan(*ob.plan, inner))
		if ob.phase == obConfirm {
			n := ob.plan.Count(onboard.Auto)
			q := "Make " + changeCount(n) + " for " + ob.plan.Who() + "?"
			box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(s.p.accent).Padding(0, 1).Width(inner)
			b.WriteString("\n\n" + box.Render(s.text.Bold(true).Render(q)+"\n"+s.subtle.Render("Only the + steps run, and each is recorded in the activity log.")))
		}
		if ob.notice != "" {
			b.WriteString("\n\n" + s.statusLine.Width(inner).Render(ob.notice))
		}
	}
	return s.card.Width(w).Render(b.String())
}

// kindColor picks the colour for a step's kind.
func (a App) kindColor(k onboard.Kind) lipgloss.Style {
	p := a.styles.p
	switch k {
	case onboard.Done:
		return lipgloss.NewStyle().Foreground(p.ok).Bold(true)
	case onboard.Auto:
		return lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	case onboard.Manual, onboard.Waiting, onboard.Blocked:
		return lipgloss.NewStyle().Foreground(p.warn).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(p.subtle)
	}
}

func (a App) viewPlan(p onboard.Plan, inner int) string {
	s := a.styles
	var rows []string
	rows = append(rows, s.text.Bold(true).Render(p.Who()))

	if !p.Ready {
		rows = append(rows, lipgloss.NewStyle().Foreground(s.p.fail).Bold(true).Width(inner).Render(p.Summary()))
		for _, d := range p.Gate.Details {
			rows = append(rows, s.subtle.Width(inner).Render(d))
		}
		if p.Gate.Fix != "" {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, s.fixArrow.Render("→ "), s.text.Width(inner-2).Render(p.Gate.Fix)))
		}
		rows = append(rows, s.subtle.Render("Nothing was changed."))
		return strings.Join(rows, "\n")
	}

	summaryColor := s.p.ok
	switch {
	case p.Failures() > 0 || p.Count(onboard.Blocked) > 0:
		summaryColor = s.p.fail
	case p.Count(onboard.Manual) > 0:
		summaryColor = s.p.warn
	case p.Count(onboard.Auto) > 0:
		summaryColor = s.p.accent
	}
	rows = append(rows, lipgloss.NewStyle().Foreground(summaryColor).Bold(true).Width(inner).Render(p.Summary()), "")

	indent := lipgloss.NewStyle().PaddingLeft(2)
	for i, st := range p.Steps {
		col := a.kindColor(st.Kind)
		icon, label := st.Kind.Icon(), st.Kind.String()
		switch {
		case i == a.onboard.current && a.onboard.phase == obRunning:
			icon, label = spinnerFrames[a.spin], "working…"
		case st.Outcome == onboard.Succeeded:
			icon, label, col = "✓", "done", lipgloss.NewStyle().Foreground(s.p.ok).Bold(true)
		case st.Outcome == onboard.Failed:
			icon, label, col = "✗", "failed", lipgloss.NewStyle().Foreground(s.p.fail).Bold(true)
		}
		left := col.Render(icon) + " " + s.text.Bold(true).Render(st.System)
		right := col.Render(label)
		gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 2 {
			gap = 2
		}
		rows = append(rows, left+strings.Repeat(" ", gap)+right)
		if st.Now != "" {
			rows = append(rows, indent.Render(s.subtle.Width(inner-2).Render(st.Now)))
		}
		switch st.Outcome {
		case onboard.Succeeded:
			rows = append(rows, indent.Render(s.text.Width(inner-2).Render(st.Result)))
		case onboard.Failed:
			rows = append(rows, indent.Render(lipgloss.NewStyle().Foreground(s.p.fail).Width(inner-2).Render(st.Result)))
		default:
			if st.Action != "" {
				rows = append(rows, indent.Render(lipgloss.JoinHorizontal(lipgloss.Top, s.fixArrow.Render("→ "), s.text.Width(inner-4).Render(st.Action))))
			}
		}
	}
	return strings.Join(rows, "\n")
}

// changeCount is "1 change" or "3 changes".
func changeCount(n int) string {
	if n == 1 {
		return "1 change"
	}
	return fmt.Sprintf("%d changes", n)
}
