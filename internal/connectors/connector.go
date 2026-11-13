// Package connectors defines how the tool talks to outside systems.
//
// Each outside system (Google Sheet, Cloudflare, Jira, Discourse) gets its own
// sub-folder containing a type that implements the Connector interface below.
// See docs/architecture.md → "How to add a new connector".
package connectors

import "context"

// Connector is the common shape every outside system must have.
//
// Phase 0 needs only setup and connection checks. Methods for looking up a
// member (Phase 1) and setting one up (Phase 2) will be added here, and every
// connector will implement them.
type Connector interface {
	// ID is a short, lowercase machine name such as "jira". It is used in
	// settings and commands, so never change it once released.
	ID() string

	// Name is the human-friendly name shown in the app,
	// e.g. "Jira Service Management".
	Name() string

	// Missing lists, in plain English, any settings or keys that still need
	// to be set up, e.g. "Jira API token (run: aps-tools setup jira)".
	// An empty list means the connector is ready to use.
	Missing() []string

	// TestConnection makes one harmless, read-only request to prove the
	// settings and keys work. It must never change anything.
	// Errors should say what went wrong AND what to do about it.
	TestConnection(ctx context.Context) error
}
