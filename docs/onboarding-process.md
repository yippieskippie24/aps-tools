# How APS member onboarding works

> This page describes the **process itself**, independent of the tool. If `aps-tools` is ever broken or
> unavailable, follow these steps by hand. If the process changes, update this page *and* the tool.

_Steps marked **TBD** will be filled in as each system is investigated._

---

## New member checklist

| # | Step | System | What the member must do afterwards |
|---|---|---|---|
| 1 | Confirm the member is on the membership Google Sheet **with "App vetted" = Y** | Google Sheets | — |
| 2 | Add their email to the APS mailing list | Cloudflare | **Click the verification email from Cloudflare.** Until they do, they receive no list mail. |
| 3 | Add them as a customer on the APS service desk | Jira Service Management | TBD — possibly accept an invite / set a password |
| 4 | Invite them to the APS Discourse forum | Discourse | Accept the invite and create their account |
| 5 | Send the welcome email with instructions | Your own mailbox | Follow the instructions in the email |
| 6 | Record that setup is complete on the Google Sheet | Google Sheets | — |

### Step details

**1. Membership sheet.** The membership chair adds each applicant to the sheet and marks **New**
while their application and background check are in progress. When everything is complete the chair
marks **App vetted** = **Y**. **Don't start steps 2–5 until App vetted is Y.** `aps-tools check`
shows this as "Waiting on member" until then. (Long-standing members who started before App vetting
began don't need it; see the setup guide.) See [setup/google-sheet.md](setup/google-sheet.md).

**2. Mailing list (Cloudflare).** APS email uses Cloudflare Email Routing with a Worker that
forwards list mail to members. In the Cloudflare dashboard: the APS domain → **Email** →
**Email Routing** → **Destination addresses** → **Add destination address**. Cloudflare then emails
the member a verification link; list mail only reaches them **after they click it**. Then add their
address to the all-members list: **Workers & Pages** → the list Worker → **Settings** →
**Variables and Secrets** → edit **`LIST_NEWS`** (add `, their@address`) → **Deploy**. Add them to
any other group lists (`LIST_PHOTOS`, …) the same way. See [setup/cloudflare.md](setup/cloudflare.md).

**3. Jira customer.** In Jira, open the APS service desk project → **Customers** → **Add customers**,
then enter the member's email. Customers do not use any of the 3 licensed agent seats.
TBD — confirm whether customers are grouped into an organization.

**4. Discourse.** The forum is **invite-only**, so every member needs an invite. On the forum,
open your profile → **Invites** → **Invite** and enter their email. They'll get an email to create
their account. Invites expire after 90 days; if one expires, send a new one. APS doesn't use a
member group, and new accounts don't need approval. The exact sign-up settings are summarised by `aps-tools discourse discover`. See
[setup/discourse.md](setup/discourse.md).

**5. Welcome email.** TBD — the standard template will be written in Phase 2.

## "Am I set up correctly?" — checking a member by hand

1. Search the Google Sheet for their email, and check **App vetted** is **Y**.
2. Cloudflare: under **Destination addresses**, check their address is there **and shows as
   verified**. Then check it's in the list Worker's **`LIST_NEWS`** variable.
3. Jira: search the service desk's **Customers** list for their email.
4. Discourse: Admin → Users → search their email. If there's no account, check your profile →
   **Invites** for a pending or expired invite. Or just run `aps-tools check <email>`.
