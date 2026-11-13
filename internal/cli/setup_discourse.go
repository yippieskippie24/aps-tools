package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/browser"
	"github.com/yippieskippie24/aps-tools/internal/clipboard"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors/discourse"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// setupDiscourse is the guided setup for the Discourse forum
// (`aps-tools setup discourse`). Guide: docs/setup/discourse.md.
// Nothing is saved unless the key is tested successfully first.
//
// There are two ways to get a key (docs/decisions/0008-discourse-user-api-key.md):
//   - "user": the webmaster approves a read-only key in their browser.
//     Works on every Discourse plan, so it's the default.
//   - "admin": paste an admin API key. Only Pro plans and higher have these.
func setupDiscourse(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	const rerun = "aps-tools setup discourse"

	site := cfg.Discourse.URL
	keyType := config.DiscourseUserKey
	if cfg.Discourse.UsesAdminKey() {
		keyType = config.DiscourseAdminKey
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("Connect the Discourse forum").
			Description("The tool gets a READ-ONLY key for your own admin account.\n"+
				"Step-by-step guide: docs/setup/discourse.md\n\n"+
				"Nothing is saved until the key has been tested."),
		huh.NewInput().
			Title("Forum address").
			Description("e.g. forum.example.org — pasting a full forum link is fine.").
			Placeholder("forum.example.org").
			Value(&site).
			Validate(func(s string) error {
				if !strings.Contains(discourse.CleanURL(s), ".") {
					return errors.New("enter the forum's address, like forum.example.org")
				}
				return nil
			}),
		huh.NewSelect[string]().
			Title("How should the tool get its key?").
			Options(
				huh.NewOption("Approve it in my browser — works on every Discourse plan (recommended)", config.DiscourseUserKey),
				huh.NewOption("Paste an admin API key — only Pro plans and higher have these", config.DiscourseAdminKey),
			).
			Value(&keyType),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}

	// 1. Get the key, still only in memory.
	var (
		client   *discourse.Client
		keyValue string
		clientID string
		adminKey *secretAsk // only for admin keys
	)
	if keyType == config.DiscourseUserKey {
		clientID = cfg.Discourse.ClientID
		if clientID == "" {
			clientID = discourse.NewClientID()
		}
		keyValue, err = approveUserKey(ctx, out, site, clientID)
		if err != nil {
			return err
		}
		client = discourse.NewUserKeyClient(site, "", keyValue, clientID)
	} else {
		username := cfg.Discourse.Username
		adminKey = &secretAsk{cfg: cfg, key: secrets.DiscourseAPIKey, label: "Discourse admin API key"}
		form := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Your Discourse username").
				Description("Your own admin account — the API key must be made for this user.").
				Value(&username).
				Validate(func(s string) error {
					if strings.TrimSpace(strings.TrimPrefix(s, "@")) == "" {
						return errors.New("enter your username")
					}
					return nil
				}),
			adminKey.field("Create one on the forum: Admin → Advanced → API keys → New API key\n→ User level: Single user (you), Scope: Read-only."),
		)).WithTheme(formTheme)
		if err := form.RunWithContext(ctx); err != nil {
			return cancelled(err)
		}
		if cfg.Secrets.UsesOnePassword() {
			lipgloss.Fprintln(out, dimStyle.Render("Reading the key from 1Password… (approve the prompt if one appears)"))
		}
		if keyValue, err = adminKey.value(); err != nil {
			return failed(out, err.Error(), rerun)
		}
		client = discourse.NewClient(site, username, keyValue)
	}

	// 2. Test it.
	lipgloss.Fprintln(out, dimStyle.Render("Checking the key with the forum…"))
	me, err := client.Me(ctx)
	if err != nil {
		return failed(out, err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Connected as "+boldStyle.Render("@"+me.Username)+" (admin).")
	if _, _, err := client.FindUserByEmail(ctx, "nobody@example.invalid"); err != nil {
		return failed(out, "The key works, but can't look up members: "+err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Can look up members.")

	// 3. Optionally choose the group every member should be in.
	memberGroup, err := pickMemberGroup(ctx, out, client, cfg.Discourse.MemberGroup)
	if err != nil {
		return err
	}

	// 4. Save — everything has been tested.
	var where string
	if adminKey != nil {
		if err := adminKey.save(); err != nil {
			return err
		}
		where = adminKey.where()
	} else if where, err = saveUserKey(ctx, out, cfg, keyValue); err != nil {
		return err
	}
	cfg.Discourse = config.Discourse{
		URL:         discourse.CleanURL(site),
		Username:    me.Username,
		MemberGroup: memberGroup,
		KeyType:     keyType,
		ClientID:    clientID,
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved. Settings: "+path+dimStyle.Render("  ·  "+where))
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, "Next, see how sign-up works on the forum:  "+boldStyle.Render("aps-tools discourse discover"))
	return nil
}

// approveUserKey sends the webmaster to the forum to approve a read-only
// user API key, then unlocks the text they paste back.
func approveUserKey(ctx context.Context, out io.Writer, site, clientID string) (string, error) {
	req, err := discourse.NewUserKeyRequest(clientID)
	if err != nil {
		return "", err
	}
	link := req.URL(site)

	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, boldStyle.Render("Approve the key on the forum"))
	lipgloss.Fprintln(out, "  1. A forum page opens in your browser. Sign in as yourself if asked.")
	lipgloss.Fprintln(out, "  2. Check it names "+boldStyle.Render("aps-tools")+" and asks only to "+boldStyle.Render("read")+", then click "+boldStyle.Render("Authorize")+".")
	lipgloss.Fprintln(out, "  3. Copy the whole block of text the forum shows, and paste it below.")
	if err := browser.Open(link); err != nil {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"Couldn't open your browser. Open this link yourself:")
		lipgloss.Fprintln(out, link)
	}
	lipgloss.Fprintln(out)

	var pasted, key string
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Text from the forum").
			Description("If the forum shows an error instead, press Esc and see\n" +
				"\"If the forum won't approve the key\" in docs/setup/discourse.md.").
			Placeholder("paste it here").
			CharLimit(20000).
			Value(&pasted).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("paste the text the forum shows after you click Authorize")
				}
				k, err := req.Unlock(s)
				if err != nil {
					return err
				}
				key = k
				return nil
			}),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return "", cancelled(err)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Key received.")
	return key, nil
}

