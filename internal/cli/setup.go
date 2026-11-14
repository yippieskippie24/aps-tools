package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/clipboard"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// This file holds the parts of `aps-tools setup` shared by every system,
// including how API keys are asked for (Keychain or 1Password). Each
// system's own setup steps live in setup_<system>.go.

// newSetupCmd groups the guided setup for each outside system.
func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Connect the tool to an APS system (guided)",
		Long: `Walks you through connecting one APS system: it asks for the details it
needs, tests them, and only then saves them. API keys are kept in the macOS
Keychain or in 1Password (choose with "aps-tools setup secrets"); everything
else goes into your settings file.

Run "aps-tools setup" on its own to see what's connected.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showSetupStatus(cmd.OutOrStdout())
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "webmaster",
			Short: "Enter your name and reply-to email (used in welcome emails)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setupWebmaster(cmd.Context(), cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "email",
			Short: "Set up the welcome email: Getting Started link and your template",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setupEmail(cmd.Context(), cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "secrets",
			Short: "Choose where API keys are kept: macOS Keychain or 1Password",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setupSecrets(cmd.Context(), cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "cloudflare",
			Short: "Connect Cloudflare (APS email and the mailing list)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setupCloudflare(cmd.Context(), cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "discourse",
			Short: "Connect the Discourse forum",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setupDiscourse(cmd.Context(), cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "sheet",
			Short: "Connect the membership Google Sheet",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setupSheet(cmd.Context(), cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "jira",
			Short: "Connect Jira Service Management",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setupJira(cmd.Context(), cmd.OutOrStdout())
			},
		},
	)
	return cmd
}

// Shared output styles for setup screens.
var (
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Green).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Yellow).Bold(true)
	failStyle = lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true)
	boldStyle = lipgloss.NewStyle().Bold(true)
	dimStyle  = lipgloss.NewStyle().Faint(true)
)

// storeLabel names the key store in plain English.
func storeLabel(cfg *config.Config) string {
	if cfg.Secrets.UsesOnePassword() {
		return "1Password"
	}
	return "macOS Keychain"
}

// showSetupStatus lists each system and whether it's connected.
func showSetupStatus(out io.Writer) error {
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	lipgloss.Fprintln(out, boldStyle.Render("API keys are kept in: ")+storeLabel(cfg)+dimStyle.Render("  (change: aps-tools setup secrets)"))
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, boldStyle.Render("APS systems"))
	for _, c := range connectors.All(cfg, secrets.ForConfig(cfg)) {
		if len(c.Missing()) == 0 {
			lipgloss.Fprintln(out, okStyle.Render("  ✓ ")+c.Name()+dimStyle.Render("  connected — re-run: aps-tools setup "+c.ID()))
		} else {
			lipgloss.Fprintln(out, warnStyle.Render("  ! ")+c.Name()+dimStyle.Render("  not connected — run: aps-tools setup "+c.ID()))
		}
	}
	for _, name := range connectors.NotYetBuilt() {
		lipgloss.Fprintln(out, dimStyle.Render("  · "+name+"  (coming soon)"))
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, boldStyle.Render("Welcome email"))
	if path, err := welcome.Welcome.Path(); err == nil {
		if _, err := os.Stat(path); err == nil && cfg.Discourse.GettingStartedURL != "" {
			lipgloss.Fprintln(out, okStyle.Render("  ✓ ")+"Template and Getting Started link set"+dimStyle.Render("  — edit: aps-tools setup email"))
		} else {
			lipgloss.Fprintln(out, warnStyle.Render("  ! ")+"Not set up yet"+dimStyle.Render("  — run: aps-tools setup email"))
		}
	}
	return nil
}

// requireTerminal stops setup from hanging when run without a keyboard
// (e.g. from a script), since it needs to ask questions.
func requireTerminal() error {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return errors.New("setup asks questions, so it must be run in a Terminal window")
	}
	return nil
}

// formTheme matches the app's look.
var formTheme = huh.ThemeFunc(huh.ThemeCharm)

// setupSecrets lets the webmaster choose between the Keychain and 1Password.
func setupSecrets(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	choice := config.StoreKeychain
	if cfg.Secrets.UsesOnePassword() {
		choice = config.StoreOnePassword
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Where should the tool find your API keys?").
			Description("Either way, keys never go in the settings file. Guide: docs/setup/1password.md").
			Options(
				huh.NewOption("1Password — keys stay in your 1Password vault (approve with Touch ID)", config.StoreOnePassword),
				huh.NewOption("macOS Keychain — built in, nothing to install", config.StoreKeychain),
			).
			Value(&choice),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}

	if choice == config.StoreOnePassword {
		lipgloss.Fprintln(out, dimStyle.Render("Checking 1Password… (approve the prompt if one appears)"))
		if err := secrets.CheckOnePassword(); err != nil {
			lipgloss.Fprintln(out, failStyle.Render("✗ ")+err.Error())
			lipgloss.Fprintln(out, dimStyle.Render("Nothing was changed. See docs/setup/1password.md, then run: aps-tools setup secrets"))
			return errSilent
		}
		lipgloss.Fprintln(out, okStyle.Render("✓ ")+"1Password is ready.")
	}

	cfg.Secrets.Store = choice
	if err := config.Save(cfg); err != nil {
		return err
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"API keys will be read from the "+storeLabel(cfg)+".")
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, "Next, connect each system (it will ask where its key is):  "+boldStyle.Render("aps-tools setup jira"))
	if choice == config.StoreOnePassword {
		lipgloss.Fprintln(out, dimStyle.Render("Any keys already in the Keychain are left alone, but no longer used."))
	}
	return nil
}

// secretAsk asks for one API key in whichever way the chosen store needs:
//   - Keychain: the key itself (hidden as you type), saved to the Keychain.
//   - 1Password: the key's secret reference (op://...), saved to settings.
type secretAsk struct {
	cfg   *config.Config
	key   string // e.g. secrets.JiraAPIToken
	label string // e.g. "Atlassian API token"

	typed      string // what the webmaster entered
	haveStored bool   // a key is already in the Keychain (Keychain mode)
}

// refField returns where the 1Password reference for a key is kept in the
// settings. Add a case here when a new connector needs a key.
func refField(cfg *config.Config, key string) *string {
	switch key {
	case secrets.JiraAPIToken:
		return &cfg.Secrets.JiraAPITokenRef
	case secrets.CloudflareAPIToken:
		return &cfg.Secrets.CloudflareAPITokenRef
	case secrets.DiscourseAPIKey:
		return &cfg.Secrets.DiscourseAPIKeyRef
	case secrets.GoogleServiceAccountKey:
		return &cfg.Secrets.GoogleServiceAccountKeyRef
	}
	panic("refField: no settings field for secret " + key)
}

// field builds the form question. howToGet explains where to create the key.
func (s *secretAsk) field(howToGet string) huh.Field {
	if s.cfg.Secrets.UsesOnePassword() {
		s.typed = *refField(s.cfg, s.key)
		return huh.NewInput().
			Title(s.label + " — 1Password secret reference").
			Description(howToGet + "\nSave it in 1Password, then in the item click the arrow next to the field\n→ \"Copy Secret Reference\", and paste it here (it starts with op://).").
			Placeholder("op://Vault/Item/field").
			Value(&s.typed).
			Validate(func(v string) error {
				if !secrets.LooksLikeReference(v) {
					return errors.New("paste a secret reference — it looks like op://Vault/Item/field")
				}
				return nil
			})
	}

	_, err := secrets.Keychain().Get(s.key)
	s.haveStored = err == nil
	desc, placeholder := howToGet+"\nIt's hidden as you type or paste.", "paste the "+s.label
	if s.haveStored {
		desc = "One is already saved in your Keychain. Leave this blank to keep it,\nor paste a new one to replace it."
		placeholder = "leave blank to keep the saved one"
	}
	return huh.NewInput().
		Title(s.label).
		Description(desc).
		Placeholder(placeholder).
		EchoMode(huh.EchoModePassword).
		Value(&s.typed).
		Validate(func(v string) error {
			if strings.TrimSpace(v) == "" && !s.haveStored {
				return errors.New("paste the " + s.label)
			}
			return nil
		})
}

// value returns the actual key so setup can test it before saving.
func (s *secretAsk) value() (string, error) {
	typed := strings.TrimSpace(s.typed)
	if s.cfg.Secrets.UsesOnePassword() {
		return secrets.ReadReference(secrets.CleanReference(typed))
	}
	if typed == "" {
		return secrets.Keychain().Get(s.key)
	}
	return typed, nil
}

// save stores the key (Keychain) or its reference (1Password, via settings,
// which the caller then saves). Call only after the key has been tested.
func (s *secretAsk) save() error {
	typed := strings.TrimSpace(s.typed)
	if s.cfg.Secrets.UsesOnePassword() {
		*refField(s.cfg, s.key) = secrets.CleanReference(typed)
		return nil
	}
	if typed == "" {
		return nil // keeping the key that's already saved
	}
	return secrets.Keychain().Set(s.key, typed)
}

// where describes where the key ended up, for the final message.
func (s *secretAsk) where() string {
	if s.cfg.Secrets.UsesOnePassword() {
		return "key: 1Password (" + secrets.CleanReference(s.typed) + ")"
	}
	return "key: Keychain (aps-tools)"
}

// generatedSecret is a key the tool obtained itself (approved in the
// browser, or read from a downloaded file) and must now store.
type generatedSecret struct {
	key      string // secret name, e.g. secrets.DiscourseAPIKey
	value    string
	itemName string // suggested 1Password item name
	warning  string // optional reminder shown under "on your clipboard"
	rerun    string // command to run again if it fails
	// explain says why a value read back from 1Password isn't value,
	// without showing either; "" means they match.
	explain func(stored, want string) string
}

// saveGeneratedSecret stores a key the tool obtained itself. The Keychain
// takes it directly. 1Password is never written to by the tool
// (docs/decisions/0006), so the key goes on the clipboard for a moment, the
// webmaster saves it in 1Password and pastes back its secret reference, and
// the tool reads it back to make sure it matches. Returns where it went.
func saveGeneratedSecret(ctx context.Context, out io.Writer, cfg *config.Config, g generatedSecret) (string, error) {
	if !cfg.Secrets.UsesOnePassword() {
		if err := secrets.Keychain().Set(g.key, g.value); err != nil {
			return "", err
		}
		return "key: Keychain (aps-tools)", nil
	}

	if err := clipboard.Copy(g.value); err != nil {
		return "", errors.New("couldn't copy the key to the clipboard: " + err.Error())
	}
	defer clipboard.Clear()
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, boldStyle.Render("Save the key in 1Password"))
	lipgloss.Fprintln(out, "  The key is now "+boldStyle.Render("on your clipboard")+fmt.Sprintf(" (%d characters).", len(g.value)))
	if g.warning != "" {
		lipgloss.Fprintln(out, dimStyle.Render("  "+g.warning+" Don't copy anything else until step 2."))
	}
	lipgloss.Fprintln(out, "  1. In 1Password, create a new "+boldStyle.Render("API Credential")+" item, e.g. \""+g.itemName+"\".")
	lipgloss.Fprintln(out, "     (Updating an item from an earlier try? Paste over its old value.)")
	lipgloss.Fprintln(out, "  2. Paste into its "+boldStyle.Render("credential")+" field and save.")
	lipgloss.Fprintln(out, "  3. Click the arrow next to that field → "+boldStyle.Render("Copy Secret Reference")+", and paste it below.")
	lipgloss.Fprintln(out)

	ref := *refField(cfg, g.key)
	for attempt := 1; ; attempt++ {
		form := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("1Password secret reference").
				Placeholder("op://Vault/Item/credential").
				Value(&ref).
				Validate(func(v string) error {
					if !secrets.LooksLikeReference(v) {
						return errors.New("paste a secret reference — it looks like op://Vault/Item/field")
					}
					return nil
				}),
		)).WithTheme(formTheme)
		if err := form.RunWithContext(ctx); err != nil {
			return "", cancelled(err)
		}
		ref = secrets.CleanReference(ref)
		lipgloss.Fprintln(out, dimStyle.Render("Reading it back from 1Password… (approve the prompt if one appears)"))
		got, err := secrets.ReadReference(ref)
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = g.explain(got, g.value)
		}
		if msg == "" {
			break
		}
		if attempt == 3 {
			return "", errors.New(msg + ". Nothing was saved — run: " + g.rerun)
		}
		_ = clipboard.Copy(g.value)
		lipgloss.Fprintln(out, warnStyle.Render("! ")+msg+".")
		lipgloss.Fprintln(out, dimStyle.Render("  The key is on your clipboard again. Fix the item in 1Password, then paste the reference again."))
	}
	*refField(cfg, g.key) = ref
	return "key: 1Password (" + ref + ")", nil
}

// errSilent signals failure after the message has already been printed.
var errSilent = errors.New("")

// cancelled turns "user pressed ctrl+c / esc" into a friendly message.
func cancelled(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return errors.New("setup cancelled — nothing was saved")
	}
	return err
}

// failed prints a setup failure and the promise that nothing was saved.
func failed(out io.Writer, msg, rerun string) error {
	lipgloss.Fprintln(out, failStyle.Render("✗ ")+msg)
	lipgloss.Fprintln(out, dimStyle.Render("Nothing was saved. Fix the problem above and run: "+rerun))
	return errSilent
}
