// Package onboard is guided onboarding: setting a newly vetted member up in
// every APS system, one step at a time. It powers both `aps-tools onboard`
// and the "Onboard a new member" screen, so they always agree.
//
// It works in two halves, so nothing changes by surprise:
//
//  1. Prepare (read-only). Checks the membership sheet first, as a gate:
//     only a vetted member goes any further. Then checks every system, the
//     same way `aps-tools check` does, and builds a Plan listing what each
//     system needs. Anything already done is "nothing to do".
//  2. RunStep / Run (makes changes). Only after the webmaster says Go, and
//     only for steps the tool can do itself. Every change, and every
//     failure, is written to the activity log (internal/activity).
//
// Systems the tool can't set up yet are listed as steps to do by hand, with
// the instructions from the member check, so the plan doubles as a
// checklist. As each system learns to set members up (it implements
// connectors.Onboarder), its step becomes automatic with no change here.
//
// Plan: docs/plans/phase-2-onboarding.md.
package onboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// stepTimeout limits how long any one system may take to answer.
const stepTimeout = 30 * time.Second

// Kind says what a step needs.
type Kind int

const (
	// Done means the member is already set up here: nothing to do.
	Done Kind = iota
	// Auto means the tool will do it when the webmaster says Go.
	Auto
	// Manual means it's needed but the tool can't do it yet: do it by hand.
	Manual
	// Waiting means it's started and the member has to do something (e.g.
	// click a verification email). Nothing for the webmaster to do.
	Waiting
	// Blocked means the system couldn't be checked, so nothing can be
	// planned for it. Fix the connection (aps-tools doctor) first.
	Blocked
	// Later is part of onboarding the tool will do in a future version,
	// shown so the plan is complete. Nothing to do for now.
	Later
)

// String is a short label for the kind, used in reports.
func (k Kind) String() string {
	switch k {
	case Done:
		return "nothing to do"
	case Auto:
		return "the tool will do this"
	case Manual:
		return "do this by hand"
	case Waiting:
		return "waiting on the member"
	case Blocked:
		return "couldn't check"
	default:
		return "coming later"
	}
}

// Icon is a one-character symbol for the kind (plain text, no colour).
func (k Kind) Icon() string {
	switch k {
	case Done:
		return "✓"
	case Auto:
		return "+"
	case Manual:
		return "□"
	case Waiting:
		return "…"
	case Blocked:
		return "?"
	default:
		return "○"
	}
}

// Outcome is what happened when a step was run.
type Outcome int

const (
	// NotRun means the step hasn't been run (or isn't one the tool runs).
	NotRun Outcome = iota
	// Succeeded means the change was made (or was found already made).
	Succeeded
	// Failed means the change couldn't be made; Result says why.
	Failed
)

// Step is one system's part of onboarding.
type Step struct {
	// System is the human name, e.g. "Mailing list (Cloudflare)".
	System string
	// Kind says what the step needs.
	Kind Kind
	// Now is what the system says about the member right now.
	Now string
	// Action is what the tool will do (Auto), or what to do by hand
	// (Manual, Waiting, Blocked). Empty when there's nothing to do.
	Action string
	// Warning is something to double-check before going ahead, e.g. a
	// member whose status isn't Active. It doesn't stop anything.
	Warning string

	// Outcome and Result are filled in when the step is run.
	Outcome Outcome
	Result  string

	onboarder connectors.Onboarder // set for Auto steps
}

// Plan is everything onboarding found and intends to do for one member.
type Plan struct {
	// Person is who is being onboarded, as the membership sheet has them.
	Person connectors.Person
	// Ready is true only when the membership sheet says they're vetted, or
	// the webmaster overrode it (see Override). When false, Steps is empty
	// and Gate says why.
	Ready bool
	// Override is the webmaster's reason for onboarding someone the sheet
	// didn't pass. Empty when there was no override.
	Override string
	// Gate is the membership sheet's answer.
	Gate connectors.MemberStatus
	// Steps are in onboarding order (docs/onboarding-process.md).
	Steps []Step
	// PreparedAt is when the systems were checked.
	PreparedAt time.Time
	// States is each system's state when the plan was made, keyed by its
	// machine name (e.g. "discourse"), for suggesting an email.
	States map[string]connectors.State
}

