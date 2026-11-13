package cloudflare

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// This file powers `aps-tools cloudflare discover`: a READ-ONLY look at how
// APS email is wired up in Cloudflare, so the webmaster (and the tool's
// maintainers) can see where the mailing list's member list is kept.
//
// Privacy: member email addresses are never shown. Anything that looks like
// an email address is replaced with a placeholder before it is displayed.

// Discovery is everything `cloudflare discover` found.
type Discovery struct {
	Zone     string
	Account  string
	Routing  RoutingSettings
	Rules    []RuleSummary
	CatchAll RuleSummary

	AddressesTotal    int
	AddressesVerified int

	Workers []WorkerSummary
	// Lists are the group addresses handled by a Worker, with counts only.
	Lists []ListSummary
	// ListsErr explains why the lists couldn't be read, if they couldn't.
	ListsErr string
	// Notes are plain-English conclusions, e.g. where the list probably lives.
	Notes []string
}

// ListSummary describes one mailing list using counts only — no addresses.
type ListSummary struct {
	Address        string
	Variable       string
	Worker         string
	Enabled        bool
	Defined        bool
	Secret         bool
	Members        int
	Unverified     int // on the list, added as a destination, not verified yet
	NotDestination int // on the list, but not a destination address at all
}

// RuleSummary describes one routing rule without revealing member emails.
type RuleSummary struct {
	Address string // the APS address mail is sent to (e.g. list@…), or "Catch-all"
	Action  string // e.g. `Worker "mailing-list"` or "forward to 2 addresses"
	Enabled bool
	Worker  string // set when the action is a Worker
}

// WorkerSummary describes a Worker that handles email.
type WorkerSummary struct {
	Name         string
	Bindings     []BindingSummary
	Modules      []ModuleSummary
	EnvUsed      []string // env.X names the code refers to
	Forwards     bool     // calls message.forward(...)
	RedactedCode string   // full code with emails hidden (for --save-code)
	Err          string   // why the Worker couldn't be read, if it couldn't
}

// BindingSummary describes one Worker binding.
type BindingSummary struct {
	Name   string
	Kind   string // plain-English type
	Detail string // e.g. "312 keys" — never contains raw emails
}

// ModuleSummary describes one file of Worker code.
type ModuleSummary struct {
	Name       string
	Bytes      int
	EmailCount int // email-like strings in this file
}

// emailPattern finds anything shaped like an email address.
var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// Redact replaces every email-like string with "[email hidden]", except
// addresses on the APS domain itself (like list@aps-domain), which are
// configuration rather than personal data.
func Redact(s, keepDomain string) string {
	return emailPattern.ReplaceAllStringFunc(s, func(e string) string {
		if keepDomain != "" && strings.HasSuffix(strings.ToLower(e), "@"+strings.ToLower(keepDomain)) {
			return e
		}
		return "[email hidden]"
	})
}

// countEmails counts email-like strings, ignoring APS-domain addresses.
func countEmails(s, keepDomain string) int {
	n := 0
	for _, e := range emailPattern.FindAllString(s, -1) {
		if keepDomain == "" || !strings.HasSuffix(strings.ToLower(e), "@"+strings.ToLower(keepDomain)) {
			n++
		}
	}
	return n
}

var envPattern = regexp.MustCompile(`\benv\.([A-Za-z_][A-Za-z0-9_]*)`)

