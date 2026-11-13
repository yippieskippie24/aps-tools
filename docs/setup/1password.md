# Setup: where API keys are kept (Keychain or 1Password)

The tool needs an API key for each APS system. You choose where those keys live:

| Option | Good for | What the tool stores in its settings file |
|---|---|---|
| **macOS Keychain** (default) | Anyone. Nothing to install. | Nothing. Keys go straight into the Keychain. |
| **1Password** | Webmasters who already keep everything in 1Password. | Only a *secret reference* (an `op://…` link), never the key. |

Either way, keys **never** go in the settings file or the GitHub repository. You can switch at
any time with `aps-tools setup secrets`, then re-run each system's setup.

---

## Using 1Password

### 1. Install 1Password's command-line tool (one time)

The tool reads keys through 1Password's free command-line tool, called `op`. In Terminal:

```bash
brew install --cask 1password-cli
```

### 2. Connect it to the 1Password app (one time)

1. Open and unlock the **1Password** app.
2. Go to **Settings → Developer**.
3. Turn on **Integrate with 1Password CLI**.
4. Optional, recommended: turn on **Touch ID** in 1Password's security settings, so you approve
   with a fingerprint.

### 3. Tell the tool to use 1Password

```bash
aps-tools setup secrets
```

Choose **1Password**. The tool checks that `op` is installed and connected. If it isn't, it says
which step above to fix.

### 4. Save each key in 1Password, then give the tool its reference

When a system's setup (e.g. `aps-tools setup jira`) asks for a key:

1. Save the key in 1Password, e.g. as an **API Credential** item named `Jira API token` in the
   vault of your choice.
2. Open the item, click the **arrow (⌄) next to the field** that holds the key, and choose
   **Copy Secret Reference**.
3. Paste that into setup. It looks like `op://Private/Jira API token/credential`.

Setup reads the key through 1Password and tests it before saving anything.

### What to expect day to day

- The first time the tool needs a key, 1Password asks you to approve it (Touch ID or your
  password). The approval lasts for a while, so you won't be asked on every command.
- If you rename or move the item in 1Password, the reference changes. Copy it again and re-run
  that system's setup.
- `aps-tools doctor` shows **1Password ✓** when everything is connected.

### Troubleshooting

| Message | Fix |
|---|---|
| "command-line tool isn't installed" | Step 1. |
| "isn't connected to the app" | Step 2, and make sure 1Password is unlocked. |
| "couldn't find that secret reference" | Copy the reference again from the item (step 4). |
| "access wasn't approved" | Run the command again and approve the 1Password prompt. |

---

## Using the macOS Keychain

Nothing to set up. Each system's setup asks for the key itself (hidden as you type) and saves it in
your login Keychain under the name **aps-tools**. To see or delete keys, open the **Keychain
Access** app and search for `aps-tools`.

---

## For the next webmaster

Keys belong to the person who created them. When you take over, create **your own** keys, store
them your way (Keychain or your own 1Password), and run each setup again. See the
[Handover Guide](../handover.md).
