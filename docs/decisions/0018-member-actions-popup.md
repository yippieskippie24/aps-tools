# 0018 — One actions popup for a member, on every screen

- **Status:** Accepted
- **Date:** 2026-11-14

## Context
Each screen had its own letter keys for acting on a member: `o` to onboard and `e` to email on
Members, `Enter` to check on Check a member, `g` and `w` while onboarding. In the first real
onboarding, the webmaster found this confusing. They couldn't start onboarding from the Members
list or a check, and the letters weren't obvious. They asked for every screen to offer exactly the
same things for a member, in a clearer, more visual way.

We built mockups and then working demos of several layouts:
- a popup menu;
- a full action screen;
- a split view with the list on the left and the member's details on the right.

The webmaster liked the split view and the popups, and chose a mix of the two.

## Decision
- **Members** and **Check a member** are both a list beside the highlighted member's details.
  The details show each system, any warnings, and the suggested next step.
- **Enter** on a member opens one **actions popup** with everything you can do for them. It is
  built by the same code (`internal/tui/memberactions.go`) wherever it opens. The actions are:
  - onboard, or override the sheet when it isn't ready;
  - email them;
  - re-send Cloudflare's verification email;
  - check every system;
  - their activity log.

  Actions that don't apply right now stay in the list, greyed out, with the reason (for example
  "already verified"). The best next step is marked **suggested** and highlighted first.
- **Confirmations happen in popups too**, so nothing changes from a single key press:
  - the onboarding plan, ending in "Make N changes now";
  - which email to open, then "Did you send it?";
  - the re-send;
  - the override reason, which must be a few words long.

  **Esc** always steps back one popup.
- **Results show on the bottom line** (green for success). The member is then re-checked
  automatically, so the details pane is up to date.
- The screen behind a popup is dimmed so it's clear what has your attention.

The **Onboard a new member** screen first kept its own flow (`g`, `w`, `e`). **Update, same
day:** it was moved onto the popups, but then the plan showed twice (in the popup and on the screen
behind it). So the screen was **removed**. A newly vetted member is on the sheet, so they appear
under Members → Needs setup and in Check a member, where Enter → **Onboard** does the job. The
menu item **Onboard a new member** stays, as a shortcut to Members → Needs setup. Someone who isn't
on the sheet is onboarded by typing their full email in Check a member and choosing **Override the
sheet and onboard…**, with a reason.

After "Make N changes now", every screen shows a popup of what each step did. It offers the email
that fits them now (usually the welcome email, with the new invite link) and copying the plan as a
checklist. The old Email screen was removed too.

## Consequences
- One place to add a new member action, and it appears everywhere.
- Fewer keys to remember: arrows, Enter and Esc. Numbers 1–9 pick a popup item directly.
- No screen has its own letter keys for acting on a member any more.
