// Package browser opens web pages in the webmaster's default browser, using
// macOS's built-in "open" command.
package browser

import "os/exec"

// Open shows url in the default browser. If it fails, callers print the
// link so it can be opened by hand.
func Open(url string) error {
	return exec.Command("open", url).Run()
}
