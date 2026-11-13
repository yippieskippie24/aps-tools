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

[jira]
# Your Jira Cloud site address, without https:// — e.g. "example.atlassian.net".
site = {{ q .Jira.Site }}
# The email address of YOUR Atlassian account (the one that owns the API token).
email = {{ q .Jira.Email }}
# Which service desk members are added to. "aps-tools setup jira" fills these in.
service_desk_id = {{ q .Jira.ServiceDeskID }}
service_desk_name = {{ q .Jira.ServiceDeskName }}

# More sections (Google Sheet, Cloudflare, Discourse) will appear here as
# each connection is added to the tool.
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
