# 0008 — Connect to Discourse with a user API key approved in the browser

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
The Discourse connector was first built around an **admin API key** (Admin → Advanced → API
keys). The APS forum is on Discourse's hosted **Starter** plan, and Discourse only offers
admin API keys and webhooks on **Pro** and higher. Upgrading would break the zero-budget rule.

Discourse has a second kind of key, the **user API key**. It's how Discourse's own phone app
signs in. A user approves it on the forum, in their browser. It acts with that user's own
permissions and only within the scopes they approved. The "read" scope allows any page the
user could read (GET requests only), so a key approved by an admin can use the same admin
lookup pages the admin key did. It still can't change anything.

## Decision
`aps-tools setup discourse` gets a user API key by default:

1. The tool makes a fresh RSA key pair in memory and opens
   `/user-api-key/new?scopes=read&…` on the forum, with the public half.
2. The webmaster checks the page names aps-tools and asks only to read, then clicks **Authorize**.
3. Without a redirect address, the forum shows the new key locked with the public half. The
   webmaster pastes that text back, and only the tool can unlock it.
4. The tool tests the key (must be an admin; must be able to look members up), then saves it.
   It goes in the Keychain, or, for 1Password users, on the clipboard for a moment so they can
   save it in 1Password and paste its secret reference.

Requests then send `User-Api-Key` and `User-Api-Client-Id` headers. The client ID is random,
saved in settings (`[discourse] client_id`), and reused on later approvals. That way Discourse
replaces the old key instead of keeping both. It isn't secret: it's useless without the key.

The admin API key path is kept as the second choice (`key_type = "admin"`) for a future APS
on a Pro plan.

## Alternatives considered
- **Upgrade to Pro.** Costs money every month.
- **Sign in with the browser's login cookie.** Fragile (it expires and breaks on logout), would
  mean storing a full-power session, and isn't a supported way to use Discourse.
- **Read a user export (CSV).** Goes stale immediately, doesn't include pending invites, and
  leaves a file of member emails on the Mac.
- **Check by hand.** Kept as the fallback if the forum refuses user API keys. The report
  would say what to look for.
- **A redirect back to the tool instead of copy and paste.** Discourse only redirects to
  addresses an admin has allowed (`allowed_user_api_auth_redirects`). Copy and paste needs no
  forum settings changed.

## Consequences
- Not yet confirmed on a live Starter forum. If Discourse blocks user API keys on Starter,
  the approval page shows an error. The guide's "If the forum won't approve the key" section
  covers what to do then.
- Discourse removes user API keys that go unused for a while (**180 days** by default,
  setting `revoke_user_api_keys_unused_days`). After a long break, run setup again.
- The key appears in the webmaster's forum preferences under authorized apps, where it can
  be revoked. That's also how to cut it off on handover.
- Phase 2 (sending invites) will need a key with write access, approved the same way with
  more scopes.
