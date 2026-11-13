// Command aps-tools is the APS Webmaster Tool.
//
// This file is deliberately tiny: all the real work starts in internal/cli.
// Running `aps-tools` with no arguments opens the interactive app (TUI);
// `aps-tools --help` lists the plain commands.
package main

import (
	"os"

	"github.com/yippieskippie24/aps-tools/internal/cli"
)

func main() {
	// Execute returns a non-nil error if a command failed. Cobra has already
	// printed the error message, so we only need to set the exit code.
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
