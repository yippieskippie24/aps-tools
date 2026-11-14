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

The **Onboard a new member** screen keeps its own flow (`g`, `w`, `e`). It's for typing an
address you don't have in a list.

## Consequences
- One place to add a new member action, and it appears everywhere.
- Fewer keys to remember: arrows, Enter and Esc. Numbers 1–9 pick a popup item directly.
- The older Email screen is now used only from Onboard. It could be removed once Onboard uses the
  popups too.
