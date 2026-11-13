// Package discourse connects the tool to the APS Discourse forum.
//
// A member is fully set up on the forum when they have an account that is:
// activated (email confirmed), approved (if the forum requires approval),
// not suspended, and — if a member group is configured — in that group.
// Before they sign up, an outstanding invite counts as "waiting on member".
//
// How it authenticates: a Discourse API key created for the webmaster's own
// admin username with the "Read-only" scope, so it can look but never change.
// Setup guide: docs/setup/discourse.md.
package discourse

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// ID is this connector's machine name (used in `aps-tools setup discourse`).
const ID = "discourse"

// Connector checks members against the forum.
type Connector struct {
	cfg    config.Discourse
	client *Client
	key    string
	keyErr error
}

// New builds the connector from saved settings and the secret store.
func New(cfg config.Discourse, store secrets.Store) *Connector {
	key, err := store.Get(secrets.DiscourseAPIKey)
	return &Connector{cfg: cfg, client: NewClient(cfg.URL, cfg.Username, key), key: key, keyErr: err}
}

// ID implements connectors.Connector.
func (c *Connector) ID() string { return ID }

// Name implements connectors.Connector.
func (c *Connector) Name() string { return "Discourse forum" }

// Missing implements connectors.Connector.
func (c *Connector) Missing() []string {
	var m []string
	if c.cfg.URL == "" {
		m = append(m, "the forum address")
	}
	if c.cfg.Username == "" {
		m = append(m, "your Discourse username")
	}
	if c.key == "" {
		if c.keyErr != nil && !errors.Is(c.keyErr, secrets.ErrNotFound) {
			m = append(m, "a readable Discourse API key ("+c.keyErr.Error()+")")
		} else {
			m = append(m, "a Discourse API key")
		}
	}
	if len(m) > 0 {
		m[len(m)-1] += " (run: aps-tools setup discourse)"
	}
	return m
}

// TestConnection implements connectors.Connector. Read-only.
func (c *Connector) TestConnection(ctx context.Context) error {
	if _, err := c.client.Me(ctx); err != nil {
		return err
	}
	// Prove the key can see the admin pages the check relies on.
	if _, _, err := c.client.FindUserByEmail(ctx, "nobody@example.invalid"); err != nil {
		return errors.New("the key works, but can't look up users: " + err.Error())
	}
	return nil
}

// CheckMember implements connectors.Connector. Read-only.
func (c *Connector) CheckMember(ctx context.Context, email string) (connectors.MemberStatus, error) {
	user, found, err := c.client.FindUserByEmail(ctx, email)
	if err != nil {
		return connectors.MemberStatus{}, err
	}
	var invites []Invite
	if !found || user.Staged {
		// Only look for invites when there's no real account yet.
		pending, err := c.client.Invites(ctx, "pending", email)
		if err != nil {
			return connectors.MemberStatus{}, errors.New("couldn't read invites: " + err.Error())
		}
		expired, err := c.client.Invites(ctx, "expired", email)
		if err != nil {
			return connectors.MemberStatus{}, errors.New("couldn't read invites: " + err.Error())
		}
		invites = append(pending, expired...)
	}
	return memberStatus(user, found, invites, email, c.cfg.MemberGroup, time.Now()), nil
}

const fixInvite = "Send them an invite: on the forum, open your profile → Invites → Invite (enter their email)."

