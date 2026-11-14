package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/doctor"
)

// ---- Test connections screen ----

// statusIcon returns the coloured symbol for a check result.
func (a App) statusIcon(st doctor.Status) string {
	p := a.styles.p
	switch st {
	case doctor.OK:
		return lipgloss.NewStyle().Foreground(p.ok).Bold(true).Render("✓")
	case doctor.Info:
		return lipgloss.NewStyle().Foreground(p.info).Bold(true).Render("i")
	case doctor.Warn:
		return lipgloss.NewStyle().Foreground(p.warn).Bold(true).Render("!")
	default:
		return lipgloss.NewStyle().Foreground(p.fail).Bold(true).Render("✗")
	}
}

func (a App) viewDoctor(w int) string {
	s := a.styles
	inner := w - 6 // card border + padding

	if a.checking {
		spinner := lipgloss.NewStyle().Foreground(s.p.accent).Render(spinnerFrames[a.spin])
		return s.heading.Render("Test connections") + "\n" +
			spinner + " " + s.subtle.Render("Checking your setup…")
	}

	// Line up the details in a column after the longest check name.
	nameWidth := 0
	for _, r := range a.results {
		if n := lipgloss.Width(r.Name); n > nameWidth {
			nameWidth = n
		}
	}
	nameStyle := s.text.Bold(true).Width(nameWidth + 2)
	detailWidth := inner - 2 - (nameWidth + 2)

	var rows []string
	for _, r := range a.results {
		line := lipgloss.JoinHorizontal(lipgloss.Top,
			a.statusIcon(r.Status)+" ",
			nameStyle.Render(r.Name),
			s.subtle.Width(detailWidth).Render(r.Detail),
		)
		rows = append(rows, line)
		if r.Fix != "" {
			fix := lipgloss.JoinHorizontal(lipgloss.Top, s.fixArrow.Render("→ "), s.text.Width(detailWidth-2).Render(r.Fix))
			rows = append(rows, lipgloss.NewStyle().PaddingLeft(2+nameWidth+2).Render(fix))
		}
	}

	summaryText, summaryColor := doctor.Summary(a.results), s.p.ok
	switch doctor.Worst(a.results) {
	case doctor.Fail:
		summaryColor = s.p.fail
	case doctor.Warn:
		summaryColor = s.p.warn
	}
	summary := lipgloss.NewStyle().Foreground(summaryColor).Bold(true).Render(summaryText)

	return s.heading.Render("Test connections") + "\n" +
		strings.Join(rows, "\n") + "\n\n" + summary
}

// ---- Help screen ----

func (a App) viewHelp(w int) string {
	s := a.styles
	// key renders a "key  description" row; width sets the key column so
	// each section's descriptions line up.
	key := func(width int, k, desc string) string {
		return s.footerKey.Width(width).Render(k) + s.text.Render(desc)
	}
	section := func(title string) string {
		return lipgloss.NewStyle().Bold(true).Foreground(s.p.accent).Render(title)
	}

	body := strings.Join([]string{
		s.heading.Render("Help"),
		section("Moving around"),
		key(12, "↑ ↓  j k", "Move up and down"),
		key(12, "enter", "Choose the highlighted item"),
		key(12, "1 – 7", "Jump straight to a menu item"),
		key(12, "esc  q", "Go back (or quit from the home screen)"),
		key(12, "ctrl+c", "Quit from anywhere"),
		"",
		section("Where things are kept"),
		s.text.Render("Settings   ") + s.subtle.Render("~/.config/aps-tools/config.toml"),
		s.text.Render("           ") + s.subtle.Render("(run: aps-tools config path)"),
		s.text.Render("API keys   ") + s.subtle.Render("Keychain Access app → search \"aps-tools\""),
		s.text.Render("Changes    ") + s.subtle.Render("activity.log, next to the settings file"),
		s.text.Render("           ") + s.subtle.Render("(run: aps-tools log)"),
		"",
		section("Documentation"),
		s.subtle.Render("github.com/yippieskippie24/aps-tools → docs/"),
		s.subtle.Render("New webmaster? Start with docs/handover.md"),
		"",
		section("Commands outside the app"),
		key(19, "aps-tools doctor", "Test connections"),
		key(19, "aps-tools log", "Recent changes the tool made"),
		key(19, "aps-tools --help", "List every command"),
	}, "\n")

	return body
}
