# Setup: Discourse (the APS forum)

Connects the tool to the APS Discourse forum so it can check whether a member has a working
account, or an invite they haven't accepted yet.

**What the tool can do with Discourse right now:** look members up and summarise how sign-up works
(read-only). Sending invites automatically comes in Phase 2.

## What you need

| Item | Where to find it |
|---|---|
| **Forum address** | e.g. `forum.example.org` |
| **Your Discourse username** | Your own **admin** account |
| **A read-only API key** | Created in step 1 below |

## 1. Create a read-only API key

1. On the forum, open the **Admin** area. In newer versions, API keys are under
   **Advanced → API keys**; in older ones, the **API** tab → **Keys**.
2. Click **New API Key** (or **Add API key**).
3. Fill in:

   | Field | Value |
   |---|---|
   | **Description** | `aps-tools (read-only)` |
   | **User Level** | **Single User**, then pick **your own username** |
   | **Scope** | **Read-only** |

4. Click **Save**, then **copy the key now**. Discourse shows it only once. Save it in 1Password
   (and copy its secret reference), or keep it on the clipboard if you use the Keychain.

> **Why "Single User" and "Read-only"?** The key can then do only what *you* can do, and only by
> looking, never changing. Your account must be an admin, because member lookups use the admin
> pages. Phase 2 (sending invites) will need the scope changed later.

## 2. Run the setup

```bash
aps-tools setup discourse
```

It asks for the forum address, your username, and the key (or its 1Password secret reference),
then:

1. **checks** the key works and belongs to an admin,
2. **checks** it can look up members,
3. asks whether **every member should be in a particular group** (e.g. `members`). If you pick
   one, the member check confirms each person is in it. If not, choose "No",
4. **saves**, but only after all of that worked.

## 3. See how sign-up works

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
| ? **Couldn't check** | The forum couldn't be reached, or the key was refused. Run `aps-tools doctor`. |

**Limitation:** Discourse only shows the tool the invites **you** sent (the key's owner). If
another admin invited someone, the check won't see that invite.

**Privacy:** the tool finds members by filtering on their email. It never asks Discourse to
*display* email addresses, so checks don't clutter the forum's staff action log.

## What's stored where

| Setting | Stored in |
|---|---|
| Forum address, your username, member group | Settings file, `[discourse]` section |
| API key | Keychain (`aps-tools`), or 1Password, in which case only its `op://` reference is in the settings file |

Technical reference: the connector is in `internal/connectors/discourse/`. Every call is a
read-only `GET` with `Api-Key` / `Api-Username` headers.
