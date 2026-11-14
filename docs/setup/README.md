# Setup guides

One guide per connection. Do them in this order:

| # | Guide | What it connects | Status |
|---|---|---|---|
| 0 | Your details: `aps-tools setup webmaster` | Your name and reply-to email (they sign welcome emails) | Available |
| 1 | [Where API keys are kept](1password.md) | macOS Keychain (default) or 1Password | Available |
| 2 | [Jira Service Management](jira.md) | The APS service desk's customer list | Available |
| 3 | [Mailing list (Cloudflare)](cloudflare.md) | APS email and the mailing list | Available |
| 4 | [Membership Google Sheet](google-sheet.md) | The master member list | Available |
| 5 | [Discourse](discourse.md) | The APS forum | Available |
| 6 | [Member emails](../member-emails.md): `aps-tools setup email` | The Getting Started link and your email templates | Available |

After each one, run `aps-tools doctor` to confirm it's connected.

> **Working on the code?** To try unreleased changes, run commands from the project folder with
> `go run ./cmd/aps-tools` in place of `aps-tools`, e.g. `go run ./cmd/aps-tools doctor`.
> See [development.md](../development.md).
