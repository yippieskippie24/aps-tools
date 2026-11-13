package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors/cloudflare"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// newCloudflareCmd groups Cloudflare-specific tools.
func newCloudflareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloudflare",
		Short: "Cloudflare tools (APS email and the mailing list)",
	}
	var saveCode string
	discover := &cobra.Command{
		Use:   "discover",
		Short: "Show how APS email and the mailing list are set up (read-only)",
		Long: `Looks at APS email in Cloudflare and explains how it's wired up: which
addresses go where, how many members have verified their address, and which
Worker runs the mailing list and where it keeps its member list.

Read-only: nothing is changed. Member email addresses are never shown —
they're replaced with "[email hidden]".

Add --save-code <folder> to also save the Worker's code (with member emails
hidden) so it can be read or shared with whoever maintains this tool.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscover(cmd, saveCode)
		},
	}
	discover.Flags().StringVar(&saveCode, "save-code", "", "folder to save the Worker's code into (member emails hidden)")
	cmd.AddCommand(discover)
	return cmd
}

func runDiscover(cmd *cobra.Command, saveCode string) error {
	out := cmd.OutOrStdout()
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	if cfg.Cloudflare.ZoneID == "" {
		return errors.New("Cloudflare isn't connected yet — run: aps-tools setup cloudflare")
	}
	token, err := secrets.ForConfig(cfg).Get(secrets.CloudflareAPIToken)
	if err != nil {
		return fmt.Errorf("couldn't get the Cloudflare API token: %w (run: aps-tools setup cloudflare)", err)
	}

	lipgloss.Fprintln(out, dimStyle.Render("Looking at "+cfg.Cloudflare.ZoneName+" in Cloudflare… (read-only)"))
	d, err := cloudflare.NewClient(token).Discover(cmd.Context(),
		cfg.Cloudflare.AccountID, cfg.Cloudflare.AccountName, cfg.Cloudflare.ZoneID, cfg.Cloudflare.ZoneName)
	if err != nil {
		return err
	}
	printDiscovery(out, d, cfg.Cloudflare.MembersListName())

	if saveCode != "" {
		for _, w := range d.Workers {
			if w.RedactedCode == "" {
				continue
			}
			path := filepath.Join(saveCode, "cloudflare-worker-"+w.Name+".js")
			if err := os.WriteFile(path, []byte(w.RedactedCode), 0o600); err != nil {
				return fmt.Errorf("couldn't save the Worker's code: %w", err)
			}
			lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved Worker code (member emails hidden): "+path)
		}
	}
	return nil
}

func printDiscovery(out io.Writer, d cloudflare.Discovery, membersList string) {
	h := func(s string) { lipgloss.Fprintln(out, "\n"+boldStyle.Render(s)) }
	line := func(s string) { lipgloss.Fprintln(out, "  "+s) }

	lipgloss.Fprintln(out, boldStyle.Render("Cloudflare email: "+d.Zone)+dimStyle.Render("  (account: "+d.Account+")"))
	status := okStyle.Render("on")
	if !d.Routing.Enabled {
		status = failStyle.Render("OFF")
	}
	line("Email Routing: " + status + dimStyle.Render("  ("+d.Routing.Status+")"))

	h("Where mail to each address goes")
	// Line the arrows up after the longest address.
	width := len(d.CatchAll.Address)
	for _, r := range d.Rules {
		if len(r.Address) > width {
			width = len(r.Address)
		}
	}
	for _, r := range d.Rules {
		state := ""
		if !r.Enabled {
			state = warnStyle.Render("  (disabled)")
		}
		line(fmt.Sprintf("%-*s → %s%s", width, r.Address, r.Action, state))
	}
	if d.CatchAll.Action != "" {
		line(fmt.Sprintf("%-*s → %s", width, d.CatchAll.Address, d.CatchAll.Action))
	}

	h("Destination addresses (people Cloudflare may forward to)")
	pending := d.AddressesTotal - d.AddressesVerified
	line(fmt.Sprintf("%d total — %s verified, %s waiting for the person to verify",
		d.AddressesTotal, okStyle.Render(fmt.Sprint(d.AddressesVerified)), warnStyle.Render(fmt.Sprint(pending))))

	if len(d.Lists) > 0 {
		h("Mailing lists")
		width := 0
		for _, l := range d.Lists {
			if len(l.Address) > width {
				width = len(l.Address)
			}
		}
		for _, l := range d.Lists {
			var status string
			switch {
			case l.Secret:
				status = failStyle.Render("can't read (" + l.Variable + " is a Secret)")
			case !l.Defined:
				status = failStyle.Render("no " + l.Variable + " variable")
			default:
				status = fmt.Sprintf("%d member(s)", l.Members)
				if l.NotDestination > 0 {
					status += failStyle.Render(fmt.Sprintf(", %d not a destination", l.NotDestination))
				}
				if l.Unverified > 0 {
					status += warnStyle.Render(fmt.Sprintf(", %d unverified", l.Unverified))
				}
				if l.NotDestination == 0 && l.Unverified == 0 && l.Members > 0 {
					status += okStyle.Render(" ✓")
				}
			}
			tag := ""
			if strings.HasPrefix(l.Address, strings.ToLower(membersList)+"@") {
				tag = boldStyle.Render("  ← all members")
			}
			if !l.Enabled {
				tag += warnStyle.Render("  (rule disabled)")
			}
			line(fmt.Sprintf("%-*s  %s%s", width, l.Address, status, tag))
		}
	}

	for _, w := range d.Workers {
		h(fmt.Sprintf("Worker %q", w.Name))
		if w.Err != "" {
			line(failStyle.Render("✗ ") + w.Err)
		}
		if len(w.Bindings) > 0 {
			line("Connected storage and settings:")
			for _, b := range w.Bindings {
				detail := ""
				if b.Detail != "" {
					detail = dimStyle.Render(" — " + b.Detail)
				}
				line(fmt.Sprintf("  %-16s %s%s", b.Name, b.Kind, detail))
			}
		}
		for _, m := range w.Modules {
			line(fmt.Sprintf("Code file %s: %d bytes, %d member email address(es) written in it", m.Name, m.Bytes, m.EmailCount))
		}
		if len(w.EnvUsed) > 0 {
			line("Code uses: env." + strings.Join(w.EnvUsed, ", env."))
		}
		if w.Forwards {
			line("Code forwards messages (message.forward).")
		}
	}

	if len(d.Notes) > 0 {
		h("What this means")
		for _, n := range d.Notes {
			line("• " + n)
		}
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, dimStyle.Render("Nothing was changed. Member email addresses are hidden."))
}
