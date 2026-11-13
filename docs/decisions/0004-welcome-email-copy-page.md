# 0004 — Welcome email is a "copy page", not sent by the tool

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
Welcome emails are sent from the webmaster's personal iCloud Mail (custom domain). iCloud has no
sending API, and sending through a third-party email service would cost money or add another
account to manage.

## Decision
The tool generates a local HTML page for each welcome email, with buttons to:
- **Copy To** — the member's address,
- **Copy Subject**,
- **Copy Body** — as formatted rich text that pastes cleanly into Mail.

You paste each into a new email and send it yourself. The tool then asks whether you sent it and
records the answer in its activity log.

## Alternatives considered
- **Sending via SMTP with an app-specific password** — possible with iCloud, but riskier (the tool
  could send mistakes automatically) and leaves no chance to tweak the message.
- **A transactional email service** (e.g. SendGrid) — another account, possible cost, and emails
  wouldn't come from the webmaster.

## Consequences
- You always review the email before it goes out.
- The tool can't *know* an email was sent, so it relies on you confirming it.
