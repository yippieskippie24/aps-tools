# 0016 — Re-send Cloudflare's verification email by removing and re-adding the address

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
A member's list mail only reaches them once they click Cloudflare's verification email. Twelve
members hadn't. The "Verify your email" template reminds them, but the original verification
email is often lost. The webmaster asked whether the tool could trigger a new one when they send
the reminder, after confirming with them.

Cloudflare's dashboard has a "Resend" button, but its public API doesn't: destination addresses
can only be listed, read, created, edited and deleted. The documented workaround, used by other
tools too, is to **delete the unverified address and create it again**. Creating an address is
what sends the verification email.

## Decision
- When the webmaster opens **Verify your email** for someone whose address is still unverified
  (in the app, or `aps-tools email verify-email`), the tool asks: **"Also have Cloudflare send them
  a new verification email?"** Nothing happens without a yes.
- On a yes, the Cloudflare connector (`ResendVerification`) **checks first**, then removes and
  re-adds the address. It refuses, changing nothing, when:
  - the address isn't a destination address (onboarding adds it);
  - it's **already verified**, because removing it would stop their mail;
  - a routing rule or the catch-all **forwards to it**, because removing it could break the rule.
- If re-adding fails after the removal (for example, Cloudflare limits how often it sends
  verification emails), the error says so plainly and gives the dashboard steps to add the address
  back.
- Every attempt, successful or not, is recorded in the activity log. If the log can't be written,
  the app doesn't make the change.

## Consequences
- An unverified address gets nothing forwarded either way, so removing and re-adding it doesn't
  change anyone's mail.
- The address gets a new identifier in Cloudflare. Nothing in APS's setup uses it: the list Worker
  and its `LIST_` variables use email addresses.
- The API token needs **Email Routing Addresses: Edit** (see
  [setup/cloudflare.md](../setup/cloudflare.md)). Without it, Cloudflare refuses, and the tool says
  the token is missing a permission.
- If Cloudflare adds a real "resend" call to its API, the connector can switch to it without
  changing the screens.
