// Package all switches on every connector. Importing it (as internal/cli
// does) registers each system with internal/connectors, in display order.
//
// To add a system: write its connector package, then add one Register line
// below in the position it should appear on screen.
package all

import (
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/connectors/jira"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

func init() {
	connectors.Register(func(cfg *config.Config, store secrets.Store) connectors.Connector {
		return jira.New(cfg.Jira, store)
	})
}
