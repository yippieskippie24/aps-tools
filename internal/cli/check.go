package cli

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/clipboard"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

func newCheckCmd() *cobra.Command {
	var copyReport bool
	cmd := &cobra.Command{
		Use:   "check <email>",
		Short: "Check a member's status in every APS system (read-only)",
		Long: `Looks up one member by email in every connected APS system and reports
whether they're set up correctly. Nothing is changed anywhere.

Add --copy to also copy a plain-text version of the report, ready to paste
into an email.`,
		Example: "  aps-tools check alex@example.com\n  aps-tools check alex@example.com --copy",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadOrEmpty()
			if err != nil {
				return err
			}
			conns := connectors.All(cfg, secrets.ForConfig(cfg))
			report, err := check.Run(cmd.Context(), conns, args[0], connectors.NotYetBuilt())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printReport(out, report)
			if copyReport {
				if err := clipboard.Copy(report.PlainText()); err != nil {
					return fmt.Errorf("couldn't copy the report: %w", err)
				}
				lipgloss.Fprintln(out, dimStyle.Render("Report copied to the clipboard."))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&copyReport, "copy", false, "also copy a plain-text report to the clipboard")
	return cmd
}

// stateStyle colours a state's icon.
func stateStyle(s connectors.State) lipgloss.Style {
	switch s {
	case connectors.StateOK:
		return okStyle
	case connectors.StatePending, connectors.StateUnknown:
		return warnStyle
	default:
		return failStyle
	}
}

// printReport writes a coloured report. Colours are removed automatically
// when the output isn't a terminal.
func printReport(out io.Writer, r check.Report) {
	lipgloss.Fprintln(out, boldStyle.Render("Account check: "+r.Email))
	lipgloss.Fprintln(out)
	for _, res := range r.Results {
		st := stateStyle(res.State)
		lipgloss.Fprintln(out, st.Render(check.Icon(res.State))+" "+boldStyle.Render(res.System)+"  "+st.Render(res.State.String()))
		lipgloss.Fprintln(out, "    "+res.Summary)
		for _, d := range res.Details {
			lipgloss.Fprintln(out, dimStyle.Render("    "+d))
		}
		if res.Fix != "" {
			lipgloss.Fprintln(out, "    → "+res.Fix)
		}
	}
	if len(r.NotYetChecked) > 0 {
		lipgloss.Fprintln(out)
		lipgloss.Fprintln(out, dimStyle.Render("Not checked by the tool yet: "+strings.Join(r.NotYetChecked, ", ")))
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, stateStyle(r.Worst()).Render(r.Summary()))
}
