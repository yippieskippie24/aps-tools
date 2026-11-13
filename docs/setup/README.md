# Setup guides

One guide per connection. Do them in this order:

| # | Guide | What it connects | Status |
|---|---|---|---|
| 1 | [Where API keys are kept](1password.md) | macOS Keychain (default) or 1Password | Available |
| 2 | [Jira Service Management](jira.md) | The APS service desk's customer list | Available |
| 3 | [Mailing list (Cloudflare)](cloudflare.md) | APS email and the mailing list | Available (verification check; list check after discovery) |
| 4 | Membership Google Sheet | The master member list | Coming in Phase 1 |
| 5 | [Discourse](discourse.md) | The APS forum | Available |

After each one, run `aps-tools doctor` to confirm it's connected.

> **Before the first release:** `aps-tools` isn't installed on your Mac until the first Homebrew
> release. Until then, run every command from the project folder with `go run ./cmd/aps-tools`
> in place of `aps-tools`, e.g. `go run ./cmd/aps-tools setup jira`.
