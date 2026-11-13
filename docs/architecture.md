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
            │ (screens)      │──▶│ (and later:    │◀─ both the TUI and CLI
            └────────────────┘   │  check, onboard)│
                                 └──────┬─────────┘
                                        │ uses
                        ┌───────────────▼────────────────┐
                        │ internal/connectors            │
                        │  one folder per system:        │
                        │  sheets/ cloudflare/ jira/ ... │
                        └───────────────┬────────────────┘
                                        │ reads
                   ┌────────────────────┴─────────────────────┐
          ┌────────▼────────┐                       ┌─────────▼────────┐
          │ internal/config │  ~/.config/aps-tools/ │ internal/secrets │  macOS Keychain
          │ (settings)      │  config.toml          │ (API keys)       │  service "aps-tools"
          └─────────────────┘                       └──────────────────┘
```

**Rule of thumb:** screens (`tui`) and commands (`cli`) contain *no business logic*. They call
shared packages (`doctor`, and later `check` and `onboard`), which call connectors. That way the
TUI and the plain commands always behave the same.

## Folder guide

| Folder | What lives here |
|---|---|
| `cmd/aps-tools/` | The program's entry point. Tiny — it just starts `internal/cli`. |
| `internal/cli/` | One file per command (`doctor.go`, `config.go`, `version.go`). `root.go` opens the TUI when no command is given. |
| `internal/tui/` | The interactive app. `app.go` is the main model; `styles.go` holds colours. |
| `internal/doctor/` | Checks the tool's own setup and every connector's connection. |
| `internal/connectors/` | The `Connector` interface (`connector.go`) and the list of active connectors (`registry.go`). Each system gets its own sub-folder. |
| `internal/config/` | Loads and saves `config.toml`. `template.go` is the commented starter file. |
| `internal/secrets/` | Reads/writes the macOS Keychain. All Keychain entry names are defined here. |
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

Methods for checking and setting up members are added in Phases 1 and 2.

### How to add a new connector

1. Create `internal/connectors/<id>/` with a `<id>.go` file implementing `Connector`.
2. Add any non-secret settings as a new section in `internal/config/config.go` **and** a
   commented example in `internal/config/template.go`.
3. Add any secret names as constants in `internal/secrets/secrets.go`.
4. Register it in `internal/connectors/registry.go`.
5. Write a setup guide in `docs/setup/<id>.md` and link it from [handover.md](handover.md).
6. Update the systems table in [design.md](design.md).

To **remove** a system, reverse these steps.
