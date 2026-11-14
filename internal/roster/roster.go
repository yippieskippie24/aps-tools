// Package roster checks every member on the membership sheet against every
// system at once, and sorts them into groups: who needs setting up, who the
// tool is waiting on, who isn't ready yet, and who is all set. It powers the
// app's Members screen.
//
// It's built for speed: each system is read in bulk where it can be
// (connectors.BulkChecker), so checking 40 members takes a handful of
// requests per system rather than several per member. Everything here is
// READ-ONLY. A Roster is a snapshot: the app keeps it while it's open and
// says how old it is. Anything that changes a system (onboarding) always
// checks again, live, first.
package roster

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// bulkTimeout limits how long one system may take to check everyone.
const bulkTimeout = 90 * time.Second

// oneByOne is how many members are checked at the same time in a system
// that can't check in bulk.
const oneByOne = 4

// Group is where a member stands overall.
type Group int

// The groups, in the order the Members screen shows them.
const (
	// NeedsSetup: the sheet passes them, but a system is missing them or
	// has a problem. These are the people to onboard.
	NeedsSetup Group = iota
	// Waiting: set up as far as the tool can go; the member has to act
	// (verify their email, accept an invite).
	Waiting
	// CantCheck: a system couldn't be checked for them.
	CantCheck
	// NotReady: the sheet doesn't pass them yet (not vetted, or no start
	// date to show they joined before vetting began).
	NotReady
	// SetUp: set up everywhere.
	SetUp
)

// Groups lists every group in display order.
var Groups = []Group{NeedsSetup, Waiting, CantCheck, NotReady, SetUp}

// String is the group's short title, e.g. "Needs setup".
func (g Group) String() string {
	switch g {
	case NeedsSetup:
		return "Needs setup"
	case Waiting:
		return "Waiting on member"
	case CantCheck:
		return "Couldn't check"
	case NotReady:
		return "Not ready"
	default:
		return "All set up"
	}
}

// Explain says what the group means, in a sentence.
func (g Group) Explain() string {
	switch g {
	case NeedsSetup:
		return "On the sheet and ready, but missing (or with a problem) in at least one system. Press enter to onboard."
	case Waiting:
		return "Set up as far as the tool can go. They still need to do something, like click Cloudflare's email."
	case CantCheck:
		return "At least one system couldn't be checked. Run Test connections."
	case NotReady:
		return "The sheet doesn't pass them yet: usually \"App vetted\" isn't marked."
	default:
		return "Set up in every system. Nothing to do."
	}
}

// Entry is one member and their status everywhere.
type Entry struct {
	connectors.Person
	// Sheet is the membership sheet's verdict (the onboarding gate).
	Sheet connectors.MemberStatus
	// Systems are the other systems' results, in Roster.Systems order.
	Systems []check.SystemResult
	// Group is where they stand overall.
	Group Group
}

// Warning is the first warning about them (e.g. a status that isn't
// Active), or "".
func (e Entry) Warning() string {
	if e.Sheet.Warning != "" {
		return e.Sheet.Warning
	}
	for _, s := range e.Systems {
		if s.Warning != "" {
			return s.System + ": " + s.Warning
		}
	}
	return ""
}

// Reason is a short line saying why they're in their group, e.g.
// "Discourse forum: No forum account, and no invite found."
func (e Entry) Reason() string {
	if e.Group == NotReady {
		return e.Sheet.Summary
	}
	var parts []string
	for _, s := range e.Why() {
		parts = append(parts, s.System+": "+s.Summary)
	}
	return strings.Join(parts, " · ")
}

// Why returns the systems that put them in their group: the missing or
// problem ones for Needs setup, the waiting ones for Waiting, and so on.
// Empty for Not ready (the sheet is the reason) and All set up.
func (e Entry) Why() []check.SystemResult {
	want := map[Group][]connectors.State{
		NeedsSetup: {connectors.StateMissing, connectors.StateProblem},
		CantCheck:  {connectors.StateUnknown},
		Waiting:    {connectors.StatePending},
	}[e.Group]
	var out []check.SystemResult
	for _, s := range e.Systems {
		for _, st := range want {
			if s.State == st {
				out = append(out, s)
			}
		}
	}
	return out
}

// Roster is a snapshot of every member's status.
type Roster struct {
	// Directory is the membership sheet's name.
	Directory string
	// Systems are the other systems' names, in display order.
	Systems []string
	// Entries are sorted by last name, then first name.
	Entries []Entry
	// CheckedAt is when the snapshot was taken.
	CheckedAt time.Time
	// Problems lists systems that couldn't be checked at all, and why.
	Problems []string
}

// In returns the entries in group g.
func (r Roster) In(g Group) []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.Group == g {
			out = append(out, e)
		}
	}
	return out
}

// Count is how many entries are in group g.
func (r Roster) Count(g Group) int { return len(r.In(g)) }

// ErrNoSheet means there's no membership sheet to list members from.
var ErrNoSheet = errors.New("the membership sheet isn't connected, so there's no member list — run: aps-tools setup sheet")

