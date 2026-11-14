# APS Webmaster Tool — Design

> **Who this is for:** anyone who wants to understand *what* this tool does and *why* it was
> designed this way. If you just want to use it, start with the [README](../README.md).
> If you are taking over as APS Webmaster, read the [Handover Guide](handover.md) first.

_Last updated: 2026-11-13_

---

## 1. The problem

The APS Webmaster sets up and manages member access across several separate systems.
Doing this by hand leads to predictable mistakes:

- forgetting to add a new member to one of the systems,
- not noticing that a member never finished activating an account,
- slow replies to member questions,
- no easy way to answer "am I set up correctly?" without logging in to every system.

## 2. The goal

One installable terminal app (`aps-tools`) that:

1. **Checks a member** — type an email, get a single report showing their status in every system.
2. **Onboards a new member** — one guided flow that sets them up everywhere, then produces the welcome email.
3. **Keeps the board informed** — writes setup status back to the membership Google Sheet.
4. **Remembers what was done** — a local activity log, so nothing falls through the cracks.

### Hard constraints

| Constraint | What it means for the design |
|---|---|
| **Zero budget** | Only free services and free API tiers. No paid hosting, no paid code signing. |
| **Someone else may take over** | Everything is documented. Setup is guided. No knowledge lives only in one person's head. |
| **Not everyone is technical** | Plain-English messages, a setup wizard, and a `doctor` command that explains how to fix problems. |
| **Public repository** | No secrets, credentials, or member data ever go in the repo. All APS-specific settings live on the user's machine. |

## 3. The systems

| System | What a member needs there | How the tool talks to it | Status |
|---|---|---|---|
| **Membership Google Sheet** | A row with "App vetted" marked (this is the master list) | Google Sheets API via a free service account, shared on the sheet as Editor, read-only scope until Phase 2 ([0009](decisions/0009-google-service-account.md)) | **Check built** ([setup](setup/google-sheet.md)) |
| **Mailing list (Cloudflare)** | Their email is on the list **and** verified with Cloudflare | Cloudflare API (Email Routing + Worker), read-only token | **Check built**: verification and list membership; list check confirmed on a test account (November 2026) ([setup](setup/cloudflare.md)) |
| **Jira Service Management** (free plan) | They are a customer on the APS service desk | Jira Cloud REST API with an API token | **Check built** ([setup](setup/jira.md)) |
| **Discourse** (hosted by Discourse) | An active account (or a pending invite), optionally in a member group | User API key approved in the browser, read-only (admin API keys need a Pro plan — [0008](decisions/0008-discourse-user-api-key.md)) | **Check built and connected** to the real forum ([setup](setup/discourse.md)) |
| **Welcome email** | They receive instructions on what to do next | Generated HTML "copy page"; sent by hand from the webmaster's own mailbox | To build |
| ~~Squarespace website~~ | — | — | **Out of scope.** Being retired Dec 2026; the new site has no user accounts. |

### Notes on each system

**Cloudflare mailing list.** APS email runs on Cloudflare Email Routing. The list address routes
to a Cloudflare Worker that forwards each message to the members. Cloudflare only forwards to
*verified destination addresses*, so every member must click a verification email from Cloudflare
before they receive list mail. This "added but not verified" state is likely the #1 cause of
"I'm not getting emails". Where the member list itself is stored (inside the Worker code, in a
KV store, or elsewhere) is **still to be investigated** once API access is set up.

**Jira.** The free Jira Service Management plan allows 3 *agents* (licensed seats) but unlimited
*customers*. Members are customers, so they never use up a seat. The API token belongs to
the webmaster's own Atlassian account; a new webmaster creates their own.

**Discourse.** Hosted by Discourse on the **Starter** plan; the webmaster is the admin. Starter has
no admin API keys, so the tool uses a user API key approved in the browser
([decision 0008](decisions/0008-discourse-user-api-key.md)). Confirmed with `aps-tools discourse
discover` (November 2026):
- **invite-only**: onboarding means sending a forum invite;
- **private**: people must log in to read;
- **no admin approval** of new accounts;
- **email + password** logins only (no single sign-on);
- invites expire after **90 days**;
- the only custom group is for the board, so there's **no member group** to check.

