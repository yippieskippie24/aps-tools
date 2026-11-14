# Setup: Cloudflare (APS email and the mailing lists)

APS email runs on **Cloudflare Email Routing** (free). Each group address (`news@`, `photos@`,
`treasurer@`, …) has a routing rule that sends its mail to one Cloudflare **Worker**, which
forwards it to that group's members.

## How the mailing lists work

- Each group's members are kept in a **Text variable** on the Worker named `LIST_` plus the part
  of the address before the @, in capitals. Anything that isn't a letter or digit becomes `_`:

  | Address | Variable |
  |---|---|
  | `news@` (all members) | `LIST_NEWS` |
  | `photos@` | `LIST_PHOTOS` |
  | `board-members@` | `LIST_BOARD_MEMBERS` |

- The value is the members' addresses, separated by commas. You can see and edit it in the
  dashboard: **Workers & Pages** → the list Worker → **Settings** → **Variables and Secrets**.
- **The variables must be type "Text", not "Secret".** Cloudflare never reveals Secret values,
  so the tool couldn't read the list.
- **New group address:** add a routing rule (Send to Worker → the list Worker) plus a `LIST_`
  variable. No code change needed.
- Everyone on a list must also be a **verified destination address** (see below), or Cloudflare
  silently won't deliver to them. `aps-tools cloudflare discover` counts any who aren't.

> **Status (Nov 2026):** the switch-over to Cloudflare is planned for **December 2026**. Until then
> Email Routing is off and the list Worker doesn't exist yet, but members are already verifying
> their addresses. The member check shows who has and hasn't, and notes that routing is off.

Cloudflare only forwards mail to **destination addresses** whose owners have clicked a
verification link from Cloudflare. So a member receives list mail only if they're:

