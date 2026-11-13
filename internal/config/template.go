package config

// Template is the starter config.toml written by `aps-tools config init`.
//
// Keep it in sync with the Config struct in config.go: every setting should
// appear here with a comment explaining what it is and where to find it.
// Never put real APS values here — this file is in a public repository.
const Template = `# APS Webmaster Tool — settings
#
# This file holds NON-SECRET settings only. API keys and tokens are stored
# in the macOS Keychain (open "Keychain Access" and search for "aps-tools").
# It is safe to share this file with the next webmaster.
#
# Lines starting with # are comments. After editing, run "aps-tools doctor" to
# check everything is connected.

[webmaster]
# Your name, as it should appear when signing welcome emails.
name = ""
# The email address members should reply to.
email = ""

# More sections (Google Sheet, Cloudflare, Jira, Discourse) will appear here
# as each connection is added to the tool.
`
