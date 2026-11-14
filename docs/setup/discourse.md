# Setup: Discourse (the APS forum)

Connects the tool to the APS Discourse forum so it can check whether a member has a working
account, or an invite they haven't accepted yet.

**What the tool can do with Discourse right now:** look members up and summarise how sign-up works
(read-only). Sending invites automatically comes in Phase 2. It will need you to approve a key
with write access, in the same way.

## What you need

| Item | Where to find it |
|---|---|
| **Forum address** | e.g. `forum.example.org` |
| **Your own admin account** on the forum | You'll sign in with it to approve the tool |

That's all. The tool gets its own **read-only key**, which you approve in your browser.

> **Why not an "admin API key"?** Discourse only lets **Pro** plans (and higher) create admin
> API keys, and APS is on the **Starter** plan. The tool uses a **user API key** instead. That's
> the kind Discourse's own phone app uses, and it works on every plan. You approve it once, it
> acts as you, and it can only **read**. Background:
> [decision 0008](../decisions/0008-discourse-user-api-key.md). If APS ever moves to Pro, setup
> still offers the admin-key route (step 1, second choice).

## 1. Run the setup

```bash
aps-tools setup discourse
```

1. Enter the **forum address**.
2. Choose **"Approve it in my browser"**.
3. A forum page opens in your browser. Sign in as yourself if asked. Check that it names
   **aps-tools** and asks only to **read**, then click **Authorize**.
4. The forum shows a block of scrambled-looking text. **Copy all of it**, go back to the
   Terminal, paste it, and press Enter. Only this run of the tool can unscramble it, so the text
   is useless to anyone else.
5. The tool **checks** the key belongs to an admin and **checks** it can look members up.
6. It asks whether **every member should be in a particular group** (e.g. `members`). If you
   pick one, the member check confirms each person is in it. If not, choose "No".
7. It **saves** the key:
   - **Keychain:** saved automatically.
   - **1Password:** the tool puts the **key** on your clipboard. It's a short code, not the
     scrambled text from the forum page, so don't copy anything else yet. In 1Password, create an
     **API Credential** item (e.g. "APS Discourse key (aps-tools)"), paste into the **credential**
     field and save. If you're reusing an item from an earlier try, paste over the old value,
     because every approval makes a new key. Then use the arrow next to that field → **Copy Secret
     Reference**, and paste the reference into the Terminal. The tool reads it back to make sure
     it matches, then clears the clipboard. If it doesn't match, the tool says why (empty field,
     the forum's scrambled text, a different field, or an older key) and puts the key back on
     your clipboard so you can fix the item.

Nothing is saved until the key has passed every check.

### If the forum won't approve the key

If the forum shows an error instead of an **Authorize** button, press **Esc** in the Terminal
(nothing is saved). Then:

- **"You need to log in"** or a sign-in page: sign in, then run setup again.
- **An error about permissions, or a "page doesn't exist" message:** the forum may not allow
  user API keys on its plan. Check **Admin → Settings**, search **user api**, and make sure:
  - **allow user api key scopes** includes `read`;
  - **user api key allowed groups** includes `admins`.

  If those settings are missing or locked, the plan doesn't allow it. The member check then
  can't look at the forum, so check by hand at **Admin → Users** (search the email).

### Several "aps-tools" entries on the forum?

Each approval creates a key, even if setup then stops before saving. Unsaved ones are never used,
but it's tidy to remove them. Revoke **all** the "aps-tools" entries (see below), then run
`aps-tools setup discourse` once more and finish it.

### Turning the key off

The key shows up on the forum under **your profile → Preferences → Security** (or **Apps**), in
the list of authorized apps, as "aps-tools". **Revoke** it there to cut the tool off, e.g. when
handing over. Discourse also removes keys that go unused for about **6 months**. If the tool
says the key was refused after a long break, run `aps-tools setup discourse` again.

## 2. See how sign-up works

```bash
aps-tools discourse discover
```

A read-only summary of the settings that shape how members join: invite-only or open sign-up,
whether an admin must approve accounts, whether the forum is private, login methods (email,
Google, …), invite expiry, plus the forum's groups and how many of **your** invites are still
waiting. It ends with **"What this means"** for onboarding. No member names or emails are shown.

## How the check decides

| Result | Meaning |
|---|---|
| ✓ **Set up** | Has an active account (and is in the member group, if one is set). Shows their username, when they were last seen, and their groups. |
| … **Waiting on member** | Invited but hasn't accepted yet, **or** signed up but hasn't clicked the activation email. The fix says how to resend. |
| ✗ **Problem** | Has an account, but it's waiting for admin approval, suspended, or not in the member group. The fix says where to sort it out. |
| ✗ **Not set up** | No account and no invite, or the invite expired. |
| ? **Couldn't check** | The forum couldn't be reached, or the key was refused (revoked, or unused for months). Run `aps-tools doctor`, then `aps-tools setup discourse` if needed. |

**Limitation:** Discourse only shows the tool the invites **you** sent (the key acts as you). If
another admin invited someone, the check won't see that invite.

**Privacy:** checking one member filters by their email, so it adds nothing to the forum's staff
action log. The app's **Members** screen checks everyone at once. To do that it reads the user
list *with* email addresses, which adds one "show emails" entry to the staff action log each time
it loads or refreshes ([decision 0013](../decisions/0013-members-screen-bulk-checks.md)).

## What's stored where

| Setting | Stored in |
|---|---|
| Forum address, your username, member group, key type, client ID | Settings file, `[discourse]` section |
| The key | Keychain (`aps-tools`), or 1Password, in which case only its `op://` reference is in the settings file |

The **client ID** is a random name for this computer's copy of the tool. It isn't secret. Approving
again from the same computer replaces the old key instead of adding another.

Technical reference: the connector is in `internal/connectors/discourse/`. The approval
hand-shake is in `userkey.go`. Every call is a read-only `GET`, with `User-Api-Key` /
`User-Api-Client-Id` headers (user key) or `Api-Key` / `Api-Username` headers (admin key).
