// Package clipboard copies text to the Mac clipboard, so reports can be
// pasted into an email. It uses macOS's built-in "pbcopy" command, which
// works in every terminal app (unlike terminal-based clipboard tricks).
package clipboard

import (
	"os/exec"
	"strings"
)

// Copy puts text on the clipboard.
func Copy(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// Clear empties the clipboard, so a key copied for a moment doesn't linger.
func Clear() error { return Copy("") }
