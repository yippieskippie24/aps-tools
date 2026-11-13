package connectors

import (
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// Factory builds one connector from the saved settings and the Keychain.
type Factory func(cfg *config.Config, store secrets.Store) Connector

// factories lists every connector the tool knows about, in the order they
// appear on screen. Each connector package adds itself here from
// internal/connectors/all (see all.go there) — this avoids an import loop,
// because connector packages import this one.
var factories []Factory

// Register adds a connector to the list. Call it only from
// internal/connectors/all.
func Register(f Factory) { factories = append(factories, f) }

// All builds every registered connector, in display order.
func All(cfg *config.Config, store secrets.Store) []Connector {
	out := make([]Connector, 0, len(factories))
	for _, f := range factories {
		out = append(out, f(cfg, store))
	}
	return out
}

// NotYetBuilt names APS systems the tool can't check yet. Reports list them
// so a report never looks complete when it isn't. Remove a system from this
// list when its connector is registered.
func NotYetBuilt() []string {
	return []string{"Membership Google Sheet", "Mailing list (Cloudflare)", "Discourse"}
}
