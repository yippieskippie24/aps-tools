package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/progress"
)

// This file makes sure the webmaster can always see that the app is
// working. Anything that runs in the background (reading the sheet,
// checking members, onboarding, opening an email) is a "task":
//   - while it runs, the line along the bottom shows a spinner, what it's
//     doing, the step it's on right now (reported by the work itself, via
//     internal/progress), and how long it's taken;
//   - only one task runs at a time, so pressing a key twice can't start the
//     same thing twice.

// task is the background work running now, if any.
type task struct {
	active  bool
	label   string // what it's doing, e.g. "Opening the welcome email"
	step    string // the step it's on, e.g. "Looking up their forum invite"
	started time.Time
	ctx     context.Context // carries the progress reporter to the work
	steps   chan string
	done    chan struct{}
	gen     int // tells this task's progress apart from an earlier one's
}

// progressMsg is a step reported by the running task.
type progressMsg struct {
	gen  int
	step string
}

// begin starts a task called label. It returns the context to run the work
// with, a command that keeps the bottom line up to date, and false if
// another task is already running (then nothing should be started).
func (a *App) begin(label string) (context.Context, tea.Cmd, bool) {
	if a.task.active {
		return nil, nil, false
	}
	gen := a.task.gen + 1
	steps, done := make(chan string, 64), make(chan struct{})
	report := func(s string) {
		select {
		case steps <- s:
		default: // never hold up the work for the screen
		}
	}
	ctx := progress.With(context.Background(), report)
	a.task = task{active: true, label: label, started: time.Now(), ctx: ctx, steps: steps, done: done, gen: gen}
	return ctx, tea.Batch(listenProgress(steps, done, gen), spinTick()), true
}

// finish ends the running task.
func (a *App) finish() {
	if a.task.active {
		a.task.active = false
		close(a.task.done)
	}
}

// listenProgress waits for the next step the task reports.
func listenProgress(steps <-chan string, done <-chan struct{}, gen int) tea.Cmd {
	return func() tea.Msg {
		select {
		case s := <-steps:
			return progressMsg{gen: gen, step: s}
		case <-done:
			return nil
		}
	}
}

// onProgress shows a reported step and listens for the next.
func (a App) onProgress(msg progressMsg) (App, tea.Cmd) {
	if !a.task.active || msg.gen != a.task.gen {
		return a, nil
	}
	a.task.step = msg.step
	return a, listenProgress(a.task.steps, a.task.done, a.task.gen)
}

// finishesTask reports whether msg is the result that ends a task.
func finishesTask(msg tea.Msg) bool {
	switch msg.(type) {
	case doctorDoneMsg, peopleDoneMsg, checkDoneMsg, rosterDoneMsg, memberRefreshedMsg,
		planDoneMsg, stepDoneMsg, welcomeOpenedMsg, welcomeSentMsg, emailOpenedMsg, emailSentMsg:
		return true
	}
	return false
}

// viewTask is the bottom line while a task runs, e.g.
// "⠙ Opening the welcome email · Looking up their forum invite · 2s".
func (a App) viewTask() string {
	s := a.styles
	t := a.task
	spinner := lipgloss.NewStyle().Foreground(s.p.accent).Bold(true).Render(spinnerFrames[a.spin])
	secs := fmt.Sprintf(" · %ds", int(time.Since(t.started).Seconds()))
	room := a.frameWidth() - 4 - lipgloss.Width(secs)
	label := fitWidth(t.label, room)
	line := spinner + " " + s.text.Bold(true).Render(label)
	if t.step != "" && room-lipgloss.Width(label) > 8 {
		line += s.subtle.Render(" · " + fitWidth(t.step, room-lipgloss.Width(label)-3))
	}
	return "  " + line + s.subtle.Render(secs)
}

// connect loads the settings and builds the connectors for background
// work, saying so as it goes: reading API keys is where 1Password may ask
// for Touch ID, which can otherwise look like nothing is happening.
func (a App) connect(ctx context.Context) (*config.Config, []connectors.Connector, error) {
	progress.Report(ctx, "Reading your settings")
	cfg, err := a.deps.LoadConfig()
	if errors.Is(err, config.ErrNotFound) {
		cfg, err = &config.Config{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	progress.Report(ctx, "Reading your API keys (1Password may ask for Touch ID)")
	return cfg, a.deps.Connectors(cfg, a.deps.Secrets(cfg)), nil
}