// Count returns how many steps are of kind k.
func (p Plan) Count(k Kind) int {
	n := 0
	for _, s := range p.Steps {
		if s.Kind == k {
			n++
		}
	}
	return n
}

// Who is "Alex Example <alex@example.com>", or just the email.
func (p Plan) Who() string {
	if n := p.Person.Name(); n != "" {
		return n + " <" + p.Person.Email + ">"
	}
	return p.Person.Email
}

// Summary is a one-line verdict for the plan.
func (p Plan) Summary() string {
	if !p.Ready {
		return "Not ready to onboard: " + lowerFirst(strings.TrimSuffix(p.Gate.Summary, ".")) + "."
	}
	manual, blocked := p.Count(Manual), p.Count(Blocked)
	auto, made, failed := 0, 0, 0
	for _, s := range p.Steps {
		switch {
		case s.Outcome == Succeeded:
			made++
		case s.Outcome == Failed:
			failed++
		case s.Kind == Auto:
			auto++
		}
	}
	var parts []string
	if made > 0 {
		parts = append(parts, "made "+plural(made, "change", "changes"))
	}
	if failed > 0 {
		parts = append(parts, plural(failed, "change", "changes")+" failed")
	}
	if auto > 0 {
		parts = append(parts, plural(auto, "change", "changes")+" the tool can make")
	}
	if manual > 0 {
		parts = append(parts, plural(manual, "step", "steps")+" to do by hand")
	}
	if blocked > 0 {
		parts = append(parts, plural(blocked, "system", "systems")+" couldn't be checked")
	}
	if n := p.warnings(); n > 0 {
		parts = append(parts, plural(n, "warning", "warnings")+" to check")
	}
	prefix := ""
	if p.Override != "" {
		prefix = "Sheet overridden. "
	}
	if len(parts) == 0 {
		if p.Count(Waiting) > 0 {
			return "Nothing to do: waiting on the member to finish."
		}
		return "Nothing to do: already set up everywhere."
	}
	return prefix + capitalise(connectors.JoinPlain(parts)) + "."
}

// PlainText renders the plan as a checklist, for pasting into a note.
func (p Plan) PlainText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "APS onboarding: %s\n", p.Who())
	if p.Override != "" {
		fmt.Fprintf(&b, "Membership sheet overridden by the webmaster. Reason: %s\n", p.Override)
	}
	fmt.Fprintf(&b, "Checked %s\n\n", p.PreparedAt.Format("Mon 2 Jan 2006, 3:04 PM"))
	if !p.Ready {
		fmt.Fprintf(&b, "%s\n", p.Summary())
		if p.Gate.Fix != "" {
			fmt.Fprintf(&b, "→ %s\n", p.Gate.Fix)
		}
		return b.String()
	}
	for _, s := range p.Steps {
		fmt.Fprintf(&b, "%s %s: %s\n", s.Kind.Icon(), s.System, s.Kind)
		if s.Now != "" {
			fmt.Fprintf(&b, "    %s\n", s.Now)
		}
		if s.Warning != "" {
			fmt.Fprintf(&b, "    ! %s\n", s.Warning)
		}
		if s.Action != "" {
			fmt.Fprintf(&b, "    → %s\n", s.Action)
		}
		if s.Outcome != NotRun {
			fmt.Fprintf(&b, "    %s\n", s.OutcomeText())
		}
	}
	fmt.Fprintf(&b, "\n%s\n", p.Summary())
	return b.String()
}

// OutcomeText describes a step's outcome, e.g. "✓ Done: added to news@".
func (s Step) OutcomeText() string {
	switch s.Outcome {
	case Succeeded:
		return "✓ Done: " + s.Result
	case Failed:
		return "✗ Failed: " + s.Result
	}
	return ""
}

// Prepare checks the member everywhere and builds the plan. READ-ONLY:
// nothing is changed in any system.
//
// It returns an error only for a bad email address. Every other problem
// (sheet not connected, member not vetted, a system not answering) is part
// of the plan, so it can be shown.
func Prepare(ctx context.Context, conns []connectors.Connector, email string) (Plan, error) {
	return prepare(ctx, conns, email, "")
}

// MinReasonLength is the shortest override reason accepted: enough for a
// few words, so the record means something later.
const MinReasonLength = 10

