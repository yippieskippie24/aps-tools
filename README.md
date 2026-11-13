# aps-tools — APS Webmaster Tool

A terminal app that helps the APS Webmaster set up and check member access across every APS
system (membership sheet, mailing list, Jira service desk, Discourse) from one place.

> **Status:** early development (Phase 0 — foundation). See the [roadmap](docs/design.md#4-features-and-phases).

## Install

```bash
brew install yippieskippie24/tap/aps-tools
```

Update later with `brew upgrade aps-tools`.

<details>
<summary>Alternative: install with Go</summary>

```bash
go install github.com/yippieskippie24/aps-tools/cmd/aps-tools@latest
```
</details>

## Quick start

```bash
aps-tools                # open the interactive app
aps-tools doctor         # check that everything is connected
aps-tools config init    # create your settings file
aps-tools --help         # list every command
```

Inside the app: arrow keys (or `j`/`k`) to move, `Enter` to choose, `?` for help, `q` to go back or quit.

## Documentation

| If you want to… | Read |
|---|---|
| Take over as APS Webmaster | [Handover Guide](docs/handover.md) |
| Onboard a member by hand (or understand the process) | [How APS member onboarding works](docs/onboarding-process.md) |
| Understand what the tool does and why | [Design](docs/design.md) |
| Change the code | [Architecture](docs/architecture.md) · [Development Guide](docs/development.md) |
| Know why a decision was made | [Decision records](docs/decisions/) |

## Privacy

This repository is public and contains **no** credentials, member data, or APS-specific account
details. Those live only on the webmaster's Mac — settings in `~/.config/aps-tools/config.toml`, keys in
the macOS Keychain.

## License

[MIT](LICENSE)
