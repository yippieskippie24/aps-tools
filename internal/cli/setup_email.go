package cli

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// setupEmail gets the member emails ready (`aps-tools setup email`): it
// asks for the forum's Getting Started link, creates any missing templates
// from the built-in examples, and offers to open the folder they're in.
// Guide: docs/member-emails.md.
func setupEmail(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	link := cfg.Discourse.GettingStartedURL
	elevator := cfg.Discourse.ElevatorAccessURL
	edit := true
	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("The welcome email").
			Description("You send it from your own mail; the tool fills it in for each member\nand gives you a page to copy it from. Your template is a text file\nyou can edit, kept with your settings (not in the public repository)."),
		huh.NewInput().
			Title("Link to the forum's Getting Started topics").
			Description("Open the Getting Started tag (or category) on the forum and copy its\naddress from the browser. Leave blank to fill it in by hand in each email.").
			Placeholder("https://forum.example.org/tag/getting-started").
			Value(&link).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return nil
				}
				if u, err := url.Parse(s); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return errors.New("paste the full address, starting with https://")
				}
				return nil
			}),
		huh.NewInput().
			Title("Link to the forum topic on requesting Elevator access").
			Description("The topic that walks members through getting an Elevator account.\nLeave blank to fill it in by hand in each email.").
			Placeholder("https://forum.example.org/t/elevator-request-access/25").
			Value(&elevator).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return nil
				}
				if u, err := url.Parse(s); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return errors.New("paste the full address, starting with https://")
				}
				return nil
			}),
		huh.NewConfirm().
			Title("Open your templates folder now?").
			Description("Each email has its own file there. Open one in TextEdit to edit it.").
			Affirmative("Yes").
			Negative("Not now").
			Value(&edit),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}
	cfg.Discourse.GettingStartedURL = strings.TrimSpace(link)
	cfg.Discourse.ElevatorAccessURL = strings.TrimSpace(elevator)
	if err := config.Save(cfg); err != nil {
		return err
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved.")

	// Create any templates that don't exist yet, from the built-in examples.
	for _, k := range welcome.Kinds {
		_, created, err := k.LoadTemplate()
		if err != nil {
			return err
		}
		if created {
			path, _ := k.Path()
			lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Created "+k.Name+": "+path)
		}
	}
	dir, _ := config.Dir()
	lipgloss.Fprintln(out, dimStyle.Render("Your templates are in "+dir+" (the files ending -email.md)."))
	if edit {
		if err := exec.Command("open", dir).Run(); err != nil {
			lipgloss.Fprintln(out, warnStyle.Render("! ")+"Couldn't open the folder; open it by hand: "+dir)
		}
	}
	lipgloss.Fprintln(out, "Try one on a member:  "+boldStyle.Render("aps-tools email welcome someone@example.com"))
	return nil
}
