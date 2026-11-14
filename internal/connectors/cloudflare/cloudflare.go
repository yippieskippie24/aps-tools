// Package cloudflare connects the tool to Cloudflare Email Routing, which
// runs APS email and the mailing lists.
//
// How APS mailing lists work: each group address (news@, photos@, …) is
// routed to one Cloudflare Worker, which forwards the mail to the addresses
// in a Text variable named LIST_<NAME> (see lists.go). Cloudflare will only
// forward to *destination addresses* whose owners have clicked a
// verification link. So a member receives list mail only if they are:
//
//  1. a destination address in the Cloudflare account,
//  2. verified, and
//  3. on the all-members list (news@ unless the settings say otherwise).
//
// The member check reports all three. Setup guide: docs/setup/cloudflare.md.
package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// ID is this connector's machine name (used in `aps-tools setup cloudflare`).
const ID = "cloudflare"

// Connector checks members against Cloudflare Email Routing.
type Connector struct {
	cfg      config.Cloudflare
	client   *Client
	token    string
	tokenErr error
}

// New builds the connector from saved settings and the secret store.
func New(cfg config.Cloudflare, store secrets.Store) *Connector {
	token, err := store.Get(secrets.CloudflareAPIToken)
	return &Connector{cfg: cfg, client: NewClient(token), token: token, tokenErr: err}
}

// ID implements connectors.Connector.
func (c *Connector) ID() string { return ID }

// Name implements connectors.Connector.
func (c *Connector) Name() string { return "Mailing list (Cloudflare)" }

// Missing implements connectors.Connector.
func (c *Connector) Missing() []string {
	var m []string
	if c.token == "" {
		if c.tokenErr != nil && !errors.Is(c.tokenErr, secrets.ErrNotFound) {
			m = append(m, "a readable Cloudflare API token ("+c.tokenErr.Error()+")")
		} else {
			m = append(m, "a Cloudflare API token")
		}
	}
	if c.cfg.AccountID == "" || c.cfg.ZoneID == "" {
		m = append(m, "which Cloudflare account and domain APS email uses")
	}
	if len(m) > 0 {
		m[len(m)-1] += " (run: aps-tools setup cloudflare)"
	}
	return m
}

// TestConnection implements connectors.Connector. Read-only.
func (c *Connector) TestConnection(ctx context.Context) error {
	if err := c.client.VerifyToken(ctx, c.cfg.AccountID); err != nil {
		return err
	}
	if _, err := c.client.GetRoutingSettings(ctx, c.cfg.ZoneID); err != nil {
		return errors.New("the token works, but Email Routing for " + c.domain() + " couldn't be read: " + err.Error())
	}
	return nil
}

// CheckMember implements connectors.Connector. Read-only.
func (c *Connector) CheckMember(ctx context.Context, email string) (connectors.MemberStatus, error) {
	all, err := c.CheckMembers(ctx, []string{email})
	return all[email], err
}

// CheckMembers implements connectors.BulkChecker. Cloudflare's address list
// and mailing lists are read once, whatever the number of members, so this
// is what CheckMember uses too. Read-only.
func (c *Connector) CheckMembers(ctx context.Context, emails []string) (map[string]connectors.MemberStatus, error) {
	addrs, err := c.client.ListAddresses(ctx, c.cfg.AccountID)
	if err != nil {
		return nil, err
	}
	lists, err := c.client.LoadLists(ctx, c.cfg.AccountID, c.cfg.ZoneID)
	if err != nil {
		return nil, errors.New("couldn't read the mailing lists: " + err.Error())
	}
	// Before the switch-over, Email Routing is off and nothing is forwarded.
	// Say so, so a ✓ isn't mistaken for "they're receiving list mail".
	note := ""
	if routing, err := c.client.GetRoutingSettings(ctx, c.cfg.ZoneID); err == nil && !routing.Enabled {
		note = "Note: Email Routing is switched off for " + c.domain() + ", so Cloudflare isn't forwarding any mail yet."
	}
	out := make(map[string]connectors.MemberStatus, len(emails))
	now := time.Now()
	for _, email := range emails {
		st := memberStatus(addrs, lists, c.cfg.MembersListName(), email, now)
		if note != "" {
			st.Details = append(st.Details, note)
		}
		out[email] = st
	}
	return out, nil
}

// destination is what Cloudflare knows about one address.
type destination int

const (
	destMissing  destination = iota // not a destination address
	destPending                     // added, not verified yet
	destVerified                    // verified: Cloudflare will forward to it
)

