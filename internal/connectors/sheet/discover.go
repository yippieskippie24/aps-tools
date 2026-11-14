package sheet

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Discovery is a read-only summary of the membership sheet for
// `aps-tools sheet discover`. It holds counts and column names only —
// never names, emails, addresses or phone numbers.
type Discovery struct {
	Title          string
	ServiceAccount string
	Tabs           []string
	Tab            string // the tab that was looked at
	HeaderRow      int    // 1-based, as shown in Google Sheets
	Columns        []ColumnFound
	Rows           int // rows with an email address
	NoEmail        int // non-blank rows without one
	Active         int
	Inactive       int
	OtherStatus    int
	Vetted         int
	Established    int // not vetted, but joined before vetting began
	InProgress     int // not ready yet: not vetted and not established
	// VettingStarted is the saved vetting start date (zero if not set).
	VettingStarted time.Time
	// Pattern shows how "App vetted" lines up with start dates.
	Pattern    VettingPattern
	DuePassed  int
	Duplicates int // email addresses listed on more than one row
	Notes      []string
}

// ColumnFound is one role and where (or whether) it was found.
type ColumnFound struct {
	Role   Role
	Column string // "J", or "" if not found
	Header string
}

// Discover summarises the sheet and the chosen tab (or the first tab if
// tab is empty). vettingStarted is the saved vetting start date, or the
// zero time if there isn't one.
func (c *Client) Discover(ctx context.Context, id, tab string, vettingStarted time.Time) (Discovery, error) {
	d := Discovery{ServiceAccount: c.ServiceAccount(), VettingStarted: vettingStarted}
	s, err := c.GetSpreadsheet(ctx, id)
	if err != nil {
		return d, err
	}
	d.Title, d.Tabs = s.Title, s.Tabs
	if tab == "" && len(s.Tabs) > 0 {
		tab = s.Tabs[0]
	}
	d.Tab = tab
	rows, err := c.Rows(ctx, id, tab)
	if err != nil {
		return d, err
	}
	summarize(&d, rows)
	if newer := NewerTab(d.Tabs, tab); newer != "" {
		d.Notes = append([]string{"There's a newer tab, \"" + newer + "\". If that's the current member list, run `aps-tools setup sheet` to switch to it."}, d.Notes...)
	}
	return d, nil
}

// summarize fills in everything that comes from the tab's cells.
func summarize(d *Discovery, rows [][]string) {
	cols, ok := FindColumns(rows)
	if !ok {
		d.Notes = append(d.Notes, "No email column was found in the first 10 rows of the \""+d.Tab+"\" tab. The tool finds it by a header containing \"mail\" (e.g. \"Email\").")
		return
	}
	d.HeaderRow = cols.HeaderRow + 1
	for _, r := range Roles {
		f := ColumnFound{Role: r}
		if j, ok := cols.Index[r]; ok {
			f.Column, f.Header = ColumnLetter(j), cols.Headers[r]
		}
		d.Columns = append(d.Columns, f)
	}

	members, noEmail := Members(rows, cols)
	d.Rows, d.NoEmail = len(members), noEmail
	seen := map[string]int{}
	for _, m := range members {
		switch {
		case m.Active():
			d.Active++
		case m.Inactive():
			d.Inactive++
		default:
			d.OtherStatus++
		}
		switch {
		case m.Vetted:
			d.Vetted++
		case m.Established(d.VettingStarted):
			d.Established++
		default:
			d.InProgress++
		}
		if t, ok := ParseDate(m.DueDate); ok && t.Before(time.Now()) {
			d.DuePassed++
		}
		for _, e := range m.Emails {
			seen[e]++
		}
	}
	for _, n := range seen {
		if n > 1 {
			d.Duplicates++
		}
	}
	d.Pattern = FindVettingPattern(members)
	d.Notes = conclusions(*d, cols)
}

func conclusions(d Discovery, cols Columns) []string {
	var notes []string
	var missing []string
	for _, f := range d.Columns {
		if f.Column == "" {
			missing = append(missing, string(f.Role))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		notes = append(notes, "Not found (the check works without them, but shows less): "+strings.Join(missing, ", ")+".")
	}
	if !cols.Found(RoleVetted) {
		notes = append(notes, "Without an \"App vetted\" column, nobody can be shown as ready to set up. Its header must contain \"vetted\".")
	}
	if d.VettingStarted.IsZero() && d.Pattern.UnmarkedBefore > 0 {
		notes = append(notes, d.Pattern.Explain()+" If the chair began marking \"App vetted\" around "+FriendlyDate(d.Pattern.FirstVetted)+", members who started earlier don't need it: run `aps-tools setup sheet` to set the date vetting began.")
	}
	if d.InProgress > 0 {
		why := "aren't marked \"App vetted\" yet"
		if !d.VettingStarted.IsZero() {
			why += " and didn't start before " + FriendlyDate(d.VettingStarted)
		}
		notes = append(notes, fmt.Sprintf("%d row(s) %s. Don't set those people up until the membership chair marks them.", d.InProgress, why))
	}
	if d.Duplicates > 0 {
		notes = append(notes, fmt.Sprintf("%d email address(es) appear on more than one row. The check says which rows.", d.Duplicates))
	}
	if d.NoEmail > 0 {
		notes = append(notes, fmt.Sprintf("%d row(s) have no email address, so the member check can't find them.", d.NoEmail))
	}
	return notes
}
