// Package connectors defines how the tool talks to outside systems.
//
// Each outside system (Google Sheet, Cloudflare, Jira, Discourse) gets its own
// sub-folder containing a type that implements the Connector interface below.
// See docs/architecture.md → "How to add a new connector".
package connectors

import "context"

// Connector is the common shape every outside system must have.
//
// Setting a member up (Phase 2) is optional: a connector that can do it also
// implements Onboarder, below. Until it does, onboarding lists that system as
// a step to do by hand.
type Connector interface {
	// ID is a short, lowercase machine name such as "jira". It is used in
	// settings and commands, so never change it once released.
	ID() string

	// Name is the human-friendly name shown in the app,
	// e.g. "Jira Service Management".
	Name() string

	// Missing lists, in plain English, any settings or keys that still need
	// to be set up, e.g. "Jira API token". An empty list means the connector
	// is ready to use.
	Missing() []string

	// TestConnection makes one harmless, read-only request to prove the
	// settings and keys work. It must never change anything.
	// Errors should say what went wrong AND what to do about it.
	TestConnection(ctx context.Context) error

	// CheckMember looks up one member by email and reports their status in
	// this system. It is READ-ONLY: it must never change anything.
	//
	// Return an error only when the check itself couldn't be done (network
	// down, key rejected). "Member not found" is a normal result, reported
	// as a MemberStatus with State Missing, not as an error.
	CheckMember(ctx context.Context, email string) (MemberStatus, error)
}

// State summarises how well a member is set up in one system.
type State int

// The order matters: higher values are more serious, which lets the check
// report pick the "worst" state to summarise a member.
const (
	// StateOK means the member is fully set up.
	StateOK State = iota
	// StatePending means setup was started but the member still has to do
	// something (accept an invite, click a verification email).
	StatePending
	// StateUnknown means we couldn't tell, e.g. the system isn't connected
	// yet or didn't answer.
	StateUnknown
	// StateProblem means the member exists but something is wrong, e.g. the
	// account is deactivated or in the wrong group.
	StateProblem
	// StateMissing means the member isn't set up in this system at all.
	StateMissing
)

// String gives each state a short label used in reports.
func (s State) String() string {
	switch s {
	case StateOK:
		return "Set up"
	case StatePending:
		return "Waiting on member"
	case StateProblem:
		return "Problem"
	case StateMissing:
		return "Not set up"
	default:
		return "Couldn't check"
	}
}

// MemberStatus is one system's answer to "is this member set up here?".
type MemberStatus struct {
	// State is the overall verdict.
	State State
	// Summary is one plain-English line, e.g. "Customer on the APS Help Desk".
	Summary string
	// Details are optional extra facts shown under the summary,
	// e.g. "Display name: Alex Example".
	Details []string
	// Fix tells the webmaster what to do next. Empty when nothing is needed.
	Fix string
	// Warning is something to double-check even though the state itself is
	// fine, e.g. a vetted member whose Member Status isn't Active. Shown
	// with a "!" in the check report and the onboarding plan. The tool
	// doesn't act on it: the webmaster decides.
	Warning string
}

// JoinPlain joins items as "a, b and c", e.g. for listing what a
// connector's Missing() says is still needed.
func JoinPlain(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	s := items[0]
	for _, it := range items[1 : len(items)-1] {
		s += ", " + it
	}
	return s + " and " + items[len(items)-1]
}

// Person is a member as the membership list knows them. Onboarding reads
// their name from the list, so the webmaster never retypes it.
type Person struct {
	// Email is the address onboarding was started with (lower-case).
	Email string
	// FirstName and LastName come from the membership list; either may be
	// blank.
	FirstName string
	LastName  string
	// Row is where they are on the membership list (1 = first row), for
	// showing the webmaster. 0 if unknown.
	Row int
}

// Name is "First Last", or "" if neither is known.
func (p Person) Name() string {
	switch {
	case p.FirstName == "":
		return p.LastName
	case p.LastName == "":
		return p.FirstName
	}
	return p.FirstName + " " + p.LastName
}

// Directory is implemented by the connector that holds the master member
// list (the membership Google Sheet). Onboarding asks it first, as a gate:
// only someone it reports as StateOK (vetted) is set up anywhere else.
type Directory interface {
	Connector
	// LookupMember is CheckMember plus who the member is. READ-ONLY.
	LookupMember(ctx context.Context, email string) (Person, MemberStatus, error)
}

// Onboarder is implemented by a connector that can set a member up in its
// system (Phase 2). It must follow the project's safety rules: preview
// first, check before acting, and never repeat or undo anything.
type Onboarder interface {
	Connector
	// PlanOnboard says what Onboard would do, given what CheckMember just
	// found, as a short line such as "Add them to the news@ list". It must
	// not change anything. Return "" when there's nothing to do.
	PlanOnboard(ctx context.Context, p Person, current MemberStatus) (string, error)
	// Onboard makes the change. It must check again first and skip anything
	// that's already done, so running it twice is harmless. It returns a
	// short description of what it changed, for the activity log.
	Onboard(ctx context.Context, p Person) (string, error)
}

// BulkChecker is implemented by a connector that can check many members at
// once far more cheaply than one by one, usually by reading its whole list
// of people in a few requests. The Members screen uses it. Same rules as
// CheckMember: READ-ONLY, and "not found" is a result, not an error.
type BulkChecker interface {
	Connector
	// CheckMembers returns a status for every email asked about, keyed by
	// the email as given.
	CheckMembers(ctx context.Context, emails []string) (map[string]MemberStatus, error)
}

// InviteLinker is implemented by the forum connector, for the welcome email.
type InviteLinker interface {
	Connector
	// InviteLink returns the link to put in the member's welcome email:
	// their pending personal invite's link, or the forum's address when
	// they already have an account. An empty URL means there's neither yet
	// (onboard them first). READ-ONLY.
	InviteLink(ctx context.Context, email string) (ForumLink, error)
}

// ForumLink is the forum link for a member's welcome email.
type ForumLink struct {
	// URL is their personal invite link, or the forum's address.
	URL string
	// HasAccount is true when they already have a forum account, so URL is
	// the forum's address rather than an invite.
	HasAccount bool
	// Username is their forum username, when they have an account.
	Username string
}

// DirectoryEntry is one member on the membership list, with the list's
// verdict on them (the onboarding gate).
type DirectoryEntry struct {
	Person Person
	Status MemberStatus
}

// MemberLister is implemented by the membership list connector, to list
// everyone on it (for the Members screen). READ-ONLY.
type MemberLister interface {
	Connector
	AllMembers(ctx context.Context) ([]DirectoryEntry, error)
}

// VerificationResender is implemented by a system that can have the member
// sent a fresh verification email (the mailing list: Cloudflare). It
// CHANGES the system, so callers must ask the webmaster first. It checks
// before acting, and refuses (with an error saying why) when there's
// nothing to re-send or it isn't safe.
type VerificationResender interface {
	Connector
	// ResendVerification makes the system email the member a new
	// verification link, and says what it did.
	ResendVerification(ctx context.Context, email string) (string, error)
}