// Discover gathers the discovery report for one account and zone.
// Errors on optional parts are recorded in the report rather than returned,
// so one missing permission doesn't hide everything else.
func (c *Client) Discover(ctx context.Context, accountID, accountName, zoneID, zoneName string) (Discovery, error) {
	d := Discovery{Zone: zoneName, Account: accountName}

	var err error
	if d.Routing, err = c.GetRoutingSettings(ctx, zoneID); err != nil {
		return d, fmt.Errorf("couldn't read Email Routing settings: %w", err)
	}
	rules, err := c.ListRules(ctx, zoneID)
	if err != nil {
		return d, fmt.Errorf("couldn't read routing rules: %w", err)
	}
	for _, r := range rules {
		d.Rules = append(d.Rules, summarizeRule(r, zoneName))
	}
	if ca, err := c.GetCatchAll(ctx, zoneID); err == nil {
		d.CatchAll = summarizeRule(ca, zoneName)
		d.CatchAll.Address = "Catch-all (any other address)"
	}

	addrs, err := c.ListAddresses(ctx, accountID)
	if err != nil {
		return d, fmt.Errorf("couldn't read destination addresses: %w", err)
	}
	d.AddressesTotal = len(addrs)
	for _, a := range addrs {
		if a.IsVerified() {
			d.AddressesVerified++
		}
	}

	// Read each mailing list and count delivery problems.
	if lists, err := c.LoadLists(ctx, accountID, zoneID); err != nil {
		d.ListsErr = err.Error()
	} else {
		d.Lists = summarizeLists(lists, addrs)
	}

	// Look inside every Worker that receives email.
	seen := map[string]bool{}
	for _, r := range append(append([]RuleSummary{}, d.Rules...), d.CatchAll) {
		if r.Worker == "" || seen[r.Worker] {
			continue
		}
		seen[r.Worker] = true
		d.Workers = append(d.Workers, c.inspectWorker(ctx, accountID, r.Worker, zoneName))
	}

	d.Notes = conclusions(d)
	return d, nil
}

// summarizeLists counts, for each list, members Cloudflare can't deliver to.
func summarizeLists(lists []MailingList, addrs []Address) []ListSummary {
	var out []ListSummary
	for _, l := range lists {
		s := ListSummary{Address: l.Address, Variable: l.Variable, Worker: l.Worker,
			Enabled: l.Enabled, Defined: l.Defined, Secret: l.Secret, Members: len(l.Members)}
		for _, m := range l.Members {
			switch dest, _ := findDestination(addrs, m); dest {
			case destPending:
				s.Unverified++
			case destMissing:
				s.NotDestination++
			}
		}
		out = append(out, s)
	}
	return out
}

func summarizeRule(r Rule, zone string) RuleSummary {
	s := RuleSummary{Enabled: r.Enabled}
	for _, m := range r.Matchers {
		if m.Type == "literal" && m.Value != "" {
			s.Address = Redact(m.Value, zone)
		}
	}
	if s.Address == "" {
		// e.g. a leftover rule with no specific address
		s.Address = "(rule with no address)"
		if r.Name != "" {
			s.Address = "(" + r.Name + ")"
		}
	}
	for _, a := range r.Actions {
		switch a.Type {
		case "worker":
			if len(a.Value) > 0 {
				s.Worker = a.Value[0]
			}
			s.Action = fmt.Sprintf("Worker %q", s.Worker)
		case "forward":
			s.Action = fmt.Sprintf("forward to %d address(es)", len(a.Value))
		case "drop":
			s.Action = "drop (discard)"
		default:
			s.Action = a.Type
		}
	}
	return s
}

