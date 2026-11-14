package sheet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// vettingRows mimics a sheet where "App vetted" only began in March 2025:
// long-standing members have it blank.
var vettingRows = [][]string{
	header,
	row("3/4/2025", "Y", "", "", "Active", "", "Y", "First", "Vera", "vera@example.com"),
	row("6/1/2025", "Y", "", "", "Active", "", "Y", "Second", "Val", "val@example.com"),
	row("5/1/2019", "Y", "", "", "Active", "", "", "Longtime", "Lee", "lee@example.com"),
	row("1/1/2010", "Y", "", "", "Active", "", "", "Founder", "Fran", "fran@example.com"),
	row("8/1/2025", "Y", "", "", "Active", "", "", "Recent", "Rae", "rae@example.com"),
	row("10/1/2025", "", "", "", "", "Y", "", "Applicant", "Ann", "ann@example.com"),
	row("", "Y", "", "", "Active", "", "", "Nodate", "Ned", "ned@example.com"),
}

var vettingBegan = time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC)

func vettingMembers(t *testing.T) []Member {
	t.Helper()
	cols, ok := FindColumns(vettingRows)
	if !ok {
		t.Fatal("no columns")
	}
	members, _ := Members(vettingRows, cols)
	return members
}

func TestVettingPattern(t *testing.T) {
	p := FindVettingPattern(vettingMembers(t))
	if !p.FirstVetted.Equal(vettingBegan) || p.UnmarkedBefore != 2 || p.UnmarkedSince != 1 || p.UnmarkedNew != 1 || p.UnmarkedNoDate != 1 {
		t.Fatalf("pattern = %+v", p)
	}
	want := "The earliest start date on a row marked \"App vetted\" is Mar 4, 2025. Of the 5 unmarked rows, 2 started before then, 1 started on or after it, 1 are marked New, 1 have no start date."
	if got := p.Explain(); got != want {
		t.Errorf("Explain() =\n%q\nwant\n%q", got, want)
	}
	if got := FindVettingPattern(nil).Explain(); got != "" {
		t.Errorf("nothing to explain, got %q", got)
	}
}

func TestEstablishedMembers(t *testing.T) {
	members := vettingMembers(t)
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		email       string
		withDate    connectors.State
		withoutDate connectors.State
		summary     string // with the date set
		detail      string // with the date set
	}{
		{"vera@example.com", connectors.StateOK, connectors.StateOK, "vetted", ""},
		{"lee@example.com", connectors.StateOK, connectors.StatePending, "joined before", "started before Mar 4, 2025"},
		{"fran@example.com", connectors.StateOK, connectors.StatePending, "joined before", ""},
		{"rae@example.com", connectors.StatePending, connectors.StatePending, "isn't marked yet", ""},
		{"ann@example.com", connectors.StatePending, connectors.StatePending, "still in progress", ""},
		{"ned@example.com", connectors.StatePending, connectors.StatePending, "isn't marked yet", "No readable start date"},
	}
	for _, tc := range cases {
		st := memberStatus(Find(members, tc.email), now, vettingBegan)
		if st.State != tc.withDate || !strings.Contains(st.Summary, tc.summary) || !strings.Contains(strings.Join(st.Details, "\n"), tc.detail) {
			t.Errorf("%s with the date set: got %v %q %v", tc.email, st.State, st.Summary, st.Details)
		}
		if st := memberStatus(Find(members, tc.email), now, time.Time{}); st.State != tc.withoutDate {
			t.Errorf("%s without the date: got %v", tc.email, st.State)
		}
	}
}

func TestDiscoverWithVettingDate(t *testing.T) {
	without := Discovery{Tab: "Members"}
	summarize(&without, vettingRows)
	if without.Vetted != 2 || without.Established != 0 || without.InProgress != 5 {
		t.Errorf("without the date: %+v", without)
	}
	if notes := strings.Join(without.Notes, "\n"); !strings.Contains(notes, "set the date vetting began") {
		t.Errorf("no suggestion to set the date:\n%s", notes)
	}

	with := Discovery{Tab: "Members", VettingStarted: vettingBegan}
	summarize(&with, vettingRows)
	if with.Vetted != 2 || with.Established != 2 || with.InProgress != 3 {
		t.Errorf("with the date: %+v", with)
	}
	notes := strings.Join(with.Notes, "\n")
	if strings.Contains(notes, "set the date vetting began") || !strings.Contains(notes, "didn't start before Mar 4, 2025") {
		t.Errorf("notes with the date set:\n%s", notes)
	}
	for _, private := range []string{"example.com", "Lee", "Example St"} {
		if strings.Contains(notes, private) {
			t.Errorf("notes show private data %q", private)
		}
	}
}

func TestParseVettingStarted(t *testing.T) {
	if got, err := ParseVettingStarted(""); err != nil || !got.IsZero() {
		t.Errorf("blank: %v %v", got, err)
	}
	if got, err := ParseVettingStarted(" 2025-03-04 "); err != nil || !got.Equal(vettingBegan) {
		t.Errorf("ISO date: %v %v", got, err)
	}
	if got, err := ParseVettingStarted("3/4/2025"); err != nil || !got.Equal(vettingBegan) {
		t.Errorf("US date: %v %v", got, err)
	}
	if _, err := ParseVettingStarted("spring"); err == nil || !strings.Contains(err.Error(), "aps-tools setup sheet") {
		t.Errorf("bad date: %v", err)
	}
}

func TestBadVettingDateIsExplained(t *testing.T) {
	c := New(config.Sheet{SpreadsheetID: "sheet-1", Tab: "Members", VettingStarted: "spring"}, secrets.NewMemory())
	if _, err := c.load(context.Background()); err == nil || !strings.Contains(err.Error(), "vetting_started") {
		t.Errorf("err = %v", err)
	}
}
