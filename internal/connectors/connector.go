// Package connectors defines how the tool talks to outside systems.
//
// Each outside system (Google Sheet, Cloudflare, Jira, Discourse) gets its own
// sub-folder containing a type that implements the Connector interface below.
// See docs/architecture.md → "How to add a new connector".
package connectors

import "context"

// Connector is the common shape every outside system must have.
//
// A method for setting a member up (Phase 2) will be added here later, and
// every connector will implement it.
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
}
