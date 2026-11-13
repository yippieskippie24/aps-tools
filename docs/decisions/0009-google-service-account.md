# 0009 — Read the membership Google Sheet as a service account

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
The membership Google Sheet is the master list of members. The tool needs to read it (Phase 1)
and later write "setup complete" back to it (Phase 2). Constraints:
- zero budget;
- no personal passwords in the tool;
- a successor must be able to take over.

The sheet also holds addresses and phone numbers, so access must be limited to this one sheet.

## Decision
Use a **Google Cloud service account**: a robot Google account in a free Google Cloud project
(Sheets API enabled, no billing account). The sheet is shared with the robot's email, so the
robot can reach that sheet and nothing else.

**Shared as Editor from the start; read-only until Phase 2.** The webmaster can't share the
sheet, so the owner (the APS president) must approve the share request. To ask them **once**,
not once now and again for Phase 2, the robot is shared as **Editor** straight away. What the tool
can actually do is limited separately, by the **scope** it asks Google for when it signs in:
`spreadsheets.readonly` until Phase 2's write-back is built and reviewed. With that scope Google
refuses any change, whatever the share allows. Switching to the `spreadsheets` scope later needs
no new key and no approval from anyone.

- The robot's JSON key is stored like every other key (Keychain or 1Password reference). To stay
  under the Keychain's size limit and paste cleanly into one 1Password field, only the needed
  fields are kept, on one line.
- The tool signs in with Google's server-to-server flow: a signed JWT exchanged for an
  hour-long access token, with the `spreadsheets.readonly` scope. It's written with Go's standard
  library, so there are no new dependencies.
- **Columns are found by header text**, not by position, so the sheet's owners can rearrange it.
- **The member tab is saved by name.** The sheet has several tabs, and the member list's name
  includes a date. Setup suggests the newest "member" tab. Checks add a note when a newer dated
  copy of the saved tab appears, rather than switching silently.
- **The onboarding gate is "App vetted"**, as the membership chair's process sets it. "New" means
  the application is still in progress. Address and phone columns are never read into reports.

## Alternatives considered
- **OAuth sign-in as the webmaster (desktop app flow).** Google's spreadsheet scopes are
  "sensitive". An unverified app's sign-in expires every 7 days, and verification isn't realistic
  for a one-person tool. It would also give the tool access to everything the webmaster's account
  can see.
- **"Publish to web" CSV.** It would make member addresses and phone numbers public.
- **An API key.** Only works for public sheets.
- **An Apps Script web app inside the sheet.** Free and needs no Cloud project, but the code lives
  outside this repo. Its URL plus a shared secret would be a weaker credential, and Google's
  script-deployment screens change often.
- **Google's Go client libraries.** They work, but they bring in many dependencies for two GET
  calls and a token request.

## Consequences
- One-time setup in Google Cloud (about 10 minutes), documented in
  [docs/setup/google-sheet.md](../setup/google-sheet.md).
- **Workspace or Cloud Identity accounts** may have "Disable service account key creation" turned on
  by default. The guide explains the override, or using a personal Google account for the
  project.
- Someone tidying the sheet's sharing list could remove the robot. The guide says to tell the
  membership chair and the sheet's owner what it is.
- **Phase 2 write-back:** a code change only: ask for the `spreadsheets` scope and write to
  columns the tool owns. The Editor share and the key stay as they are.
- Until then, the key could in principle be used by *other* software to edit the sheet, because the
  share allows it. That's one more reason to keep the key only in the Keychain or 1Password and
  delete the download.
- **Handover:** the successor can keep the robot and receive the key, or make their own and
  re-share. Deleting the key in Google Cloud cuts access immediately.