1. added as a destination address,
2. **verified** (they clicked Cloudflare's "Verify email address" email), and
3. on the all-members list (`LIST_NEWS`, unless setup was told another list).

**The member check reports all three**, plus which other lists the person is on. Before the list
Worker exists, it reports 1 and 2 and says the list couldn't be checked.

## What you need

- Access to the APS Cloudflare account.
- A **read-only** Cloudflare API token, created in step 1. The tool never changes anything in
  Cloudflare.

## 1. Create a read-only API token

1. Sign in at **https://dash.cloudflare.com**.
2. Click the **profile icon** (top right) → **My Profile** → **API Tokens**. (An *account* token
   from Manage Account → Account API Tokens also works, but its form can look different.)
3. Click **Create Token**, then next to **Create Custom Token** click **Get started**.
4. **Token name:** `aps-tools (read-only)`
5. **Permissions.** The token is built from **policies**. Each policy has a **type** (Account or
   Zone), the **resources** it covers, and its **permissions**. You need **two policies**. Every
   permission is **Read**.

   **Policy 1 — type: Account.** Resources: the APS account. Permissions:

   | Permission | Access |
   |---|---|
   | Email Routing Addresses | Read |
   | Workers Scripts | Read |
   | Workers KV Storage | Read |

   **Policy 2 — type: Zone.** Click **Add new policy** at the bottom of the form. Resources:
   *Specific zone* → the APS domain. Permissions:

   | Permission | Access |
   |---|---|
   | Email Routing Rules | Read |
   | Zone Settings | Read |
   | Zone | Read |

   - **Don't add "Workers Routes".** It's for attaching Workers to web addresses, which the tool
     doesn't need.
   - **If "Workers Scripts" isn't offered:** in September 2026 Cloudflare began moving Workers
     permissions to *roles*. Instead choose **Workers** with the role **Content Read-Only**, the
     read-only equivalent that can read Worker code and settings. KV storage hasn't moved to roles
     yet, so keep **Workers KV Storage: Read**.

6. **Expiration:** optional. If you set an end date, add a calendar reminder.
7. Click **Continue to summary**, check that every permission says **Read**, then **Create Token**.
8. **Copy the token now.** Cloudflare shows it only once. Save it in 1Password (and copy its
   secret reference), or keep it on the clipboard if you use the Keychain.

> **Why read-only for now?** Checking members and `discover` only read, so a read-only token means
> even a bug can't change APS email. Adding members automatically (Phase 2) will need two
> permissions changed from **Read** to **Edit**, both in the **Account** policy:
>
> | Permission | Why Phase 2 needs Edit |
> |---|---|
> | Email Routing Addresses | Add the member as a destination address. Cloudflare then emails them the verification link. **Already used** by onboarding to add new members ([decision 0017](../decisions/0017-cloudflare-and-jira-onboarding-steps.md)), and to re-send a verification email when you send "Verify your email" ([decision 0016](../decisions/0016-resend-cloudflare-verification.md)). |
> | Workers Scripts | Add their address to `LIST_NEWS` (and other `LIST_` variables) on the list Worker. |
>
> When that feature arrives, **edit this same token** (API Tokens → ⋯ → **Edit**). Editing
> permissions doesn't change the token itself, so nothing in 1Password or the tool's settings
> needs updating. The tool will say if a write is refused for lack of permission.

## 2. Run the setup

```bash
aps-tools setup cloudflare
```

It asks for the token (or its 1Password secret reference), then:

1. **checks** the token is valid,
2. lists your domains so you can **pick the APS domain**. That also tells it the Cloudflare account,
3. **checks** the token can read Email Routing, and shows how many destination addresses exist,
4. finds the mailing lists and asks **which one goes to all members** (usually `news@`),
5. **saves**, but only after all of that worked.

> **Trying it on a test account first?** Run setup with a read-only token from the test account
> and pick its domain. When you're ready for the real thing, run `aps-tools setup cloudflare`
> again with the APS token and domain. Each run replaces the previous choice.

## 3. Discover how the mailing list is set up

```bash
aps-tools cloudflare discover
```

This read-only report shows:

- whether Email Routing is on,
- **where mail to each APS address goes** (e.g. `news@…` → Worker `"aps-email-lists"`),
- **each mailing list**: how many members it has, and how many of them are *unverified* or *not a
  destination address at all*. Those people won't get that list's mail. The all-members list is
  marked.
- how many destination addresses there are, and **how many are still waiting** for the person to verify,
- for each Worker that receives mail: its connected storage (KV, settings, databases) and whether
  member addresses are written into its code,
- **"What this means"**: where the member list most likely lives.

**Member email addresses are never shown.** They're replaced with `[email hidden]`. Addresses on
the APS domain itself (like the list address) are shown, because they're configuration.

To read the Worker's code yourself (with member emails hidden), save it to a folder:

```bash
aps-tools cloudflare discover --save-code ~/Desktop
```

## How the check decides

| Result | Meaning |
|---|---|
| ✓ **Set up** | On the all-members list, and their address is verified. Also shows the other lists they're on. |
| … **Waiting on member** | On the list, but they haven't clicked Cloudflare's verification email. **The most common cause of "I'm not getting list emails."** Ask them to search their inbox and spam for an email from Cloudflare. |
| ✗ **Problem** | On the list, but not a destination address, so Cloudflare can't deliver to them. Add them under Destination addresses. |
| ✗ **Not set up** | Not on the all-members list. The fix names the exact variable to edit (e.g. `LIST_NEWS`). |
| ? **Couldn't check** | Cloudflare couldn't be reached, or the token was rejected or lacks a permission. Run `aps-tools doctor`. |

## What's stored where

| Setting | Stored in |
|---|---|
| Account and domain (IDs and names) | Settings file, `[cloudflare]` section |
| API token | Keychain (`aps-tools`), or 1Password, in which case only its `op://` reference is in the settings file |

Technical reference: the connector is in `internal/connectors/cloudflare/`. Every call is a
read-only `GET` to `api.cloudflare.com/client/v4`.