// ErrNoReason means an override was asked for without a real reason.
var ErrNoReason = fmt.Errorf("an override needs a reason of at least %d characters, e.g. \"Paid at the meeting; the chair will update the sheet\"", MinReasonLength)

// CheckReason validates an override reason (see ErrNoReason).
func CheckReason(reason string) error {
	if len([]rune(strings.TrimSpace(reason))) < MinReasonLength {
		return ErrNoReason
	}
	return nil
}

// CanOverride reports whether the webmaster may override this plan's gate:
// the sheet was read and said "not vetted yet" or "not on the list". If the
// sheet couldn't be read at all, the connection must be fixed instead.
func (p Plan) CanOverride() bool {
	return !p.Ready && (p.Gate.State == connectors.StatePending || p.Gate.State == connectors.StateMissing)
}

// PrepareOverride is Prepare for someone the membership sheet doesn't pass
// (not vetted, or not on it), when the webmaster decides to go ahead anyway,
// for the given reason. READ-ONLY, like Prepare: the override is recorded
// by LogOverride once the webmaster confirms it. If the sheet does pass
// them, it's an ordinary plan with no override.
func PrepareOverride(ctx context.Context, conns []connectors.Connector, email, reason string) (Plan, error) {
	if err := CheckReason(reason); err != nil {
		return Plan{}, err
	}
	return prepare(ctx, conns, email, strings.TrimSpace(reason))
}

func prepare(ctx context.Context, conns []connectors.Connector, email, override string) (Plan, error) {
	email, err := check.NormalizeEmail(email)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Person: connectors.Person{Email: email}, PreparedAt: time.Now()}

	// 1. The gate: the membership sheet must say they're vetted, unless
	// the webmaster overrides it with a reason.
	dir, others := splitDirectory(conns)
	plan.Gate = gate(ctx, dir, &plan.Person)
	switch {
	case plan.Gate.State == connectors.StateOK:
		plan.Ready = true
		plan.Steps = append(plan.Steps, Step{System: dir.Name(), Kind: Done, Now: gateLine(plan), Warning: plan.Gate.Warning})
	case override != "" && plan.CanOverride():
		plan.Ready, plan.Override = true, override
		plan.Steps = append(plan.Steps, Step{System: dir.Name(), Kind: Manual,
			Now:     "Overridden by you: " + lowerFirst(gateLine(plan)),
			Action:  "Ask the membership chair to update the sheet, so it matches.",
			Warning: "Override reason: " + override})
	default:
		return plan, nil
	}

	// 2. Check every other system at once, then decide each step.
	report, err := check.Run(ctx, others, email, nil)
	if err != nil {
		return plan, err
	}
	plan.States = report.States()
	steps := make([]Step, len(others))
	var wg sync.WaitGroup
	for i, c := range others {
		wg.Add(1)
		go func() {
			defer wg.Done()
			steps[i] = planStep(ctx, c, plan.Person, report.Results[i].MemberStatus)
		}()
	}
	wg.Wait()
	plan.Steps = append(plan.Steps, steps...)

	// 3. The parts of onboarding that aren't one system's check. Someone
	// already set up everywhere is an existing member, not a new one, so
	// they don't need a welcome email.
	welcome := Step{System: WelcomeStep, Kind: Manual,
		Action: "Open it ready to copy into Mail (w in the app, or: aps-tools email welcome " + email + "), then send it."}
	if allDone(steps) {
		welcome = Step{System: WelcomeStep, Kind: Done,
			Now: "Not needed: they're already set up everywhere. (If they never had one, you can still send it.)"}
	}
	record := Step{System: "Membership sheet record", Kind: Later,
		Now: "The tool will record onboarding on the sheet in a later version."}
	if plan.Override != "" {
		record.Now = "The tool will record onboarding, the override and its reason on the sheet in a later version. For now they're in the activity log."
	}
	plan.Steps = append(plan.Steps, welcome, record)
	return plan, nil
}

// allDone reports whether every system already has the member set up.
func allDone(steps []Step) bool {
	for _, s := range steps {
		if s.Kind != Done {
			return false
		}
	}
	return true
}

