# 0012 — The webmaster can override the vetting gate, with a recorded reason

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
Onboarding only sets up members the membership sheet passes: vetted, or joined before vetting
began ([0010](0010-members-before-vetting-began.md)). Real life doesn't always match the sheet. A
member may have paid at a meeting, or the chair may not have updated the sheet yet. The webmaster
needs to be able to go ahead anyway, without the override becoming a silent back door.

A related case is a member whose **Member Status isn't Active** (e.g. "Not Active") but who
otherwise passes. The webmaster decided this should be **flagged, not blocked**.

## Decision
**Override:**
- `aps-tools onboard <email> --override "reason"`, or **o** on the app's "Not ready" screen.
- A reason is **required**, at least 10 characters, so the record means something later.
- It applies only when the sheet was **read** and said "not vetted yet" or "not on the list". If
  the sheet can't be read, the connection must be fixed: the tool won't guess.
- **The override is confirmed and recorded** in the activity log ("Override by the webmaster: …
  Reason: …") before any change is made. A dry run records nothing.
- In the plan, the sheet step becomes a step to do by hand: "Ask the membership chair to update
  the sheet, so it matches." The reason is shown, and the summary starts "Sheet overridden."

**When the sheet write-back is built** (Phase 2, build step 6), an overridden member's status on
the sheet will say so (e.g. "Complete (override)"), and the override reason will be written to its
own column. The webmaster asked for both. See the
[Phase 2 plan](../plans/phase-2-onboarding.md).

**Status that isn't Active:** a check result can now carry a **warning**
(`connectors.MemberStatus.Warning`), shown with a "!" in the member check, the onboarding plan and
the copied checklist. A vetted member whose status is filled in and isn't Active gets one. A blank
status doesn't, because it's often just not filled in. Warnings never stop anything.

## Consequences
- Nobody is set up silently outside the rules: every override has a reason and a log line.
- The membership chair still has to fix the sheet. The plan says so every time.
- Warnings are a general mechanism, so other systems can use them later.
