// Package doctor checks that the tool is set up correctly and can reach every
// outside system. It powers both `aps-tools doctor` and the "Test connections"
// screen in the app, so they always agree.
//
// Every result says what's wrong AND what to do about it, in plain English,
// because the person reading it may not be technical.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// Status is the outcome of one check.
type Status int

const (
	// OK means everything is fine.
	OK Status = iota
	// Info is neutral information, not a problem.
	Info
	// Warn means something needs attention but nothing is broken.
	Warn
	// Fail means something is broken.
	Fail
)

// String gives each status a readable name (used in tests and plain output).
func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Info:
		return "info"
	case Warn:
		return "warn"
	default:
		return "fail"
	}
}

// Result is the outcome of one check, ready to show to a person.
type Result struct {
	// Name is what was checked, e.g. "Settings file".
	Name string
	// Status is how it went.
	Status Status
	// Detail describes what was found.
	Detail string
	// Fix tells the user what to do. Empty when nothing needs doing.
	Fix string
}

// Deps are the things Run needs. Production code uses DefaultDeps; tests
// swap in fakes so they never touch your real settings or Keychain.
type Deps struct {
	// ConfigPath returns where the settings file lives.
	ConfigPath func() (string, error)
	// LoadConfig reads the settings file.
	LoadConfig func() (*config.Config, error)
	// Store is where secrets are kept.
	Store secrets.Store
	// Connectors builds the list of outside systems to test.
	Connectors func(*config.Config, secrets.Store) []connectors.Connector
}

// DefaultDeps returns the real settings file, Keychain, and connectors.
func DefaultDeps() Deps {
	return Deps{
		ConfigPath: config.Path,
		LoadConfig: config.Load,
		Store:      secrets.Keychain(),
		Connectors: connectors.All,
	}
}

// connectionTimeout limits how long we wait for any one system to answer.
const connectionTimeout = 15 * time.Second

// Run performs every check and returns the results in display order.
func Run(ctx context.Context, d Deps) []Result {
	var results []Result

	// 1. Settings file.
	path, _ := d.ConfigPath()
	cfg, err := d.LoadConfig()
	switch {
	case errors.Is(err, config.ErrNotFound):
		results = append(results, Result{
			Name: "Settings file", Status: Warn,
			Detail: "Not created yet.",
			Fix:    "Run: aps-tools config init",
		})
		cfg = &config.Config{} // carry on with empty settings
	case err != nil:
		results = append(results, Result{
			Name: "Settings file", Status: Fail,
			Detail: err.Error(),
			Fix:    "Open the file in a text editor and correct the setting named above.",
		})
		cfg = &config.Config{}
	default:
		results = append(results, Result{Name: "Settings file", Status: OK, Detail: path})

		// 2. Webmaster details (only meaningful once the file exists).
		if cfg.Webmaster.Name == "" || cfg.Webmaster.Email == "" {
			results = append(results, Result{
				Name: "Your details", Status: Warn,
				Detail: "Your name and/or email aren't filled in yet.",
				Fix:    "Edit the [webmaster] section of " + path,
			})
		} else {
			results = append(results, Result{
				Name: "Your details", Status: OK,
				Detail: fmt.Sprintf("%s <%s>", cfg.Webmaster.Name, cfg.Webmaster.Email),
			})
		}
	}

	// 3. Keychain.
	if err := secrets.CheckAccess(d.Store); err != nil {
		results = append(results, Result{
			Name: "Keychain", Status: Fail,
			Detail: err.Error(),
			Fix:    "Make sure you're logged in to your Mac and the login keychain is unlocked (Keychain Access app).",
		})
	} else {
		results = append(results, Result{Name: "Keychain", Status: OK, Detail: "Accessible."})
	}

	// 4. Outside systems.
	conns := d.Connectors(cfg, d.Store)
	if len(conns) == 0 {
		results = append(results, Result{
			Name: "APS systems", Status: Info,
			Detail: "No systems connected yet. Google Sheets, Cloudflare, Jira and Discourse are added in Phase 1.",
		})
		return results
	}
	return append(results, testConnectors(ctx, conns)...)
}

// testConnectors tests every connector at the same time (so one slow system
// doesn't hold up the rest) and returns results in the original order.
func testConnectors(ctx context.Context, conns []connectors.Connector) []Result {
	out := make([]Result, len(conns))
	var wg sync.WaitGroup
	for i, c := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = testOne(ctx, c)
		}()
	}
	wg.Wait()
	return out
}

func testOne(ctx context.Context, c connectors.Connector) Result {
	if missing := c.Missing(); len(missing) > 0 {
		return Result{
			Name: c.Name(), Status: Warn,
			Detail: "Not set up yet.",
			Fix:    "Still needed: " + joinPlain(missing),
		}
	}
	ctx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	if err := c.TestConnection(ctx); err != nil {
		return Result{Name: c.Name(), Status: Fail, Detail: err.Error()}
	}
	return Result{Name: c.Name(), Status: OK, Detail: "Connected."}
}

// joinPlain joins items as "a, b and c".
func joinPlain(items []string) string {
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

// Worst returns the most serious status among the results (OK if empty).
func Worst(results []Result) Status {
	worst := OK
	for _, r := range results {
		if r.Status > worst {
			worst = r.Status
		}
	}
	return worst
}

// Summary is a one-line, plain-English verdict on the results.
func Summary(results []Result) string {
	switch Worst(results) {
	case Fail:
		return "Something is broken — follow the → steps above."
	case Warn:
		return "Nothing is broken, but some setup is unfinished — see the → steps above."
	default:
		return "All good — everything is set up and connected."
	}
}

// Healthy reports whether no result is a failure.
func Healthy(results []Result) bool {
	for _, r := range results {
		if r.Status == Fail {
			return false
		}
	}
	return true
}