func findDestination(addrs []Address, email string) (destination, Address) {
	for _, a := range addrs {
		if strings.EqualFold(a.Email, email) {
			if a.IsVerified() {
				return destVerified, a
			}
			return destPending, a
		}
	}
	return destMissing, Address{}
}

// Fix-it text shared by several results.
const (
	fixAddDestination = "Add them as a destination address: Cloudflare → Email → Email Routing → Destination addresses → Add. They'll get a verification email."
	fixVerify         = "Ask them to find the email from Cloudflare (check spam) and click \"Verify email address\"."
)

// memberStatus combines the member's destination-address state with their
// list membership into one result. It is separate from CheckMember so it can
// be tested without a network.
func memberStatus(addrs []Address, lists []MailingList, membersList, email string, now time.Time) connectors.MemberStatus {
	dest, addr := findDestination(addrs, email)
	all, haveList := FindList(lists, membersList)

	// No list Worker yet (e.g. before the switch-over): report the
	// destination address only.
	if !haveList {
		st := destinationOnly(dest, addr, now)
		st.Details = append(st.Details, "No \""+membersList+"@\" mailing list was found in Cloudflare yet, so list membership isn't checked.")
		return st
	}

	listName := all.Name + "@"
	var details []string
	if others := otherLists(lists, all.Name, email); len(others) > 0 {
		details = append(details, "Also on: "+strings.Join(others, ", ")+".")
	}
	if all.Secret {
		details = append(details, "The "+all.Variable+" variable is a Secret, so the tool can't read it — change it to Text in the Worker's settings.")
	}
	onList := all.Has(email)

	switch {
	case onList && dest == destVerified:
		return connectors.MemberStatus{
			State:   connectors.StateOK,
			Summary: "On the " + listName + " list and verified with Cloudflare.",
			Details: append([]string{"Verified " + friendlyDate(addr.Verified, now) + "."}, details...),
		}
	case onList && dest == destPending:
		return connectors.MemberStatus{
			State:   connectors.StatePending,
			Summary: "On the " + listName + " list, but they haven't clicked Cloudflare's verification email (added " + friendlyDate(addr.Created, now) + ").",
			Details: append([]string{"Until they do, Cloudflare won't deliver list mail to them."}, details...),
			Fix:     fixVerify,
		}
	case onList && dest == destMissing:
		return connectors.MemberStatus{
			State:   connectors.StateProblem,
			Summary: "On the " + listName + " list, but not a destination address — Cloudflare can't deliver to them.",
			Details: details,
			Fix:     fixAddDestination,
		}
	case dest == destVerified:
		return connectors.MemberStatus{
			State:   connectors.StateMissing,
			Summary: "Verified with Cloudflare, but not on the " + listName + " list.",
			Details: details,
			Fix:     "Add their address to the " + all.Variable + " variable on the \"" + all.Worker + "\" Worker (Settings → Variables and Secrets).",
		}
	default:
		fix := "Add their address to " + all.Variable + " on the \"" + all.Worker + "\" Worker, and "
		if dest == destPending {
			fix += "ask them to click Cloudflare's verification email."
		} else {
			fix += "add them as a destination address (Email Routing → Destination addresses)."
		}
		summary := "Not on the " + listName + " list, and not a destination address."
		if dest == destPending {
			summary = "Not on the " + listName + " list, and they haven't verified their address yet."
		}
		return connectors.MemberStatus{State: connectors.StateMissing, Summary: summary, Details: details, Fix: fix}
	}
}

// destinationOnly reports just the destination-address state.
func destinationOnly(dest destination, addr Address, now time.Time) connectors.MemberStatus {
	switch dest {
	case destVerified:
		return connectors.MemberStatus{
			State:   connectors.StateOK,
			Summary: "Email address verified with Cloudflare, so list mail can reach them.",
			Details: []string{"Verified " + friendlyDate(addr.Verified, now) + "."},
		}
	case destPending:
		return connectors.MemberStatus{
			State:   connectors.StatePending,
			Summary: "Added " + friendlyDate(addr.Created, now) + ", but they haven't clicked Cloudflare's verification email.",
			Details: []string{"Until they do, Cloudflare won't deliver list mail to them."},
			Fix:     fixVerify,
		}
	default:
		return connectors.MemberStatus{
			State:   connectors.StateMissing,
			Summary: "Not a destination address in Cloudflare, so list mail can't reach them.",
			Fix:     fixAddDestination,
		}
	}
}

