package cli

import (
	"context"
	"errors"
	"io"
	"net/mail"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/config"
)

// setupWebmaster asks for the webmaster's own name and email
// (`aps-tools setup webmaster`). They sign the welcome email and tell
// members where to reply. Nothing secret, so no testing is needed.
func setupWebmaster(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	name, email := cfg.Webmaster.Name, cfg.Webmaster.Email
	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("About you, the webmaster").
			Description("Used to sign the welcome email and tell members where to reply.\nA new webmaster changes these when they take over."),
		huh.NewInput().
			Title("Your name").
			Description("As it should appear when signing emails.").
			Placeholder("Alex Example").
			Value(&name).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("enter your name")
				}
				return nil
			}),
		huh.NewInput().
			Title("Your reply-to email address").
			Description("Where members should send questions.").
			Placeholder("webmaster@example.org").
			Value(&email).
			Validate(func(s string) error {
				if _, err := mail.ParseAddress(strings.TrimSpace(s)); err != nil || !strings.Contains(s, "@") {
					return errors.New("enter an email address, like webmaster@example.org")
				}
				return nil
			}),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}
	cfg.Webmaster.Name = strings.TrimSpace(name)
	cfg.Webmaster.Email = strings.TrimSpace(email)
	if err := config.Save(cfg); err != nil {
		return err
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved: "+boldStyle.Render(cfg.Webmaster.Name)+" <"+cfg.Webmaster.Email+">")
	return nil
}
