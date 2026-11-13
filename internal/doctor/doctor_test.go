package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// fakeConnector lets tests decide how a connector behaves.
type fakeConnector struct {
	name    string
	missing []string
	testErr error
}

func (f fakeConnector) ID() string                           { return f.name }
func (f fakeConnector) Name() string                         { return f.name }
func (f fakeConnector) Missing() []string                    { return f.missing }
func (f fakeConnector) TestConnection(context.Context) error { return f.testErr }
func (f fakeConnector) CheckMember(context.Context, string) (connectors.MemberStatus, error) {
	return connectors.MemberStatus{}, nil
}

func deps(cfg *config.Config, loadErr error, conns ...connectors.Connector) Deps {
	return Deps{
		ConfigPath:       func() (string, error) { return "/tmp/example/config.toml", nil },
		LoadConfig:       func() (*config.Config, error) { return cfg, loadErr },
		Secrets:          func(*config.Config) secrets.Store { return secrets.NewMemory() },
		CheckOnePassword: func() error { return nil },
		Connectors:       func(*config.Config, secrets.Store) []connectors.Connector { return conns },
	}
}

func find(t *testing.T, rs []Result, name string) Result {
	t.Helper()
	for _, r := range rs {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no result named %q in %+v", name, rs)
	return Result{}
}

func TestMissingSettingsFileIsAWarningWithAFix(t *testing.T) {
	rs := Run(context.Background(), deps(nil, config.ErrNotFound))
	r := find(t, rs, "Settings file")
	if r.Status != Warn || r.Fix == "" {
		t.Errorf("got %+v, want a warning with a fix", r)
	}
	if !Healthy(rs) {
		t.Error("a missing settings file should not count as a failure")
	}
}

func TestCompleteSettings(t *testing.T) {
	cfg := &config.Config{Webmaster: config.Webmaster{Name: "Alex Example", Email: "alex@example.com"}}
	rs := Run(context.Background(), deps(cfg, nil))
	if r := find(t, rs, "Your details"); r.Status != OK {
		t.Errorf("Your details = %+v, want OK", r)
	}
	if r := find(t, rs, "APS systems"); r.Status != Info {
		t.Errorf("with no connectors, want an Info row, got %+v", r)
	}
}

func TestConnectorResultsKeepOrderAndReportProblems(t *testing.T) {
	cfg := &config.Config{}
	rs := Run(context.Background(), deps(cfg, nil,
		fakeConnector{name: "Ready"},
		fakeConnector{name: "Unconfigured", missing: []string{"an API key", "a site URL"}},
		fakeConnector{name: "Broken", testErr: errors.New("401 unauthorised")},
	))

	if r := find(t, rs, "Ready"); r.Status != OK {
		t.Errorf("Ready = %+v", r)
	}
	if r := find(t, rs, "Unconfigured"); r.Status != Warn || r.Fix != "Still needed: an API key and a site URL" {
		t.Errorf("Unconfigured = %+v", r)
	}
	if r := find(t, rs, "Broken"); r.Status != Fail {
		t.Errorf("Broken = %+v", r)
	}
	if Healthy(rs) {
		t.Error("a failing connector should make the result unhealthy")
	}

	// Connectors run in parallel but must be reported in registry order.
	pos := map[string]int{}
	for i, r := range rs {
		pos[r.Name] = i
	}
	if !(pos["Ready"] < pos["Unconfigured"] && pos["Unconfigured"] < pos["Broken"]) {
		t.Errorf("connector results out of order: %+v", rs)
	}
}