**Google Sheet.** The master membership list, run by the membership chair. An applicant is added
with **New** marked while their application and background check are in progress. The chair marks
**App vetted** = Y when everything is complete, and only then should the webmaster set them up.
Long-standing members joined before App vetted was used, so members who started before a set date
(and aren't marked New) count as vetted ([decision 0010](decisions/0010-members-before-vetting-began.md)).
The tool reads it as a Google service account
([decision 0009](decisions/0009-google-service-account.md)) and finds columns by header text. It
never shows the address or phone columns. In Phase 2 it will write back setup status so other
board members can see who is fully set up. It will write full details to a **tab it owns**, and a
short status in **one tool-owned column** on the member directory. That status links to the
member's row on the details tab. See the [Phase 2 plan](plans/phase-2-onboarding.md).

**Welcome email.** The webmaster sends from a personal iCloud (custom domain) mailbox, which has no
sending API. Instead of sending, the tool generates a local HTML page with **Copy To**,
**Copy Subject**, and **Copy Body** buttons (the body copies as formatted rich text). After you
send it, the tool asks "Did you send it?" and records the answer in the activity log. The email
content will be written together and stored as an editable template.

## 4. Features and phases

The work is split into phases so that something useful exists early and every phase is safe to
stop at.

### Phase 0 — Foundation ✅ done
- Project skeleton, `aps-tools` command, interactive TUI home screen.
- Config file (`~/.config/aps-tools/config.toml`) and macOS Keychain storage for secrets.
- `aps-tools doctor` to test every connection.
- Release pipeline: GitHub Releases + Homebrew tap, so `brew install` works from day one.
- Documentation structure (this file, handover guide, architecture, decision records).

### Phase 1 — Member check (read-only) 🔨 in progress

Progress: the check engine, `aps-tools check`, the "Check a member" screen, guided setup, and
1Password support are built. **Jira is connected and confirmed with real data.** **Cloudflare**:
the full check is built: verification, all-members list (`LIST_NEWS`), and other lists. So is
`cloudflare discover`, which counts undeliverable list members. It works on the test account
now, and on the APS account after the December 2026 switch-over
([decision 0007](decisions/0007-mailing-lists-in-worker-variables.md)). **Discourse**: the check and `discourse discover` are built and connected to the real forum. **Google Sheet**: the check and `sheet discover` are built. **Every Phase 1 system is now built**; the Cloudflare list check has been confirmed against a test account running the list Worker. What remains is connecting the sheet, which is waiting on the owner's share approval.

- Connectors for Google Sheet, Cloudflare, Jira, Discourse that can **look up** a member.
- `aps-tools check <email>` and a TUI "Check a member" screen.
- Report shows nuanced states, not just yes/no (e.g. *invited but not accepted*,
  *added but not verified*).
- Read-only means zero risk, and it proves every API connection works.

### Phase 2 — Onboarding + welcome email
Detailed plan (draft, for review): [plans/phase-2-onboarding.md](plans/phase-2-onboarding.md).
- "Onboard a new member" flow: preview every step → run → live status per step.
- Every step is **idempotent**: it checks first, only acts if something is missing, and can be
  safely re-run to finish a half-done onboarding.
- Welcome email template (written together) and the HTML copy page.
- Write setup status back to the Google Sheet.
- Local activity log (SQLite).

### Phase 3 — Keeping on top of things
- **Bulk audit:** compare the Google Sheet against every system and list the gaps.
- **Follow-ups:** flag members who were invited/added but never finished activating.
- **Offboarding:** remove a lapsed member everywhere.
- **Board changes:** half the board is elected each July. Update the role mailing lists
  (board-members@, treasurer@, …) and the forum's board group, with a preview. Needed before July
  2027. See [the Phase 2 plan, §5](plans/phase-2-onboarding.md).

### Phase 4 — Reply tracking
- Discourse: flag private messages waiting on a reply (Discourse API).
- iCloud mail: flag member emails waiting on a reply (IMAP with an app-specific password — free).

## 5. How it works (design principles)

1. **Connectors.** Each system is a self-contained "connector" with the same operations
   (test connection, check a member, and later set up / remove a member). Adding, changing, or
   removing a system only touches that system's folder. See [architecture.md](architecture.md).
2. **Check before act.** Every change first checks the current state. Running something twice
   never duplicates or breaks anything.
3. **Preview, then confirm.** Nothing changes in any system without showing you what will happen
   first. A `--dry-run` option is available on commands that make changes.
4. **Manual steps are first-class.** If a system can't be automated, it still appears as a
   checklist step you confirm, so it can't be forgotten.
5. **Two ways to use it.** An interactive TUI (`aps-tools`) for day-to-day work, and plain commands
   (`aps-tools check someone@example.com`) for quick answers.
6. **Secrets stay in the Keychain or 1Password.** API keys and tokens are stored in the macOS
   Keychain or read from the webmaster's 1Password ([decision 0006](decisions/0006-optional-1password.md)), never
   in files. Non-secret settings (URLs, IDs) live in `~/.config/aps-tools/config.toml`.
7. **Plain English.** Error messages say what went wrong *and what to do about it*.

## 6. Open questions

| # | Question | How we'll answer it |
|---|---|---|
| 1 | Where does the Cloudflare Worker keep the list of member addresses? | **Answered (2026-11-13):** there is no list Worker yet. APS email moves to Cloudflare in **December 2026**. Until then Email Routing is off, but members are already verifying their addresses. The list design (tested in a separate account) keeps each list in a Text variable `LIST_<NAME>` on one Worker; see [decision 0007](decisions/0007-mailing-lists-in-worker-variables.md). |
| 2 | How is Discourse sign-up configured (invite-only, approval, groups, SSO)? | **Answered:** invite-only, private, no approval, email logins, 90-day invites, no member group (see "Discourse" above). |
| 3 | What are the Google Sheet's column headers, and which columns should the tool write? | **Headers answered:** start/paid/due dates, Member Status (Active/Inactive), New, App vetted, names, email, address, phones. The membership chair marks "New" while an application is in progress and "App vetted" = Y when complete; that's the onboarding gate. **Still open:** which column(s) Phase 2 should write. |
| 4 | What should the welcome email say? | Write it together in Phase 2. |
| 5 | ~~Which Jira service desk ID / customer setup is used?~~ | **Answered:** `aps-tools setup jira` lists the service desks and saves the chosen one. Confirmed on 2026-11-13 against the real APS service desk: known customers and a known non-customer were all reported correctly. |

## 7. Decisions

Significant decisions are recorded as short "decision records" in [`docs/decisions/`](decisions/),
each explaining the choice, the alternatives, and why. Read these before changing something
fundamental.
