# 0003 — Secrets in the macOS Keychain, settings in a TOML file

- **Status:** Accepted. Extended by [0006](0006-optional-1password.md), which adds 1Password as an option.
- **Date:** 2026-11-13

## Context
The tool needs API keys for several systems. These must never end up in the public repo, in
screenshots of config files, or in backups that get shared.

## Decision
- **Secrets** (API tokens, API keys, the Google service-account key) are stored in the
  **macOS Keychain** under the service name `aps-tools`. The tool reads them when needed.
- **Non-secret settings** (site URLs, account IDs, sheet ID) are stored in a plain-text file at
  `~/.config/aps-tools/config.toml` that is easy to read and edit. You can share this file with a
  successor safely.
- The exact Keychain entry names are defined in one place: `internal/secrets/secrets.go`.

## Alternatives considered
- **Everything in the config file** — simple, but a plain-text file of passwords is easy to leak.
- **Environment variables** — invisible, easy to lose, and awkward for non-technical users.

## Consequences
- You can view or delete entries in the **Keychain Access** app (search for `aps-tools`).
- The first time the tool reads a secret, macOS may ask permission. Choose **Always Allow**.
- Handing over to a new webmaster means they create *their own* keys; nothing to transfer
  insecurely. See [handover.md](../handover.md).
