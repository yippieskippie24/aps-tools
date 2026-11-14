# aps-tools — APS Webmaster Tool

A terminal app that helps the APS Webmaster set up and check member access across every APS
system (membership sheet, mailing list, Jira service desk, Discourse) from one place.

> **Status:** Phase 1 (read-only member checks across all four systems) is complete. Phase 2
> (guided onboarding) is under way. Today the tool:
> - shows everyone on the membership sheet, grouped by what they still need;
> - plans each onboarding, gated on the membership sheet (with a recorded override when you know
>   better);
> - adds new members in Cloudflare (destination address) and Jira (customer), and creates their
>   personal forum invite;
> - follows up on many members at once: re-sends Cloudflare's verification email, onboards
>   whoever you tick, and helps you email them;
> - writes the emails you send members, ready to copy into Mail.
>
> Adding members to the all-members list (`LIST_NEWS`) and writing status back to the sheet are
> next. See the
> [roadmap](docs/design.md#4-features-and-phases).

## Install

```bash
brew install yippieskippie24/tap/aps-tools
```

Update later with:

```bash
brew update && brew upgrade aps-tools
```

<details>
<summary>Alternative: install with Go</summary>

```bash
go install github.com/yippieskippie24/aps-tools/cmd/aps-tools@latest
```
</details>

## Quick start

```bash
aps-tools                          # open the interactive app
aps-tools check alex@example.com   # check a member in every system
aps-tools onboard alex@example.com # plan (and do) a new member's setup
aps-tools followups                # members only part-way set up, and what to do
aps-tools email welcome alex@example.com  # open an email, ready to copy into Mail
aps-tools email --help             # list every kind of member email
aps-tools log                      # see what the tool has changed
aps-tools setup                    # connect APS systems (guided)
aps-tools doctor                   # check that everything is connected
aps-tools --help                   # list every command
```

### Inside the app

Arrow keys move (`j`/`k` too, except where you're typing), **Enter** chooses, **Esc** goes back,
and **?** shows help. The keys for each screen are listed along the bottom. The app fills your
Terminal window and scrolls long screens. Whenever it's working, the bottom line says what it's
doing and which step it's on. **Nothing changes in any system unless you confirm it**, and every
change goes in the activity log.

Whatever screen a member is on, **Enter** opens the same popup of everything you can do for them:
- onboard them (or override the sheet, with a reason);
- email them;
- re-send Cloudflare's verification email;
- check every system;
- see their activity.

Confirmations and choices appear in popups too, and the result shows on the bottom line.

| Menu | What it's for |
|---|---|
| **1 Members** | Everyone on the membership sheet, grouped by what they still need (←/→ switches group). The list is on the left; the highlighted member's details and suggested next step are on the right. |
| **2 Follow-ups** | Members only part-way set up: haven't verified their email, need setting up (usually the forum), or haven't used their forum invite. Pick one, tick who to include with **space** (nobody is ticked to start), then **Enter**. The tool works through them one at a time, then helps you email them: one email with everyone in Bcc when it has nothing personal in it (Verify your email), otherwise one by one. |
| **3 Check a member** | Find anyone on the sheet straight away (only the sheet is read). Type part of a name, or paste an email, and press Enter for their popup. Type a full email address to act on someone who isn't on the sheet. |
| **4 Onboard a new member** | Opens Members on **Needs setup**, where newly vetted members appear. Enter on one → **Onboard** shows their plan; **Make N changes now** sets them up, then offers their welcome email. Someone not on the sheet yet? In Check a member, type their email and choose **Override the sheet and onboard…** with a reason. |
| **5 Activity log** | Every change the tool has made, newest first. |
| **6 Test connections** | Make sure the tool can reach every system. |

## Documentation

| If you want to… | Read |
|---|---|
| Take over as APS Webmaster | [Handover Guide](docs/handover.md) |
| Connect a system (Jira, 1Password, …) | [Setup guides](docs/setup/README.md) |
| Set up or edit the member emails | [Member emails](docs/member-emails.md) |
| Onboard a member by hand (or understand the process) | [How APS member onboarding works](docs/onboarding-process.md) |
| Understand what the tool does and why | [Design](docs/design.md) |
| Change the code | [Architecture](docs/architecture.md) · [Development Guide](docs/development.md) |
| Know why a decision was made | [Decision records](docs/decisions/) |

## Privacy

This repository is public and contains **no** credentials, member data, or APS-specific account
details. Those live only on the webmaster's Mac:
- settings in `~/.config/aps-tools/config.toml`;
- email templates (`*-email.md`) next to the settings;
- the activity log (`activity.log`), also next to the settings, readable only by you;
- API keys in the macOS Keychain or the webmaster's 1Password.

Email pages are served from memory by the tool's own local web server, never saved to disk.

## License

[MIT](LICENSE)