func (c *Client) inspectWorker(ctx context.Context, accountID, name, zone string) WorkerSummary {
	w := WorkerSummary{Name: name}

	bindings, err := c.GetWorkerBindings(ctx, accountID, name)
	if err != nil {
		w.Err = "couldn't read its settings: " + err.Error()
	}
	for _, b := range bindings {
		bs := BindingSummary{Name: b.Name, Kind: b.Type}
		switch b.Type {
		case "kv_namespace":
			bs.Kind = "KV storage"
			n, sample, err := c.CountKVKeys(ctx, accountID, b.NamespaceID, 10000)
			if err != nil {
				bs.Detail = "couldn't list keys: " + err.Error()
				break
			}
			bs.Detail = fmt.Sprintf("%d keys", n)
			if len(sample) > 0 {
				bs.Detail += "; e.g. " + Redact(strings.Join(sample, ", "), zone)
			}
		case "plain_text":
			bs.Kind = "text setting"
			if n := countEmails(b.Text, zone); n > 0 {
				bs.Detail = fmt.Sprintf("contains %d email address(es)", n)
			} else {
				bs.Detail = fmt.Sprintf("%d characters", len(b.Text))
			}
		case "secret_text":
			bs.Kind = "secret setting"
			bs.Detail = "value hidden by Cloudflare"
		case "d1":
			bs.Kind = "D1 database"
		}
		w.Bindings = append(w.Bindings, bs)
	}

	mods, err := c.GetWorkerCode(ctx, accountID, name)
	if err != nil {
		if w.Err == "" {
			w.Err = "couldn't read its code: " + err.Error()
		}
		return w
	}
	envSeen := map[string]bool{}
	var code strings.Builder
	for _, m := range mods {
		w.Modules = append(w.Modules, ModuleSummary{Name: m.Name, Bytes: len(m.Code), EmailCount: countEmails(m.Code, zone)})
		if strings.Contains(m.Code, ".forward(") {
			w.Forwards = true
		}
		for _, match := range envPattern.FindAllStringSubmatch(m.Code, -1) {
			envSeen[match[1]] = true
		}
		fmt.Fprintf(&code, "// ===== %s =====\n%s\n", m.Name, Redact(m.Code, zone))
	}
	for k := range envSeen {
		w.EnvUsed = append(w.EnvUsed, k)
	}
	sort.Strings(w.EnvUsed)
	w.RedactedCode = code.String()
	return w
}

// conclusions turns the raw findings into plain-English notes.
func conclusions(d Discovery) []string {
	var notes []string
	if !d.Routing.Enabled {
		notes = append(notes, "Email Routing is switched OFF for this domain.")
	}
	if pending := d.AddressesTotal - d.AddressesVerified; pending > 0 {
		notes = append(notes, fmt.Sprintf("%d destination address(es) are still waiting for their owner to click Cloudflare's verification email — those people won't get list mail.", pending))
	}
	for _, w := range d.Workers {
		for _, m := range w.Modules {
			if m.EmailCount > 0 {
				notes = append(notes, fmt.Sprintf("Worker %q has %d email address(es) written into its code (%s) — the member list is probably kept in the code itself.", w.Name, m.EmailCount, m.Name))
			}
		}
		for _, b := range w.Bindings {
			if b.Kind == "KV storage" {
				notes = append(notes, fmt.Sprintf("Worker %q uses KV storage %q (%s) — the member list may be kept there.", w.Name, b.Name, b.Detail))
			}
			if b.Kind == "text setting" && strings.HasPrefix(b.Detail, "contains") && !strings.HasPrefix(b.Name, "LIST_") {
				notes = append(notes, fmt.Sprintf("Worker %q's setting %q %s — the member list may be kept there.", w.Name, b.Name, b.Detail))
			}
			if b.Kind == "D1 database" {
				notes = append(notes, fmt.Sprintf("Worker %q uses a D1 database %q — the member list may be kept there.", w.Name, b.Name))
			}
		}
	}
	for _, l := range d.Lists {
		switch {
		case l.Secret:
			notes = append(notes, fmt.Sprintf("%s is a Secret, so the tool can't read the %s list — change it to Text in the Worker's settings.", l.Variable, l.Address))
		case !l.Defined:
			notes = append(notes, fmt.Sprintf("Mail to %s goes to the Worker, but there's no %s variable — the Worker rejects that mail.", l.Address, l.Variable))
		}
		if l.NotDestination > 0 {
			notes = append(notes, fmt.Sprintf("%d address(es) on %s aren't destination addresses — Cloudflare can't deliver to them. Add them under Destination addresses.", l.NotDestination, l.Address))
		}
		if l.Unverified > 0 {
			notes = append(notes, fmt.Sprintf("%d address(es) on %s haven't been verified yet — they won't get mail from that list until they do.", l.Unverified, l.Address))
		}
	}
	if d.ListsErr != "" {
		notes = append(notes, "The mailing lists couldn't be read: "+d.ListsErr)
	}
	if len(d.Workers) == 0 {
		notes = append(notes, "No routing rule sends mail to a Worker. The list may forward to addresses directly (see the rules above).")
	}
	return notes
}
