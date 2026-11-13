// Package cli defines the `aps-tools` commands (doctor, config, version, ...).
//
// Each command lives in its own file. Running `aps-tools` with no command opens the
// interactive app (internal/tui). Commands contain no business logic; they
// call shared packages and print the results.
package cli

import (
	"github.com/spf13/cobra"

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
		// Don't print the usage text after every error; it buries the message.
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(doctor.DefaultDeps())
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		newDoctorCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)
	return root
}

// Execute runs the command line. It is called from cmd/aps-tools/main.go.
func Execute() error {
	return newRootCmd().Execute()
}
