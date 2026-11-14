# Plan: Phase 2 — guided onboarding

- **Status:** Draft for the webmaster's review. Nothing here is built yet.
- **Date:** 2026-11-13

Phase 1 can **see** where a member stands in every system. Phase 2 lets the tool **do** the
onboarding: one guided flow that sets a vetted member up everywhere, sends nothing by surprise, and
can be stopped and safely re-run at any point.

## 1. What onboarding a member will look like

From the app (**2 Onboard a new member**) or the command line (`aps-tools onboard alex@example.com`):

1. **Gate: the membership sheet.** The tool finds them on the sheet.
   - If **App vetted** isn't **Y**, it stops: "Not vetted yet, so wait for the membership chair."
   - If they're vetted, it reads their name from the sheet, so you don't retype it.
2. **Plan.** It checks every system (exactly what `aps-tools check` does today) and lists what it
   *would* do, skipping anything already done:
   ```
   Onboarding Alex Example <alex@example.com>
     ✓ Membership sheet      vetted (row 42)                  nothing to do
     + Mailing list          add destination address; add to LIST_NEWS
     ✓ Jira                  already a customer               nothing to do
     + Discourse forum       create a personal invite link (no email from Discourse)
     + Welcome email         open the copy page
     + Membership sheet      record "set up" (date)
   ```
3. **Confirm.** Nothing changes until you say **Go**. `--dry-run` stops here, always.
4. **Do it, one step at a time,** with a live ✓ or ✗ per step. If a step fails, the rest still run
   where it's safe, and the failure says how to fix it. Re-running onboarding later picks up only
   what's still missing.
5. **Re-check.** It runs the member check again and shows the result.
6. **Log it.** Every action, including what changed and when, goes into a local activity log on
   your Mac.

## 2. The steps in detail

| # | System | What the tool does | What the member must do | Needs |
|---|---|---|---|---|
| 1 | Membership sheet | Checks they're vetted; reads their name | — | Done (read) |
| 2 | Mailing list | Adds a **destination address**, which makes Cloudflare send a verification email. Then adds them to **`LIST_NEWS`** on the list Worker. Only `news@`: the other lists are for board roles (see §5). | **Click Cloudflare's verification email.** No list mail arrives until they do. | Token has Edit ✓ |
| 3 | Jira | Creates them as a **customer** and adds them to the APS service desk, so their emails asking for help aren't blocked. Members never log in to Jira, and the welcome email doesn't mention it. | Nothing | Token already allows it (to confirm) |
| 4 | Discourse | Creates a **personal invite** tied to the member's email, **without** Discourse emailing them. The invite's link goes into the welcome email. Single-use, expires in 90 days, and shows in the member check as a pending invite. Re-running onboarding reuses the existing invite's link. The invite is set to **land them on the forum's welcome topic** after they sign up. | Click the link in the welcome email and create an account | A read-and-write key, approved in the browser |
| 5 | Welcome email | Opens the **copy page** (To / Subject / Body buttons); you send it from your own mail. Then it asks "Sent?" and logs the answer. | Read it | The email text, written together |
| 6 | Membership sheet | Writes the member's full details to a **tab the tool owns**. Sets a short **status** (e.g. Pending / Complete, wording to decide) in **one tool-owned column on the member directory tab**, as a link to their row on the details tab. | — | Share approved as Editor |

### Safety rules (from CLAUDE.md, applied to every step)
- **Preview first:** every change is shown before it happens.
- **Check before acting:** a step that's already done is skipped, never repeated. For example, no
  second Cloudflare verification email and no duplicate Discourse invite.
- **Dry-run:** `--dry-run` previews and changes nothing.
- **One member at a time** to start with. Batches come later (Discourse allows 10 invites a day).
- Writes go only to what each step names. The tool never deletes anything in Phase 2.

