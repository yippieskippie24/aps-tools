// Package followup finds members who are only part-way set up, and helps
// the webmaster follow up on many of them at once:
//   - members who haven't clicked Cloudflare's verification email (so APS
//     emails don't reach them): re-send it to everyone chosen;
//   - vetted members missing from a system, often the forum: onboard
//     everyone chosen;
//   - members with a forum invite they haven't used: email each of them
//     their invite link again.
//
// The lists come from a roster snapshot (internal/roster), which is
// READ-ONLY. The bulk actions change outside systems, so they follow the
// project's rules: the webmaster sees the list and chooses who's included
// first, each member is checked again live just before anything is done
// for them (so nothing is repeated), and every change goes in the activity
// log. Members are done one at a time, so a slow or rate-limited system
// (the forum allows about 20 requests a minute) is waited for, not
// overwhelmed.
package followup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
	"github.com/yippieskippie24/aps-tools/internal/progress"
	"github.com/yippieskippie24/aps-tools/internal/roster"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// Kind is a kind of follow-up.
type Kind int

// The follow-ups, in the order they're shown.
const (
	// Unverified: Cloudflare is waiting for them to click its verification
	// email, so APS emails don't reach them yet.
	Unverified Kind = iota
	// NeedsSetup: the sheet passes them, but at least one system is
	// missing them (often the forum).
	NeedsSetup
	// InviteWaiting: they have a forum invite they haven't used yet.
	InviteWaiting
)

// Kinds lists every follow-up in display order.
var Kinds = []Kind{Unverified, NeedsSetup, InviteWaiting}

// ID is the follow-up's name on the command line, e.g. "unverified".
func (k Kind) ID() string {
	switch k {
	case Unverified:
		return "unverified"
	case NeedsSetup:
		return "setup"
	default:
		return "invites"
	}
}

// Title is a short heading, e.g. "Haven't verified their email".
func (k Kind) Title() string {
	switch k {
	case Unverified:
		return "Haven't verified their email"
	case NeedsSetup:
		return "Need setting up"
	default:
		return "Haven't used their forum invite"
	}
}

// Explain says what the follow-up means and what the tool will do.
func (k Kind) Explain() string {
	switch k {
	case Unverified:
		return "Cloudflare is waiting for them to click its verification email, so APS emails don't reach them yet. The tool can have Cloudflare send each of them a new one, then you can email them about it."
	case NeedsSetup:
		return "Vetted on the sheet, but missing from at least one system (usually the forum). The tool can onboard them: each is checked again first, and only what's missing is set up. Then you can email each of them."
	default:
		return "They have a forum invite but haven't used it. Email each of them their invite link again."
	}
}

// Action is the change the tool makes for everyone chosen, e.g.
// "Re-send Cloudflare's verification email", or "" when the follow-up is
// only an email.
func (k Kind) Action() string {
	switch k {
	case Unverified:
		return "Re-send Cloudflare's verification email"
	case NeedsSetup:
		return "Onboard them"
	default:
		return ""
	}
}

// List is one follow-up and the members it applies to.
type List struct {
	Kind    Kind
	Members []roster.Entry
}

// Lists finds every follow-up in a roster snapshot: one List per Kind, in
// order, even when it's empty. Only members the sheet passes are included.
// A member can be in more than one list.
func Lists(r roster.Roster) []List {
	out := make([]List, len(Kinds))
	for i, k := range Kinds {
		out[i].Kind = k
		for _, e := range r.Entries {
			if k.applies(r, e) {
				out[i].Members = append(out[i].Members, e)
			}
		}
	}
	return out
}

func (k Kind) applies(r roster.Roster, e roster.Entry) bool {
	if e.Sheet.State != connectors.StateOK {
		return false
	}
	states := r.States(e)
	switch k {
	case Unverified:
		return states["cloudflare"] == connectors.StatePending
	case NeedsSetup:
		return e.Group == roster.NeedsSetup
	default:
		return states["discourse"] == connectors.StatePending
	}
}

// Detail is a short line about a member in this follow-up, e.g. "Added 46
// days ago, but they haven't clicked Cloudflare's verification email." or
// "Missing from Jira and the forum".
func (k Kind) Detail(e roster.Entry) string {
	switch k {
	case NeedsSetup:
		var missing []string
		for _, s := range e.Systems {
			if s.State == connectors.StateMissing || s.State == connectors.StateProblem {
				missing = append(missing, s.Short())
			}
		}
		return "Missing from " + connectors.JoinPlain(missing)
	case Unverified:
		return summaryOf(e, "cloudflare")
	default:
		return summaryOf(e, "discourse")
	}
}

func summaryOf(e roster.Entry, id string) string {
	for _, s := range e.Systems {
		if s.ID == id {
			return s.Summary
		}
	}
	return ""
}

// Email is the email to send a member after this follow-up: Verify your
// email for the unverified, otherwise the one welcome.Suggest picks from
// where they stand now (e.g. a Welcome email for someone just onboarded).
func (k Kind) Email(sheet connectors.State, states map[string]connectors.State) welcome.Kind {
	if k == Unverified {
		return welcome.VerifyEmail
	}
	return welcome.Suggest(sheet, states)
}