// splitDirectory separates the membership list from the other systems.
func splitDirectory(conns []connectors.Connector) (connectors.Directory, []connectors.Connector) {
	var dir connectors.Directory
	var others []connectors.Connector
	for _, c := range conns {
		if d, ok := c.(connectors.Directory); ok && dir == nil {
			dir = d
			continue
		}
		others = append(others, c)
	}
	return dir, others
}

// gate asks the membership sheet about the member, filling in their name.
func gate(ctx context.Context, dir connectors.Directory, person *connectors.Person) connectors.MemberStatus {
	if dir == nil {
		return connectors.MemberStatus{State: connectors.StateUnknown,
			Summary: "the tool has no membership list to check them against",
			Fix:     "Run: aps-tools setup sheet"}
	}
	if missing := dir.Missing(); len(missing) > 0 {
		rerun := "aps-tools setup " + dir.ID()
		needed := strings.TrimSuffix(connectors.JoinPlain(missing), " (run: "+rerun+")")
		return connectors.MemberStatus{State: connectors.StateUnknown,
			Summary: "the membership sheet isn't connected, so the tool can't confirm they're vetted",
			Details: []string{"Still needed: " + needed + "."},
			Fix:     "Run: " + rerun}
	}
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	p, st, err := dir.LookupMember(ctx, person.Email)
	if err != nil {
		return connectors.MemberStatus{State: connectors.StateUnknown,
			Summary: "couldn't read the membership sheet: " + err.Error(),
			Fix:     "Run: aps-tools doctor"}
	}
	person.FirstName, person.LastName, person.Row = p.FirstName, p.LastName, p.Row
	return st
}

// gateLine is the membership sheet step's "now" line, e.g.
// "Vetted (row 42)".
func gateLine(p Plan) string {
	if p.Person.Row > 0 {
		return fmt.Sprintf("Row %d: %s", p.Person.Row, lowerFirst(p.Gate.Summary))
	}
	return p.Gate.Summary
}

// planStep decides one system's step from its check result.
func planStep(ctx context.Context, c connectors.Connector, person connectors.Person, st connectors.MemberStatus) Step {
	step := Step{System: c.Name(), Now: st.Summary, Warning: st.Warning}
	if st.State == connectors.StateUnknown {
		step.Kind, step.Action = Blocked, st.Fix
		return step
	}
	if o, ok := c.(connectors.Onboarder); ok {
		ctx, cancel := context.WithTimeout(ctx, stepTimeout)
		defer cancel()
		action, err := o.PlanOnboard(ctx, person, st)
		switch {
		case err != nil:
			step.Kind, step.Action = Blocked, "Couldn't plan this step: "+err.Error()
			return step
		case action != "":
			step.Kind, step.Action, step.onboarder = Auto, action, o
			return step
		}
	}
	switch st.State {
	case connectors.StateOK:
		step.Kind = Done
	case connectors.StatePending:
		step.Kind, step.Action = Waiting, st.Fix
	default: // missing or a problem, and the tool can't fix it itself yet
		step.Kind, step.Action = Manual, st.Fix
		if step.Action == "" {
			step.Action = "Set them up by hand: see docs/onboarding-process.md."
		}
	}
	return step
}

// ErrNotReady means a plan that didn't pass the gate was asked to run.
var ErrNotReady = errors.New("this member isn't vetted on the membership sheet, so onboarding won't make any changes")

