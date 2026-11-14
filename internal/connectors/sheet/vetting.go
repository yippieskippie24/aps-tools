package sheet

import (
	"fmt"
	"strings"
	"time"
)

// Members who joined before "App vetted" was used.
//
// The membership chair only started marking "App vetted" recently, so
// long-standing members have a blank in that column. They are not
// unfinished applications. The webmaster can set the date vetting began
// (`vetting_started` in settings, asked for by `aps-tools setup sheet`).
// A member whose start date is before that date, and who isn't marked New,
// is "established": treated as vetted, without anyone having to backfill
// the column. Members who started on or after that date still need
// "App vetted", so the onboarding gate keeps working for new people.

// DateLayout is how dates are written in the settings file (2006-01-02 is
// Go's way of spelling "year-month-day").
const DateLayout = "2006-01-02"

// ParseVettingStarted reads the `vetting_started` setting. A blank setting
// gives the zero time, meaning "every member needs App vetted".
func ParseVettingStarted(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(DateLayout, s); err == nil {
		return t, nil
	}
	if t, ok := ParseDate(s); ok {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("vetting_started = %q in the settings file isn't a date (write it like 2025-03-01, or leave it blank); run: aps-tools setup sheet", s)
}

// FriendlyDate shows a date the way people write it, e.g. "Mar 1, 2025".
func FriendlyDate(t time.Time) string { return t.Format("Jan 2, 2006") }

// Start is the member's start date, if it's filled in and readable.
func (m Member) Start() (time.Time, bool) { return ParseDate(m.StartDate) }

// Established reports whether the member joined before vetting began, so a
// blank "App vetted" doesn't matter. Never true when vettingStarted is the
// zero time (not set), when they're already vetted, when they're marked New
// (still applying), or when their start date is blank or unreadable: when in
// doubt, the tool asks for "App vetted".
func (m Member) Established(vettingStarted time.Time) bool {
	if vettingStarted.IsZero() || m.Vetted || m.New {
		return false
	}
	t, ok := m.Start()
	return ok && t.Before(vettingStarted)
}

// Ready reports whether the member can be set up in the other systems:
// "App vetted" is marked, or they joined before vetting began.
func (m Member) Ready(vettingStarted time.Time) bool {
	return m.Vetted || m.Established(vettingStarted)
}

// VettingPattern shows how "App vetted" lines up with start dates, so the
// webmaster can see when the chair began using it. Counts and one date only:
// no names or emails.
type VettingPattern struct {
	// FirstVetted is the earliest start date on a row marked "App vetted"
	// (zero if no vetted row has a readable start date). It's the suggested
	// vetting start date.
	FirstVetted time.Time
	// Rows not marked "App vetted", split up:
	UnmarkedNew    int // marked New: genuinely still applying
	UnmarkedBefore int // not New, started before FirstVetted
	UnmarkedSince  int // not New, started on or after FirstVetted
	UnmarkedNoDate int // not New, start date blank or unreadable
}

// FindVettingPattern works out the VettingPattern for these members.
func FindVettingPattern(members []Member) VettingPattern {
	var p VettingPattern
	for _, m := range members {
		if !m.Vetted {
			continue
		}
		if t, ok := m.Start(); ok && (p.FirstVetted.IsZero() || t.Before(p.FirstVetted)) {
			p.FirstVetted = t
		}
	}
	for _, m := range members {
		if m.Vetted {
			continue
		}
		t, ok := m.Start()
		switch {
		case m.New:
			p.UnmarkedNew++
		case !ok:
			p.UnmarkedNoDate++
		case !p.FirstVetted.IsZero() && t.Before(p.FirstVetted):
			p.UnmarkedBefore++
		default:
			p.UnmarkedSince++
		}
	}
	return p
}

// Explain describes the pattern in a sentence or two, for discover and setup.
// It returns "" when there's nothing to say (every row is vetted, or no
// vetted row has a start date to compare against).
func (p VettingPattern) Explain() string {
	unmarked := p.UnmarkedNew + p.UnmarkedBefore + p.UnmarkedSince + p.UnmarkedNoDate
	if unmarked == 0 || p.FirstVetted.IsZero() {
		return ""
	}
	s := fmt.Sprintf("The earliest start date on a row marked \"App vetted\" is %s. Of the %d unmarked rows, %d started before then",
		FriendlyDate(p.FirstVetted), unmarked, p.UnmarkedBefore)
	var rest []string
	if p.UnmarkedSince > 0 {
		rest = append(rest, fmt.Sprintf("%d started on or after it", p.UnmarkedSince))
	}
	if p.UnmarkedNew > 0 {
		rest = append(rest, fmt.Sprintf("%d are marked New", p.UnmarkedNew))
	}
	if p.UnmarkedNoDate > 0 {
		rest = append(rest, fmt.Sprintf("%d have no start date", p.UnmarkedNoDate))
	}
	if len(rest) > 0 {
		s += ", " + strings.Join(rest, ", ")
	}
	return s + "."
}
