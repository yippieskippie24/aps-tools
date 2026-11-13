package sheet

// This file makes sense of the membership sheet's rows. Columns are found
// by their header text (e.g. any header containing "mail" is the email
// column), so reordering or slightly renaming columns doesn't break the tool.
// If a header is renamed beyond recognition, `aps-tools sheet discover`
// shows which columns were (and weren't) found.

import (
	"regexp"
	"strings"
	"time"
)

// Role is what a column means to the tool.
type Role string

// The columns the tool understands. Others (address, phone, …) are ignored
// on purpose: the tool never reads them into a report.
const (
	RoleEmail     Role = "email"
	RoleFirstName Role = "first name"
	RoleLastName  Role = "last name"
	RoleStatus    Role = "member status"
	RoleVetted    Role = "application vetted"
	RoleNew       Role = "new"
	RolePaid      Role = "paid"
	RolePaidDate  Role = "paid date"
	RoleDueDate   Role = "due date"
	RoleStartDate Role = "start date"
)

// Roles lists every role in display order.
var Roles = []Role{RoleEmail, RoleFirstName, RoleLastName, RoleStatus, RoleVetted, RoleNew, RolePaid, RolePaidDate, RoleDueDate, RoleStartDate}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// normHeader turns "e-Mail Address:" into "e mail address".
func normHeader(h string) string {
	return strings.TrimSpace(nonAlnum.ReplaceAllString(strings.ToLower(h), " "))
}

// roleOf decides what a header means, or "" if the tool doesn't use it.
// Order matters: "paid date" must be tested before "paid".
func roleOf(header string) Role {
	h := normHeader(header)
	has := func(s string) bool { return strings.Contains(" "+h+" ", " "+s+" ") }
	switch {
	case strings.Contains(h, "mail"):
		return RoleEmail
	case has("first name") || h == "first":
		return RoleFirstName
	case has("last name") || h == "last" || has("surname"):
		return RoleLastName
	case has("status"):
		return RoleStatus
	case strings.Contains(h, "vetted") || strings.Contains(h, "vetting"):
		return RoleVetted
	case has("paid") && has("date"):
		return RolePaidDate
	case has("paid"):
		return RolePaid
	case has("due"):
		return RoleDueDate
	case has("start") || has("joined"):
		return RoleStartDate
	case h == "new" || strings.HasPrefix(h, "new "):
		return RoleNew
	}
	return ""
}

// Columns says which column (0 = A) holds each role, and the header row.
type Columns struct {
	HeaderRow int             // 0-based row index of the headers
	Index     map[Role]int    // role → column
	Headers   map[Role]string // role → the header text as written
}

// Found reports whether the sheet has a column for role.
func (c Columns) Found(r Role) bool { _, ok := c.Index[r]; return ok }

// FindColumns looks for the header row in the first 10 rows: the first row
// with an email column. ok=false means no email column was found.
func FindColumns(rows [][]string) (Columns, bool) {
	for i := 0; i < len(rows) && i < 10; i++ {
		c := Columns{HeaderRow: i, Index: map[Role]int{}, Headers: map[Role]string{}}
		for j, h := range rows[i] {
			r := roleOf(h)
			if r == "" {
				continue
			}
			if _, taken := c.Index[r]; !taken { // leftmost column wins
				c.Index[r] = j
				c.Headers[r] = h
			}
		}
		if c.Found(RoleEmail) {
			return c, true
		}
	}
	return Columns{}, false
}

// ColumnLetter turns 0 into "A", 26 into "AA".
func ColumnLetter(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// Member is one row of the membership sheet, reduced to what the tool uses.
type Member struct {
	Row       int      // the row number as shown in Google Sheets (1 = first row)
	Emails    []string // lower-case; a cell may hold more than one
	FirstName string
	LastName  string
	Status    string
	Vetted    bool
	New       bool
	Paid      string // as written ("Y", "TRUE", …); see IsYes
	PaidDate  string
	DueDate   string
	StartDate string
}

// Name is "First Last", or "" if neither is filled in.
func (m Member) Name() string { return strings.TrimSpace(m.FirstName + " " + m.LastName) }

// Active reports whether Member Status says "Active".
func (m Member) Active() bool { return strings.HasPrefix(strings.ToLower(m.Status), "active") }

var emailIn = regexp.MustCompile(`[^\s,;:<>()"'/]+@[^\s,;:<>()"'/]+\.[^\s,;:<>()"'/]+`)

// Members turns the rows below the header into Members. Rows with no email
// address are skipped (they're counted by Discover).
func Members(rows [][]string, cols Columns) (members []Member, noEmail int) {
	cell := func(row []string, r Role) string {
		j, ok := cols.Index[r]
		if !ok || j >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[j])
	}
	for i := cols.HeaderRow + 1; i < len(rows); i++ {
		row := rows[i]
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue // blank row
		}
		var emails []string
		for _, e := range emailIn.FindAllString(cell(row, RoleEmail), -1) {
			emails = append(emails, strings.ToLower(strings.TrimRight(e, ".")))
		}
		if len(emails) == 0 {
			noEmail++
			continue
		}
		members = append(members, Member{
			Row:       i + 1,
			Emails:    emails,
			FirstName: cell(row, RoleFirstName),
			LastName:  cell(row, RoleLastName),
			Status:    cell(row, RoleStatus),
			Vetted:    IsYes(cell(row, RoleVetted)),
			New:       IsYes(cell(row, RoleNew)),
			Paid:      cell(row, RolePaid),
			PaidDate:  cell(row, RolePaidDate),
			DueDate:   cell(row, RoleDueDate),
			StartDate: cell(row, RoleStartDate),
		})
	}
	return members, noEmail
}

// IsYes reads a "yes" cell: Y, Yes, X, a ticked checkbox (TRUE), ✓ …
func IsYes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "x", "true", "✓", "✔", "✅", "done", "1":
		return true
	}
	return false
}

// dateLayouts are the ways a date might be typed in the sheet.
var dateLayouts = []string{
	"1/2/2006", "1/2/06", "2006-01-02", "1-2-2006", "Jan 2, 2006", "January 2, 2006",
	"2-Jan-2006", "2 Jan 2006", "Jan 2006", "January 2006", "1/2006",
}

// ParseDate understands the common ways dates are typed. ok=false if not.
func ParseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
