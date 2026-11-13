// Package sheet connects the tool to the APS membership Google Sheet, the
// list of who is a member. It is the starting point of onboarding: the
// membership chair adds an applicant, marks them "New" while their
// application and background check are in progress, and marks
// "App vetted" = Y once everything is complete. Only then should the
// webmaster set them up in the other systems.
//
// How it authenticates: a Google "service account" (a robot Google account
// in a free Google Cloud project) that the sheet is shared with as Editor.
// The Editor share is for Phase 2's write-back; for now the tool signs in
// with a read-only scope, so it can only read.
// Its key is kept in the Keychain or 1Password. Setup guide:
// docs/setup/google-sheet.md.
//
// Privacy: the sheet also holds addresses and phone numbers. The tool never
// reads those columns into anything it shows or copies.
package sheet

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// ID is this connector's machine name (used in `aps-tools setup sheet`).
const ID = "sheet"

// Connector checks members against the membership sheet.
type Connector struct {
	cfg    config.Sheet
	client *Client
	keyErr error // why the key couldn't be loaded or read, if it couldn't
}

// New builds the connector from saved settings and the secret store.
func New(cfg config.Sheet, store secrets.Store) *Connector {
	c := &Connector{cfg: cfg}
	raw, err := store.Get(secrets.GoogleServiceAccountKey)
	if err != nil {
		c.keyErr = err
		return c
	}
	key, err := ParseServiceAccountKey(raw)
	if err != nil {
		c.keyErr = err
		return c
	}
	c.client = NewClient(key)
	return c
}

// ID implements connectors.Connector.
func (c *Connector) ID() string { return ID }

// Name implements connectors.Connector.
func (c *Connector) Name() string { return "Membership Google Sheet" }

// Missing implements connectors.Connector.
func (c *Connector) Missing() []string {
	var m []string
	if c.cfg.SpreadsheetID == "" || c.cfg.Tab == "" {
		m = append(m, "which sheet and tab list the members")
	}
	if c.client == nil {
		if c.keyErr != nil && !errors.Is(c.keyErr, secrets.ErrNotFound) {
			m = append(m, "a readable Google service account key ("+c.keyErr.Error()+")")
		} else {
			m = append(m, "a Google service account key")
		}
	}
	if len(m) > 0 {
		m[len(m)-1] += " (run: aps-tools setup sheet)"
	}
	return m
}

// TestConnection implements connectors.Connector. Read-only.
func (c *Connector) TestConnection(ctx context.Context) error {
	_, err := c.load(ctx)
	return err
}

// load reads the member tab and finds its columns.
func (c *Connector) load(ctx context.Context) ([]Member, error) {
	rows, err := c.client.Rows(ctx, c.cfg.SpreadsheetID, c.cfg.Tab)
	if err != nil {
		return nil, err
	}
	cols, ok := FindColumns(rows)
	if !ok {
		return nil, fmt.Errorf("couldn't find an email column on the %q tab — was the header renamed? Run: aps-tools sheet discover", c.cfg.Tab)
	}
	members, _ := Members(rows, cols)
	return members, nil
}

// CheckMember implements connectors.Connector. Read-only.
func (c *Connector) CheckMember(ctx context.Context, email string) (connectors.MemberStatus, error) {
	members, err := c.load(ctx)
	if err != nil {
		return connectors.MemberStatus{}, err
	}
	st := memberStatus(Find(members, email), time.Now())
	if note := c.newerTabNote(ctx); note != "" {
		st.Details = append(st.Details, note)
	}
	return st, nil
}

// newerTabNote warns when the sheet has a newer dated copy of the saved tab
// (e.g. "Member Directory Nov 2026" after "Member Directory May 2026"), so an
// out-of-date list never goes unnoticed. Problems reading the tab list are
// ignored: the check itself already worked.
func (c *Connector) newerTabNote(ctx context.Context) string {
	book, err := c.client.GetSpreadsheet(ctx, c.cfg.SpreadsheetID)
	if err != nil {
		return ""
	}
	if newer := NewerTab(book.Tabs, c.cfg.Tab); newer != "" {
		return "Note: checked the \"" + c.cfg.Tab + "\" tab, but there's a newer one, \"" + newer + "\". If that's the current list, run: aps-tools setup sheet"
	}
	return ""
}

// Find returns every row listing this email address.
func Find(members []Member, email string) []Member {
	email = strings.ToLower(strings.TrimSpace(email))
	var out []Member
	for _, m := range members {
		for _, e := range m.Emails {
			if e == email {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// memberStatus decides the result. It is separate from CheckMember so it
// can be tested without a network.
//
// The rule (from the membership chair's process): someone is ready to be
// set up once "App vetted" is marked. Until then their application is still
// in progress. Other columns are shown for information.
func memberStatus(rows []Member, now time.Time) connectors.MemberStatus {
	if len(rows) == 0 {
		return connectors.MemberStatus{State: connectors.StateMissing,
			Summary: "Not on the membership list.",
			Fix:     "If they've applied, check with the membership chair — they add new members to the sheet."}
	}

	// With duplicates, judge on a vetted row if there is one, else the last.
	m := rows[len(rows)-1]
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Vetted {
			m = rows[i]
			break
		}
	}

	var details []string
	who := "Row " + strconv.Itoa(m.Row)
	if n := m.Name(); n != "" {
		who = n + " (row " + strconv.Itoa(m.Row) + ")"
	}
	details = append(details, who+".")
	if len(rows) > 1 {
		var nums []string
		for _, r := range rows {
			nums = append(nums, strconv.Itoa(r.Row))
		}
		details = append(details, "Listed more than once: rows "+strings.Join(nums, ", ")+".")
	}
	if m.Status != "" {
		line := "Member Status: " + m.Status + "."
		if !m.Active() {
			line += " Note: not marked Active."
		}
		details = append(details, line)
	}
	if m.StartDate != "" {
		details = append(details, "Start date: "+m.StartDate+".")
	}
	if m.Paid != "" || m.PaidDate != "" {
		paid := "Paid: " + strings.TrimSpace(m.Paid)
		if m.PaidDate != "" {
			paid += " (" + m.PaidDate + ")"
		}
		details = append(details, strings.TrimSpace(paid)+".")
	}
	if m.DueDate != "" {
		line := "Dues due: " + m.DueDate + "."
		if t, ok := ParseDate(m.DueDate); ok && t.Before(now) {
			line += " That date has passed."
		}
		details = append(details, line)
	}

	if !m.Vetted {
		summary := "On the list, but \"App vetted\" isn't marked yet."
		if m.New {
			summary = "On the list as New: their application is still in progress."
		}
		return connectors.MemberStatus{State: connectors.StatePending, Summary: summary, Details: details,
			Fix: "Wait until the membership chair marks \"App vetted\" as Y before setting them up anywhere else."}
	}
	summary := "On the membership list, and their application is vetted."
	if m.New {
		details = append(details, "Still marked New, even though vetted.")
	}
	return connectors.MemberStatus{State: connectors.StateOK, Summary: summary, Details: details}
}