### Things that need extra care
- **`LIST_NEWS` lives in the Worker's settings.** Cloudflare updates a Worker's settings as a
  whole. So the tool must read every setting, change only `LIST_NEWS`, and write them all back,
  then read again to confirm nothing else changed. It also checks the ~5 KB size limit before
  writing.
- **Testing writes safely.** The APS account has no list Worker until December, and the token is
  now limited to the APS account. To try the mailing-list step first on the **test** account,
  either add the test account back to the token (Include → specific accounts: APS + test) or
  wait for the APS Worker. Recommendation: add the test account back while building this.
- **Jira customer creation** must not send members anything confusing, because they don't know
  Jira exists. Check the service desk's notification settings first (read-only) and confirm before
  building.
- **The welcome email text is APS-specific**, and this repository is public. So the real template
  lives on the webmaster's Mac, next to the settings file, where it can be edited. The repository
  ships a generic example, with placeholders the tool fills in:
  - `FIRST_NAME`, from the sheet;
  - `NEWS_ADDRESS` and `FORUM_ADDRESS`, from setup;
  - `WEBMASTER_NAME`, from `setup webmaster`;
  - `FORUM_INVITE_LINK`, the member's personal invite, created during onboarding;
  - `GETTING_STARTED_LINK`, the forum's Getting Started tag, from `setup discourse`.

## 3. Build order

Each item is usable on its own, safest first:

1. **Framework:**
   - the onboarding plan/preview screen;
   - `--dry-run`;
   - the activity log;
   - the "Onboard" menu item, which today shows "coming soon".

   No outside system is changed yet; it runs the plan and stops.
2. **Welcome email copy page.** It changes nothing anywhere, and only needs the text.
3. **Mailing list (Cloudflare):** destination address, then `LIST_NEWS`. Tried on the test account
   first.
4. **Jira customer.**
5. **Discourse personal invite link:** re-approve the forum key with write access when we get
   here. Confirm Discourse's invite API (`POST /invites.json` with the member's email and
   `skip_email`) returns the link, and that its `topic_id` option sends new members to the welcome
   topic. The welcome email copy page already has a slot for the link, so this step fills it in.
   `setup discourse` will ask for the **welcome topic** and the **Getting Started** tag. You paste
   their links, and they're saved in your settings, not in this repository.
6. **Sheet write-back:** the details tab and status column, once the share is approved.

Each step gets its own decision record where there's a real choice (e.g. how `LIST_NEWS` is
edited), plus updates to the setup guides and [onboarding-process.md](../onboarding-process.md).

## 4. Decisions (answered 2026-11-13)

1. **Sheet write-back:** two parts.
   - **A details tab the tool owns** (e.g. "aps-tools"), with one row per member: email, name, and
     each system's state and dates (added, verified, invited, accepted, …), plus when it was last
     checked.
   - **One status column on the member directory tab** (e.g. "Webmaster status"), so the board sees
     at a glance where each member stands. Wording to decide later: something like *Not started*,
     *Pending (waiting on member)*, *Complete*, *Needs attention*.
   - **Each status is a link** to that member's row on the details tab. Google Sheets supports
     links within a sheet, in the form `=HYPERLINK("#gid=<tab id>&range=A<row>", "Complete")`, so
     clicking "Pending" jumps to the details.

   How it stays safe and correct:
   - **The tool writes only its own column**, found by its header text, like every other column.
     If the column doesn't exist, the tool offers to add it, with a preview, instead of guessing.
     No other cell on the directory tab is ever changed.
   - **Rows are found by email at the moment of writing**, never by a remembered row number, because
     people sort and insert rows. The details tab is only edited by the tool. It keeps one row per
     member and updates it in place, so links stay valid.
   - **Statuses go stale on their own.** "Pending" turns into "Complete" when a member clicks
     Cloudflare's link or accepts the forum invite, and the tool isn't told. So there will be a
     **"Refresh sheet status"** action that re-checks members and updates the column. It runs after
     each onboarding and on demand (the Phase 3 bulk audit builds on it).
   - **New dated directory tabs** (e.g. a new "Member Directory …" copy) carry the column and its
     links along, and the links keep working because they point at the details tab.
