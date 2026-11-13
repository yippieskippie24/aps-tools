# Notes for AI assistants (and humans) working on this repo

`aps-tools` is a Go TUI/CLI that helps the APS Webmaster manage member access across Google Sheets,
Cloudflare Email Routing, Jira Service Management, and Discourse. Read `docs/design.md` for goals
and `docs/architecture.md` for code layout before making changes.

## Non-negotiable rules

- **Public repo: never commit secrets, member data, or APS-specific IDs/URLs.** Use `example.com`
  and fake IDs in tests and docs. Real values belong in `~/.config/aps-tools/config.toml` (settings)
  or the macOS Keychain (secrets).
- **Documentation is part of every change.** The maintainer may be a non-technical successor.
  Update the relevant file in `docs/` in the same change; add a decision record in
  `docs/decisions/` for significant choices.
- **Comment for Go beginners.** Every exported identifier gets a doc comment; explain *why*.
- **No business logic in `internal/tui` or `internal/cli`.** They call shared packages.
- **Connectors start read-only.** Any action that changes an outside system must preview first,
  check-before-act (idempotent), and support dry-run.
- **Zero budget.** Don't introduce paid services or dependencies that need them.
- **The TUI should look polished as well as be easy to use.** Keep colours in
  `internal/tui/styles.go`, support light and dark terminals, and check new screens with the
  preview images described in `docs/development.md`.
- **API keys go through `secrets.Store` only.** The store is the Keychain or 1Password (`op read`
  on an `op://` reference), chosen in settings. Never read a key any other way.
- **The command is `aps-tools`, never `aps`.** The webmaster has unrelated `aps` folders, so
  the name must stay distinct.

## Commands

```bash
go test ./...            # tests
go vet ./...             # lint
go run ./cmd/aps-tools   # run the TUI
XDG_CONFIG_HOME=/tmp/aps-dev go run ./cmd/aps-tools doctor   # run against a throwaway config
```

## Stack

Go · Bubble Tea v2 (`charm.land/bubbletea/v2`) · Lip Gloss v2 (`charm.land/lipgloss/v2`) ·
Bubbles v2 · Huh v2 (setup forms) · Cobra · zalando/go-keyring · BurntSushi/toml · GoReleaser (Homebrew cask in `yippieskippie24/homebrew-tap`).
