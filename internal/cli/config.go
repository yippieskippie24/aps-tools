package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/config"
)

// newConfigCmd groups commands for the settings file (~/.config/aps-tools/config.toml).
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Create or find your settings file",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print where your settings file is kept",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), p)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create a new settings file with explanations of every setting",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.WriteTemplate()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Created your settings file:")
			fmt.Fprintln(out, "  "+p)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Next: open it in a text editor, fill in your details, then run: aps-tools doctor")
			fmt.Fprintln(out, "  (tip: open -e "+p+")")
			return nil
		},
	})
	return cmd
}
