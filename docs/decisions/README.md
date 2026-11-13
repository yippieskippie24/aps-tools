# Decision Records

Each file in this folder records **one significant decision**: what was decided, what else was
considered, and why. They exist so a future maintainer understands the reasoning before
changing something fundamental.

## How to add one

1. Copy the template below into a new file named `NNNN-short-title.md` (next number in sequence).
2. Fill it in. Keep it short — a few paragraphs is plenty.
3. Never delete an old record. If a decision changes, write a new record and mark the old one
   **Superseded by NNNN**.

```markdown
# NNNN — Title

- **Status:** Accepted | Superseded by NNNN
- **Date:** YYYY-MM-DD

## Context
What problem were we solving? What constraints applied?

## Decision
What did we decide?

## Alternatives considered
What else did we look at, and why not?

## Consequences
What does this make easier or harder?
```

## Index

| # | Decision |
|---|---|
| [0001](0001-go-and-charm.md) | Build in Go with the Charm TUI libraries |
| [0002](0002-homebrew-distribution.md) | Distribute via GitHub Releases + a Homebrew tap |
| [0003](0003-secrets-in-keychain.md) | Store secrets in the macOS Keychain, settings in a TOML file |
| [0004](0004-welcome-email-copy-page.md) | Welcome email is a "copy page", not sent by the tool |
| [0005](0005-public-repo-no-secrets.md) | Public repository; nothing APS-specific in the code |
| [0006](0006-optional-1password.md) | 1Password as an optional place to keep API keys |
