package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// This file is the "Email a member" screen. Press e wherever a member is
// open (the Members list, their onboarding, or their check) to choose one of
// the email templates: the one that fits them best is first and marked
// "suggested". It opens in the browser, ready to copy into Mail; then the
// screen asks "Did you send it?" and a yes goes in the activity log. esc
// goes back to where you came from. Choosing and building the email is done
// by internal/welcome; this file only draws it.

// emailView holds the state of the Email screen.
type emailView struct {
	person  connectors.Person
	choices []welcome.Kind // the emails, the suggested one first
	pick    int            // highlighted email
	opening bool           // building and opening it
	sending *welcome.Kind  // the email that's open: "Did you send it?"
	notice  string
	back    screen // where esc returns to
}

// emailOpenedMsg reports that the email was opened (or why not).
type emailOpenedMsg struct {
	kind    welcome.Kind
	person  connectors.Person
	notes   []string
	missing []string
	err     error
}

// emailSentMsg reports that "sent" was recorded (or why not).
type emailSentMsg struct {
	kind welcome.Kind
	err  error
}

// openEmail shows the Email screen for p. sheet and states are what the
// tool knows about them (for the suggestion); back is the screen to return
// to.
func (a App) openEmail(p connectors.Person, sheet connectors.State, states map[string]connectors.State) App {
	suggested := welcome.Suggest(sheet, states)
	choices := []welcome.Kind{suggested}
	for _, k := range welcome.Kinds {
		if k.ID != suggested.ID {
			choices = append(choices, k)
		}
	}
	a.email = emailView{person: p, choices: choices, back: a.screen}
	a.screen = screenEmail
	return a
}

func (a App) updateEmail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ev := &a.email
	key := msg.String()
	switch {
	case ev.opening:
		return a, nil
	case ev.sending != nil:
		k := *ev.sending
		ev.sending = nil
		if key == "y" {
			return a, a.recordEmailSent(k, ev.person.Email)
		}
		ev.notice = "Not recorded as sent. Choose it again to reopen it."
		return a, nil
	}
	switch key {
	case "esc", "q":
		a.screen = ev.back
	case "up", "k":
		ev.pick = (ev.pick - 1 + len(ev.choices)) % len(ev.choices)
	case "down", "j":
		ev.pick = (ev.pick + 1) % len(ev.choices)
	case "enter":
		ev.opening, ev.notice = true, ""
		return a, tea.Batch(a.openEmailPage(ev.choices[ev.pick]), spinTick())
	}
	return a, nil
}

// openEmailPage builds and opens the email in the background. It reads the
// member's sheet entry first, for their name, and the forum for their
// invite or username.
func (a App) openEmailPage(k welcome.Kind) tea.Cmd {
	deps, p := a.deps, a.email.person
	return func() tea.Msg {
		cfg, err := deps.LoadConfig()
		if errors.Is(err, config.ErrNotFound) {
			cfg, err = &config.Config{}, nil
		}
		if err != nil {
			return emailOpenedMsg{kind: k, err: err}
		}
		ctx := context.Background()
		conns := deps.Connectors(cfg, deps.Secrets(cfg))
		for _, c := range conns {
			if d, ok := c.(connectors.Directory); ok && len(c.Missing()) == 0 {
				if found, _, err := d.LookupMember(ctx, p.Email); err == nil && found.Name() != "" {
					p = found
				}
			}
		}
		link, err := welcome.InviteLink(ctx, conns, p.Email)
		switch {
		case errors.Is(err, welcome.ErrNoInvite) && k.NeedsInvite:
			return emailOpenedMsg{kind: k, person: p, err: errors.New("they don't have a forum invite yet. Open their onboarding (3 on the home menu, or enter on Members) and press g to create it")}
		case errors.Is(err, welcome.ErrNoInvite):
			err = nil
		case err != nil:
			return emailOpenedMsg{kind: k, person: p, err: err}
		}
		who := p.Email
		if n := p.Name(); n != "" {
			who = n + " <" + p.Email + ">"
		}
		email, err := showEmail(k, cfg, p, who, link)
		return emailOpenedMsg{kind: k, person: p, notes: email.Notes, missing: email.Missing, err: err}
	}
}

