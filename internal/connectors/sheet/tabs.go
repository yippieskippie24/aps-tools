package sheet

// The membership sheet has several tabs, and the member list's tab is named
// with a date (e.g. "Member Directory May 2026"). A fresh dated copy may be
// made from time to time. This file helps setup suggest the right tab, and
// warns when a newer copy of the saved tab appears, so the tool doesn't
// quietly keep reading an old list.

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	monthYear = regexp.MustCompile(`(?i)\b(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?[\s,-]*(\d{4})\b`)
	yearOnly  = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	dateBits  = regexp.MustCompile(`(?i)\b(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\b|\b\d+\b`)
)

// tabDate finds a month and year (or just a year) in a tab name.
func tabDate(name string) (time.Time, bool) {
	if m := monthYear.FindStringSubmatch(name); m != nil {
		year, _ := strconv.Atoi(m[2])
		if t, err := time.Parse("Jan", strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:3])); err == nil {
			return time.Date(year, t.Month(), 1, 0, 0, 0, 0, time.UTC), true
		}
	}
	if y := yearOnly.FindString(name); y != "" {
		year, _ := strconv.Atoi(y)
		return time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

// tabStem is a tab name without its date: "Member Directory May 2026" →
// "member directory". Tabs with the same stem are copies of one list.
func tabStem(name string) string {
	return normHeader(dateBits.ReplaceAllString(name, " "))
}

// NewerTab returns a tab that looks like a newer dated copy of current
// (same name apart from a later date), or "" if there isn't one.
func NewerTab(tabs []string, current string) string {
	curDate, ok := tabDate(current)
	if !ok {
		return ""
	}
	stem, best, bestDate := tabStem(current), "", curDate
	for _, t := range tabs {
		if t == current || tabStem(t) != stem {
			continue
		}
		if d, ok := tabDate(t); ok && d.After(bestDate) {
			best, bestDate = t, d
		}
	}
	return best
}

// SuggestTab picks the tab setup should offer first: the saved one if it
// still exists (or its newest copy), else the newest tab whose name
// mentions members, else the first tab.
func SuggestTab(tabs []string, saved string) string {
	for _, t := range tabs {
		if t == saved {
			if newer := NewerTab(tabs, saved); newer != "" {
				return newer
			}
			return saved
		}
	}
	best, bestDate := "", time.Time{}
	for _, t := range tabs {
		if !strings.Contains(strings.ToLower(t), "member") {
			continue
		}
		d, _ := tabDate(t)
		if best == "" || d.After(bestDate) {
			best, bestDate = t, d
		}
	}
	if best != "" {
		return best
	}
	if len(tabs) > 0 {
		return tabs[0]
	}
	return ""
}
