# 0005 — Public repository; nothing APS-specific in the code

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
A public GitHub repo is free, makes Homebrew installs simple (no access tokens), and keeps the
tool available even if the original webmaster's account access changes.

## Decision
The repository is **public**. The rule is: **the code knows how to talk to each kind of system;
your machine's config knows which APS accounts to talk to.**

Never commit:
- API keys, tokens, passwords, or service-account files,
- member names, emails, or any data from the membership sheet,
- APS-specific IDs or URLs in code (they belong in `~/.config/aps-tools/config.toml`).

Example values in docs and tests use `example.com` / `example.org` and obviously fake IDs.

## Alternatives considered
- **Private repo** — Homebrew installs would need a GitHub token on every machine.

## Consequences
- `.gitignore` excludes common secret file types as a safety net.
- Contributors (including AI assistants) must follow this rule — it is repeated in
  [`CLAUDE.md`](../../CLAUDE.md) and [development.md](../development.md).