// GroupEmail is the email that can go to everyone chosen at once, as ONE
// email with them all in Bcc, if there is one. Only "Verify your email"
// can: it has nothing personal in it. The others carry each member's own
// single-use forum invite link, so they go one by one.
func (k Kind) GroupEmail() (welcome.Kind, bool) {
	if k == Unverified {
		return welcome.VerifyEmail, true
	}
	return welcome.Kind{}, false
}

// LogGroupSent records in the activity log that one email of kind wk was
// sent to each of people.
func LogGroupSent(log *activity.Log, wk welcome.Kind, people []connectors.Person) error {
	for _, p := range people {
		if err := onboard.LogEmailSent(log, p.Email, wk.Name+" (one email to "+fmt.Sprint(len(people))+" members, in Bcc)"); err != nil {
			return err
		}
	}
	return nil
}

// Result is what happened for one member.
type Result struct {
	Person connectors.Person
	OK     bool
	// What happened, e.g. "Cloudflare is sending them a new verification
	// email." or why it didn't.
	What string
}

// Count returns how many results succeeded and how many didn't.
func Count(results []Result) (ok, failed int) {
	for _, r := range results {
		if r.OK {
			ok++
		} else {
			failed++
		}
	}
	return ok, failed
}

// Run does k's Action for each person in turn and returns what happened
// for each. It reports "3 of 11 · Alex Example" as it goes. Stopping ctx
// stops before the next member. The follow-up must have an Action.
func Run(ctx context.Context, k Kind, conns []connectors.Connector, people []connectors.Person, log *activity.Log) []Result {
	var out []Result
	for i, p := range people {
		if ctx.Err() != nil {
			break
		}
		who := p.Name()
		if who == "" {
			who = p.Email
		}
		pctx := progress.Prefix(ctx, fmt.Sprintf("%d of %d · %s", i+1, len(people), who))
		progress.Report(pctx, k.Action())
		var r Result
		switch k {
		case Unverified:
			r = resend(pctx, conns, p, log)
		case NeedsSetup:
			r = onboardOne(pctx, conns, p, log)
		default:
			r = Result{Person: p, What: "There's nothing for the tool to change: email them instead."}
		}
		out = append(out, r)
	}
	return out
}

// ErrNoMailingList means there's no connected mailing list (Cloudflare)
// that can re-send verification emails.
var ErrNoMailingList = errors.New("the mailing list (Cloudflare) isn't connected")

// ResendOne has Cloudflare send one member a new verification email, and
// records it in the activity log. It returns what was done. The connector
// checks first that it's safe (see decision 0016).
func ResendOne(ctx context.Context, conns []connectors.Connector, email string, log *activity.Log) (string, error) {
	for _, c := range conns {
		r, ok := c.(connectors.VerificationResender)
		if !ok || len(c.Missing()) > 0 {
			continue
		}
		did, err := r.ResendVerification(ctx, email)
		entry := activity.Entry{Member: email, System: c.Name(), OK: err == nil, What: did}
		if err != nil {
			entry.What = "Re-send verification email: " + err.Error()
		}
		if lerr := log.Add(entry); lerr != nil && err == nil {
			return did, fmt.Errorf("%s, but it couldn't be recorded in the activity log: %w", strings.TrimSuffix(did, "."), lerr)
		}
		return did, err
	}
	return "", ErrNoMailingList
}

func resend(ctx context.Context, conns []connectors.Connector, p connectors.Person, log *activity.Log) Result {
	did, err := ResendOne(ctx, conns, p.Email, log)
	if err != nil {
		return Result{Person: p, What: err.Error()}
	}
	return Result{Person: p, OK: true, What: did}
}

// onboardOne checks a member again, live, and makes whatever changes the
// tool can that are still needed (exactly as onboarding one member does).
func onboardOne(ctx context.Context, conns []connectors.Connector, p connectors.Person, log *activity.Log) Result {
	plan, err := onboard.Prepare(ctx, conns, p.Email)
	if err != nil {
		return Result{Person: p, What: err.Error()}
	}
	if plan.Person.Name() != "" {
		p = plan.Person
	}
	if !plan.Ready {
		return Result{Person: p, What: "Skipped: " + plan.Summary()}
	}
	if plan.Next() < 0 {
		return Result{Person: p, OK: true, What: "Nothing for the tool to do: already set up as far as it can go."}
	}
	if err := plan.Run(ctx, log, nil); err != nil {
		return Result{Person: p, What: "Stopped: " + err.Error()}
	}
	var did, failed []string
	for _, s := range plan.Steps {
		switch s.Outcome {
		case onboard.Succeeded:
			did = append(did, strings.TrimSuffix(s.Result, "."))
		case onboard.Failed:
			failed = append(failed, s.System+": "+strings.TrimSuffix(s.Result, "."))
		}
	}
	r := Result{Person: p, OK: len(failed) == 0}
	switch {
	case len(failed) > 0:
		r.What = "Failed: " + strings.Join(failed, "; ") + "."
		if len(did) > 0 {
			r.What += " Done: " + strings.Join(did, "; ") + "."
		}
	default:
		r.What = strings.Join(did, "; ") + "."
	}
	var byHand []string
	for _, s := range plan.Steps {
		if s.Kind == onboard.Manual && s.System != onboard.WelcomeStep {
			byHand = append(byHand, s.System)
		}
	}
	if len(byHand) > 0 {
		r.What += " Still to do by hand: " + connectors.JoinPlain(byHand) + "."
	}
	return r
}
