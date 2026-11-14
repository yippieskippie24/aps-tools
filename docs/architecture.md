# Architecture

> **Who this is for:** anyone changing the tool's code. It explains how the pieces fit together
> and where to make a change. For *why* things are this way, see [design.md](design.md) and the
> [decision records](decisions/).

---

## The big picture

```
                 ┌──────────────────────────────┐
  you type  ───▶ │  cmd/aps-tools (the program) │
                 └──────────────┬───────────────┘
                                │
                 ┌──────────────▼───────────────┐
                 │  internal/cli                │  "aps-tools check", "aps-tools doctor", ...
                 │  (no command → opens the TUI)│
                 └───────┬──────────────┬───────┘
                         │              │
            ┌────────────▼───┐   ┌──────▼─────────┐
            │ internal/tui   │   │ internal/doctor│   shared logic used by
            │ (screens)      │──▶│ check, onboard │◀─ both the TUI and CLI
            └────────────────┘   │ activity       │
                                 └──────┬─────────┘
                                        │ uses
                        ┌───────────────▼────────────────┐
                        │ internal/connectors            │
                        │  one folder per system:        │
                        │  sheet/ cloudflare/ jira/ ...  │
                        └───────────────┬────────────────┘
                                        │ reads
                   ┌────────────────────┴─────────────────────┐
          ┌────────▼────────┐                       ┌─────────▼────────┐
          │ internal/config │  ~/.config/aps-tools/ │ internal/secrets │  macOS Keychain
          │ (settings)      │  config.toml          │ (API keys)       │  service "aps-tools"
          └─────────────────┘                       └──────────────────┘
```

**Rule of thumb:** screens (`tui`) and commands (`cli`) contain *no business logic*. They call
shared packages (`doctor`, `check`, `onboard`), which call connectors. That way the
TUI and the plain commands always behave the same.

## Folder guide

| Folder | What lives here |
|---|---|
| `cmd/aps-tools/` | The program's entry point. Tiny — it just starts `internal/cli`. |
| `internal/cli/` | One file per command (`check.go`, `doctor.go`, `config.go`, `version.go`, `cloudflare.go`, `discourse.go` and `sheet.go` for the `discover` commands, `onboard.go` for `onboard` and `log`, `email.go` for the welcome email). `setup.go` holds shared setup code (including asking for API keys); each system's guided setup is in `setup_<system>.go`. `root.go` opens the TUI when no command is given. |
| `internal/tui/` | The interactive app. `app.go` is the main model and screen switching; `home.go` the menu; `membersscreen.go` "Members" (and `memberemail.go`, its e-for-email list); `checkscreen.go` "Check a member" (a quick, searchable list from the sheet alone, via `roster.Directory`, then a full check); `logscreen.go` "Activity log"; `onboardscreen.go` "Onboard a new member"; `scroll.go` makes every screen fill the window and scroll when it's taller; `task.go` shows what the app is doing (and the current step) along the bottom whenever background work runs, and stops a key press from starting the same work twice; `screens.go` Test connections and Help; `styles.go` holds every colour. |
| `internal/check/` | Checks one member against every connector at once and builds the report (also its plain-text copy). |
| `internal/doctor/` | Checks the tool's own setup and every connector's connection. |
| `internal/onboard/` | Guided onboarding: the vetting gate, the plan (what each system needs), and running the tool's own steps. See [decision 0011](decisions/0011-onboarding-plan-then-run.md). |
| `internal/welcome/` | Member emails: one `Kind` per situation (welcome, forum access help, …), each with a starting template (the `*-email.md` files). Fills a template in for a member, suggests which email fits them, and builds the copy page (`page.html`). See [member-emails.md](member-emails.md). |
| `internal/pageserver/` | A tiny web server on 127.0.0.1 that shows pages (like an email to copy) in the browser from memory, at random addresses. See [decision 0015](decisions/0015-member-emails-and-page-server.md). |
| `internal/roster/` | Everyone on the sheet, checked in every system at once and sorted into groups, for the Members screen. See [decision 0013](decisions/0013-members-screen-bulk-checks.md). |
| `internal/progress/` | Lets long-running work say which step it's on (`progress.Report`), so the app can show it. The command line ignores it. |
| `internal/activity/` | The activity log: one line per change the tool makes, in `activity.log` next to the settings file. |
| `internal/connectors/` | The `Connector` interface and member states (`connector.go`) and the registry (`registry.go`). Each system gets its own sub-folder, e.g. `jira/`, `cloudflare/`. `all/` switches every connector on, in display order. In `cloudflare/`, `lists.go` reads the mailing lists exactly as the list Worker does. Keep the two in step. In `discourse/`, `userkey.go` does the approve-in-browser hand-shake for the forum key. In `sheet/`, `google.go` signs in to Google as the service account and `columns.go` finds columns by header text. `vetting.go` decides who counts as vetted, including members who joined before App vetted was used. |
| `internal/config/` | Loads and saves `config.toml`. `template.go` is the file's layout with a comment on every setting; saving always goes through it, so comments are never lost. |
| `internal/secrets/` | Where API keys come from: the macOS Keychain (`secrets.go`) or 1Password via the `op` tool (`onepassword.go`). All key names are defined in `secrets.go`. |
| `internal/clipboard/` | Copies text to the Mac clipboard (used to copy reports). |
| `internal/browser/` | Opens a web page in the default browser (used to approve the Discourse key). |
| `internal/version/` | The version number, stamped in at release time. |
| `docs/` | All documentation. |
| `.github/workflows/` | CI (tests on every push) and releases (on version tags). |
| `.goreleaser.yaml` | How release binaries and the Homebrew cask are built. |

