package discourse

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// This file powers `aps-tools discourse discover`: a READ-ONLY summary of
// how the forum handles sign-ups, so onboarding can be designed around it.
// It shows settings and counts only — never members' names or emails.

// SettingSummary is one sign-up-related setting in plain English.
type SettingSummary struct {
	Name    string // Discourse's setting name, e.g. "invite_only"
	Label   string // plain English, e.g. "New members need an invite"
	Value   string
	Present bool
}

// Discovery is everything `discourse discover` found.
type Discovery struct {
	Site           string
	Admin          string
	Settings       []SettingSummary
	LoginMethods   []string // e.g. "email and password", "Google"
	Groups         []Group  // non-automatic groups only
	PendingInvites int      // invites sent by the webmaster, not yet accepted
	ExpiredInvites int
	Notes          []string
	SettingsErr    string
}

// signupSettings are the settings that shape how members join, with a
// plain-English label for each.
var signupSettings = []struct{ name, label string }{
	{"login_required", "Only signed-in people can read the forum"},
	{"invite_only", "New members need an invite (no open sign-up)"},
	{"must_approve_users", "An admin must approve each new account"},
	{"allow_new_registrations", "New accounts can be created"},
	{"enable_local_logins", "People can log in with email + password"},
	{"enable_discourse_connect", "Logins are handled by another website (DiscourseConnect)"},
	{"invite_expiry_days", "Invites expire after (days)"},
	{"max_invites_per_day", "Max invites per day"},
}

// loginProviders maps "enable_X_logins" settings to a readable name.
var loginProviders = map[string]string{
	"enable_google_oauth2_logins": "Google",
	"enable_facebook_logins":      "Facebook",
	"enable_github_logins":        "GitHub",
	"enable_twitter_logins":       "X (Twitter)",
	"enable_discord_logins":       "Discord",
	"enable_apple_logins":         "Apple",
	"enable_linkedin_oidc_logins": "LinkedIn",
	"openid_connect_enabled":      "OpenID Connect",
}

// Discover gathers the discovery report. Optional parts that fail are noted
// in the report rather than failing the whole thing.
func (c *Client) Discover(ctx context.Context) (Discovery, error) {
	d := Discovery{Site: c.baseURL}
	me, err := c.Me(ctx)
	if err != nil {
		return d, err
	}
	d.Admin = "@" + me.Username

	settings, err := c.SiteSettings(ctx)
	if err != nil {
		d.SettingsErr = err.Error()
	}
	for _, s := range signupSettings {
		v, ok := settings[s.name]
		d.Settings = append(d.Settings, SettingSummary{Name: s.name, Label: s.label, Value: v, Present: ok})
	}
	if settings["enable_local_logins"] == "true" {
		d.LoginMethods = append(d.LoginMethods, "email and password")
	}
	var providers []string
	for k, name := range loginProviders {
		if settings[k] == "true" {
			providers = append(providers, name)
		}
	}
	sort.Strings(providers)
	d.LoginMethods = append(d.LoginMethods, providers...)

	groups, err := c.Groups(ctx)
	if err != nil {
		d.Notes = append(d.Notes, "Couldn't list groups: "+err.Error())
	}
	for _, g := range groups {
		if !g.Automatic {
			d.Groups = append(d.Groups, g)
		}
	}

	if inv, err := c.Invites(ctx, "pending", ""); err == nil {
		d.PendingInvites = len(inv)
	} else {
		d.Notes = append(d.Notes, "Couldn't list invites: "+err.Error())
	}
	if inv, err := c.Invites(ctx, "expired", ""); err == nil {
		d.ExpiredInvites = len(inv)
	}

	d.Notes = append(d.Notes, conclusions(d, settings)...)
	return d, nil
}

func conclusions(d Discovery, s map[string]string) []string {
	var notes []string
	if d.SettingsErr != "" {
		return []string{"Couldn't read the forum's settings: " + d.SettingsErr}
	}
	switch {
	case s["enable_discourse_connect"] == "true":
		notes = append(notes, "Logins come from another website (DiscourseConnect), so accounts are created there, not by forum invites.")
	case s["invite_only"] == "true":
		notes = append(notes, "Sign-up is invite-only: onboarding a member means sending them a forum invite.")
	default:
		notes = append(notes, "Anyone can sign up (not invite-only). Onboarding can send an invite, or just tell members to sign up.")
	}
	if s["must_approve_users"] == "true" {
		notes = append(notes, "New accounts wait for admin approval. The member check flags accounts still waiting.")
	}
	if s["login_required"] == "true" {
		notes = append(notes, "The forum is private: people must log in to read it.")
	}
	if d.PendingInvites > 0 {
		notes = append(notes, fmt.Sprintf("You have %d invite(s) not yet accepted.", d.PendingInvites))
	}
	if len(d.Groups) > 0 {
		var names []string
		for _, g := range d.Groups {
			names = append(names, g.Name)
		}
		notes = append(notes, "If every member should be in one of these groups ("+strings.Join(names, ", ")+"), choose it in `aps-tools setup discourse` and the check will confirm it.")
	}
	return notes
}
