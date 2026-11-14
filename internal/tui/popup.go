package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// This file is the popup: a box drawn over whatever screen is showing, for
// choosing what to do (a member's actions, which email to send) and for
// confirming changes ("Make 3 changes for Robin?"). One popup shows at a
// time; esc goes back to the popup it came from, or closes it, leaving you
// exactly where you were.

// popItem is one choice in a popup.
type popItem struct {
	label     string
	off       string // why it can't be chosen right now ("" = it can)
	suggested bool
	// run does the choice. It gets the app with the popup already closed,
	// so it can open another popup or start a task.
	run func(a App) (App, tea.Cmd)
}

// popup is a box of choices over the current screen.
type popup struct {
	title string
	sub   string   // a line under the title, e.g. their email
	lines []string // already-styled lines between the title and the choices
	items []popItem
	pick  int
	// An optional text box (e.g. the reason for an override). When there's
	// one, enter submits it to onInput instead of choosing an item.
	input      *textinput.Model
	inputErr   string
	onInput    func(a App, value string) (App, tea.Cmd)
	back       *popup // esc returns here; nil closes the popup
	footerHint string // overrides the default key help inside the box
}

// openPopup shows p, remembering the popup it came from (if any) for esc.
func (a App) openPopup(p *popup) (App, tea.Cmd) {
	if p.back == nil {
		p.back = a.pop
		if p.back == nil {
			p.back = a.popFrom // opened by a choice in another popup
		}
	}
	p.pick = firstAvailable(p.items)
	a.pop = p
	if p.input != nil {
		return a, p.input.Focus()
	}
	return a, nil
}

// closePopups closes every popup.
func (a App) closePopups() App {
	a.pop = nil
	return a
}

func firstAvailable(items []popItem) int {
	for i, it := range items {
		if it.off == "" {
			return i
		}
	}
	return 0
}

// runChoice runs item it of popup p. The popup is closed first, but
// remembered in popFrom while the choice runs, so a popup the choice opens
// (e.g. "Email them…" opening the list of emails) can go back to it.
func (a App) runChoice(p *popup, it popItem) (tea.Model, tea.Cmd) {
	a.pop, a.popFrom = nil, p
	a, cmd := it.run(a)
	a.popFrom = nil
	return a, cmd
}

// updatePopup handles a key while a popup is open.
func (a App) updatePopup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := a.pop
	key := msg.String()
	if key == "esc" {
		a.pop = p.back
		return a, nil
	}
	if p.input != nil {
		if key == "enter" {
			if p.onInput != nil {
				return p.onInput(a, p.input.Value())
			}
			return a, nil
		}
		var cmd tea.Cmd
		*p.input, cmd = p.input.Update(msg)
		p.inputErr = ""
		return a, cmd
	}
	n := len(p.items)
	if n == 0 {
		if key == "enter" {
			a.pop = p.back
		}
		return a, nil
	}
	move := func(step int) {
		for i := 0; i < n; i++ {
			p.pick = (p.pick + step + n) % n
			if p.items[p.pick].off == "" {
				return
			}
		}
	}
	switch key {
	case "up", "k", "shift+tab":
		move(-1)
	case "down", "j", "tab":
		move(1)
	case "enter":
		it := p.items[p.pick]
		if it.off != "" || it.run == nil {
			return a, nil
		}
		return a.runChoice(p, it)
	default:
		// 1–9 pick an item directly.
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			if i := int(key[0] - '1'); i < n && p.items[i].off == "" && p.items[i].run != nil {
				return a.runChoice(p, p.items[i])
			}
		}
	}
	return a, nil
}

// popupWidth is how wide popups are for a frame w wide.
func popupWidth(w int) int {
	return min(max(w*3/4, 44), w-4, 90)
}

// viewPopup draws the popup box (without placing it).
func (a App) viewPopup(w int) string {
	s := a.styles
	p := a.pop
	width := popupWidth(w)
	inner := width - 4 // border + padding
	var b strings.Builder
	b.WriteString(s.text.Bold(true).Width(inner).Render(p.title))
	if p.sub != "" {
		b.WriteString("\n" + s.subtle.Width(inner).Render(p.sub))
	}
	if len(p.lines) > 0 {
		b.WriteString("\n")
		for _, l := range p.lines {
			b.WriteString("\n" + lipgloss.NewStyle().Width(inner).Render(l))
		}
	}
	if p.input != nil {
		p.input.SetWidth(inner - 4)
		b.WriteString("\n\n" + p.input.View())
		if p.inputErr != "" {
			b.WriteString("\n" + lipgloss.NewStyle().Foreground(s.p.fail).Width(inner).Render(p.inputErr))
		}
	}
	if len(p.items) > 0 {
		b.WriteString("\n")
		for i, it := range p.items {
			label := it.label
			style := s.text
			marker := "  "
			switch {
			case it.off != "":
				style = s.subtle
				label += " (" + it.off + ")"
			case i == p.pick:
				style = lipgloss.NewStyle().Foreground(s.p.accent).Bold(true)
				marker = "▸ "
			}
			line := marker + style.Render(label)
			if it.suggested && it.off == "" {
				line += "  " + lipgloss.NewStyle().Foreground(s.p.ok).Render("suggested")
			}
			b.WriteString("\n" + lipgloss.NewStyle().Width(inner).Render(line))
		}
	}
	hint := p.footerHint
	if hint == "" {
		switch {
		case p.input != nil:
			hint = "enter OK · esc back"
		case len(p.items) == 0:
			hint = "enter or esc close"
		default:
			hint = "↑/↓ choose · enter do it · esc back"
		}
	}
	b.WriteString("\n\n" + s.subtle.Render(hint))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.p.accent).
		// No background colour: styled words inside would reset it, leaving
		// patches. The box's own spaces still hide the screen behind it.
		Padding(0, 1).
		Width(width)
	return box.Render(b.String())
}

// overlay draws the popup on top of page, centred across and a little
// below the top.
func (a App) overlay(page string) string {
	if a.pop == nil {
		return page
	}
	box := a.viewPopup(a.frameWidth())
	pw, ph := lipgloss.Width(page), lipgloss.Height(page)
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x := max((pw-bw)/2, 0)
	y := max(min(5, ph-bh), 0)
	// A compositor places each layer at its own position (a layer composed
	// straight onto a canvas would ignore X and Y and replace the page).
	// The screen behind is dimmed (its colours dropped, drawn in the subtle
	// colour) so the popup is clearly what has your attention.
	dim := strings.Split(ansi.Strip(page), "\n")
	for i, l := range dim {
		dim[i] = a.styles.subtle.Render(l)
	}
	c := lipgloss.NewCanvas(pw, ph)
	c.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(dim, "\n")),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	))
	return c.Render()
}
