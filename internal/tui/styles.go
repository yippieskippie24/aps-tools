package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// This file is the app's "theme": every colour and text style lives here, so
// the look can be changed in one place.
//
// Colours come in light/dark pairs. The app asks the terminal for its
// background colour at startup and picks the matching set, so it stays
// readable on both light and dark terminal themes.

// palette is the set of colours used across the app.
type palette struct {
	accent   color.Color // brand colour: title, selection bar
	accent2  color.Color // second brand colour for the title gradient
	text     color.Color // normal text
	subtle   color.Color // secondary text (descriptions, hints)
	faint    color.Color // borders and dividers
	selected color.Color // background of the highlighted menu row
	ok       color.Color
	warn     color.Color
	fail     color.Color
	info     color.Color
}

func newPalette(isDark bool) palette {
	// pick chooses the light-theme or dark-theme colour.
	pick := lipgloss.LightDark(isDark)
	return palette{
		accent:   pick(lipgloss.Color("#5A4FCF"), lipgloss.Color("#9D8CFF")),
		accent2:  pick(lipgloss.Color("#0E8A9E"), lipgloss.Color("#5BD6E8")),
		text:     pick(lipgloss.Color("#1F2328"), lipgloss.Color("#E6E6EF")),
		subtle:   pick(lipgloss.Color("#6E7781"), lipgloss.Color("#9A9AB0")),
		faint:    pick(lipgloss.Color("#D0D7DE"), lipgloss.Color("#3A3A4E")),
		selected: pick(lipgloss.Color("#EEEBFF"), lipgloss.Color("#26233F")),
		ok:       pick(lipgloss.Color("#1A7F37"), lipgloss.Color("#56D364")),
		warn:     pick(lipgloss.Color("#9A6700"), lipgloss.Color("#E3B341")),
		fail:     pick(lipgloss.Color("#CF222E"), lipgloss.Color("#FF7B72")),
		info:     pick(lipgloss.Color("#0969DA"), lipgloss.Color("#79C0FF")),
	}
}

// styles holds ready-to-use Lip Gloss styles built from a palette.
type styles struct {
	p palette

	header     lipgloss.Style // the bordered title bar
	title      lipgloss.Style
	version    lipgloss.Style
	heading    lipgloss.Style // section heading, e.g. "What would you like to do?"
	text       lipgloss.Style
	subtle     lipgloss.Style
	itemTitle  lipgloss.Style // menu item, not highlighted
	itemDesc   lipgloss.Style
	selRow     lipgloss.Style // wrapper for the highlighted menu item
	selTitle   lipgloss.Style
	selDesc    lipgloss.Style
	badge      lipgloss.Style // small "SOON" label
	card       lipgloss.Style // rounded box around screen content
	footerKey  lipgloss.Style
	statusLine lipgloss.Style // message shown under the menu
	fixArrow   lipgloss.Style
}

func newStyles(isDark bool) styles {
	p := newPalette(isDark)
	s := styles{p: p}

	s.header = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForegroundBlend(p.accent, p.accent2). // gradient border
		Padding(0, 2)
	s.title = lipgloss.NewStyle().Bold(true).Foreground(p.accent)
	s.version = lipgloss.NewStyle().Foreground(p.subtle)
	s.heading = lipgloss.NewStyle().Bold(true).Foreground(p.text).MarginBottom(1)
	s.text = lipgloss.NewStyle().Foreground(p.text)
	s.subtle = lipgloss.NewStyle().Foreground(p.subtle)

	// Menu rows: two lines (title + description), indented to line up with
	// the highlighted row's left bar.
	s.itemTitle = lipgloss.NewStyle().Foreground(p.text).PaddingLeft(2)
	s.itemDesc = lipgloss.NewStyle().Foreground(p.subtle).PaddingLeft(2)
	s.selRow = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true). // left bar only
		BorderForeground(p.accent).
		Background(p.selected).
		PaddingLeft(1)
	s.selTitle = lipgloss.NewStyle().Bold(true).Foreground(p.accent).Background(p.selected)
	s.selDesc = lipgloss.NewStyle().Foreground(p.text).Background(p.selected)
	s.badge = lipgloss.NewStyle().Foreground(p.warn).Bold(true)

	s.card = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.faint).
		Padding(1, 2)

	s.footerKey = lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	s.statusLine = lipgloss.NewStyle().Foreground(p.info).Italic(true)
	s.fixArrow = lipgloss.NewStyle().Foreground(p.accent)
	return s
}