> `internal/` is a Go convention meaning "only this program can use these packages".

## Connectors

A **connector** is the code that talks to one outside system. Every connector implements the
same `Connector` interface (see `internal/connectors/connector.go`):

| Method | Purpose |
|---|---|
| `ID()` | Short machine name, e.g. `jira`. Used in config and commands. |
| `Name()` | Human name, e.g. `Jira Service Management`. Shown in the UI. |
| `Missing()` | Plain-English list of settings or keys not set up yet. Empty = ready. |
| `TestConnection(ctx)` | Makes a harmless read-only call to prove the keys work. |
| `CheckMember(ctx, email)` | Read-only: is this member set up here? Returns a `MemberStatus` (state, summary, details, and what to do). |

Two optional interfaces, in the same file, are for onboarding:

| Interface | Who implements it | Purpose |
|---|---|---|
| `Directory` | The membership sheet | `LookupMember`: the check plus the member's name. Onboarding's gate: only a vetted member goes further. |
| `Onboarder` | Each system, as its Phase 2 step is built | `PlanOnboard` says what it would do (changing nothing). `Onboard` does it, checking again first so it's safe to repeat. |

`InviteLinker` (the forum) gives the welcome email the member's invite link.

Two more optional interfaces are for the Members screen: `MemberLister` (the sheet lists everyone)
and `BulkChecker` (`CheckMembers`: check many members in a few requests, by reading the system's
whole list of people). A connector without `BulkChecker` is checked one member at a time, a few at
once.

A connector without `Onboarder` still works: onboarding lists its step as "do this by hand",
using the check's "what to do" line.

### Member states

Every check result has one of these states (`internal/connectors/connector.go`), from best to worst:

| State | Shown as | Meaning |
|---|---|---|
| `StateOK` | ✓ Set up | Fully set up. |
| `StatePending` | … Waiting on member | Started, but the member must act (accept an invite, verify an email). |
| `StateUnknown` | ? Couldn't check | System not connected, or didn't answer. |
| `StateProblem` | ✗ Problem | Exists, but something is wrong (e.g. deactivated). |
| `StateMissing` | ✗ Not set up | Not set up at all. |

A missing member is a normal result, not an error. Connectors return an error only when
the check itself couldn't be done.

### How to add a new connector

1. Create `internal/connectors/<id>/` with a `<id>.go` file implementing `Connector`, plus
   tests that use a fake server (see `jira/jira_test.go`).
2. Add any non-secret settings as a new section in `internal/config/config.go` **and** in the
   file layout in `internal/config/template.go` (a test fails if you forget).
3. For each API key: add a name in `internal/secrets/secrets.go`; add a `<name>_ref` field to
   the `Secrets` settings (for 1Password); map it in `secrets.ForConfig` and in `refField`
   (`internal/cli/setup.go`).
4. Register it in `internal/connectors/all/all.go`, and remove it from `NotYetBuilt()` in
   `internal/connectors/registry.go`.
5. Add a guided setup in `internal/cli/setup_<id>.go` and register the sub-command in `setup.go`.
6. Write a setup guide in `docs/setup/<id>.md` and link it from [setup/README.md](setup/README.md)
   and [handover.md](handover.md).
7. Update the systems table in [design.md](design.md).
8. When it can set members up, implement `Onboarder` (see above). Follow the safety rules:
   preview first, check before acting, test with fake servers, and say what changed so it can be
   logged.

To **remove** a system, reverse these steps.
