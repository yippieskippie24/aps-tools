# aps-tools — APS Webmaster Tool

A terminal app that helps the APS Webmaster set up and check member access across every APS
system (membership sheet, mailing list, Jira service desk, Discourse) from one place.

> **Status:** Phase 1 (read-only member checks across all four systems) is complete. Phase 2
> (guided onboarding) is under way. Today the tool:
> - plans each onboarding, gated on the membership sheet;
> - creates each new member's personal forum invite;
> - writes the emails you send members;
> - can re-send Cloudflare's verification email.
>
> Mailing-list and Jira setup and writing status back to the sheet are next. See the
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
aps-tools email welcome alex@example.com  # open an email, ready to copy into Mail
aps-tools email --help             # list every kind of member email
aps-tools log                      # see what the tool has changed
aps-tools setup                    # connect APS systems (guided)
aps-tools doctor                   # check that everything is connected
aps-tools --help                   # list every command
```

Inside the app: arrow keys to move (`j`/`k` too, except where you're typing), `Enter` to choose,
`Esc` to go back, and `?` for help. The keys for each screen are listed along the bottom. The app
fills your Terminal window and scrolls long screens. Nothing changes in any system unless you
confirm it, and every change goes in the activity log. Start with **1 Members**: everyone on
the membership sheet, grouped by what they still need (←/→ switches group, `Enter` onboards the
highlighted member, `e` emails them). To look up just one person, use **2 Check a member**: it
lists everyone on the sheet straight away (only the sheet is read), sorted by name (←/→ sorts by
last name or email). Type part of a name to narrow it, ↑/↓ to highlight someone, and Enter to
check them. **3 Onboard a new member** plans and does a member's setup (`g` to go, `w` for their
welcome email). **4 Activity log** shows every change the tool has made.

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
