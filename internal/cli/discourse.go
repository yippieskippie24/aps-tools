package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors/discourse"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// newDiscourseCmd groups Discourse-specific tools.
func newDiscourseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discourse",
		Short: "Discourse forum tools",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "discover",
		Short: "Show how sign-up works on the forum (read-only)",
		Long: `Summarises the forum settings that shape how members join — invite-only or
open, admin approval, login methods — plus its groups and your outstanding
invites. Read-only: nothing is changed. No member names or emails are shown.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.LoadOrEmpty()
			if err != nil {
				return err
			}
			if cfg.Discourse.URL == "" {
				return errors.New("the forum isn't connected yet — run: aps-tools setup discourse")
			}
			key, err := secrets.ForConfig(cfg).Get(secrets.DiscourseAPIKey)
			if err != nil {
				return fmt.Errorf("couldn't get the Discourse key: %w (run: aps-tools setup discourse)", err)
			}
			lipgloss.Fprintln(out, dimStyle.Render("Looking at "+cfg.Discourse.URL+"… (read-only)"))
			d, err := discourse.ClientFor(cfg.Discourse, key).Discover(cmd.Context())
			if err != nil {
				return err
			}
			printDiscourseDiscovery(out, d, cfg.Discourse.MemberGroup)
			return nil
		},
	})
	return cmd
}

func printDiscourseDiscovery(out io.Writer, d discourse.Discovery, memberGroup string) {
	h := func(s string) { lipgloss.Fprintln(out, "\n"+boldStyle.Render(s)) }
	line := func(s string) { lipgloss.Fprintln(out, "  "+s) }

	lipgloss.Fprintln(out, boldStyle.Render("Discourse forum: "+d.Site)+dimStyle.Render("  (checked as "+d.Admin+")"))

	if d.SettingsErr == "" {
		h("How people join")
		width := 0
		for _, s := range d.Settings {
			if len(s.Label) > width {
				width = len(s.Label)
			}
		}
		for _, s := range d.Settings {
			v := s.Value
			switch v {
			case "true":
				v = okStyle.Render("yes")
			case "false":
				v = dimStyle.Render("no")
			case "":
				v = dimStyle.Render("—")
			}
			line(fmt.Sprintf("%-*s  %s", width, s.Label, v))
		}
		if len(d.LoginMethods) > 0 {
			line("Login methods: " + strings.Join(d.LoginMethods, ", "))
		}
	}

	h("Groups (not counting built-in ones)")
	if len(d.Groups) == 0 {
		line(dimStyle.Render("none"))
	}
	for _, g := range d.Groups {
		tag := ""
		if memberGroup != "" && strings.EqualFold(g.Name, memberGroup) {
			tag = boldStyle.Render("  ← every member should be in this")
		}
		line(fmt.Sprintf("%s  %s%s", g.Name, dimStyle.Render(fmt.Sprintf("(%d people)", g.UserCount)), tag))
	}

	h("Your invites")
	line(fmt.Sprintf("%d waiting to be accepted, %d expired", d.PendingInvites, d.ExpiredInvites))

	if len(d.Notes) > 0 {
		h("What this means")
		for _, n := range d.Notes {
			line("• " + n)
		}
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, dimStyle.Render("Nothing was changed. No member names or emails are shown."))
}
