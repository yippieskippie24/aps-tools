package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors/discourse"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// setupDiscourse is the guided setup for the Discourse forum
// (`aps-tools setup discourse`). Guide: docs/setup/discourse.md.
// Nothing is saved unless the key is tested successfully first.
func setupDiscourse(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	const rerun = "aps-tools setup discourse"

	site, username := cfg.Discourse.URL, cfg.Discourse.Username
	key := &secretAsk{cfg: cfg, key: secrets.DiscourseAPIKey, label: "Discourse API key"}
	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("Connect the Discourse forum").
			Description("You'll need the forum address, your admin username, and a\n"+
				"READ-ONLY API key. Step-by-step guide: docs/setup/discourse.md\n\n"+
				"Nothing is saved until the details have been tested."),
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
		key.field("Create one on the forum: Admin → Advanced → API keys → New API key\n→ User level: Single user (you), Scope: Read-only."),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}

	// 1. Test the details.
	if cfg.Secrets.UsesOnePassword() {
		lipgloss.Fprintln(out, dimStyle.Render("Reading the key from 1Password… (approve the prompt if one appears)"))
	}
	keyValue, err := key.value()
	if err != nil {
		return failed(out, err.Error(), rerun)
	}
	client := discourse.NewClient(site, username, keyValue)
	lipgloss.Fprintln(out, dimStyle.Render("Checking your details with the forum…"))
	me, err := client.Me(ctx)
	if err != nil {
		return failed(out, err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Connected as "+boldStyle.Render("@"+me.Username)+" (admin).")
	if _, _, err := client.FindUserByEmail(ctx, "nobody@example.invalid"); err != nil {
		return failed(out, "The key works, but can't look up users: "+err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Can look up members.")

	// 2. Optionally choose the group every member should be in.
	memberGroup := cfg.Discourse.MemberGroup
	groups, err := client.Groups(ctx)
	if err != nil {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"Couldn't list groups ("+err.Error()+"); skipping the group check.")
	} else {
		opts := []huh.Option[string]{huh.NewOption("No — don't check groups", "")}
		for _, g := range groups {
			if !g.Automatic {
				opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%d people)", g.Name, g.UserCount), g.Name))
			}
		}
		if len(opts) > 1 {
			pick := huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Should every member be in one of these groups?").
					Description("If you pick one, the member check confirms each person is in it.").
					Options(opts...).
					Value(&memberGroup),
			)).WithTheme(formTheme)
			if err := pick.RunWithContext(ctx); err != nil {
				return cancelled(err)
			}
		} else {
			memberGroup = ""
		}
	}

	// 3. Save — everything has been tested.
	if err := key.save(); err != nil {
		return err
	}
	cfg.Discourse = config.Discourse{
		URL:         discourse.CleanURL(site),
		Username:    me.Username,
		MemberGroup: memberGroup,
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved. Settings: "+path+dimStyle.Render("  ·  "+key.where()))
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, "Next, see how sign-up works on the forum:  "+boldStyle.Render("aps-tools discourse discover"))
	return nil
}