// otherLists names the lists (other than skip) that email is on.
func otherLists(lists []MailingList, skip, email string) []string {
	var out []string
	for _, l := range lists {
		if l.Name != skip && l.Has(email) {
			out = append(out, l.Name+"@")
		}
	}
	return out
}

// statusFor reports a member's state when there are no lists to check.
// Kept for callers and tests that only have destination addresses.
func statusFor(addrs []Address, email string, now time.Time) connectors.MemberStatus {
	dest, addr := findDestination(addrs, email)
	return destinationOnly(dest, addr, now)
}

// friendlyDate turns an API timestamp into "on 3 Oct 2026 (12 days ago)".
func friendlyDate(ts string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "at an unknown date"
	}
	days := int(now.Sub(t).Hours() / 24)
	ago := "today"
	switch {
	case days == 1:
		ago = "yesterday"
	case days > 1:
		ago = strconv.Itoa(days) + " days ago"
	}
	return "on " + t.Format("2 Jan 2006") + " (" + ago + ")"
}

func (c *Connector) domain() string {
	if c.cfg.ZoneName != "" {
		return c.cfg.ZoneName
	}
	return "the APS domain"
}

// ResendVerification implements connectors.VerificationResender.
// Cloudflare's API can't re-send a verification email, so this does what
// its documentation suggests instead: remove the member's still-unverified
// destination address and add it again, which makes Cloudflare send a new
// one. Nothing is forwarded to an unverified address, so mail delivery
// doesn't change. It checks first, and refuses when:
//   - the address isn't a destination address (onboarding adds it);
//   - it's already verified (nothing to re-send, and removing it would
//     stop their mail);
//   - a routing rule forwards to it (removing it could break the rule).
//
// This CHANGES the account: callers must ask the webmaster first.
func (c *Connector) ResendVerification(ctx context.Context, email string) (string, error) {
	addrs, err := c.client.ListAddresses(ctx, c.cfg.AccountID)
	if err != nil {
		return "", err
	}
	var addr Address
	for _, a := range addrs {
		if strings.EqualFold(a.Email, email) {
			addr = a
		}
	}
	switch {
	case addr.Email == "":
		return "", errors.New("they aren't a destination address yet, so there's nothing to re-send (onboarding adds them)")
	case addr.IsVerified():
		return "", errors.New("their address is already verified, so there's nothing to re-send")
	case addr.ID == "":
		return "", errors.New("Cloudflare didn't say which address record this is; re-send it from the dashboard instead (Email → Email Routing → Destination addresses)")
	}
	if rule, used, err := c.ruleForwardingTo(ctx, email); err != nil {
		return "", errors.New("couldn't check the routing rules first: " + err.Error())
	} else if used {
		return "", fmt.Errorf("the routing rule %q forwards to this address, so the tool won't remove it; re-send from the dashboard instead (Email → Email Routing → Destination addresses)", rule)
	}

	if err := c.client.DeleteAddress(ctx, c.cfg.AccountID, addr.ID); err != nil {
		return "", errors.New("couldn't remove the unverified address: " + err.Error())
	}
	if _, err := c.client.CreateAddress(ctx, c.cfg.AccountID, addr.Email); err != nil {
		return "", errors.New("removed their unverified address but couldn't add it again: " + err.Error() +
			". Add it back by hand: Cloudflare → Email → Email Routing → Destination addresses → Add destination address")
	}
	return "Cloudflare is sending them a new verification email (their unverified address was removed and added again).", nil
}

// ruleForwardingTo reports whether a routing rule (or the catch-all)
// forwards mail straight to email, and the rule's name.
func (c *Connector) ruleForwardingTo(ctx context.Context, email string) (string, bool, error) {
	rules, err := c.client.ListRules(ctx, c.cfg.ZoneID)
	if err != nil {
		return "", false, err
	}
	if catchAll, err := c.client.GetCatchAll(ctx, c.cfg.ZoneID); err == nil {
		catchAll.Name = "catch-all"
		rules = append(rules, catchAll)
	}
	for _, r := range rules {
		for _, a := range r.Actions {
			if a.Type != "forward" {
				continue
			}
			for _, v := range a.Value {
				if strings.EqualFold(v, email) {
					name := r.Name
					if name == "" {
						name = "(unnamed)"
					}
					return name, true, nil
				}
			}
		}
	}
	return "", false, nil
}
