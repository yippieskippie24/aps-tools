# Setup: Jira Service Management

Connects the tool to the APS service desk so it can check whether a member is a **customer**
there. Members are customers, so they never use one of the free plan's 3 agent seats.

**What the tool can do with Jira right now:** look members up (read-only). Adding customers
automatically comes in Phase 2.

## What you need

| Item | Where to find it |
|---|---|
| **Jira site address** | The start of any Jira link, e.g. `example.atlassian.net` |
| **Your Atlassian account email** | The email you log in to Jira with |
| **An Atlassian API token** | Created in step 1 below |
| Agent or admin access to the APS service desk | Ask the outgoing webmaster if you don't have it |

## 1. Create an API token

1. Go to **https://id.atlassian.com/manage-profile/security/api-tokens** (signed in as yourself).
2. Click **Create API token**. Use the plain version, **not** "Create API token with scopes".
3. **Name:** `aps-tools`. **Expires on:** the latest date allowed (Atlassian allows up to 1 year).
4. Click **Create**, then **Copy**. Atlassian shows the token only once.
5. Store it right away:
   - **Using 1Password:** save it in 1Password (see [1password.md](1password.md)), then copy
     its **secret reference**.
   - **Using the Keychain:** keep it on your clipboard for step 2.
6. Put a reminder in your calendar a week before it expires.

## 2. Run the setup

```bash
aps-tools setup jira
```

It asks for your site address, your Atlassian email, and the token (or its 1Password secret
reference). Then it:

1. **tests** the details with Jira and shows the name of the account it connected as,
2. lists the service desks your account can see so you can **pick the APS one**,
3. **saves** everything, but only once it has all worked.

## 3. Check it works

```bash
aps-tools doctor
aps-tools check someone@example.com
```

Try `check` with the email of a member you *know* is a customer, and one you know isn't.

## How the check decides

| Result | Meaning |
|---|---|
| ✓ **Set up** | Their email is on the service desk's customer list. |
| ✗ **Not set up** | No customer on the service desk has that email. |
| ✗ **Problem** | They're a customer, but their Atlassian account is deactivated. |
| ? **Couldn't check** | Jira couldn't be reached or the token was rejected. Run `aps-tools doctor`. |

> **Known limitation to watch for:** Some Jira users report that the customer search through
> the API can miss customers who do appear on the **Customers** page in Jira. On the APS
> service desk, results matched the Customers page when tested (2026-11-13). If the tool ever
> says "Not set up" for someone you can see on that page, note their situation (e.g. how they
> were added, or whether they belong to an organization) so the tool can be adjusted.

## When the token expires

Checks start failing with "Jira rejected the email or API token". Create a new token (step 1),
then run `aps-tools setup jira` again. With 1Password, if you replaced the token *inside the
same item*, nothing else needs to change.

## What's stored where

| Setting | Stored in |
|---|---|
| Site, your email, service desk ID and name | Settings file (`aps-tools config path`), `[jira]` section |
| API token | Keychain (`aps-tools`), or 1Password. In that case only its `op://` reference is in the settings file. |

Technical reference: the connector is in `internal/connectors/jira/`. It uses Jira's
`/rest/servicedeskapi/servicedesk/{id}/customer` endpoint, which Atlassian marks "experimental".
