# 0010 — Members who joined before "App vetted" was used count as vetted

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
The member check treats **App vetted** as the onboarding gate: until the membership chair marks
it, the member shows as "Waiting on member" and onboarding refuses to set them up.

The first read of the real sheet showed far more Active members than vetted ones. The webmaster
explained why: the current version of the member sheet is new, so only recent members have App
vetted filled in. Long-standing members have a blank there, and they are not unfinished
applications. Their **Start Date** shows the pattern: unmarked rows started before vetting was
tracked.

Options considered:
1. **Ask the chair to backfill App vetted** for every long-standing member. Accurate, but a chore
   for a volunteer, and nothing breaks loudly if it's forgotten.
2. **Treat every Active member as vetted.** Simple, but a new applicant who is marked Active early
   would slip past the gate.
3. **A vetting start date** (chosen).

## Decision
The settings file has `vetting_started` (a date, in the `[google_sheet]` section). A member counts
as vetted, and the tool calls them "established", when **all** of these are true:
- App vetted is blank;
- they're **not** marked New;
- their Start Date is filled in and is **before** `vetting_started`.

Anything else still needs App vetted. That includes a blank or unreadable start date: when in
doubt, the tool waits for the chair.

`aps-tools setup sheet` suggests the date: the **earliest start date on a row marked App vetted**.
It also shows how many unmarked rows started before and after it. `aps-tools sheet discover`
explains the same pattern and, while no date is set, suggests setting one. A blank setting keeps
the strict rule (everyone needs App vetted).

## Consequences
- Long-standing members show as set-up-ready without anyone editing the sheet.
- New applicants are still gated by App vetted, so the onboarding rule doesn't weaken.
- If the chair backfills App vetted for an old member, nothing changes: vetted always counts.
- If someone marks an old member vetted, the suggested date moves earlier. That only makes the
  rule stricter (more members wait for App vetted), and setup shows the counts so it's noticed.
- Members with no start date and a blank App vetted keep showing as waiting, and the check says
  adding a start date would settle it.
