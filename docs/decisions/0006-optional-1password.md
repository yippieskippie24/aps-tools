# 0006 — 1Password as an optional place to keep API keys

- **Status:** Accepted (extends [0003](0003-secrets-in-keychain.md))
- **Date:** 2026-11-13

## Context
The current webmaster keeps every password and key in 1Password and wants the tool's API keys
there too, rather than in a second place (the Keychain). A future webmaster may not use
1Password at all.

## Decision
Support **both**, chosen by `[secrets] store` in the settings file (`aps-tools setup secrets`):

- **`keychain`** (default): unchanged from decision 0003.
- **`1password`**: the settings file holds a **secret reference** (`op://Vault/Item/field`)
  for each key. The tool reads it by running 1Password's own command-line tool, `op read`,
  which asks for approval in the 1Password app (Touch ID). The tool never writes to 1Password
  and never writes the key itself to disk.

All the code that needs a key goes through one interface (`secrets.Store`), so connectors
don't know or care which option is in use.

## Alternatives considered
- **1Password only.** Would force a successor to buy 1Password. That conflicts with the zero-budget
  and easy-handover goals.
- **1Password SDK / service accounts.** Service accounts need a separate token stored somewhere
  (back to the original problem) and are aimed at business plans. The `op` CLI with
  desktop-app integration uses the webmaster's normal 1Password unlock.
- **Tool creates the 1Password items itself.** More automation, but it would need write access to
  the vault. Pasting a secret reference is simple and keeps the tool read-only.

## Consequences
- 1Password users must install `op` and turn on **Settings → Developer → Integrate with
  1Password CLI** (see [docs/setup/1password.md](../setup/1password.md)).
- Each key needs a settings field for its reference (`[secrets] <name>_ref`), plus an entry in
  `secrets.ForConfig` and `refField` in `internal/cli/setup.go`.
- Moving or renaming the 1Password item changes its reference, so setup must be re-run.
