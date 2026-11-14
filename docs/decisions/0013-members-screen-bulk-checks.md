# 0013 — The Members screen: bulk checks and an in-memory snapshot

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
The webmaster mostly uses the app, not the commands. They want to see everyone at once: who's
fully set up, who still needs setting up, and who the tool is waiting on. That means checking
every member on the sheet (about 40) in every system.

Checking members one by one is too slow. The forum allows a "user API key" about **20 requests a
minute**, and a single forum check takes up to 4 requests, so 40 members would take several
minutes. The webmaster also asked whether data could be cached to make things faster.

## Decision
**Bulk checks.** A connector can implement `connectors.BulkChecker`: check many members at once,
reading its whole list of people in a few requests:

| System | How it reads everyone |
|---|---|
| Cloudflare | the destination address list and mailing lists, once (also used for single checks now) |
| Jira | the desk's customer list, 50 per page. Anyone not found is searched for individually, but only if Jira hides someone's email |
| Discourse | the admin user list with emails (100 per page) and your invites. Full records are fetched only for suspended or silenced accounts, or when a member group is set |

A system without bulk support is checked a few members at a time. The sheet lists everyone through
`connectors.MemberLister`. `internal/roster` combines them and sorts members into groups: needs
setup, waiting on member, couldn't check, not ready (sheet), and all set up.

**Discourse's staff action log.** Asking the forum to show email addresses adds one "show emails"
entry to its staff action log per page (so one per refresh for APS). Single checks still filter by
email instead, which adds nothing. That's the trade for a list that loads in seconds.

**Caching: in memory, for now.** The roster is a **snapshot** that the app keeps while it's open.
It shows its age ("checked 3 min ago"), and **r** refreshes it. Moving between groups and
members is instant. Anything that **changes** a system, which is onboarding, always checks that
member again, live, first. Returning to the list after onboarding re-checks just that member.

**No cache on disk, yet.** Saving the snapshot would make the screen appear instantly on
start-up, but:
- it would put member names, emails and statuses in another file on the Mac;
- a stale list could mislead;
- with bulk checks, a full refresh takes only seconds.

Revisit if it ever feels slow. The safe version is "show the saved list straight away, marked with
its age, while refreshing in the background", in a file readable only by the user, like the
activity log.

**Finding one member quickly.** "Check a member" doesn't load the roster. It lists everyone from
the sheet alone (`roster.Directory`: one read), searchable by name or email, and checks only the
person you choose. That's for days when you only need one member and don't know their email.

## Consequences
- The Members screen is the app's front page: menu item 1.
- Other connectors can opt into bulk checks without changing the screen.
- A system that can't be read at all shows every member as "Couldn't check", with the reason at
  the top of the screen.
