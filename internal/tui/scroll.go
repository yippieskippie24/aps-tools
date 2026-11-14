package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// This file makes the app fit any terminal window. Every screen draws its
// content into one card that fills the space between the title bar and the
// key help. When the content is taller than the card, it scrolls: ↑/↓ (or
// j/k) a line at a time, PgUp/PgDn a page, Home/End to the ends. Little
// "more above / more below" markers show there's more to see.

// toBottom scrolls as far down as the content goes (Update limits it), so
// a question or result shown under long content is in view.
const toBottom = 1 << 30

// cardInnerHeight is how many lines of content fit in the card (its height
// less the top and bottom border and padding).
func (a App) cardInnerHeight() int {
	return max(a.cardHeight()-4, 1)
}

// contentLines renders the current screen's content as lines, the same way
// View does, so scrolling can be limited to what's actually there.
func (a App) contentLines() []string {
	w := a.frameWidth()
	body, _ := a.screenContent(w)
	return wrapLines(body, innerWidth(w))
}

// wrapLines wraps body to the card's inner width and splits it into lines,
// so a long line counts as the lines it really takes up.
func wrapLines(body string, width int) []string {
	return strings.Split(lipgloss.NewStyle().Width(width).Render(body), "\n")
}

// maxScroll is the furthest the current screen can scroll.
func (a App) maxScroll() int {
	return max(len(a.contentLines())-a.cardInnerHeight(), 0)
}

// scrolls reports whether the current screen uses the arrow keys for
// scrolling. The home menu uses them to move instead, and so do screens
// that are waiting for typing.
func (a App) scrolls() bool {
	switch a.screen {
	case screenDoctor, screenHelp:
		return true
	case screenCheck:
		return a.member.report != nil && !a.member.running
	case screenOnboard:
		switch a.onboard.phase {
		case obPlan, obDone, obRunning, obConfirm:
			return true
		}
	}
	return false
}

// scrollKey moves the content for a scrolling key. ok is false for any
// other key, which the screen then handles itself.
func (a App) scrollKey(msg tea.KeyPressMsg) (App, bool) {
	if !a.scrolls() {
		return a, false
	}
	page := max(a.cardInnerHeight()-1, 1)
	switch msg.String() {
	case "up", "k":
		a.scroll--
	case "down", "j":
		a.scroll++
	case "pgup", "ctrl+u":
		a.scroll -= page
	case "pgdown", "ctrl+d", "space":
		a.scroll += page
	case "home":
		a.scroll = 0
	case "end":
		a.scroll = a.maxScroll()
	default:
		return a, false
	}
	a.scroll = min(max(a.scroll, 0), a.maxScroll())
	return a, true
}

// viewCard draws body in a card w wide and h tall (border included),
// showing the scrolled-to part when it doesn't all fit.
func (a App) viewCard(body string, w, h int) string {
	s := a.styles
	room := max(h-4, 1)
	lines := wrapLines(body, innerWidth(w))
	total := len(lines)
	if total > room {
		top := min(max(a.scroll, 0), len(lines)-room)
		lines = lines[top : top+room]
		more := lipgloss.NewStyle().Foreground(s.p.accent)
		if top > 0 {
			lines[0] = more.Render("▲ more above  (↑ to scroll)")
		}
		if top+room < total {
			lines[room-1] = more.Render("▼ more below  (↓ to scroll)")
		}
	}
	return s.card.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}
