# Development Guide

> **Who this is for:** anyone changing the tool's code or publishing a new version.
> You don't need this page just to *use* the tool.

---

## Ground rules

1. **No secrets or member data in the repo — ever.** This repository is public. See
   [decision 0005](decisions/0005-public-repo-no-secrets.md).
2. **Document as you go.** A change isn't finished until the docs match it: update
   [design.md](design.md), [architecture.md](architecture.md), [handover.md](handover.md), or
   [onboarding-process.md](onboarding-process.md) as relevant, and add a decision record for
   anything significant.
3. **Write code for someone new to Go.** Clear names, a comment on every exported function, and a
   comment explaining *why* on anything non-obvious.
4. **Read-only first.** New connector features start with "check" before "change".

## One-time setup on your Mac

```bash
brew install go
git clone https://github.com/yippieskippie24/aps-tools.git
cd aps-tools
```

## Everyday commands

| Task | Command |
|---|---|
| Run the app from source | `go run ./cmd/aps-tools` |
| Run a specific command | `go run ./cmd/aps-tools doctor` |
| Run all tests | `go test ./...` |
| Check for common mistakes | `go vet ./...` |
| Tidy dependencies | `go mod tidy` |
| Build a binary | `go build -o aps-tools ./cmd/aps-tools` |

### Previewing the app's look

To review screen designs without clicking through the app, save every screen (light and dark
theme) as a coloured text file, then turn them into images with Charm's `freeze` tool:

```bash
brew install charmbracelet/tap/freeze   # note: plain "brew install freeze" is a different app
mkdir -p /tmp/aps-previews
APS_PREVIEW_DIR=/tmp/aps-previews go test ./internal/tui -run Preview
freeze /tmp/aps-previews/home-dark.ans -o home-dark.png --window
```

Colours and styles all live in `internal/tui/styles.go`.

### Using a throwaway config

Use a throwaway config while developing so you don't touch your real one:

```bash
XDG_CONFIG_HOME=/tmp/aps-dev go run ./cmd/aps-tools config init
```

## Releasing a new version

Releases are automatic once the [one-time setup](#one-time-release-setup) is done.

1. Make sure `main` is green (tests pass on GitHub).
2. Pick the next version number ([semantic versioning](https://semver.org): `v0.MINOR.PATCH`
   while the tool is young — bump MINOR for new features, PATCH for fixes).
3. Tag and push:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```
4. GitHub Actions runs GoReleaser, which publishes the release and updates the Homebrew tap.
   Watch progress in the repo's **Actions** tab.
5. Users get it with `brew upgrade aps-tools`.

## One-time release setup

Do this once when the GitHub repository is first created (or if the tap is lost).

1. **Create the main repo:** on GitHub, create a **public** repo named `aps-tools`, then:
   ```bash
   git remote add origin git@github.com:yippieskippie24/aps-tools.git
   git push -u origin main
   ```
2. **Create the tap repo:** create a second **public** repo named exactly `homebrew-tap`
   (Homebrew requires the `homebrew-` prefix). It can be empty. Initialize it with a README.
3. **Create a token** that lets the release job update the tap. Create `homebrew-tap` (step 2)
   first, because you pick it from a list here.
   1. On GitHub, click your **profile picture** (top right) → **Settings**.
   2. In the left sidebar, scroll to the bottom → **Developer settings**.
   3. Left sidebar → **Personal access tokens** → **Fine-grained tokens** → **Generate new token**.
      (Use a *fine-grained* token, not a "classic" one: it can be limited to a single repo.)
   4. Fill in the form:

      | Field | Value |
      |---|---|
      | **Token name** | `aps-tools homebrew tap` |
      | **Description** | `Lets aps-tools releases update the homebrew-tap repo` |
      | **Resource owner** | your own account (`yippieskippie24`) |
      | **Expiration** | the longest custom date allowed (up to 1 year) |
      | **Repository access** | **Only select repositories** → choose `homebrew-tap` only |
      | **Permissions → Repository permissions** | **Contents: Read and write**. Leave everything else as *No access*. GitHub adds **Metadata: Read-only** automatically; that's expected. |

   5. Click **Generate token**.
   6. **Copy the token now** (it starts with `github_pat_`). GitHub shows it only once. Don't save
      it anywhere else; it goes straight into step 4.
   7. Add a calendar reminder a week before the expiry date to repeat steps 3–4.

   > GitHub also deletes tokens that go unused for a year. If you don't release for a year,
   > expect to make a new token.
4. **Give the token to the main repo:** in `aps-tools` → Settings → Secrets and variables →
   Actions → **New repository secret**, name `HOMEBREW_TAP_GITHUB_TOKEN`, paste the token.

If the token expires, releases still publish to GitHub but the Homebrew update fails. Repeat
steps 3–4.

## Testing a release locally (optional)

```bash
brew install goreleaser
goreleaser check                      # validates .goreleaser.yaml
goreleaser release --snapshot --clean # builds into ./dist without publishing
```
