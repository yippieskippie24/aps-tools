package cli

import (
	"context"
	"fmt"
	"io"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors/cloudflare"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// setupCloudflare is the guided setup for Cloudflare Email Routing
// (`aps-tools setup cloudflare`). Guide: docs/setup/cloudflare.md.
// Nothing is saved unless the token is tested successfully first.
func setupCloudflare(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	const rerun = "aps-tools setup cloudflare"

	token := &secretAsk{cfg: cfg, key: secrets.CloudflareAPIToken, label: "Cloudflare API token"}
	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("Connect Cloudflare (APS email and the mailing list)").
			Description("You'll need a READ-ONLY Cloudflare API token. The exact permissions\n"+
				"to tick are in docs/setup/cloudflare.md.\n\n"+
				"Nothing is saved until the token has been tested."),
		token.field("Create one at dash.cloudflare.com → My Profile → API Tokens → Create Token\n→ \"Create Custom Token\" with the read-only permissions in the guide."),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}

	// 1. Test the token.
	if cfg.Secrets.UsesOnePassword() {
		lipgloss.Fprintln(out, dimStyle.Render("Reading the token from 1Password… (approve the prompt if one appears)"))
	}
	tokenValue, err := token.value()
	if err != nil {
		return failed(out, err.Error(), rerun)
	}
	client := cloudflare.NewClient(tokenValue)
	lipgloss.Fprintln(out, dimStyle.Render("Checking the token with Cloudflare…"))
	// Listing domains proves the token works, whichever kind of token it is
	// (user or account), and tells us the account.
	zones, err := client.ListZones(ctx)
	if err != nil {
		return failed(out, "Couldn't list your domains: "+err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Token is valid.")

	// 2. Choose the APS domain (this also tells us the account).
	if len(zones) == 0 {
		return failed(out, "The token can't see any domains. Give it the \"Zone → Zone → Read\" permission for the APS domain.", rerun)
	}
	zone := zones[0]
	if len(zones) > 1 {
		opts := make([]huh.Option[int], len(zones))
		chosen := 0
		for i, z := range zones {
			opts[i] = huh.NewOption(fmt.Sprintf("%s  (account: %s)", z.Name, z.Account.Name), i)
			if z.ID == cfg.Cloudflare.ZoneID {
				chosen = i
			}
		}
		pick := huh.NewForm(huh.NewGroup(
			huh.NewSelect[int]().
				Title("Which domain does APS email use?").
				Options(opts...).
				Value(&chosen),
		)).WithTheme(formTheme)
		if err := pick.RunWithContext(ctx); err != nil {
			return cancelled(err)
		}
		zone = zones[chosen]
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Domain: "+boldStyle.Render(zone.Name)+dimStyle.Render("  (account: "+zone.Account.Name+")"))

	// 3. Make sure the token can read everything the check needs.
	lipgloss.Fprintln(out, dimStyle.Render("Checking the token's permissions…"))
	if _, err := client.GetRoutingSettings(ctx, zone.ID); err != nil {
		return failed(out, "Can't read Email Routing for "+zone.Name+": "+err.Error(), rerun)
	}
	addrs, err := client.ListAddresses(ctx, zone.Account.ID)
	if err != nil {
		return failed(out, "Can't read destination addresses: "+err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+fmt.Sprintf("Can read Email Routing (%d destination addresses).", len(addrs)))

	// 4. Find the mailing lists and ask which one goes to all members.
	// Lists are optional (there are none before the switch-over), so a
	// problem here is a warning, not a failure.
	membersList := cfg.Cloudflare.MembersList
	if zone.ID != cfg.Cloudflare.ZoneID {
		membersList = "" // different domain: don't carry the old choice over
	}
	lists, err := client.LoadLists(ctx, zone.Account.ID, zone.ID)
	switch {
	case err != nil:
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"Couldn't read the mailing lists: "+err.Error())
		lipgloss.Fprintln(out, dimStyle.Render("  List checks won't work until this is fixed (the token needs Workers Scripts: Read)."))
	case len(lists) == 0:
		lipgloss.Fprintln(out, dimStyle.Render("No mailing lists yet (no address is routed to a Worker). The all-members list defaults to news@."))
	default:
		lipgloss.Fprintln(out, okStyle.Render("✓ ")+fmt.Sprintf("Found %d mailing list(s).", len(lists)))
		chosen := config.DefaultMembersList
		if membersList != "" {
			chosen = membersList
		}
		if _, ok := cloudflare.FindList(lists, chosen); !ok {
			chosen = lists[0].Name
		}
		opts := make([]huh.Option[string], len(lists))
		for i, l := range lists {
			opts[i] = huh.NewOption(fmt.Sprintf("%s  (%d addresses)", l.Address, len(l.Members)), l.Name)
		}
		pick := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which list goes to ALL members?").
				Description("The member check looks for each person on this list.").
				Options(opts...).
				Value(&chosen),
		)).WithTheme(formTheme)
		if err := pick.RunWithContext(ctx); err != nil {
			return cancelled(err)
		}
		membersList = chosen
		lipgloss.Fprintln(out, okStyle.Render("✓ ")+"All-members list: "+boldStyle.Render(chosen+"@"))
	}

	// 5. Save.
	if err := token.save(); err != nil {
		return err
	}
	cfg.Cloudflare = config.Cloudflare{
		AccountID: zone.Account.ID, AccountName: zone.Account.Name,
		ZoneID: zone.ID, ZoneName: zone.Name,
		MembersList: membersList,
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved. Settings: "+path+dimStyle.Render("  ·  "+token.where()))
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, "Next, see how the mailing list is wired up:  "+boldStyle.Render("aps-tools cloudflare discover"))
	return nil
}