// saveUserKey stores an approved user key. The Keychain takes it directly.
// 1Password is never written to by the tool (docs/decisions/0006), so the
// key goes on the clipboard for a moment and the webmaster saves it there.
func saveUserKey(ctx context.Context, out io.Writer, cfg *config.Config, key string) (string, error) {
	if !cfg.Secrets.UsesOnePassword() {
		if err := secrets.Keychain().Set(secrets.DiscourseAPIKey, key); err != nil {
			return "", err
		}
		return "key: Keychain (aps-tools)", nil
	}

	if err := clipboard.Copy(key); err != nil {
		return "", errors.New("couldn't copy the key to the clipboard: " + err.Error())
	}
	defer clipboard.Clear()
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, boldStyle.Render("Save the key in 1Password"))
	lipgloss.Fprintln(out, "  The key is now "+boldStyle.Render("on your clipboard")+fmt.Sprintf(" (%d characters).", len(key)))
	lipgloss.Fprintln(out, dimStyle.Render("  It is NOT the scrambled text from the forum page. Don't copy anything else until step 2."))
	lipgloss.Fprintln(out, "  1. In 1Password, create a new "+boldStyle.Render("API Credential")+" item, e.g. \"APS Discourse key (aps-tools)\".")
	lipgloss.Fprintln(out, "     (Updating an item from an earlier try? Paste over its old value.)")
	lipgloss.Fprintln(out, "  2. Paste into its "+boldStyle.Render("credential")+" field and save.")
	lipgloss.Fprintln(out, "  3. Click the arrow next to that field → "+boldStyle.Render("Copy Secret Reference")+", and paste it below.")
	lipgloss.Fprintln(out)

	ref := cfg.Secrets.DiscourseAPIKeyRef
	for attempt := 1; ; attempt++ {
		form := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("1Password secret reference").
				Placeholder("op://Vault/Item/credential").
				Value(&ref).
				Validate(func(v string) error {
					if !secrets.LooksLikeReference(v) {
						return errors.New("paste a secret reference — it looks like op://Vault/Item/field")
					}
					return nil
				}),
		)).WithTheme(formTheme)
		if err := form.RunWithContext(ctx); err != nil {
			return "", cancelled(err)
		}
		ref = secrets.CleanReference(ref)
		lipgloss.Fprintln(out, dimStyle.Render("Reading it back from 1Password… (approve the prompt if one appears)"))
		got, err := secrets.ReadReference(ref)
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = discourse.ExplainStoredKey(got, key)
		}
		if msg == "" {
			break
		}
		if attempt == 3 {
			return "", errors.New(msg + ". Nothing was saved — run: aps-tools setup discourse")
		}
		_ = clipboard.Copy(key)
		lipgloss.Fprintln(out, warnStyle.Render("! ")+msg+".")
		lipgloss.Fprintln(out, dimStyle.Render("  The key is on your clipboard again. Fix the item in 1Password, then paste the reference again."))
	}
	cfg.Secrets.DiscourseAPIKeyRef = ref
	return "key: 1Password (" + ref + ")", nil
}

// pickMemberGroup asks which group, if any, every member should be in.
func pickMemberGroup(ctx context.Context, out io.Writer, client *discourse.Client, current string) (string, error) {
	groups, err := client.Groups(ctx)
	if err != nil {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"Couldn't list groups ("+err.Error()+"); skipping the group check.")
		return current, nil
	}
	opts := []huh.Option[string]{huh.NewOption("No — don't check groups", "")}
	for _, g := range groups {
		if !g.Automatic {
			opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%d people)", g.Name, g.UserCount), g.Name))
		}
	}
	if len(opts) == 1 {
		return "", nil
	}
	pick := current
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Should every member be in one of these groups?").
			Description("If you pick one, the member check confirms each person is in it.").
			Options(opts...).
			Value(&pick),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return "", cancelled(err)
	}
	return pick, nil
}
