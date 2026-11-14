// Package tui is the interactive app you see when you run `aps-tools` with no
// command.
//
// It follows Bubble Tea's "Elm architecture":
//   - the model (struct App) holds everything on screen,
//   - Update changes the model in response to key presses and other events,
//   - View draws the model as text.
//
// Screens contain no business logic: they call shared packages such as
// internal/doctor, so the app and the plain commands always agree.
package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/version"
)

// screen identifies which page of the app is showing.
type screen int

const (
	screenHome screen = iota
	screenDoctor
	screenHelp
	screenCheck
	screenOnboard
	screenMembers
)

// App is the whole application's state (the Bubble Tea "model").
type App struct {
	deps doctor.Deps // how to reach settings, Keychain, and systems

	screen screen
	width  int // terminal size, updated when the window is resized
	height int
	styles styles

	// Home screen
	cursor int    // which menu item is highlighted
	status string // one-line message under the menu

	// Test connections screen
	checking bool
	spin     int // which spinner frame to draw (shared by all screens)
	results  []doctor.Result

	// scroll is how far the current screen's content is scrolled down, in
	// lines (see scroll.go). It goes back to the top on a new screen.
	scroll int

	// Check a member screen (see checkscreen.go)
	member memberCheck

	// Onboard a new member screen (see onboardscreen.go)
	onboard onboarding

	// Members screen (see membersscreen.go)
	members membersView

	// back is where esc returns to from Check and Onboard: the home menu,
	// or the Members list when a member was opened from there.
	back screen
}

// New creates the app. deps is normally doctor.DefaultDeps().
func New(deps doctor.Deps) App {
	s := newStyles(true)
	return App{deps: deps, styles: s, width: 80, height: 24, member: newMemberCheck(s), onboard: newOnboarding(s)}
}

// Run starts the app and blocks until the user quits.
func Run(deps doctor.Deps) error {
	_, err := tea.NewProgram(New(deps)).Run()
	return err
}

// ---- Messages: things that happen, delivered to Update ----

// doctorDoneMsg carries the results of a connection test.
type doctorDoneMsg struct{ results []doctor.Result }

// spinTickMsg advances the "checking…" spinner.
type spinTickMsg struct{}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinTick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return spinTickMsg{} })
}

// runDoctor performs the connection checks in the background, so the screen
// stays responsive while slow systems answer.
func (a App) runDoctor() tea.Cmd {
	deps := a.deps
	return func() tea.Msg {
		return doctorDoneMsg{results: doctor.Run(context.Background(), deps)}
	}
}

// Init runs once at startup. We ask the terminal for its background colour
// so the theme can match a light or dark terminal.
func (a App) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

// Update handles one event and returns the new state.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := a.screen
	m, cmd := a.update(msg)
	a = m.(App)
	if a.screen != before {
		a.scroll = 0 // every screen opens at the top
	}
	a.scroll = min(a.scroll, a.maxScroll()) // e.g. after the window grows
	return a, cmd
}

func (a App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		a.styles = newStyles(msg.IsDark())
		a.member.restyle(a.styles)
		a.onboard.restyle(a.styles)
		return a, nil

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil

	case spinTickMsg:
		busy := a.onboard.phase == obPlanning || a.onboard.phase == obRunning || a.members.loading || a.members.opening
		if !a.checking && !a.member.running && !busy {
			return a, nil
		}
		a.spin = (a.spin + 1) % len(spinnerFrames)
		return a, spinTick()

	case doctorDoneMsg:
		a.checking, a.scroll = false, 0
		a.results = msg.results
		return a, nil

	case checkDoneMsg:
		a.member.running = false
		if msg.err != nil {
			a.member.err = msg.err.Error()
			return a, a.member.input.Focus()
		}
		a.member.report, a.scroll = &msg.report, 0
		return a, nil

	case copiedMsg:
		if msg.err != nil {
			a.member.notice = "Couldn't copy: " + msg.err.Error()
		} else {
			a.member.notice = "Report copied — paste it into an email or note."
		}
		return a, nil

	case memberEmailOpenedMsg, memberEmailSentMsg:
		return a.memberEmailMsg(msg), nil

	case rosterDoneMsg, memberRefreshedMsg:
		return a.membersMsg(msg), nil

	case welcomeOpenedMsg, welcomeSentMsg:
		return a.welcomeMsg(msg), nil

	case planDoneMsg, stepDoneMsg, obCopiedMsg:
		if _, ok := msg.(planDoneMsg); ok {
			a.scroll = 0
		}
		a, cmd, _ := a.onboardMsg(msg)
		return a, cmd

	case tea.KeyPressMsg:
		// ctrl+c always quits, from anywhere.
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if scrolled, ok := a.scrollKey(msg); ok {
			return scrolled, nil
		}
		switch a.screen {
		case screenHome:
			return a.updateHome(msg)
		case screenDoctor:
			return a.updateDoctor(msg)
		case screenHelp:
			return a.updateHelp(msg)
		case screenCheck:
			return a.updateCheck(msg)
		case screenOnboard:
			return a.updateOnboard(msg)
		case screenMembers:
			return a.updateMembers(msg)
		}
	}
	return a, nil
}

