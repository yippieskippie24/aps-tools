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
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/check"
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
	screenMembers
	screenLog
	screenFollowups
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

	// Members screen (see membersscreen.go)
	members membersView

	// The quick member list for Check a member (see checkscreen.go), and
	// how it's sorted (an index into checkSorts; kept while the app is open).
	people     people
	peopleSort int

	// Activity log screen (see logscreen.go)
	log logView

	// task is the background work running now, shown along the bottom
	// (see task.go).
	task task

	// pop is the popup showing over the screen, if any (see popup.go).
	pop *popup

	// flash is a one-line result ("✓ Onboarded Robin Example") shown along
	// the bottom until the next key press.
	flash string
	// popFrom is the popup whose choice is running right now (see runChoice
	// in popup.go); it's only set for that moment.
	popFrom *popup
	// followups is the Follow-ups screen (followupscreen.go).
	followups followupsView
	// queue is a list of members being emailed one by one, if any.
	queue *emailQueue

	// checked keeps full check results from Check a member, by email, so
	// the details pane can show every system (see checkscreen.go).
	checked map[string]check.Report

	// back is where esc returns to from Check and Onboard: the home menu,
	// or the Members list when a member was opened from there.
	back screen
}

// New creates the app. deps is normally doctor.DefaultDeps().
func New(deps doctor.Deps) App {
	s := newStyles(true)
	return App{deps: deps, styles: s, width: 80, height: 24, member: newMemberCheck(s), log: newLogView(s)}
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
func (a App) runDoctor() (App, tea.Cmd) {
	ctx, watch, ok := a.begin("Testing connections")
	if !ok {
		return a, nil
	}
	a.checking, a.results = true, nil
	deps := a.deps
	return a, tea.Batch(watch, func() tea.Msg {
		return doctorDoneMsg{results: doctor.Run(ctx, deps)}
	})
}

// Init runs once at startup. We ask the terminal for its background colour
// so the theme can match a light or dark terminal.
func (a App) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

// Update handles one event and returns the new state.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := a.screen
	if p, ok := msg.(progressMsg); ok {
		return a.onProgress(p)
	}
	// A finished result ends its task before it's handled, so the handler
	// can start a follow-up task (e.g. re-checking someone after
	// onboarding them).
	if finishesTask(msg) {
		a.finish()
	}
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
		a.log.restyle(a.styles)
		return a, nil

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil

	case spinTickMsg:
		if !a.task.active {
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

	case emailOpenedMsg, popPlanMsg, popRunMsg, popEmailSentMsg, popResendMsg, popCheckedMsg, planCopiedMsg:
		return a.memberMsg(msg)

	case peopleDoneMsg:
		return a.peopleMsg(msg), nil

	case rosterDoneMsg, memberRefreshedMsg:
		return a.membersMsg(msg), nil

	case followupDoneMsg:
		return a.followupMsg(msg)
	case groupOpenedMsg, groupSentMsg:
		return a.groupMsg(msg)

	case tea.PasteMsg:
		if a.pop != nil && a.pop.input != nil {
			var cmd tea.Cmd
			*a.pop.input, cmd = a.pop.input.Update(msg)
			return a, cmd
		}
		return a.paste(msg)

	case tea.KeyPressMsg:
		// ctrl+c always quits, from anywhere.
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		a.flash = "" // a result stays up until the next key press
		if a.pop != nil {
			return a.updatePopup(msg)
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
		case screenMembers:
			return a.updateMembers(msg)
		case screenLog:
			return a.updateLog(msg)
		case screenFollowups:
			return a.updateFollowups(msg)
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
	case actionFollowups:
		return a.openFollowups()
	case actionCheck:
		a.back = screenHome
		return a.openCheck()
	case actionOnboard:
		// New members are on the sheet, so they're under Members → Needs
		// setup, where Enter onboards them (see memberactions.go).
		a.back = screenHome
		a.members.tab, a.members.cursor, a.members.top = 0, 0, 0
		return a.openMembers()
	case actionLog:
		return a.openLog(), nil
	case actionDoctor:
		a.screen = screenDoctor
		return a.runDoctor()
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
			return a.runDoctor()
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
	switch {
	case a.task.active:
		keys = a.viewTask() // always show that something is happening
	case a.flash != "":
		keys = a.viewFlash()
	}

	page := lipgloss.JoinVertical(lipgloss.Left,
		a.viewHeader(w),
		a.viewCard(body, w, a.cardHeight()),
		keys,
	)

	v := tea.NewView(a.overlay(lipgloss.NewStyle().PaddingLeft(1).Render(page)))
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
	case screenMembers:
		return a.viewMembers(w), a.membersKeys()
	case screenLog:
		return a.viewLog(w), a.logKeys()
	case screenFollowups:
		return a.viewFollowups(w), a.followupKeys()
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

// keyHelp renders "key action · key action" pairs for the footer. Pairs
// are listed most important first: on a narrow window, the ones that don't
// fit are left off, so the footer never wraps.
func (a App) keyHelp(pairs ...string) string {
	room := a.frameWidth()
	sep := a.styles.subtle.Render("  ·  ")
	out := "  "
	for i := 0; i+1 < len(pairs); i += 2 {
		part := a.styles.footerKey.Render(pairs[i]) + " " + a.styles.subtle.Render(pairs[i+1])
		next := out + part
		if i > 0 {
			next = out + sep + part
		}
		if lipgloss.Width(next) > room {
			break
		}
		out = next
	}
	return out
}

// paste sends pasted text (⌘V) to whichever text box is in use. Terminals
// deliver a paste as one PasteMsg rather than as key presses, so without
// this, pasting would do nothing.
func (a App) paste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case a.screen == screenCheck && a.member.report == nil && !a.member.running:
		a.member.input, cmd = a.member.input.Update(msg)
		a.member.cursor, a.member.top, a.member.err = 0, 0, ""
	case a.screen == screenLog && a.log.editing:
		a.log.filter, cmd = a.log.filter.Update(msg)
		a.scroll = 0
	}
	return a, cmd
}
