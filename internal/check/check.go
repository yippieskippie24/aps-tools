// Package check answers "is this member set up correctly everywhere?".
//
// It asks every connector about one email address at the same time and
// gathers the answers into a Report. It powers both `aps-tools check` and the
// "Check a member" screen in the app, so they always agree.
//
// Checking is strictly READ-ONLY: nothing in any system is changed.
package check

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// systemTimeout limits how long we wait for any one system to answer.
const systemTimeout = 20 * time.Second

// SystemResult is one system's part of the report.
type SystemResult struct {
	// System is the human name of the system, e.g. "Jira Service Management".
	System string
	// ID is the system's machine name, e.g. "jira".
	ID string
	// Status is what the system said about the member.
	connectors.MemberStatus
}

// Report is the full answer for one member.
type Report struct {
	// Email is the (cleaned-up) address that was checked.
	Email string
	// Results has one entry per connected system, in display order.
	Results []SystemResult
	// NotYetChecked names systems the tool can't check yet (still being built).
	NotYetChecked []string
	// CheckedAt is when the check ran.
	CheckedAt time.Time
}

// ErrInvalidEmail means the text entered doesn't look like an email address.
var ErrInvalidEmail = errors.New("that doesn't look like an email address")

// NormalizeEmail trims spaces and lower-cases an address, and rejects
// anything that isn't a plain address like name@example.com.
func NormalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	addr, err := mail.ParseAddress(s)
	// ParseAddress also accepts "Name <x@y>", so insist the input was the
	// bare address, and that the domain part has a dot.
	if err != nil || addr.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return "", ErrInvalidEmail
	}
	return s, nil
}

// Run checks one member against every connector, all at once.
// notYetChecked is passed straight through to the report.
func Run(ctx context.Context, conns []connectors.Connector, email string, notYetChecked []string) (Report, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return Report{}, err
	}

	results := make([]SystemResult, len(conns))
	var wg sync.WaitGroup
	for i, c := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = SystemResult{System: c.Name(), ID: c.ID(), MemberStatus: checkOne(ctx, c, email)}
		}()
	}
	wg.Wait()

	return Report{
		Email:         email,
		Results:       results,
		NotYetChecked: notYetChecked,
		CheckedAt:     time.Now(),
	}, nil
}

// checkOne asks a single connector, turning setup gaps and errors into a
// readable "Couldn't check" result rather than failing the whole report.
func checkOne(ctx context.Context, c connectors.Connector, email string) connectors.MemberStatus {
	if missing := c.Missing(); len(missing) > 0 {
		// Say exactly what's missing (e.g. a share waiting for approval),
		// without repeating the "run setup" hint the Fix line already gives.
		rerun := fmt.Sprintf("aps-tools setup %s", c.ID())
		needed := strings.TrimSuffix(connectors.JoinPlain(missing), " (run: "+rerun+")")
		return connectors.MemberStatus{
			State:   connectors.StateUnknown,
			Summary: "Not connected yet.",
			Details: []string{"Still needed: " + needed + "."},
			Fix:     "Run: " + rerun,
		}
	}
	ctx, cancel := context.WithTimeout(ctx, systemTimeout)
	defer cancel()
	st, err := c.CheckMember(ctx, email)
	if err != nil {
		return connectors.MemberStatus{
			State:   connectors.StateUnknown,
			Summary: err.Error(),
			Fix:     "Run: aps-tools doctor",
		}
	}
	return st
}

// States is each system's state, keyed by its machine name (e.g.
// "discourse"), for suggesting which email fits the member.
func (r Report) States() map[string]connectors.State {
	out := map[string]connectors.State{}
	for _, res := range r.Results {
		out[res.ID] = res.State
	}
	return out
}

// Worst returns the most serious state in the report (StateOK if empty).
func (r Report) Worst() connectors.State {
	worst := connectors.StateOK
	for _, res := range r.Results {
		if res.State > worst {
			worst = res.State
		}
	}
	return worst
}

// Summary is a one-line, plain-English verdict for the whole report.
func (r Report) Summary() string {
	if len(r.Results) == 0 {
		return "No systems are connected yet, so nothing could be checked."
	}
	issues := 0
	for _, res := range r.Results {
		if res.State != connectors.StateOK {
			issues++
		}
	}
	switch {
	case issues == 0 && len(r.NotYetChecked) > 0:
		return "Set up in every system checked so far."
	case issues == 0:
		return "Fully set up everywhere."
	case issues == 1:
		return "1 system needs attention."
	default:
		return fmt.Sprintf("%d systems need attention.", issues)
	}
}

// PlainText renders the report as plain text, suitable for pasting into an
// email to a member or a note to the board.
func (r Report) PlainText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "APS account check for %s\n", r.Email)
	fmt.Fprintf(&b, "Checked %s\n\n", r.CheckedAt.Format("Mon 2 Jan 2006, 3:04 PM"))
	for _, res := range r.Results {
		fmt.Fprintf(&b, "%s %s: %s — %s\n", Icon(res.State), res.System, res.State, res.Summary)
		for _, d := range res.Details {
			fmt.Fprintf(&b, "    %s\n", d)
		}
		if res.Warning != "" {
			fmt.Fprintf(&b, "    ! %s\n", res.Warning)
		}
		if res.Fix != "" {
			fmt.Fprintf(&b, "    → %s\n", res.Fix)
		}
	}
	if len(r.NotYetChecked) > 0 {
		fmt.Fprintf(&b, "\nNot checked by the tool yet: %s\n", strings.Join(r.NotYetChecked, ", "))
	}
	fmt.Fprintf(&b, "\n%s\n", r.Summary())
	return b.String()
}

// Icon returns a one-character symbol for a state (plain text, no colour).
func Icon(s connectors.State) string {
	switch s {
	case connectors.StateOK:
		return "✓"
	case connectors.StatePending:
		return "…"
	case connectors.StateUnknown:
		return "?"
	default:
		return "✗"
	}
}
