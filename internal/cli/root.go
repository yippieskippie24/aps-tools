// Package cli defines the `aps-tools` commands (doctor, config, version, ...).
//
// Each command lives in its own file. Running `aps-tools` with no command opens the
// interactive app (internal/tui). Commands contain no business logic; they
// call shared packages and print the results.
package cli

import (
	"errors"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	// Registers every APS system connector (see internal/connectors/all).
	_ "github.com/yippieskippie24/aps-tools/internal/connectors/all"
	"github.com/yippieskippie24/aps-tools/internal/doctor"
	"github.com/yippieskippie24/aps-tools/internal/tui"
	"github.com/yippieskippie24/aps-tools/internal/version"
)

// newRootCmd builds the top-level `aps-tools` command and attaches every
// sub-command. To add a command, write a newXxxCmd function in its own file
// and add it to AddCommand below.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "aps-tools",
		Short: "The APS Webmaster Tool",
		Long: `The APS Webmaster Tool helps you set up and check member access across
every APS system from one place.

Run "aps-tools" on its own to open the interactive app.
New webmaster? Start with docs/handover.md in the GitHub repository.`,
		Version: version.String(),
		// Don't print the usage text after every error; it buries the
		// message. Errors are printed by Execute, below.
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(doctor.DefaultDeps())
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		newCheckCmd(),
		newSetupCmd(),
		newDoctorCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)
	return root
}

// Execute runs the command line. It is called from cmd/aps-tools/main.go.
func Execute() error {
	err := newRootCmd().Execute()
	// Some commands print their own detailed failure message and return a
	// marker error; don't repeat it.
	if err != nil && !errors.Is(err, errSilent) && !errors.Is(err, errUnhealthy) {
		lipgloss.Fprintln(os.Stderr, failStyle.Render("Error: ")+err.Error())
	}
	return err
}
