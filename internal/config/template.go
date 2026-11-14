package config

import (
	"fmt"
	"strings"
	"text/template"
)

// fileTemplate is the layout of config.toml, with a comment above every
// setting. Both `aps-tools config init` and Save write the file through this
// template, so the explanations are never lost when the tool updates a value.
//
// Keep it in sync with the Config struct in config.go: every setting must
// appear here, or Save will silently drop it (a test checks this).
// Never put real APS values here — this file is in a public repository.
const fileTemplate = `# APS Webmaster Tool — settings
#
# This file holds NON-SECRET settings only. API keys and tokens are kept in
# the macOS Keychain or in 1Password — see the [secrets] section.
# It is safe to share this file with the next webmaster.
#
# The tool updates this file when you run "aps-tools setup ...". You can also
# edit it by hand; run "aps-tools doctor" afterwards to check it.

[webmaster]
# Your name, as it should appear when signing welcome emails.
name = {{ q .Webmaster.Name }}
# The email address members should reply to.
email = {{ q .Webmaster.Email }}

[secrets]
# Where API keys are kept: "keychain" (the macOS Keychain — the default) or
# "1password". Change it with: aps-tools setup secrets
store = {{ q .Secrets.Store }}
# When using 1Password, the tool stores only a "secret reference" (op://...)
# for each key — never the key itself. In 1Password, open the item, click the
# arrow next to the field, and choose "Copy Secret Reference".
jira_api_token_ref = {{ q .Secrets.JiraAPITokenRef }}
cloudflare_api_token_ref = {{ q .Secrets.CloudflareAPITokenRef }}
discourse_api_key_ref = {{ q .Secrets.DiscourseAPIKeyRef }}
google_service_account_key_ref = {{ q .Secrets.GoogleServiceAccountKeyRef }}

[jira]
# Your Jira Cloud site address, without https:// — e.g. "example.atlassian.net".
site = {{ q .Jira.Site }}
# The email address of YOUR Atlassian account (the one that owns the API token).
email = {{ q .Jira.Email }}
# Which service desk members are added to. "aps-tools setup jira" fills these in.
service_desk_id = {{ q .Jira.ServiceDeskID }}
service_desk_name = {{ q .Jira.ServiceDeskName }}

[cloudflare]
# The Cloudflare account and domain (zone) that APS email runs on.
# "aps-tools setup cloudflare" fills these in.
account_id = {{ q .Cloudflare.AccountID }}
account_name = {{ q .Cloudflare.AccountName }}
zone_id = {{ q .Cloudflare.ZoneID }}
zone_name = {{ q .Cloudflare.ZoneName }}
# Which group address goes to ALL members — the part before the @.
# Leave empty for "news" (news@). "aps-tools setup cloudflare" can set it.
members_list = {{ q .Cloudflare.MembersList }}

[discourse]
# The forum's address, e.g. "https://forum.example.org".
url = {{ q .Discourse.URL }}
# YOUR Discourse username — the admin account the key belongs to.
username = {{ q .Discourse.Username }}
# The group every member should be in, e.g. "members". Leave empty to skip
# the group check. "aps-tools setup discourse" lets you pick it.
member_group = {{ q .Discourse.MemberGroup }}
# Which kind of key the tool uses: "user" (approved in your browser — works
# on every Discourse plan) or "admin" (an admin API key — Pro plans only).
key_type = {{ q .Discourse.KeyType }}
# Identifies this computer's copy of the tool to the forum (user keys only).
# Not a secret. "aps-tools setup discourse" fills it in.
client_id = {{ q .Discourse.ClientID }}
# The link to the forum's "Getting Started" topics (a tag or category), used
# in the welcome email. "aps-tools setup email" asks for it.
getting_started_url = {{ q .Discourse.GettingStartedURL }}
# true lets onboarding create each new member's personal invite link (no
# email from the forum; the link goes in your welcome email). It needs a key
# with write access: "aps-tools setup discourse" asks.
create_invites = {{ .Discourse.CreateInvites }}
# The topic new members land on after signing up through their invite, e.g.
# https://forum.example.org/t/welcome/5 (optional).
welcome_topic_url = {{ q .Discourse.WelcomeTopicURL }}

[google_sheet]
# The membership Google Sheet. "aps-tools setup sheet" fills these in.
# spreadsheet_id is the long code in the sheet's link: .../spreadsheets/d/<ID>/edit
spreadsheet_id = {{ q .Sheet.SpreadsheetID }}
spreadsheet_title = {{ q .Sheet.SpreadsheetTitle }}
# The tab (at the bottom of the sheet) that lists the members.
tab = {{ q .Sheet.Tab }}
# The Google "service account" the tool reads as. Share the sheet with this
# address as an Editor, or the tool can't see it. (Editor is for writing back
# setup status in a later version; the tool currently only reads.)
service_account = {{ q .Sheet.ServiceAccount }}
# true while your request to share the sheet with the tool is waiting for the
# owner's approval. "aps-tools setup sheet" sets and clears it.
access_requested = {{ .Sheet.AccessRequested }}
# The date the membership chair began marking "App vetted", written like
# 2025-03-01. Members who started before it (and aren't marked New) count as
# vetted, because they joined before it was tracked. Leave blank to require
# "App vetted" for everyone. "aps-tools setup sheet" suggests a date.
vetting_started = {{ q .Sheet.VettingStarted }}
`

var tmpl = template.Must(template.New("config").Funcs(template.FuncMap{"q": tomlString}).Parse(fileTemplate))

// render produces the full text of config.toml for the given settings.
func render(cfg *Config) (string, error) {
	var b strings.Builder
	if err := tmpl.Execute(&b, cfg); err != nil {
		return "", fmt.Errorf("could not prepare settings file: %w", err)
	}
	return b.String(), nil
}

// tomlString writes s as a quoted TOML string, escaping anything that would
// break the file (quotes, backslashes, control characters).
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
