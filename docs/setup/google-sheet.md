# Setup: the membership Google Sheet

Connects the tool to the membership Google Sheet, the list of who is an APS member, so the
member check can say whether someone is on it and whether their application is **vetted**.

**What the tool can do with the sheet right now:** read it. Writing "setup complete" back to the
sheet comes in Phase 2.

## How the tool decides

The membership chair runs the application process:
1. A new applicant is added to the sheet with **New** marked.
2. Once their application and background check are complete, the chair marks **App vetted** = **Y**.

Only then should the webmaster set them up anywhere else. So the check says:

| Result | Meaning |
|---|---|
| ✓ **Set up** | On the sheet, and **App vetted** is marked. |
| … **Waiting on member** | On the sheet, but not vetted yet (usually still marked **New**). **Don't set them up yet.** |
| ✗ **Not set up** | Their email isn't on the sheet. |
| ? **Couldn't check** | The sheet couldn't be read. Run `aps-tools doctor`. |

The check also shows their name and row number, plus Member Status (with a note if it isn't
"Active"), start date, paid and due dates (with a note if the due date has passed). It notes when
the same email is on more than one row. It **never** shows addresses or phone numbers.

**How columns are found:** by their header text, so moving or slightly renaming columns is fine:

| The tool looks for a header containing… | Used as |
|---|---|
| "mail" | email (a cell may hold more than one address) |
| "first name" / "last name" | name |
| "status" | Member Status ("Active" counts as active) |
| "vetted" | App vetted (Y, Yes, X or a ticked checkbox count as yes) |
| exactly "New" | New |
| "paid", "paid date", "due", "start" | shown for information |

`aps-tools sheet discover` shows which columns were found.

## Why a "service account"?

Google doesn't let a program use your password. Instead, you create a **service account**: a
robot Google account with its own email address (like
`aps-tools-reader@your-project.iam.gserviceaccount.com`) and a key file. You **share the sheet
with the robot as a Viewer**, just as you'd share it with a person. The tool signs in as the robot,
so it can read that one sheet and nothing else in anyone's Google Drive.

It's **free**: a Google Cloud project with the Sheets API needs no billing account. Background:
[decision 0009](../decisions/0009-google-service-account.md).

## 1. Create the service account (one time, about 10 minutes)

Use any Google account you control; it doesn't have to own the sheet. The APS Google account
you open the sheet with is a good choice.

1. Go to **console.cloud.google.com** and sign in. If it's your first time, accept the terms.
   It doesn't need a credit card. Ignore any "free trial" offers.
2. **Create a project:** click the project picker at the top → **New project** → name it
   `aps-tools` → **Create**. Make sure the new project is selected at the top.
3. **Turn on the Sheets API:** type **Google Sheets API** in the search bar at the top → open it
   → **Enable**.
4. **Create the robot:** menu ☰ → **IAM & Admin** → **Service Accounts** → **+ Create service
   account**.
   - **Name:** `aps-tools-reader`
   - **Create and continue**
   - Skip **Permissions** and **Principals with access**: it needs no roles.
   - **Done**
5. **Make its key:** click the new service account → **Keys** tab → **Add key** → **Create new
   key** → **JSON** → **Create**. A `.json` file downloads to your Downloads folder.

> **Treat that file like a password.** Anyone with it can read the membership sheet, including
> addresses and phone numbers. Setup saves it in the Keychain or 1Password and then offers to delete
> the downloaded copy.

**"Service account key creation is disabled"?** Your Google account belongs to an organization
(Google Workspace or Cloud Identity) with a security policy that blocks keys. Two ways round it:
- **If you're its administrator:** **IAM & Admin** → **Organization Policies** → **Disable service
  account key creation** → **Manage policy** → **Override parent's policy** → **Off** for this
  project.
- **Otherwise:** create the project with a personal Google account instead. The robot is shared on
  the sheet either way.

### Using 1Password? You can keep the key file itself there

Instead of letting setup squeeze the key onto your clipboard (step 5 below), you can store the
downloaded file in 1Password first:

1. In 1Password, create an item (e.g. an **API Credential** called "APS Google Sheet key"). Attach
   the `.json` file: **Edit** → **Add More** → **File** (or drag the file onto the item). Save.
2. Its secret reference is `op://<vault>/<item name>/<file name>`. For example:
   `op://Private/APS Google Sheet key/aps-tools-1a2b3c.json`.
3. In setup, give that reference where it asks for the key file.
4. Delete the downloaded copy from Downloads yourself.

Attach the file to an item; don't store it as a separate **Document**. 1Password's command-line
tool documents reading attachments by file name, but not Document items. Either way, setup reads
the key back and stops if it isn't a working key.

## 2. Run the setup

```bash
aps-tools setup sheet
```

1. Paste the **link to the sheet** from your browser's address bar.
2. Give the **key file**. Setup suggests the newest key in Downloads. You can also drag the file
   into the Terminal window. With 1Password, you can instead give the reference of the key file
   attached in 1Password (see above).
3. Setup tries to read the sheet. The first time, it can't, because the sheet isn't shared yet. It
   then puts the robot's email on your clipboard and asks you to:
   - open the sheet → **Share**;
   - paste the robot's email and set it to **Viewer**;
   - untick **Notify people**, then **Share**.

   Then choose **Yes, I've shared it**. If you can't share the sheet, ask its owner to.
4. Pick the **tab** that lists the members. The sheet has several tabs. Setup suggests the newest
   one with "member" in its name, e.g. "Member Directory May 2026". Setup then shows which column
   it found for each thing.
5. It **saves** the key:
   - **Keychain:** saved automatically.
   - **1Password**, if you gave a file reference in step 2: nothing more to do.
   - **1Password**, if you gave a downloaded file: the key goes on your clipboard as one line. Create an **API Credential** item
     (e.g. "APS Google Sheet key (aps-tools)"), paste it into the **credential** field and save.
     Then **Copy Secret Reference** on that field and paste it into the Terminal. The tool reads it
     back to check it.
6. It offers to **delete the downloaded key file**. Say yes.

**Tell the membership chair (and the sheet's owner)** that `aps-tools-reader@…` in the sharing list
is the webmaster tool, read-only. Otherwise someone tidying the sharing list may remove it.

## 3. See what the tool understands

```bash
aps-tools sheet discover
```

Shows the tabs, which column is used for what, and counts:
- rows;
- Active / Inactive;
- vetted / not yet;
- due dates passed.

It also notes duplicate emails and rows without an email. No names, emails, addresses or phone
numbers are shown.

## When a new member tab is made

The member list's tab is named with a date. If a newer copy appears, e.g. "Member Directory Nov
2026" next to "Member Directory May 2026", the tool **keeps reading the tab you chose**, but every
check and `sheet discover` adds a note naming the newer tab. If it's the new current list, run
`aps-tools setup sheet` again. The newer tab is suggested, and nothing else needs re-entering.

If the chosen tab is **renamed or deleted**, checks say "Couldn't check" and ask you to re-run setup.

## Turning it off

- **Quick:** remove the robot from the sheet's **Share** list.
- **Completely:** in Google Cloud, open the service account → **Keys** → delete the key (or
  delete the whole `aps-tools` project).

## Phase 2: writing back

To record "setup complete" on the sheet, the robot will need to be changed from **Viewer** to
**Editor** in the sheet's Share list. No new key is needed.

## What's stored where

| Setting | Stored in |
|---|---|
| Sheet ID and name, the tab, the robot's email | Settings file, `[google_sheet]` section |
| The service account key | Keychain (`aps-tools`), or 1Password, in which case only its `op://` reference is in the settings file |

Technical reference: the connector is in `internal/connectors/sheet/`:
- `google.go`: Google sign-in;
- `client.go`: Sheets API;
- `columns.go`: finding columns.

The tool signs in with the `spreadsheets.readonly` scope, and every Sheets call is a read-only `GET`.
