package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
	"github.com/yippieskippie24/aps-tools/internal/pageserver"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// newEmailCmd groups the member emails the tool can write: one sub-command
// per kind (welcome.Kinds), e.g. `aps-tools email welcome <email>`. Each
// opens the email ready to copy into Mail, then records that it was sent.
func newEmailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "email",
		Short: "Open an email for a member, ready to copy into Mail",
		Long: `Fills in one of your email templates for a member (their first name comes
from the membership sheet, forum details from the forum) and opens it in
your browser with Copy To, Copy subject and Copy body buttons. You send it
from your own mail; the tool never sends email. Afterwards it asks whether
you sent it, and records a yes in the activity log.

Your templates: run "aps-tools setup email" to set them up and open them.`,
	}
	for _, k := range welcome.Kinds {
		cmd.AddCommand(&cobra.Command{
			Use:     k.ID + " <email>",
			Short:   k.Name + ": for " + k.For,
			Example: "  aps-tools email " + k.ID + " alex@example.com",
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runEmail(cmd, k, args[0])
			},
		})
	}
	return cmd
}

// runEmail opens one member's email of kind k and asks whether it was sent.
func runEmail(cmd *cobra.Command, k welcome.Kind, address string) error {
	out := cmd.OutOrStdout()
	email, err := check.NormalizeEmail(address)
	if err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}

	// Their name comes from the sheet; carry on without it if the sheet
	// can't be read (the page then highlights FIRST_NAME).
	person := connectors.Person{Email: email}
	conns := connectors.All(cfg, secrets.ForConfig(cfg))
	for _, c := range conns {
		if d, ok := c.(connectors.Directory); ok && len(d.Missing()) == 0 {
			if p, st, err := d.LookupMember(cmd.Context(), email); err == nil {
				person = p
				if st.State != connectors.StateOK {
					lipgloss.Fprintln(out, warnStyle.Render("! ")+"The membership sheet says: "+st.Summary)
				}
			}
		}
	}

	// The forum link: an email that links to their invite can create it
	// first; the others just need to know about their account.
	var link connectors.ForumLink
	if k.NeedsInvite {
		link, err = inviteLinkFor(cmd, cfg, conns, person)
	} else {
		link, err = welcome.InviteLink(cmd.Context(), conns, email)
		if errors.Is(err, welcome.ErrNoInvite) {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	if k.ID == welcome.VerifyEmail.ID {
		if err := offerResend(cmd, conns, email); err != nil {
			return err
		}
	}
	e, created, err := k.ForMember(cfg, person, link)
	if err != nil {
		return err
	}
	if created {
		path, _ := k.Path()
		lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Created your "+strings.ToLower(k.Name)+" template from the example: "+path)
	}
	who := email
	if n := person.Name(); n != "" {
		who = n + " <" + email + ">"
	}
	url, err := welcome.Open(e, who)
	if err != nil {
		return err
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Opened the "+strings.ToLower(k.Name)+" for "+who+" in your browser.")
	lipgloss.Fprintln(out, dimStyle.Render("  The page is served by this command, so keep it running until you've copied the email: "+url))
	for _, n := range e.Notes {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+n)
	}
	for _, m := range e.Missing {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+fmt.Sprintf("Fill in {{%s}} after pasting: %s", m, welcome.Placeholders[m]))
	}

	if !term.IsTerminal(os.Stdin.Fd()) {
		// No one to ask: give the browser time to load the page, which
		// goes away when this command ends.
		if srv, err := pageserver.Shared(); err == nil {
			srv.WaitFetched(url, 30*time.Second)
		}
		return nil
	}
	sent := false
	ask := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("Did you send it?").
		Description("Copy it into Mail and send it, then answer. A yes is recorded in the activity log.").
		Affirmative("Yes, sent").
		Negative("Not yet").
		Value(&sent))).WithTheme(formTheme)
	if err := ask.RunWithContext(cmd.Context()); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		return err
	}
	if !sent {
		lipgloss.Fprintln(out, dimStyle.Render("Not recorded. Run this again when you've sent it."))
		return nil
	}
	log, err := activity.Default()
	if err != nil {
		return err
	}
	if err := onboard.LogEmailSent(log, email, k.Name); err != nil {
		return err
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Recorded in the activity log.")
	return nil
}

