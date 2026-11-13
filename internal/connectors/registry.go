package connectors

import (
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// All builds every connector the tool knows about, in the order they should
// appear on screen.
//
// To add a system, construct its connector here, e.g.:
//
//	jira.New(cfg, store),
//
// Connectors are added in Phase 1 (see docs/design.md), so the list is empty
// for now.
func All(cfg *config.Config, store secrets.Store) []Connector {
	_ = cfg   // used once connectors are added
	_ = store // used once connectors are added
	return []Connector{}
}
