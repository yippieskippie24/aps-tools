package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// action is what happens when a menu item is chosen.
type action int

const (
	actionComingSoon action = iota // feature not built yet; explain when it arrives
	actionMembers
	actionCheck
	actionOnboard
	actionDoctor
	actionHelp
	actionQuit
)

// menuItem is one entry on the home screen.
type menuItem struct {
	key    string // number key that jumps straight to this item
	title  string
	desc   string
	action action
	phase  string // for actionComingSoon: which phase delivers it
}

// menu is the home screen, top to bottom. To add a feature, add an item here
// and handle its action in App.choose (app.go).
var menu = []menuItem{
	{key: "1", title: "Members", desc: "Everyone on the sheet, grouped by what they still need", action: actionMembers},
	{key: "2", title: "Check a member", desc: "See a member's status in every APS system", action: actionCheck},
	{key: "3", title: "Onboard a new member", desc: "Set up a new member everywhere, then draft their welcome email", action: actionOnboard},
	{key: "4", title: "Test connections", desc: "Make sure the tool can reach every system", action: actionDoctor},
	{key: "5", title: "Help", desc: "Keys, documentation, and where things are kept", action: actionHelp},
	{key: "6", title: "Quit", desc: "Close the app", action: actionQuit},
}

func (a App) viewHome(w int) string {
	s := a.styles
	var b strings.Builder
	b.WriteString(s.heading.Render("What would you like to do?"))
	b.WriteString("\n")

	// On a short window, drop the blank lines between items, then the
	// descriptions of all but the highlighted item, so every item fits.
	room := a.cardInnerHeight() - 2 // less the heading
	gaps := room >= 3*len(menu)-1
	descs := room >= 2*len(menu)

	for i, item := range menu {
		title := item.key + "  " + item.title
		badge := ""
		if item.action == actionComingSoon {
			badge = "  " + s.badge.Render("SOON")
		}
		if i == a.cursor {
			// Highlighted row: accent bar on the left, tinted background
			// spanning the full width.
			// Each line is padded to the full width with the tint colour so
			// the highlight is one solid block with no gaps.
			lineW := innerWidth(w) - 2 // minus the left bar and its padding
			fill := lipgloss.NewStyle().Background(s.p.selected).Width(lineW)
			titleLine := s.selTitle.Render(title)
			if badge != "" {
				titleLine += s.selTitle.Render("  ") + s.badge.Background(s.p.selected).Render("SOON")
			}
			row := fill.Render(titleLine) + "\n" + fill.Render(s.selDesc.Render("   "+item.desc))
			b.WriteString(s.selRow.Render(row))
		} else {
			b.WriteString(s.itemTitle.Render(title) + badge)
			if descs {
				b.WriteString("\n" + s.itemDesc.Render("   "+item.desc))
			}
		}
		b.WriteString("\n")
		if gaps && i < len(menu)-1 {
			b.WriteString("\n")
		}
	}

	if a.status != "" {
		b.WriteString("\n")
		b.WriteString(s.statusLine.Width(innerWidth(w)).Render("  " + a.status))
	}
	return b.String()
}
