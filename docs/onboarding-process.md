# How APS member onboarding works

> This page describes the **process itself**, independent of the tool. If `aps-tools` is ever broken or
> unavailable, follow these steps by hand. If the process changes, update this page *and* the tool.

_Steps marked **TBD** will be filled in as each system is investigated._

---

## New member checklist

| # | Step | System | What the member must do afterwards |
|---|---|---|---|
| 1 | Confirm the member is on the membership Google Sheet | Google Sheets | — |
| 2 | Add their email to the APS mailing list | Cloudflare | **Click the verification email from Cloudflare.** Until they do, they receive no list mail. |
| 3 | Add them as a customer on the APS service desk | Jira Service Management | TBD — possibly accept an invite / set a password |
| 4 | Invite them to the APS Discourse forum | Discourse | Accept the invite and create their account |
| 5 | Send the welcome email with instructions | Your own mailbox | Follow the instructions in the email |
| 6 | Record that setup is complete on the Google Sheet | Google Sheets | — |

### Step details

**2. Mailing list (Cloudflare).** TBD — APS email uses Cloudflare Email Routing with a Worker
that forwards list mail to members. Where the member list is stored will be documented once
investigated.

**3. Jira customer.** In Jira, open the APS service desk project → **Customers** → **Add customers**,
then enter the member's email. Customers do not use any of the 3 licensed agent seats.
TBD — confirm whether customers are grouped into an organization.

**4. Discourse.** TBD — document the invite method and any groups once Discourse's configuration
has been reviewed.

**5. Welcome email.** TBD — the standard template will be written in Phase 2.

## "Am I set up correctly?" — checking a member by hand

1. Search the Google Sheet for their email.
2. Cloudflare: check that their address is on the list **and shows as verified**.
3. Jira: search the service desk's **Customers** list for their email.
4. Discourse: Admin → Users → search their email. Check whether a pending invite was ever accepted.
