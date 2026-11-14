package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors/sheet"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// newSheetCmd groups membership Google Sheet tools.
func newSheetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sheet",
		Short: "Membership Google Sheet tools",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "discover",
		Short: "Show what the tool understands of the membership sheet (read-only)",
		Long: `Shows the sheet's tabs, which column the tool uses for what (email, status,
App vetted, …), and counts of members by status. Read-only: nothing is
changed. No names, emails, addresses or phone numbers are shown.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.LoadOrEmpty()
			if err != nil {
				return err
			}
			if cfg.Sheet.SpreadsheetID == "" {
				return errors.New("the membership sheet isn't connected yet — run: aps-tools setup sheet")
			}
			raw, err := secrets.ForConfig(cfg).Get(secrets.GoogleServiceAccountKey)
			if err != nil {
				return fmt.Errorf("couldn't get the Google key: %w (run: aps-tools setup sheet)", err)
			}
			key, err := sheet.ParseServiceAccountKey(raw)
			if err != nil {
				return fmt.Errorf("the saved Google key can't be used: %w (run: aps-tools setup sheet)", err)
			}
			started, err := sheet.ParseVettingStarted(cfg.Sheet.VettingStarted)
			if err != nil {
				return err
			}
			lipgloss.Fprintln(out, dimStyle.Render("Reading the membership sheet… (read-only)"))
			d, err := sheet.NewClient(key).Discover(cmd.Context(), cfg.Sheet.SpreadsheetID, cfg.Sheet.Tab, started)
			if err != nil {
				return err
			}
			printSheetDiscovery(out, d)
			return nil
		},
	})
	return cmd
}

func printSheetDiscovery(out io.Writer, d sheet.Discovery) {
	h := func(s string) { lipgloss.Fprintln(out, "\n"+boldStyle.Render(s)) }
	line := func(s string) { lipgloss.Fprintln(out, "  "+s) }

	lipgloss.Fprintln(out, boldStyle.Render("Membership sheet: "+d.Title)+dimStyle.Render("  (read as "+d.ServiceAccount+")"))
	var tabs []string
	for _, t := range d.Tabs {
		if t == d.Tab {
			t = boldStyle.Render(t) + dimStyle.Render(" ← members")
		}
		tabs = append(tabs, t)
	}
	line("Tabs: " + strings.Join(tabs, ", "))

	if len(d.Columns) > 0 {
		h(fmt.Sprintf("Columns the tool uses  (headers on row %d)", d.HeaderRow))
		for _, c := range d.Columns {
			if c.Column == "" {
				line(fmt.Sprintf("%-20s %s", c.Role, warnStyle.Render("not found")))
			} else {
				line(fmt.Sprintf("%-20s column %-3s %s", c.Role, c.Column, dimStyle.Render(fmt.Sprintf("%q", c.Header))))
			}
		}
		line(dimStyle.Render("Other columns (address, phone, …) are never read into reports."))

		h("Members")
		line(fmt.Sprintf("%d rows with an email address", d.Rows))
		line(fmt.Sprintf("Member Status: %d Active, %d Inactive, %d other or blank", d.Active, d.Inactive, d.OtherStatus))
		if d.VettingStarted.IsZero() {
			line(fmt.Sprintf("App vetted: %d yes, %d not yet", d.Vetted, d.InProgress))
		} else {
			line(fmt.Sprintf("App vetted: %d yes, %d joined before vetting began (%s), %d not yet",
				d.Vetted, d.Established, sheet.FriendlyDate(d.VettingStarted), d.InProgress))
		}
		line(fmt.Sprintf("Dues date already passed: %d", d.DuePassed))
	}

	if len(d.Notes) > 0 {
		h("What this means")
		for _, n := range d.Notes {
			line("• " + n)
		}
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, dimStyle.Render("Nothing was changed. No names, emails, addresses or phone numbers are shown."))
}
