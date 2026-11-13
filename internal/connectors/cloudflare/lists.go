package cloudflare

import (
	"context"
	"regexp"
	"strings"
)

// This file reads APS's mailing lists the same way the list Worker does.
//
// How the APS list Worker works (see docs/setup/cloudflare.md):
//   - Each group address (news@, photos@, …) has a routing rule that sends
//     its mail to one Worker.
//   - The Worker looks up a Text variable named LIST_ + the part of the
//     address before the @, in capitals, with anything that isn't a letter or
//     digit turned into "_":  news@ → LIST_NEWS, board-members@ → LIST_BOARD_MEMBERS.
//   - The variable holds the members' addresses, separated by commas,
//     spaces or semicolons. The Worker forwards to each one.
//
// IMPORTANT: ListVariable and parseMembers must match the Worker's code
// exactly. If the Worker's naming or parsing changes, change these too.

// MailingList is one group address and who is on it.
type MailingList struct {
	Address  string   // full address, e.g. "news@example.org"
	Name     string   // the part before the @, e.g. "news"
	Worker   string   // the Worker that handles it
	Variable string   // e.g. "LIST_NEWS"
	Members  []string // lower-cased addresses, no duplicates
	Defined  bool     // the variable exists on the Worker
	Secret   bool     // the variable is a Secret, so its value can't be read
	Enabled  bool     // the routing rule is switched on
}

var notListChar = regexp.MustCompile(`[^A-Z0-9]`)

// ListVariable returns the Worker variable that holds a list's members,
// following the Worker's rule (see the file comment).
func ListVariable(address string) string {
	local := strings.ToLower(address)
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	if i := strings.Index(local, "+"); i >= 0 {
		local = local[:i]
	}
	return "LIST_" + notListChar.ReplaceAllString(strings.ToUpper(local), "_")
}

var listSeparators = regexp.MustCompile(`[\s,;]+`)

// parseMembers splits a LIST_ variable into addresses, like the Worker does.
func parseMembers(v string) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range listSeparators.Split(v, -1) {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// Has reports whether email is on the list.
func (l MailingList) Has(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, m := range l.Members {
		if m == email {
			return true
		}
	}
	return false
}

// LoadLists finds every group address routed to a Worker and reads its
// member list from that Worker's variables. It returns no lists (and no
// error) when no address is routed to a Worker — e.g. before the switch-over.
func (c *Client) LoadLists(ctx context.Context, accountID, zoneID string) ([]MailingList, error) {
	rules, err := c.ListRules(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	bindingsByWorker := map[string][]Binding{}
	var lists []MailingList
	for _, r := range rules {
		address, worker := "", ""
		for _, m := range r.Matchers {
			if m.Type == "literal" && m.Field == "to" && m.Value != "" {
				address = strings.ToLower(m.Value)
			}
		}
		for _, a := range r.Actions {
			if a.Type == "worker" && len(a.Value) > 0 {
				worker = a.Value[0]
			}
		}
		if address == "" || worker == "" {
			continue
		}
		bindings, ok := bindingsByWorker[worker]
		if !ok {
			if bindings, err = c.GetWorkerBindings(ctx, accountID, worker); err != nil {
				return nil, err
			}
			bindingsByWorker[worker] = bindings
		}
		l := MailingList{
			Address:  address,
			Name:     address[:strings.Index(address, "@")],
			Worker:   worker,
			Variable: ListVariable(address),
			Enabled:  r.Enabled,
		}
		for _, b := range bindings {
			if b.Name != l.Variable {
				continue
			}
			l.Defined = true
			switch b.Type {
			case "plain_text":
				l.Members = parseMembers(b.Text)
			case "secret_text":
				l.Secret = true
			}
		}
		lists = append(lists, l)
	}
	return lists, nil
}

// FindList returns the list whose name (the part before the @) matches
// name, which may be given as "news" or "news@example.org".
func FindList(lists []MailingList, name string) (MailingList, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	for _, l := range lists {
		if l.Name == name {
			return l, true
		}
	}
	return MailingList{}, false
}
