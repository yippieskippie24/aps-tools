package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/followup"
	"github.com/yippieskippie24/aps-tools/internal/progress"
	"github.com/yippieskippie24/aps-tools/internal/roster"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// newFollowupsCmd is `aps-tools followups [unverified|setup|invites]`:
// list members who are only part-way set up, and follow up on the chosen
// ones. The work is done by internal/followup.
func newFollowupsCmd() *cobra.Command {
	var dryRun bool
	var skip []string
	cmd := &cobra.Command{
		Use:   "followups [unverified|setup|invites]",
		Short: "Follow up on members who are only part-way set up",
		Long: `On its own, lists every follow-up (read-only):
  unverified  haven't clicked Cloudflare's verification email
  setup       vetted, but missing from a system (usually the forum)
  invites     have a forum invite they haven't used

Name one to act on it. You tick who's included (nobody is ticked to
start; --skip leaves people out of the list), then confirm. "unverified" has Cloudflare
re-send each of them its verification email; "setup" onboards each of them
(checked again first, so only what's missing is done). Every change is in
the activity log. "invites" only lists them: send their emails from the
app's Follow-ups screen, or with aps-tools email.

The app's Follow-ups screen does the same, and then helps you email each
member one by one.`,
		Example:   "  aps-tools followups\n  aps-tools followups unverified --dry-run\n  aps-tools followups setup --skip alex@example.com",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{followup.Unverified.ID(), followup.NeedsSetup.ID(), followup.InviteWaiting.ID()},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			var kind followup.Kind
			if len(args) == 1 {
				k, ok := kindByID(args[0])
				if !ok {
					return fmt.Errorf("there's no follow-up called %q: use unverified, setup or invites", args[0])
				}
				kind = k
			}
			cfg, err := config.LoadOrEmpty()
			if err != nil {
				return err
			}
			conns := connectors.All(cfg, secrets.ForConfig(cfg))
			lipgloss.Fprintln(out, dimStyle.Render("Checking everyone on the sheet in every system… (read-only)"))
			r, err := roster.Load(cmd.Context(), conns)
			if err != nil {
				return err
			}
			lists := followup.Lists(r)
			if len(args) == 0 {
				for _, l := range lists {
					printFollowup(out, l, nil)
				}
				lipgloss.Fprintln(out, dimStyle.Render("To act on one: aps-tools followups unverified (or setup, or invites)."))
				return nil
			}

			l := lists[slices.Index(followup.Kinds, kind)]
			printFollowup(out, l, skip)
			people := chosenPeople(l, skip)
			if len(people) == 0 || kind.Action() == "" {
				if kind == followup.InviteWaiting && len(people) > 0 {
					lipgloss.Fprintln(out, dimStyle.Render("Email each of them their invite link: the app's Follow-ups screen, or aps-tools email forum-invite <their email>."))
				}
				return nil
			}
			if dryRun {
				lipgloss.Fprintln(out, dimStyle.Render("Dry run: nothing was changed."))
				return nil
			}
			if !term.IsTerminal(os.Stdin.Fd()) {
				return errors.New("follow-ups ask before changing anything, so run this in a Terminal window (or add --dry-run to just see the list)")
			}
			people, err = pickPeople(cmd, kind, people)
			if err != nil || len(people) == 0 {
				if err == nil {
					lipgloss.Fprintln(out, dimStyle.Render("Nobody chosen: nothing was changed."))
				}
				return err
			}
			log, err := activity.Default()
			if err != nil {
				return err
			}
			ctx := progress.With(cmd.Context(), func(step string) {
				lipgloss.Fprintln(out, dimStyle.Render("  "+step))
			})
			results := followup.Run(ctx, kind, conns, people, log)
			lipgloss.Fprintln(out)
			for _, res := range results {
				mark := okStyle.Render("✓ ")
				if !res.OK {
					mark = failStyle.Render("✗ ")
				}
				lipgloss.Fprintln(out, mark+personLabel(res.Person)+dimStyle.Render(": "+res.What))
			}
			okN, failed := followup.Count(results)
			lipgloss.Fprintln(out, fmt.Sprintf("\n%d done, %d didn't work. Everything is in the activity log (aps-tools log).", okN, failed))
			if failed > 0 {
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show who would be included, and change nothing")
	cmd.Flags().StringSliceVar(&skip, "skip", nil, "leave these members out (their email addresses, comma-separated)")
	return cmd
}

// kindByID finds a follow-up by its command-line name.
func kindByID(id string) (followup.Kind, bool) {
	for _, k := range followup.Kinds {
		if strings.EqualFold(k.ID(), id) {
			return k, true
		}
	}
	return 0, false
}

// chosenPeople is the follow-up's members, less any skipped.
func chosenPeople(l followup.List, skip []string) []connectors.Person {
	var out []connectors.Person
	for _, e := range l.Members {
		if !slices.ContainsFunc(skip, func(s string) bool { return strings.EqualFold(strings.TrimSpace(s), e.Email) }) {
			out = append(out, e.Person)
		}
	}
	return out
}

// printFollowup prints a follow-up and its members, marking skipped ones.
func printFollowup(out io.Writer, l followup.List, skip []string) {
	lipgloss.Fprintln(out, "\n"+boldStyle.Render(fmt.Sprintf("%s (%d)", l.Kind.Title(), len(l.Members)))+dimStyle.Render("  aps-tools followups "+l.Kind.ID()))
	if len(l.Members) == 0 {
		lipgloss.Fprintln(out, okStyle.Render("  ✓ ")+"Nobody right now.")
		return
	}
	for _, e := range l.Members {
		line := "  " + personLabel(e.Person) + dimStyle.Render(" · "+l.Kind.Detail(e))
		if slices.ContainsFunc(skip, func(s string) bool { return strings.EqualFold(strings.TrimSpace(s), e.Email) }) {
			line = dimStyle.Render("  (skipped) " + personLabel(e.Person))
		}
		lipgloss.Fprintln(out, line)
	}
}

// personLabel is "Alex Example <alex@example.com>", or just the email.
func personLabel(p connectors.Person) string {
	if n := p.Name(); n != "" {
		return n + " <" + p.Email + ">"
	}
	return p.Email
}

// pickPeople lets the webmaster untick anyone, then confirm.
func pickPeople(cmd *cobra.Command, kind followup.Kind, people []connectors.Person) ([]connectors.Person, error) {
	var opts []huh.Option[string]
	byEmail := map[string]connectors.Person{}
	for _, p := range people {
		byEmail[p.Email] = p
		opts = append(opts, huh.NewOption(personLabel(p), p.Email))
	}
	var picked []string
	goAhead := false
	form := huh.NewForm(
		huh.NewGroup(huh.NewMultiSelect[string]().
			Title(kind.Action()+": who's included?").
			Description("Nobody is ticked yet: space ticks someone (ctrl+a ticks everyone); enter when done.").
			Options(opts...).
			Value(&picked)),
		huh.NewGroup(huh.NewConfirm().
			TitleFunc(func() string {
				return fmt.Sprintf("%s for %d %s?", kind.Action(), len(picked), map[bool]string{true: "member", false: "members"}[len(picked) == 1])
			}, &picked).
			Description("Each one is checked again first, and recorded in the activity log.").
			Affirmative("Go").
			Negative("Cancel").
			Value(&goAhead)),
	).WithTheme(formTheme)
	if err := form.RunWithContext(cmd.Context()); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		return nil, err
	}
	if !goAhead {
		return nil, nil
	}
	var out []connectors.Person
	for _, e := range picked {
		out = append(out, byEmail[e])
	}
	return out, nil
}