func (a App) updateHome(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return a, tea.Quit
	case "up", "k":
		a.cursor = (a.cursor - 1 + len(menu)) % len(menu)
		a.status = ""
	case "down", "j":
		a.cursor = (a.cursor + 1) % len(menu)
		a.status = ""
	case "?":
		a.screen = screenHelp
	case "enter", "space":
		return a.choose(menu[a.cursor])
	default:
		// Number keys jump straight to a menu item.
		for i, item := range menu {
			if msg.String() == item.key {
				a.cursor = i
				return a.choose(item)
			}
		}
	}
	return a, nil
}

// choose acts on a menu item.
func (a App) choose(item menuItem) (tea.Model, tea.Cmd) {
	switch item.action {
	case actionMembers:
		a.back = screenHome
		return a.openMembers()
	case actionCheck:
		a.back = screenHome
		return a.openCheck()
	case actionOnboard:
		a.back = screenHome
		return a.openOnboard()
	case actionDoctor:
		a.screen = screenDoctor
		a.checking = true
		a.results = nil
		return a, tea.Batch(a.runDoctor(), spinTick())
	case actionHelp:
		a.screen = screenHelp
	case actionQuit:
		return a, tea.Quit
	case actionComingSoon:
		a.status = item.title + " arrives in " + item.phase + ". See docs/design.md for the roadmap."
	}
	return a, nil
}

func (a App) updateDoctor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "backspace":
		a.screen = screenHome
	case "r":
		if !a.checking {
			a.checking = true
			a.results = nil
			return a, tea.Batch(a.runDoctor(), spinTick())
		}
	case "?":
		a.screen = screenHelp
	}
	return a, nil
}

func (a App) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "backspace", "?", "enter":
		a.screen = screenHome
	}
	return a, nil
}

// ---- Drawing ----

// View draws the current screen, filling the terminal window: the title
// bar across the top, the screen's content in a card that takes up the
// rest, and the keys along the bottom. Content taller than the card
// scrolls (see scroll.go).
func (a App) View() tea.View {
	w := a.frameWidth()
	body, keys := a.screenContent(w)

	page := lipgloss.JoinVertical(lipgloss.Left,
		a.viewHeader(w),
		a.viewCard(body, w, a.cardHeight()),
		keys,
	)

	v := tea.NewView(lipgloss.NewStyle().PaddingLeft(1).Render(page))
	v.AltScreen = true // full-window app; your terminal is restored on exit
	v.WindowTitle = "APS Webmaster"
	return v
}

// screenContent is the current screen's content (inside the card) and its
// key help. w is the full frame width.
func (a App) screenContent(w int) (body, keys string) {
	switch a.screen {
	case screenHome:
		return a.viewHome(w), a.keyHelp("↑/↓", "move", "enter", "select", "?", "help", "q", "quit")
	case screenDoctor:
		return a.viewDoctor(w), a.keyHelp("r", "re-check", "esc", "back", "?", "help")
	case screenHelp:
		return a.viewHelp(w), a.keyHelp("esc", "back")
	case screenCheck:
		return a.viewCheck(w), a.checkKeys()
	case screenOnboard:
		return a.viewOnboard(w), a.onboardKeys()
	case screenMembers:
		return a.viewMembers(w), a.membersKeys()
	}
	return "", ""
}

// minWidth and minHeight are the smallest window the layout is designed
// for; anything smaller is drawn at this size and the terminal clips it.
const (
	minWidth  = 40
	minHeight = 12
)

// frameWidth is the width of the title bar and card: the whole window,
// less a one-column margin each side.
func (a App) frameWidth() int {
	return max(a.width-2, minWidth)
}

// cardHeight is the height of the content card, border included: the
// window less the title bar (3 lines) and the key help (1 line).
func (a App) cardHeight() int {
	return max(a.height, minHeight) - 4
}

// innerWidth is the width available to text inside the card (the frame
// less the card's border and side padding). Screens lay out to this.
func innerWidth(w int) int { return w - 6 }

// viewHeader draws the title bar: app name on the left, version on the right.
func (a App) viewHeader(w int) string {
	s := a.styles
	title := s.title.Render("◆ APS Webmaster")
	ver := s.version.Render(shortVersion())
	inner := w - 2 - 4 // minus border and padding
	gap := inner - lipgloss.Width(title) - lipgloss.Width(ver)
	if gap < 1 {
		gap = 1
	}
	return s.header.Render(title + strings.Repeat(" ", gap) + ver)
}

func shortVersion() string {
	if version.Version == "dev" {
		return "dev build"
	}
	return version.Version
}

// keyHelp renders "key action · key action" pairs for the footer.
func (a App) keyHelp(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, a.styles.footerKey.Render(pairs[i])+" "+a.styles.subtle.Render(pairs[i+1]))
	}
	return "  " + strings.Join(parts, a.styles.subtle.Render("  ·  "))
}
