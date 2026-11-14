# Member emails

The tool writes the emails you send members most often: welcoming a new member, helping
someone get into the forum, reminding them to verify their email, and so on. It fills each one in
for the member, but **you send it** from your own mail, so you always read it first
([decision 0004](decisions/0004-welcome-email-copy-page.md)).

## The emails

| Email | For | Command |
|---|---|---|
| **Welcome email** | a newly vetted member: verify their email, join the forum with their personal invite | `aps-tools email welcome` |
| **Forum access help** | someone who already has a forum account but can't get in: their username, the sign-in page, resetting their password | `aps-tools email forum-access` |
| **New forum invite** | someone whose invite expired or was never used: a fresh personal invite | `aps-tools email forum-invite` |
| **Verify your email** | someone who hasn't clicked Cloudflare's verification email, so APS emails can't reach them | `aps-tools email verify-email` |
| **Not getting APS emails** | someone verified who says APS emails aren't arriving (usually spam filters) | `aps-tools email not-getting-emails` |
| **Application in progress** | someone asking about access before their application is complete | `aps-tools email application-in-progress` |
| **Changing your email address** | a member who wants APS to use a different address | `aps-tools email email-change` |

## Set up (once)

```bash
aps-tools setup email
```

It asks for the **link to the forum's Getting Started topics**, creates any templates you don't
have yet from the built-in examples, and can open the folder they're in. Your templates live next
to your settings, one file each (`welcome-email.md`, `forum-access-email.md`, …):

```
~/.config/aps-tools/
```

Edit them in TextEdit whenever you like. The tool reads them fresh each time. They stay on your
Mac, not in the public repository. A template you've deleted is recreated from the example the
next time it's used.

## Sending one

- **In the app:** press **e** wherever a member is open: highlighted on **Members**, in their
  **onboarding**, or in their **check**. The **Email a member** screen lists every email, with the
  one that best fits them first and marked **suggested**:
  - not vetted yet → Application in progress;
  - not on the list or forum → Welcome email;
  - on the list but no forum account → New forum invite;
  - hasn't verified → Verify your email;
  - has a forum account → Forum access help.

  Pick one and press **Enter**. **Esc** takes you back to where you were.
- **In the app, while onboarding:** **w** is a shortcut for the welcome email.
- **From the command line:** `aps-tools email <which> alex@example.com`, e.g.
  `aps-tools email forum-access alex@example.com`.

The email opens in your browser:
1. Click **Copy To**, **Copy subject** and **Copy body**, and paste each into a new email in Mail.
   "Copy body" keeps the bold text, lists and the highlighted box.
2. Everything is filled in: their name, their forum username, the links, and their **personal
   forum invite**. Emails that link to an invite need it to exist first. Onboarding creates it
   when you press **Go**, and the command line offers to create it. Anything the tool still can't
   fill in (a misspelt placeholder, say) is **highlighted in yellow** and listed at the top.
3. A blue note appears if the email doesn't seem to fit. For example: a welcome email for someone
   who already has a forum account, or forum help for someone who doesn't have one.
4. Read it over and send it, then answer **"Did you send it?"** in the tool. A yes is recorded in
   the activity log (`aps-tools log`). The tool can't see your Mail, so it relies on your answer.

## The template format

Plain text, with a few simple rules (they're also at the top of each file):

| You write | You get |
|---|---|
| `# a note` at the start of a line | Left out: notes to yourself |
| `Subject: ...` (first line that isn't a note) | The subject |
| A blank line | A new paragraph |
| Return within a paragraph | A line break (e.g. in the sign-off) |
| `**words**` | **Bold** |
| `- item` lines | A bulleted list |
| `1. item` lines | A numbered list |
| `> ` at the start of lines | A highlighted box (the "short version") |
| `https://...` | A link |
| `{{FIRST_NAME}}` etc. | Filled in for each member |

| Placeholder | Filled in from |
|---|---|
| `{{FIRST_NAME}}` | the membership sheet |
| `{{MEMBER_EMAIL}}` | the address you're writing to |
| `{{NEWS_ADDRESS}}` | the all-members list and domain (`aps-tools setup cloudflare`) |
| `{{FORUM_ADDRESS}}` | the forum's address (`aps-tools setup discourse`) |
| `{{FORUM_LOGIN_LINK}}`, `{{PASSWORD_RESET_LINK}}` | the forum's sign-in and "forgot password" pages |
| `{{FORUM_USERNAME}}` | their account, looked up on the forum |
| `{{FORUM_INVITE_LINK}}` | their personal invite (needs "create invites" in `aps-tools setup discourse`) |
| `{{GETTING_STARTED_LINK}}` | `aps-tools setup email` |
| `{{WEBMASTER_NAME}}` | `aps-tools setup webmaster` |

You can use any placeholder in any template.

## How the page is shown (and privacy)

The page isn't saved as a file. The tool runs a tiny web server on your Mac
(`http://127.0.0.1:…`, reachable only from this computer). It serves the page from memory at a
long random address, and opens it in your default browser. Pages disappear when the tool closes,
so with `aps-tools email …` keep the command running until you've copied the email. Nothing with
a member's details is left on disk ([decision 0015](decisions/0015-member-emails-and-page-server.md)).

## Adding a new kind of email

Editing a template's wording only needs TextEdit. Adding a brand-new kind of email (for example,
how to get access to Elevator, once that process is written up) is a small code change:
1. add a template file in `internal/welcome/`;
2. add a `Kind` to the list in `internal/welcome/welcome.go`;
3. optionally, teach `Suggest` when to suggest it.