// memberStatus decides the result. It is separate from CheckMember so it can
// be tested without a network.
func memberStatus(u AdminUser, found bool, invites []Invite, email, memberGroup string, now time.Time) connectors.MemberStatus {
	if !found || u.Staged {
		return noAccountStatus(u, found, invites, email, now)
	}

	details := []string{"Username: @" + u.Username + "."}
	if u.LastSeenAt != "" {
		details = append(details, "Last seen "+ago(u.LastSeenAt, now)+".")
	} else {
		details = append(details, "Has never visited since signing up.")
	}
	if groups := userGroups(u); len(groups) > 0 {
		details = append(details, "Groups: "+strings.Join(groups, ", ")+".")
	}

	switch {
	case u.SuspendedTill != "" && after(u.SuspendedTill, now):
		return connectors.MemberStatus{State: connectors.StateProblem, Details: details,
			Summary: "Has an account, but it's suspended until " + day(u.SuspendedTill) + ".",
			Fix:     "If that's a mistake: Admin → Users → @" + u.Username + " → Unsuspend."}
	case !u.IsActive():
		return connectors.MemberStatus{State: connectors.StatePending, Details: details,
			Summary: "Signed up " + ago(u.CreatedAt, now) + ", but hasn't confirmed their email address.",
			Fix:     "Ask them to find the activation email from the forum (check spam), or resend it: Admin → Users → @" + u.Username + " → Send Activation Email."}
	case !u.IsApproved():
		// Discourse marks users approved automatically unless the forum
		// requires approval, so false here means it's waiting on an admin.
		return connectors.MemberStatus{State: connectors.StateProblem, Details: details,
			Summary: "Has an account that's waiting for an admin to approve it.",
			Fix:     "Approve it: Admin → Users → @" + u.Username + " → Approve (or the Review queue)."}
	case memberGroup != "" && !inGroup(u, memberGroup):
		return connectors.MemberStatus{State: connectors.StateProblem, Details: details,
			Summary: "Has an account, but isn't in the \"" + memberGroup + "\" group.",
			Fix:     "Add them: forum → Groups → " + memberGroup + " → Add Users → @" + u.Username + "."}
	}
	if u.SilencedTill != "" && after(u.SilencedTill, now) {
		details = append(details, "Note: silenced (can't post) until "+day(u.SilencedTill)+".")
	}
	summary := "Has an active account."
	if memberGroup != "" {
		summary = "Has an active account and is in the \"" + memberGroup + "\" group."
	}
	return connectors.MemberStatus{State: connectors.StateOK, Summary: summary, Details: details}
}

func noAccountStatus(u AdminUser, found bool, invites []Invite, email string, now time.Time) connectors.MemberStatus {
	var details []string
	if found && u.Staged {
		details = append(details, "There's a placeholder account (@"+u.Username+") from them emailing the forum, but they've never signed up.")
	}
	var latest *Invite
	for i := range invites {
		inv := &invites[i]
		if !strings.EqualFold(inv.Email, email) {
			continue
		}
		if latest == nil || inv.CreatedAt > latest.CreatedAt {
			latest = inv
		}
	}
	if latest != nil && !latest.Expired && (latest.ExpiresAt == "" || after(latest.ExpiresAt, now)) {
		if latest.ExpiresAt != "" {
			details = append(details, "The invite expires "+day(latest.ExpiresAt)+".")
		}
		return connectors.MemberStatus{State: connectors.StatePending, Details: details,
			Summary: "Invited " + ago(latest.CreatedAt, now) + ", but hasn't accepted yet.",
			Fix:     "Remind them to look for the invite email (check spam), or resend it: your profile → Invites → Pending → Resend."}
	}
	if latest != nil {
		return connectors.MemberStatus{State: connectors.StateMissing, Details: details,
			Summary: "Invited " + ago(latest.CreatedAt, now) + ", but the invite expired before they accepted.",
			Fix:     fixInvite}
	}
	details = append(details, "Only invites you sent are visible to the tool; another admin's invite wouldn't show here.")
	return connectors.MemberStatus{State: connectors.StateMissing, Details: details,
		Summary: "No forum account, and no invite found.",
		Fix:     fixInvite}
}

// userGroups lists the user's own (non-automatic) groups.
func userGroups(u AdminUser) []string {
	var out []string
	for _, g := range u.Groups {
		if !g.Automatic {
			out = append(out, g.Name)
		}
	}
	return out
}

func inGroup(u AdminUser, name string) bool {
	for _, g := range u.Groups {
		if strings.EqualFold(g.Name, name) {
			return true
		}
	}
	return false
}

func parseTime(ts string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, ts)
	return t, err == nil
}

func after(ts string, now time.Time) bool {
	t, ok := parseTime(ts)
	return ok && t.After(now)
}

// day formats a timestamp as "3 Oct 2026".
func day(ts string) string {
	if t, ok := parseTime(ts); ok {
		return t.Format("2 Jan 2006")
	}
	return "an unknown date"
}

// ago formats a timestamp as "on 3 Oct 2026 (12 days ago)".
func ago(ts string, now time.Time) string {
	t, ok := parseTime(ts)
	if !ok {
		return "at an unknown date"
	}
	days := int(now.Sub(t).Hours() / 24)
	rel := "today"
	switch {
	case days == 1:
		rel = "yesterday"
	case days > 1:
		rel = strconv.Itoa(days) + " days ago"
	}
	return "on " + t.Format("2 Jan 2006") + " (" + rel + ")"
}
