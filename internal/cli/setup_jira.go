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
	"github.com/yippieskippie24/aps-tools/internal/connectors/jira"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// setupJira is the guided setup for Jira Service Management
// (`aps-tools setup jira`). Guide: docs/setup/jira.md.
// Nothing is saved unless the details are tested successfully first.
func setupJira(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	const rerun = "aps-tools setup jira"

	site, email := cfg.Jira.Site, cfg.Jira.Email
	token := &secretAsk{cfg: cfg, key: secrets.JiraAPIToken, label: "Atlassian API token"}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Connect Jira Service Management").
				Description("You'll need your Jira site address, your Atlassian email,\n"+
					"and an Atlassian API token. Step-by-step guide: docs/setup/jira.md\n\n"+
					"Nothing is saved until the details have been tested."),
			huh.NewInput().
				Title("Jira site address").
				Description("e.g. example.atlassian.net — pasting a full Jira link is fine.").
				Placeholder("example.atlassian.net").
				Value(&site).
				Validate(func(s string) error {
					if !strings.Contains(jira.CleanSite(s), ".") {
						return errors.New("enter your site address, like example.atlassian.net")
					}
					return nil
				}),
			huh.NewInput().
				Title("Your Atlassian account email").
				Description("The account you log in to Jira with (it owns the API token).").
				Value(&email).
				Validate(func(s string) error {
					if !strings.Contains(s, "@") {
						return errors.New("enter an email address")
					}
					return nil
				}),
			token.field("Create one at id.atlassian.com/manage-profile/security/api-tokens\n"+
				"→ \"Create API token\" (not \"with scopes\")."),
		),
	).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}

	// 1. Test the details.
	if cfg.Secrets.UsesOnePassword() {
		lipgloss.Fprintln(out, dimStyle.Render("Reading the token from 1Password… (approve the prompt if one appears)"))
	}
	tokenValue, err := token.value()
	if err != nil {
		return failed(out, err.Error(), rerun)
	}
	c := jira.NewForSetup(site, email, tokenValue)
	lipgloss.Fprintln(out, dimStyle.Render("Checking your details with Jira…"))
	me, err := c.WhoAmI(ctx)
	if err != nil {
		return failed(out, err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Connected as "+boldStyle.Render(me.DisplayName))

	// 2. Choose the service desk.
	desks, err := c.ListServiceDesks(ctx)
	if err != nil {
		return failed(out, "Couldn't list service desks: "+err.Error(), rerun)
	}
	if len(desks) == 0 {
		return failed(out, "Your account can't see any service desks. Make sure it's an agent or admin on the APS service desk.", rerun)
	}
	desk := desks[0]
	if len(desks) > 1 {
		opts := make([]huh.Option[int], len(desks))
		chosen := 0
		for i, d := range desks {
			opts[i] = huh.NewOption(fmt.Sprintf("%s (%s)", d.ProjectName, d.ProjectKey), i)
			if d.ID == cfg.Jira.ServiceDeskID {
				chosen = i
			}
		}
		pick := huh.NewForm(huh.NewGroup(
			huh.NewSelect[int]().
				Title("Which service desk are APS members added to?").
				Options(opts...).
				Value(&chosen),
		)).WithTheme(formTheme)
		if err := pick.RunWithContext(ctx); err != nil {
			return cancelled(err)
		}
		desk = desks[chosen]
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Service desk: "+boldStyle.Render(desk.ProjectName))

	// 3. Save — everything has been tested.
	if err := token.save(); err != nil {
		return err
	}
	cfg.Jira = config.Jira{
		Site:            jira.CleanSite(site),
		Email:           strings.TrimSpace(email),
		ServiceDeskID:   desk.ID,
		ServiceDeskName: desk.ProjectName,
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved. Settings: "+path+dimStyle.Render("  ·  "+token.where()))
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, "Try it:  "+boldStyle.Render("aps-tools check someone@example.com"))
	lipgloss.Fprintln(out, dimStyle.Render("Reminder: Atlassian tokens expire (max 1 year). Put a renewal date in your calendar."))
	return nil
}
