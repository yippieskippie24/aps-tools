# 0014 — Onboarding creates personal forum invites, with a write-access key

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
The welcome email links each new member to their **personal forum invite**
([Phase 2 plan, §4.5](../plans/phase-2-onboarding.md)). The webmaster tried the welcome email page
and didn't want to fill in the invite link by hand: the tool knows who the member is, so it should
fill it in itself. That means the tool has to create the invite, which is its first change on the
forum.

Discourse's user API keys (the only kind on the Starter plan,
[decision 0008](0008-discourse-user-api-key.md)) have only broad scopes. There's no
"create invites" scope: **"write" allows anything the webmaster's admin account can do.**

The alternative considered was the forum's existing shared, multi-use invite link. It's read-only
and works today, but anyone with the link can join, and it isn't tied to the member.

## Decision
- **The webmaster chooses**, in `aps-tools setup discourse`: "Look up members, and create invites
  for new members" (recommended; the key asks for **read and write**) or "Only look things up"
  (read-only, as before). The choice is saved as `create_invites` in the settings.
- **The tool's code only ever makes one kind of change:** `POST /invites.json`
  (`Client.CreateInvite`). The tests fail if the connector sends any other kind of request.
- **The invite:** for the member's email, single use (`max_redemptions_allowed: 1`), with
  **`skip_email`**, so the forum sends nothing and the link goes in the webmaster's welcome
  email. It can land on the **welcome topic** (`topic_id`, from the optional `welcome_topic_url`
  setting). The forum's own expiry setting applies (90 days for APS).
- **It's an onboarding step,** so it follows the safety rules:
  - it's previewed in the plan ("+ Create a personal invite link");
  - it only happens after **Go**;
  - it checks again first: no invite for someone who now has an account, and a pending invite
    is reused rather than duplicated;
  - it's recorded in the activity log.

  `aps-tools email welcome <email>` can also create it, after asking.
- **The welcome email fills in the link.** It uses the pending invite's link, or the forum's
  address if they already have an account. In the app, pressing **w** before the invite exists
  says to press **g** first.

## Consequences
- The welcome email needs no hand editing for a normal onboarding.
- The forum key is more powerful. If the Mac or the key's storage were compromised, someone could
  act as the webmaster on the forum. The key stays in the Keychain or 1Password like every other
  key, and can be revoked on the forum (profile → Preferences → Apps). A read-only setup is still
  available.
- Re-approving the key (to switch between read-only and invites) replaces the old key, because the
  tool reuses its client ID.
