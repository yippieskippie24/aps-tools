# 0019 — Bulk follow-ups: choose who, then one member at a time

- **Status:** Accepted
- **Date:** 2026-11-14

## Context
The Members screen showed a backlog of members who were only part-way set up: about a dozen
hadn't clicked Cloudflare's verification email (so APS emails don't reach them), and about a dozen
had no forum account. Fixing them one at a time from each member's popup works, but it's slow.
The webmaster asked for bulk follow-ups, and to be able to **include or exclude individual
members** each time.

## Decision
**Three follow-ups**, found in the roster snapshot by `internal/followup`. Only members the sheet
passes are included, and a member can be in more than one list:

| Follow-up | Who | What the tool does | Then email |
|---|---|---|---|
| Haven't verified their email | Cloudflare says Pending | Has Cloudflare re-send its verification email ([0016](0016-resend-cloudflare-verification.md)) | Verify your email |
| Need setting up | The Members screen's Needs setup group | Onboards them, exactly as for one member | The suggested email (usually Welcome or New forum invite) |
| Haven't used their forum invite | The forum says Pending | Nothing | New forum invite (their link again) |

**Choosing who.** Each follow-up opens as a list of tick boxes with **nobody ticked**, so every
member included is a deliberate choice (the webmaster asked for this after first trying it with
everyone ticked). Space ticks a member; `a` ticks or unticks everyone. Enter means "done
choosing": it never ticks, but moves to the suggested action, and a second Enter does it. With
nobody ticked, it says to tick someone first. The action's label counts who's ticked ("Onboard 9
members"), and nothing changes until it's chosen. On the command line,
`aps-tools followups <name>` shows the same tick list (`--skip` leaves people out of it,
`--dry-run` changes nothing).
Exclusions aren't remembered between runs. If some members should never be followed up, that's
a later change.

**One member at a time.** The tool works through the chosen members in turn, showing "3 of 9 ·
Alex Example" on the progress line. Each member is checked again, live, just before anything is
done for them, so nothing is repeated, and each change goes in the activity log. A failure for
one member doesn't stop the rest. Afterwards everyone is checked again, and a popup lists what
happened for each member, problems first.

**The forum's rate limit.** A forum user API key allows about 20 requests a minute, and onboarding
a member takes several. When the forum answers "too many requests", nothing was done, so the
forum client now waits as long as the forum asks (capped at a minute), says so on the progress
line, and tries again (up to 4 times). The per-system time limits were raised to allow for that
wait. Every single request still has its own 20-second limit.

**Emailing them.** Emails are still sent by the webmaster from their own mailbox
([0015](0015-member-emails-and-page-server.md)), in one of two ways:
- **One email to everyone ticked**, with them all in **Bcc** (so members never see each other's
  addresses), the webmaster in To, and `{{FIRST_NAME}}` replaced by "everyone". This only works
  for an email with nothing personal in it. A member's single-use forum invite link, forum
  username or own address can't be shared, so `welcome.ForGroup` refuses any template that uses
  `{{FORUM_INVITE_LINK}}`, `{{FORUM_USERNAME}}` or `{{MEMBER_EMAIL}}`. Of the follow-ups, only
  **Verify your email** qualifies. Welcome and New forum invite both carry the invite link,
  including the reminder for an unused invite. When sent, it's recorded in the activity log once
  per member.
- **One by one:** the tool steps through the members: open the email in the browser, "Did you
  send it?", next. Each one can be skipped, and esc stops, saying how far it got.

**A related fix.** `welcome.Suggest` now treats an unused forum invite like having no forum
account. So someone just onboarded gets the Welcome email suggested, rather than Verify your email.

## Consequences
- Clearing the backlog is a few minutes' work rather than dozens of separate actions.
- A large "Need setting up" run can take several minutes because of the forum's limit. The
  progress line keeps saying what it's doing.
- Moving the re-send logic into `internal/followup` (`ResendOne`) removed business logic from the
  app's code.
