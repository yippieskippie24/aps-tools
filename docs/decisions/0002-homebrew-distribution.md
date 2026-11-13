# 0002 — Distribute via GitHub Releases + a Homebrew tap

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
Installing and updating should be one command, and must be free. macOS blocks unsigned apps
downloaded through a web browser ("unidentified developer"), and Apple's signing program costs
$99/year.

## Decision
- **GoReleaser** (run automatically by GitHub Actions) builds the app whenever a version tag such
  as `v0.1.0` is pushed, and publishes it to **GitHub Releases**.
- GoReleaser also updates a **Homebrew tap** (a small separate repo, `homebrew-tap`), so users run:
  ```
  brew install yippieskippie24/tap/aps-tools
  brew upgrade aps-tools
  ```
- The Homebrew cask removes macOS's download "quarantine" flag after install, so the unsigned
  binary runs without the Gatekeeper warning. No Apple developer account needed.
- Fallback for developers: `go install github.com/yippieskippie24/aps-tools/cmd/aps-tools@latest`.

## Alternatives considered
- **Apple code signing + notarization** — cleanest, but $99/year.
- **Downloading a zip from the Releases page** — triggers the Gatekeeper warning and requires
  manual right-click → Open.
- **Building from source only** — requires Go on every user's machine.

## Consequences
- One-time setup is needed: create the `homebrew-tap` repo and a GitHub token. See
  [development.md](../development.md#one-time-release-setup).
- GitHub Actions and Releases are free for public repos.