// Load lists everyone on the membership sheet and checks them in every
// other system. READ-ONLY.
func Load(ctx context.Context, conns []connectors.Connector) (Roster, error) {
	lister, others := split(conns)
	if lister == nil || len(lister.Missing()) > 0 {
		return Roster{}, ErrNoSheet
	}
	r := Roster{Directory: lister.Name(), CheckedAt: time.Now()}
	for _, c := range others {
		r.Systems = append(r.Systems, c.Name())
	}

	ctx, cancel := context.WithTimeout(ctx, bulkTimeout)
	defer cancel()
	people, err := lister.AllMembers(ctx)
	if err != nil {
		return Roster{}, errors.New("couldn't read the membership sheet: " + err.Error())
	}
	emails := make([]string, len(people))
	for i, p := range people {
		emails[i] = p.Person.Email
	}

	// Check every system at once; each checks everyone.
	results := make([]map[string]connectors.MemberStatus, len(others))
	problems := make([]string, len(others))
	var wg sync.WaitGroup
	for i, c := range others {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], problems[i] = checkAll(ctx, c, emails)
		}()
	}
	wg.Wait()
	for _, p := range problems {
		if p != "" {
			r.Problems = append(r.Problems, p)
		}
	}

	for _, p := range people {
		e := Entry{Person: p.Person, Sheet: p.Status}
		for i, c := range others {
			e.Systems = append(e.Systems, check.SystemResult{System: c.Name(), MemberStatus: results[i][p.Person.Email]})
		}
		e.Group = groupOf(e)
		r.Entries = append(r.Entries, e)
	}
	sort.SliceStable(r.Entries, func(i, j int) bool {
		a, b := r.Entries[i], r.Entries[j]
		if !strings.EqualFold(a.LastName, b.LastName) {
			return strings.ToLower(a.LastName) < strings.ToLower(b.LastName)
		}
		return strings.ToLower(a.FirstName) < strings.ToLower(b.FirstName)
	})
	return r, nil
}

// Refresh re-checks one member everywhere (e.g. after onboarding them) and
// updates their entry. READ-ONLY.
func (r *Roster) Refresh(ctx context.Context, conns []connectors.Connector, email string) error {
	lister, others := split(conns)
	if lister == nil {
		return ErrNoSheet
	}
	for i := range r.Entries {
		if !strings.EqualFold(r.Entries[i].Email, email) {
			continue
		}
		p, sheet, err := lister.LookupMember(ctx, email)
		if err != nil {
			return err
		}
		rep, err := check.Run(ctx, others, email, nil)
		if err != nil {
			return err
		}
		e := Entry{Person: p, Sheet: sheet, Systems: rep.Results}
		e.Group = groupOf(e)
		r.Entries[i] = e
		return nil
	}
	return nil
}

// lister is the membership sheet as the roster uses it.
type lister interface {
	connectors.MemberLister
	connectors.Directory
}

// split separates the membership sheet from the other systems.
func split(conns []connectors.Connector) (lister, []connectors.Connector) {
	var l lister
	var others []connectors.Connector
	for _, c := range conns {
		if x, ok := c.(lister); ok && l == nil {
			l = x
			continue
		}
		others = append(others, c)
	}
	return l, others
}

// checkAll checks everyone in one system: in bulk if it can, else a few at
// a time. A system that can't be checked gives every member "Couldn't
// check", plus a problem line for the screen.
func checkAll(ctx context.Context, c connectors.Connector, emails []string) (map[string]connectors.MemberStatus, string) {
	out := make(map[string]connectors.MemberStatus, len(emails))
	unknown := func(summary, fix string) (map[string]connectors.MemberStatus, string) {
		for _, e := range emails {
			out[e] = connectors.MemberStatus{State: connectors.StateUnknown, Summary: summary, Fix: fix}
		}
		return out, c.Name() + ": " + summary
	}
	if missing := c.Missing(); len(missing) > 0 {
		return unknown("Not connected yet.", "Run: aps-tools setup "+c.ID())
	}
	if b, ok := c.(connectors.BulkChecker); ok {
		got, err := b.CheckMembers(ctx, emails)
		if err != nil {
			return unknown(err.Error(), "Run: aps-tools doctor")
		}
		for _, e := range emails {
			out[e] = got[e]
		}
		return out, ""
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, oneByOne)
	for _, e := range emails {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			st, err := c.CheckMember(ctx, e)
			if err != nil {
				st = connectors.MemberStatus{State: connectors.StateUnknown, Summary: err.Error(), Fix: "Run: aps-tools doctor"}
			}
			mu.Lock()
			out[e] = st
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, ""
}

// groupOf decides an entry's group.
func groupOf(e Entry) Group {
	if e.Sheet.State != connectors.StateOK {
		return NotReady
	}
	has := func(states ...connectors.State) bool {
		for _, s := range e.Systems {
			for _, st := range states {
				if s.State == st {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has(connectors.StateMissing, connectors.StateProblem):
		return NeedsSetup
	case has(connectors.StateUnknown):
		return CantCheck
	case has(connectors.StatePending):
		return Waiting
	}
	return SetUp
}
