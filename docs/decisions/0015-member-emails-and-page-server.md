# 0015 — Templates for common member emails, shown by a local page server

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
After the welcome email, the webmaster pointed out that most of their member emails are
repeats: someone who already has a forum account but can't get in, an invite that expired, a
member who never verified their email, and so on. They asked for a template for each common
situation. They also asked for the email page to be served by the tool rather than written to a
file.

## Decision
**One template per situation** (`welcome.Kinds`):
- Welcome email;
- Forum access help;
- New forum invite;
- Verify your email;
- Not getting APS emails;
- Application in progress;
- Changing your email address.

Each is a text file next to the settings, created from a built-in example, and edited in
TextEdit. They all share one format and one set of placeholders (see
[member-emails.md](../member-emails.md)).
- **The tool suggests the right email** from the member's status, and the webmaster chooses. In
  the app it's **e** on the Members screen. On the command line it's
  `aps-tools email <kind> <email>`.
- **Emails that link to a personal invite** need the invite to exist first: onboarding creates
  it, or the command offers to ([decision 0014](0014-discourse-invites-write-access.md)).
- A note on the page flags an email that doesn't seem to fit the member.
- Elevator access isn't included yet: the process hasn't been written up. Adding a kind is a
  small, documented code change.

**A local page server instead of files** (`internal/pageserver`):
- It uses Go's built-in web server, so it's free and needs no install.
- It listens only on 127.0.0.1, on a random port.
- Each page has a 128-bit random address, is held in memory only, and is sent with `no-store`.
- It answers only GET requests for those addresses.

This replaces writing the page to the cache folder. Nothing with member details stays on disk,
and the page is an ordinary web page.

## Consequences
- For `aps-tools email …`, the page lives only while the command runs. The "Did you send it?"
  question keeps it running. Without a terminal, it waits for the browser to load the page first.
- Another program on the same Mac could reach the server only by guessing a 128-bit address.
- More kinds can be added without changing the screens: they read the list.
