# Handover Guide — for the next APS Webmaster

> **Start here if you are taking over the APS Webmaster role.** This page lists everything you
> need: which accounts exist, what the tool needs from each, and how to get set up. You do **not**
> need to be a programmer to use the tool.

_Keep this page up to date whenever an account, key, or process changes._

---

## 1. What this tool is

`aps-tools` is a small app that runs in the macOS Terminal. It helps the webmaster:

- check whether a member is set up correctly in every APS system,
- set up new members everywhere in one go,
- keep the membership Google Sheet up to date for the board.

If the tool ever stops working, you can still do everything by hand —
see [How APS member onboarding works](onboarding-process.md).

## 2. Accounts and access you will need

The tool doesn't share keys between people. **Each webmaster creates their own** API keys from
their own admin accounts. The outgoing webmaster's job is to give you *admin access* to each
system; your job is to create keys and connect the tool.

| System | What you need from the outgoing webmaster | What the tool needs (you create this) | Setup guide |
|---|---|---|---|
| Google Sheet (membership list) | Edit access to the sheet | A free Google Cloud service account, shared on the sheet | [docs/setup/google-sheet.md](setup/google-sheet.md) |
| Cloudflare (APS email + mailing list) | Membership on the APS Cloudflare account | A read-only Cloudflare API token | [docs/setup/cloudflare.md](setup/cloudflare.md) |
| Jira Service Management | An agent seat on the APS Jira site (free plan = 3 seats) | An Atlassian API token from your own account | [docs/setup/jira.md](setup/jira.md) |
| Discourse | Admin on the APS Discourse site | A read-only key you approve in your browser during setup | [docs/setup/discourse.md](setup/discourse.md) |
| GitHub (this tool's code) | Optional: maintainer access to the repo | Nothing, unless you want to change the tool | [development.md](development.md) |

**Outgoing webmaster checklist** (do this before you leave):

- [ ] Give the new webmaster admin/agent access on every system above.
- [ ] Walk through one onboarding together using the tool.
- [ ] Revoke **your own** API keys (Cloudflare, Atlassian, Google service account key) and the
      Discourse key ("aps-tools" under your forum profile → Preferences → authorized apps).
- [ ] Remove your own admin access, or hand it to the board.
- [ ] Make sure this page and [onboarding-process.md](onboarding-process.md) are current.

## 3. Installing the tool

1. Install [Homebrew](https://brew.sh) if you don't have it (paste the command from their site into Terminal).
2. In Terminal, run:
   ```
   brew install yippieskippie24/tap/aps-tools
   ```
3. Run `aps-tools` to open the app, or `aps-tools --help` for the list of commands.
4. Enter your own name and reply-to email: `aps-tools setup webmaster`.
5. Choose where API keys are kept (Keychain or 1Password): `aps-tools setup secrets`. See
   [docs/setup/1password.md](setup/1password.md).
6. Connect each system by following the [setup guides](setup/README.md) in order.
7. Run `aps-tools doctor` to confirm everything is connected.

To update later: `brew upgrade aps-tools`.

## 4. Where the tool keeps things on your Mac

| What | Where | Safe to share? |
|---|---|---|
| Settings (URLs, IDs) | `~/.config/aps-tools/config.toml` — run `aps-tools config path` to see it | Yes — no secrets in it |
| API keys and tokens | macOS **Keychain Access** app (search `aps-tools`), **or** your 1Password, depending on `aps-tools setup secrets` | **No** |

## 5. Getting help

- Inside the app, press `?` for help on the current screen.
- `aps-tools doctor` explains what's broken and how to fix it.
- All documentation lives in the [`docs/`](.) folder of the GitHub repository.