// recordEmailSent writes the webmaster's "yes, I sent it" to the log.
func (a App) recordEmailSent(k welcome.Kind, email string) tea.Cmd {
	log, logErr := a.activityLog()
	return func() tea.Msg {
		if logErr != nil {
			return emailSentMsg{kind: k, err: logErr}
		}
		return emailSentMsg{kind: k, err: onboard.LogEmailSent(log, email, k.Name)}
	}
}

// emailMsg handles the background results.
func (a App) emailMsg(msg tea.Msg) App {
	ev := &a.email
	switch msg := msg.(type) {
	case emailOpenedMsg:
		ev.opening = false
		if msg.person.Email != "" {
			ev.person = msg.person
		}
		if msg.err != nil {
			ev.notice = "Couldn't open the " + strings.ToLower(msg.kind.Name) + ": " + msg.err.Error()
			return a
		}
		k := msg.kind
		ev.sending = &k
		parts := append([]string{}, msg.notes...)
		if len(msg.missing) > 0 {
			parts = append(parts, "Fill in after pasting (highlighted in the email): {{"+strings.Join(msg.missing, "}}, {{")+"}}")
		}
		ev.notice = strings.Join(parts, " ")
	case emailSentMsg:
		if msg.err != nil {
			ev.notice = "Couldn't record it: " + msg.err.Error()
		} else {
			ev.notice = msg.kind.Name + " recorded as sent in the activity log. Choose another, or esc to go back."
		}
	}
	return a
}

// emailKeys is the footer for the Email screen.
func (a App) emailKeys() string {
	switch {
	case a.email.opening:
		return "  " + a.styles.subtle.Render("Opening it in your browser…")
	case a.email.sending != nil:
		return a.keyHelp("y", "yes, I sent it", "n", "not yet")
	}
	return a.keyHelp("↑/↓", "choose", "enter", "open it", "esc", "back")
}

func (a App) viewEmail(w int) string {
	s := a.styles
	ev := a.email
	inner := innerWidth(w)
	who := ev.person.Name()
	if who == "" {
		who = ev.person.Email
	} else {
		who += " <" + ev.person.Email + ">"
	}

	var b strings.Builder
	b.WriteString(s.heading.Render("Email a member"))
	b.WriteString("\n")
	b.WriteString(s.text.Bold(true).Render(who) + "\n")
	b.WriteString(s.subtle.Width(inner).Render("Choose an email. It opens in your browser ready to copy into Mail; you send it yourself.") + "\n\n")

	switch {
	case ev.opening:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		b.WriteString(spinner + " " + s.subtle.Render("Filling it in and opening it…"))
		return b.String()
	case ev.sending != nil:
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(s.p.accent).Padding(0, 1).Width(inner)
		body := s.text.Bold(true).Render("The "+strings.ToLower(ev.sending.Name)+" is open in your browser. Did you send it?") + "\n" +
			s.subtle.Width(inner-4).Render("Copy the To, Subject and Body into Mail and send it. A yes is recorded in the activity log.")
		if ev.notice != "" {
			body += "\n" + lipgloss.NewStyle().Foreground(s.p.warn).Width(inner-4).Render(ev.notice)
		}
		b.WriteString(box.Render(body))
		return b.String()
	}

	compact := a.cardInnerHeight() < 2*len(ev.choices)+8
	for i, k := range ev.choices {
		title := k.Name
		if i == 0 {
			title += lipgloss.NewStyle().Foreground(s.p.ok).Render("  suggested")
		}
		marker, style := "  ", s.text
		if i == ev.pick {
			marker, style = lipgloss.NewStyle().Foreground(s.p.accent).Bold(true).Render("▸ "), s.text.Bold(true)
		}
		b.WriteString(marker + style.Render(title) + "\n")
		if !compact || i == ev.pick {
			b.WriteString(s.subtle.PaddingLeft(4).Width(inner).Render("For "+k.For+".") + "\n")
		}
	}
	if ev.notice != "" {
		b.WriteString("\n" + s.statusLine.Width(inner).Render(ev.notice))
	}
	return strings.TrimRight(b.String(), "\n")
}