// inviteLinkFor gets the member's forum invite link for the welcome email.
// If they don't have one yet and the tool may create invites, it offers to
// create one (a change on the forum, so it asks and records it). It returns
// "" when there's no link to use, leaving the placeholder highlighted.
func inviteLinkFor(cmd *cobra.Command, cfg *config.Config, conns []connectors.Connector, p connectors.Person) (connectors.ForumLink, error) {
	out := cmd.OutOrStdout()
	link, err := welcome.InviteLink(cmd.Context(), conns, p.Email)
	if !errors.Is(err, welcome.ErrNoInvite) {
		return link, err
	}
	if !cfg.Discourse.CreateInvites {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"They don't have a forum invite yet, and the tool isn't allowed to create invites (run: aps-tools setup discourse).")
		return connectors.ForumLink{}, nil
	}
	var forum connectors.Onboarder
	for _, c := range conns {
		if o, ok := c.(connectors.Onboarder); ok {
			if _, ok := c.(connectors.InviteLinker); ok {
				forum = o
			}
		}
	}
	if forum == nil || !term.IsTerminal(os.Stdin.Fd()) {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"They don't have a forum invite yet. Run: aps-tools onboard "+p.Email)
		return connectors.ForumLink{}, nil
	}
	create := true
	ask := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("They don't have a forum invite yet. Create their personal invite now?").
		Description("The forum won't email them: the link goes in this welcome email. It's recorded in the activity log.").
		Affirmative("Create it").
		Negative("Not now").
		Value(&create))).WithTheme(formTheme)
	if err := ask.RunWithContext(cmd.Context()); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		return connectors.ForumLink{}, err
	}
	if !create {
		return connectors.ForumLink{}, nil
	}
	log, err := activity.Default()
	if err != nil {
		return connectors.ForumLink{}, err
	}
	did, err := forum.Onboard(cmd.Context(), p)
	entry := activity.Entry{Member: p.Email, System: forum.Name(), OK: err == nil, What: did}
	if err != nil {
		entry.What = "Create a personal invite: " + err.Error()
	}
	if logErr := log.Add(entry); logErr != nil {
		return connectors.ForumLink{}, logErr
	}
	if err != nil {
		return connectors.ForumLink{}, err
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+did)
	return welcome.InviteLink(cmd.Context(), conns, p.Email)
}

// offerResend asks whether to have Cloudflare send the member a new
// verification email, when their address is still unverified, and does it
// on a yes (recording it in the activity log). It's a change, so it only
// happens in a terminal, after asking.
func offerResend(cmd *cobra.Command, conns []connectors.Connector, email string) error {
	out := cmd.OutOrStdout()
	if !term.IsTerminal(os.Stdin.Fd()) {
		return nil
	}
	for _, c := range conns {
		r, ok := c.(connectors.VerificationResender)
		if !ok || len(c.Missing()) > 0 {
			continue
		}
		st, err := c.CheckMember(cmd.Context(), email)
		if err != nil || st.State != connectors.StatePending {
			return nil // only for an address that's waiting to be verified
		}
		resend := true
		ask := huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title("Also have Cloudflare send them a new verification email?").
			Description("Their address isn't verified yet. Cloudflare can't re-send by itself, so the tool\n" +
				"removes their unverified address and adds it again, which makes Cloudflare email\n" +
				"them a fresh link. Their mail isn't affected. It's recorded in the activity log.").
			Affirmative("Yes, re-send it").
			Negative("No, just the email").
			Value(&resend))).WithTheme(formTheme)
		if err := ask.RunWithContext(cmd.Context()); err != nil && !errors.Is(err, huh.ErrUserAborted) {
			return err
		}
		if !resend {
			return nil
		}
		log, err := activity.Default()
		if err != nil {
			return err
		}
		did, err := r.ResendVerification(cmd.Context(), email)
		entry := activity.Entry{Member: email, System: c.Name(), OK: err == nil, What: did}
		if err != nil {
			entry.What = "Re-send verification email: " + err.Error()
		}
		if lerr := log.Add(entry); lerr != nil {
			return lerr
		}
		if err != nil {
			lipgloss.Fprintln(out, failStyle.Render("✗ ")+"Couldn't re-send the verification email: "+err.Error())
			return nil
		}
		lipgloss.Fprintln(out, okStyle.Render("✓ ")+did)
		return nil
	}
	return nil
}
