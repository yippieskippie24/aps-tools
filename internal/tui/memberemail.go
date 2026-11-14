package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
	"github.com/yippieskippie24/aps-tools/internal/roster"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// This file is emailing a member from the Members screen: press e, choose
// one of the email templates (the one that fits them best is first and
// marked "suggested"), and it opens in the browser ready to copy into
// Mail. Then the screen asks "Did you send it?" and a yes goes in the
// activity log. Choosing and building the email is done by
// internal/welcome; this file only draws it.

// memberEmailOpenedMsg reports that a member's email was opened (or why not).
type memberEmailOpenedMsg struct {
	kind    welcome.Kind
	notes   []string
	missing []string
	err     error
}

// memberEmailSentMsg reports that "sent" was recorded (or why not).
type memberEmailSentMsg struct{ err error }

// startEmailPicker shows the list of emails for e, the suggested one first.
func (a *App) startEmailPicker(e roster.Entry) {
	mv := &a.members
	suggested := welcome.Suggest(e.Sheet.State, mv.roster.States(e))
	mv.choices = []welcome.Kind{suggested}
	for _, k := range welcome.Kinds {
		if k.ID != suggested.ID {
			mv.choices = append(mv.choices, k)
		}
	}
	mv.picking, mv.pick, mv.emailFor, mv.notice = true, 0, e, ""
}

func (a App) updateMemberEmail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mv := &a.members
	key := msg.String()
	switch {
	case mv.opening:
		return a, nil
	case mv.sending != nil:
		k := *mv.sending
		mv.sending = nil
		if key == "y" {
			return a, a.recordEmailSent(k, mv.emailFor.Email)
		}
		mv.notice = "Not recorded as sent. Press e to open it again."
		return a, nil
	}
	// Choosing an email.
	switch key {
	case "esc", "q":
		mv.picking = false
	case "up", "k":
		mv.pick = (mv.pick - 1 + len(mv.choices)) % len(mv.choices)
	case "down", "j":
		mv.pick = (mv.pick + 1) % len(mv.choices)
	case "enter":
		mv.picking, mv.opening = false, true
		return a, a.openMemberEmail(mv.choices[mv.pick], mv.emailFor)
	}
	return a, nil
}

// openMemberEmail builds and opens the email in the background.
func (a App) openMemberEmail(k welcome.Kind, e roster.Entry) tea.Cmd {
	deps := a.deps
	who := e.Email
	if n := e.Name(); n != "" {
		who = n + " <" + e.Email + ">"
	}
	return func() tea.Msg {
		cfg, err := deps.LoadConfig()
		if errors.Is(err, config.ErrNotFound) {
			cfg, err = &config.Config{}, nil
		}
		if err != nil {
			return memberEmailOpenedMsg{kind: k, err: err}
		}
		link, err := welcome.InviteLink(context.Background(), deps.Connectors(cfg, deps.Secrets(cfg)), e.Email)
		switch {
		case errors.Is(err, welcome.ErrNoInvite) && k.NeedsInvite:
			return memberEmailOpenedMsg{kind: k, err: errors.New("they don't have a forum invite yet. Press enter on them to open their onboarding, then g to create it")}
		case errors.Is(err, welcome.ErrNoInvite):
			err = nil
		case err != nil:
			return memberEmailOpenedMsg{kind: k, err: err}
		}
		email, err := showEmail(k, cfg, e.Person, who, link)
		return memberEmailOpenedMsg{kind: k, notes: email.Notes, missing: email.Missing, err: err}
	}
}

// recordEmailSent writes the webmaster's "yes, I sent it" to the log.
func (a App) recordEmailSent(k welcome.Kind, email string) tea.Cmd {
	log, logErr := a.activityLog()
	return func() tea.Msg {
		if logErr != nil {
			return memberEmailSentMsg{err: logErr}
		}
		return memberEmailSentMsg{err: onboard.LogEmailSent(log, email, k.Name)}
	}
}

// memberEmailMsg handles the background results.
func (a App) memberEmailMsg(msg tea.Msg) App {
	mv := &a.members
	switch msg := msg.(type) {
	case memberEmailOpenedMsg:
		mv.opening = false
		if msg.err != nil {
			mv.notice = "Couldn't open the " + strings.ToLower(msg.kind.Name) + ": " + msg.err.Error()
			return a
		}
		k := msg.kind
		mv.sending = &k
		parts := append([]string{}, msg.notes...)
		if len(msg.missing) > 0 {
			parts = append(parts, "Fill in after pasting (highlighted in the email): {{"+strings.Join(msg.missing, "}}, {{")+"}}")
		}
		mv.notice = strings.Join(parts, " ")
	case memberEmailSentMsg:
		if msg.err != nil {
			mv.notice = "Couldn't record it: " + msg.err.Error()
		} else {
			mv.notice = "Recorded as sent in the activity log."
		}
	}
	return a
}

// viewMemberEmail draws the email list, or the "Did you send it?" box.
func (a App) viewMemberEmail(inner int) string {
	s := a.styles
	mv := a.members
	who := mv.emailFor.Name()
	if who == "" {
		who = mv.emailFor.Email
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(s.p.accent).Padding(0, 1).Width(inner)
	switch {
	case mv.opening:
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		return box.Render(spinner + " " + s.subtle.Render("Opening the email for "+who+"…"))
	case mv.sending != nil:
		body := s.text.Bold(true).Render("The "+strings.ToLower(mv.sending.Name)+" for "+who+" is open in your browser. Did you send it?") + "\n" +
			s.subtle.Width(inner-4).Render("Copy the To, Subject and Body into Mail and send it. A yes is recorded in the activity log.")
		if mv.notice != "" {
			body += "\n" + lipgloss.NewStyle().Foreground(s.p.warn).Width(inner-4).Render(mv.notice)
		}
		return box.Render(body)
	}
	var rows []string
	rows = append(rows, s.text.Bold(true).Render("Email "+who))
	for i, k := range mv.choices {
		title := k.Name
		if i == 0 {
			title += lipgloss.NewStyle().Foreground(s.p.ok).Render("  suggested")
		}
		marker, style := "  ", s.text
		if i == mv.pick {
			marker, style = lipgloss.NewStyle().Foreground(s.p.accent).Bold(true).Render("▸ "), s.text.Bold(true)
		}
		rows = append(rows, marker+style.Render(title))
		if compact := a.cardInnerHeight() < 2*len(mv.choices)+membersHeader+3; !compact || i == mv.pick {
			rows = append(rows, "    "+s.subtle.Width(inner-8).Render("For "+k.For+"."))
		}
	}
	return box.Render(strings.Join(rows, "\n"))
}
