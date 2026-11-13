package cli

import (
	"errors"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/doctor"
)

// errUnhealthy makes `aps-tools doctor` exit with a non-zero code when something is
// broken, which is useful in scripts. The details are already printed.
var errUnhealthy = errors.New("some checks failed")

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check your setup and every connection, and explain how to fix problems",
		Args:  cobra.NoArgs,
		// The failure details are printed by the command itself, so don't let
		// Cobra print "Error: some checks failed" on top of them.
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			results := doctor.Run(cmd.Context(), doctor.DefaultDeps())
			printResults(cmd, results)
			if !doctor.Healthy(results) {
				return errUnhealthy
			}
			return nil
		},
	}
}

// printResults prints doctor results. lipgloss.Fprintln strips colours
// automatically when output isn't a terminal (e.g. piped to a file).
func printResults(cmd *cobra.Command, results []doctor.Result) {
	out := cmd.OutOrStdout()
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Faint(true)

	icons := map[doctor.Status]string{
		doctor.OK:   lipgloss.NewStyle().Foreground(lipgloss.Green).Render("✓"),
		doctor.Info: lipgloss.NewStyle().Foreground(lipgloss.Blue).Render("i"),
		doctor.Warn: lipgloss.NewStyle().Foreground(lipgloss.Yellow).Render("!"),
		doctor.Fail: lipgloss.NewStyle().Foreground(lipgloss.Red).Render("✗"),
	}

	for _, r := range results {
		lipgloss.Fprintln(out, icons[r.Status]+" "+bold.Render(r.Name)+"  "+dim.Render(r.Detail))
		if r.Fix != "" {
			lipgloss.Fprintln(out, "    → "+r.Fix)
		}
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, doctor.Summary(results))
}