// RunStep runs step i if it's one the tool does (Auto) and hasn't run yet,
// and records the result in the plan and the activity log. It's safe to call
// for any step: others are left alone. The error is only for a problem
// writing the activity log; a failed step is recorded in its Outcome.
func (p *Plan) RunStep(ctx context.Context, i int, log *activity.Log) error {
	if !p.Ready {
		return ErrNotReady
	}
	s := &p.Steps[i]
	if s.Kind != Auto || s.Outcome != NotRun {
		return nil
	}
	if s.onboarder == nil { // can't happen via Prepare; never loop on it
		s.Outcome, s.Result = Failed, "the tool doesn't know how to do this step"
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	did, err := s.onboarder.Onboard(ctx, p.Person)
	entry := activity.Entry{Member: p.Person.Email, System: s.System}
	if err != nil {
		s.Outcome, s.Result = Failed, err.Error()
		entry.What = s.Action + ": " + err.Error()
	} else {
		if did == "" {
			did = s.Action
		}
		s.Outcome, s.Result = Succeeded, did
		entry.OK, entry.What = true, did
	}
	return log.Add(entry)
}

// Next returns the index of the next step the tool does that hasn't run
// yet, or -1 when there are none left. The app uses it to run steps one at
// a time, redrawing the screen after each.
func (p *Plan) Next() int {
	for i, s := range p.Steps {
		if s.Kind == Auto && s.Outcome == NotRun {
			return i
		}
	}
	return -1
}

// Failures counts the steps that failed when run.
func (p *Plan) Failures() int {
	n := 0
	for _, s := range p.Steps {
		if s.Outcome == Failed {
			n++
		}
	}
	return n
}

// WelcomeStep is the welcome email step's name in the plan.
const WelcomeStep = "Welcome email"

// WelcomeSent records that the webmaster sent the welcome email (the tool
// can't tell; they confirm it), in the activity log and in the plan.
func (p *Plan) WelcomeSent(log *activity.Log) error {
	for i := range p.Steps {
		if p.Steps[i].System == WelcomeStep {
			p.Steps[i].Outcome, p.Steps[i].Result = Succeeded, "Sent (you confirmed it)."
		}
	}
	return LogWelcomeSent(log, p.Person.Email)
}

// LogWelcomeSent records in the activity log that the webmaster confirmed
// sending a member's welcome email.
func LogWelcomeSent(log *activity.Log, email string) error {
	return LogEmailSent(log, email, WelcomeStep)
}

// LogEmailSent records in the activity log that the webmaster confirmed
// sending a member an email (named e.g. "Forum access help").
func LogEmailSent(log *activity.Log, email, name string) error {
	return log.Add(activity.Entry{Member: email, System: name, OK: true, What: "Sent by the webmaster (confirmed in the tool)"})
}

// LogOverride records the webmaster's decision to override the sheet, and
// why, in the activity log. Call it once the webmaster confirms the
// override, before any changes are made.
func (p *Plan) LogOverride(log *activity.Log) error {
	if p.Override == "" {
		return nil
	}
	return log.Add(activity.Entry{Member: p.Person.Email, System: "Membership sheet", OK: true,
		What: "Override by the webmaster: " + strings.TrimSuffix(p.Gate.Summary, ".") + ". Reason: " + p.Override})
}

// LogStart records the start of an onboarding run in the activity log.
func (p *Plan) LogStart(log *activity.Log) error {
	return log.Add(activity.Entry{Member: p.Person.Email, System: "Onboarding", OK: true,
		What: "Started: " + plural(p.Count(Auto), "change", "changes") + " to make"})
}

// LogFinish records the end of an onboarding run in the activity log.
func (p *Plan) LogFinish(log *activity.Log) error {
	end := activity.Entry{Member: p.Person.Email, System: "Onboarding", OK: true, What: "Finished"}
	if n := p.Failures(); n > 0 {
		end.OK, end.What = false, "Finished with "+plural(n, "failure", "failures")
	}
	return log.Add(end)
}

// Run runs every step the tool does, in order. A failed step doesn't stop
// the others: each system is independent, and re-running onboarding later
// picks up only what's still missing. progress, if not nil, is called after
// each step the tool runs. Steps that already ran aren't run again.
func (p *Plan) Run(ctx context.Context, log *activity.Log, progress func(Step)) error {
	if !p.Ready {
		return ErrNotReady
	}
	if p.Next() < 0 {
		return nil
	}
	if err := p.LogStart(log); err != nil {
		return err
	}
	var logErr error
	for i := p.Next(); i >= 0; i = p.Next() {
		if err := p.RunStep(ctx, i, log); err != nil && logErr == nil {
			logErr = err
		}
		if progress != nil {
			progress(p.Steps[i])
		}
	}
	if err := p.LogFinish(log); err != nil && logErr == nil {
		logErr = err
	}
	return logErr
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// warnings counts the steps with something to double-check.
func (p Plan) warnings() int {
	n := 0
	for _, s := range p.Steps {
		if s.Warning != "" {
			n++
		}
	}
	return n
}

// lowerFirst lower-cases the first letter, so a system's sentence can
// continue ours ("Not ready to onboard: not on the membership list.").
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
