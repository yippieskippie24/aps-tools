# 0011 — Onboarding plans first, lists manual steps, and logs to a text file

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
Phase 2 makes the tool change outside systems for the first time. The project's rules say every
change must be previewed, checked before it's made, and possible to dry-run. Each system will
learn to set members up at a different time (mailing list first, the forum later). The design
originally pencilled in SQLite for an activity log.

## Decision
**Two halves: Prepare, then Run.** `internal/onboard` first builds a **plan** without changing
anything:
- **The membership sheet is a gate.** If the member isn't vetted (or the sheet can't be read),
  the plan stops there, and running it is refused (`ErrNotReady`), not just hidden in the UI.
- **Every other system is checked** the same way `aps-tools check` does, and each becomes a step:
  - nothing to do;
  - the tool will do it;
  - do it by hand;
  - waiting on the member;
  - couldn't check;
  - coming later.

Only after the webmaster says **Go** are the tool's own steps run, one at a time. A failure
doesn't stop the others. `--dry-run` always stops after the plan.

**Setting members up is an optional connector interface** (`connectors.Onboarder`), not a new
required method. A system that can't do it yet shows as a **step to do by hand**, using the
member check's own "what to do" text. So the plan is a useful checklist from day one, and each
system becomes automatic as it's built, with no change to the onboarding code. The membership
sheet provides `connectors.Directory`, which also gives the member's name.

**The activity log is a plain text file**, `activity.log`, next to the settings file. It has one
tab-separated line per change, failure, and start or end of a run. It's readable in any text
editor, and `aps-tools log` shows the end of it. It's private to the Mac user (file mode 0600),
because it names members. Previews and dry runs change nothing, so they log nothing.

## Why not SQLite
A successor who isn't a programmer can open a text file. They can't open a database. The tool
only ever appends to the log and reads the end of it, so it doesn't need queries. A text file
also adds no dependency.

## Consequences
- The plan is honest: today most steps say "do this by hand", and the summary says so.
- Re-running onboarding is always safe. Steps already done are "nothing to do", and each
  connector's `Onboard` must check again before acting.
- If the log can't be written, the app doesn't start making changes, and any problem writing it
  mid-run is shown, not ignored.