2. **Other lists:** onboarding adds members to **`news@` only**. The other lists are for board and
   committee roles (see §5).
3. **Jira:** members never log in. Being a customer just means their help emails (and their
   initial account request) aren't blocked. **The welcome email doesn't mention Jira.**
4. **Welcome email:** drafted for review (see below). It covers:
   - verifying the Cloudflare email, which matters most;
   - accepting the forum invite;
   - replying with questions.
5. **Discourse invite:** a **personal invite link per member**, created by the tool with
   Discourse's own invite email turned off, and placed in the welcome email. One email from you,
   with everything in it, instead of a separate forum email. This is safer than one shared link:
   - it works only once;
   - it's tied to their email, so it can't be passed on;
   - the member check can see it.

### First login: where new members land

New members get the most from the forum if they start in the right place. APS has a
**Getting Started** tag with topics written for newcomers, and a **welcome topic** meant as the
starting point. So:
- **Each personal invite lands on the welcome topic.** Discourse invites can name a topic to open
  after sign-up.
- **The welcome email links to the Getting Started tag**, for anyone who wants to come back to it
  later.
- `aps-tools discourse discover` will show whether the configured topic and tag still exist, in
  case one is renamed or deleted.

## 5. Later: board changes (new, from the webmaster)

The other lists (board-members@, treasurer@, webmaster@, …) follow **board roles**. Half the board is
elected each July for two-year terms. So the tool will also need a "board change" flow:
- show who is on each role list;
- preview the changes (who leaves, who joins, which lists);
- update the lists.

Probably also Discourse's Board-Members group. This builds on the same `LIST_` editing as
onboarding, so it's planned for after Phase 2's mailing-list step. It needs to be ready before the
July 2027 handover.

## 6. Welcome email draft

Draft text for review. Once agreed, it becomes the example template:

> **Subject:** Welcome to APS! Two quick steps to get connected
>
> Hello {{FIRST_NAME}},
>
> Welcome to the Arboretum Photographers Society! Your membership is complete, and I've just set
> you up on the APS email list and members' forum. There are two quick things only you can do to finish.
>
> **The short version**
> 1. **Verify your email address.** Look for an email from **Cloudflare** asking you to verify
>    your address, and click the link in it. **Until you do, you won't receive any APS emails.**
> 2. **Join the forum.** Click your personal invitation link and follow the steps to create your
>    account: {{FORUM_INVITE_LINK}}
>
> The Cloudflare email should already be in your inbox. If you don't see it, please check your
> spam or junk folder.
>
> **1. The APS mailing list.** APS news and announcements go to {{NEWS_ADDRESS}}, which forwards
> them to every member. Our email runs on a service called Cloudflare. Before it can deliver
> anything to you, it needs you to confirm your address, so it sends you one verification email.
> - It comes from Cloudflare rather than APS, so it's easy to mistake for spam. It's genuine.
> - Just click the link once. There's nothing else to sign up for.
> - If you can't find it, or the link no longer works, reply to this email and I'll send a new one.
>
> **2. The members' forum.** The forum at {{FORUM_ADDRESS}} is APS's members-only discussion
> site. You'll need to log in to read it.
> - Your invitation link is: {{FORUM_INVITE_LINK}}. Click it, then follow the steps to choose a
>   username and password.
> - The link is just for you and works once, so please don't share it.
> - Please use it in the next few weeks. Invitations expire after a while (if yours does, just let
>   me know).
> - Once you've signed up, you'll land on our welcome topic, the best place to start. Our
>   **Getting Started** topics explain how the forum works: {{GETTING_STARTED_LINK}}
>
> **Questions?** Just reply to this email and I'll be happy to help.
>
> Welcome aboard!<br>
> {{WEBMASTER_NAME}}<br>
> APS Webmaster
