package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/check"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/onboard"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// newOnboardCmd is `aps-tools onboard <email>`: plan, confirm, then set a
// vetted member up. The work is done by internal/onboard.
func newOnboardCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "onboard <email>",
		Short: "Set a newly vetted member up in every APS system",
		Long: `Checks the member on the membership sheet first: if "App vetted" isn't
marked, it stops. Then it checks every system and shows the plan: what's
already done, what the tool will do, and what to do by hand.

Nothing changes until you say Go. With --dry-run it shows the plan and
stops, always. Every change is recorded in the activity log
(aps-tools log). Running it again later only does what's still missing.`,
		Example: "  aps-tools onboard alex@example.com --dry-run\n  aps-tools onboard alex@example.com",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if _, err := check.NormalizeEmail(args[0]); err != nil {
				return err
			}
			cfg, err := config.LoadOrEmpty()
			if err != nil {
				return err
			}
			conns := connectors.All(cfg, secrets.ForConfig(cfg))
			lipgloss.Fprintln(out, dimStyle.Render("Checking every system… (read-only)"))
			plan, err := onboard.Prepare(cmd.Context(), conns, args[0])
			if err != nil {
				return err
			}
			printPlan(out, plan)
			if !plan.Ready {
				return errSilent
			}
			if dryRun {
				lipgloss.Fprintln(out, dimStyle.Render("Dry run: nothing was changed."))
				return nil
			}
			auto := plan.Count(onboard.Auto)
			if auto == 0 {
				if plan.Count(onboard.Manual) > 0 {
					lipgloss.Fprintln(out, dimStyle.Render("The tool has nothing to change itself. Do the □ steps by hand, then check with: aps-tools check "+plan.Person.Email))
				}
				return nil
			}

			// Ask before changing anything.
			if !term.IsTerminal(os.Stdin.Fd()) {
				return errors.New("onboarding asks before changing anything, so run it in a Terminal window (or add --dry-run to just see the plan)")
			}
			goAhead := false
			confirm := huh.NewForm(huh.NewGroup(huh.NewConfirm().
				Title(fmt.Sprintf("Make %s for %s?", changes(auto), plan.Who())).
				Description("Only the steps marked + are done. Each one is recorded in the activity log.").
				Affirmative("Go").
				Negative("Cancel").
				Value(&goAhead))).WithTheme(formTheme)
			if err := confirm.RunWithContext(cmd.Context()); err != nil && !errors.Is(err, huh.ErrUserAborted) {
				return err
			}
			if !goAhead {
				lipgloss.Fprintln(out, dimStyle.Render("Cancelled: nothing was changed."))
				return nil
			}

			log, err := activity.Default()
			if err != nil {
				return err
			}
			lipgloss.Fprintln(out)
			runErr := plan.Run(cmd.Context(), log, func(s onboard.Step) {
				if s.Outcome == onboard.Succeeded {
					lipgloss.Fprintln(out, okStyle.Render("✓ ")+boldStyle.Render(s.System)+"  "+s.Result)
				} else {
					lipgloss.Fprintln(out, failStyle.Render("✗ ")+boldStyle.Render(s.System)+"  "+s.Result)
				}
			})
			if runErr != nil {
				lipgloss.Fprintln(out, warnStyle.Render("! ")+runErr.Error())
			}

			// Check again, so the result is what the systems now say.
			lipgloss.Fprintln(out)
			lipgloss.Fprintln(out, dimStyle.Render("Checking again…"))
			report, err := check.Run(cmd.Context(), conns, plan.Person.Email, nil)
			if err != nil {
				return err
			}
			printReport(out, report)
			lipgloss.Fprintln(out, dimStyle.Render("Recorded in the activity log: "+log.Path()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the plan and stop; change nothing")
	return cmd
}

// kindStyle colours a step's icon.
func kindStyle(k onboard.Kind) lipgloss.Style {
	switch k {
	case onboard.Done:
		return okStyle
	case onboard.Auto:
		return boldStyle.Foreground(lipgloss.Cyan)
	case onboard.Manual, onboard.Waiting, onboard.Blocked:
		return warnStyle
	default:
		return dimStyle
	}
}

// printPlan writes the onboarding plan. Colours are removed automatically
// when the output isn't a terminal.
func printPlan(out io.Writer, p onboard.Plan) {
	lipgloss.Fprintln(out, boldStyle.Render("Onboarding "+p.Who()))
	lipgloss.Fprintln(out)
	if !p.Ready {
		lipgloss.Fprintln(out, failStyle.Render("✗ ")+p.Summary())
		for _, d := range p.Gate.Details {
			lipgloss.Fprintln(out, dimStyle.Render("    "+d))
		}
		if p.Gate.Fix != "" {
			lipgloss.Fprintln(out, "    → "+p.Gate.Fix)
		}
		lipgloss.Fprintln(out, dimStyle.Render("Nothing was changed."))
		return
	}
	width := 0
	for _, s := range p.Steps {
		if n := lipgloss.Width(s.System); n > width {
			width = n
		}
	}
	for _, s := range p.Steps {
		st := kindStyle(s.Kind)
		lipgloss.Fprintln(out, st.Render(s.Kind.Icon())+" "+boldStyle.Render(s.System)+strings.Repeat(" ", width-lipgloss.Width(s.System)+2)+st.Render(s.Kind.String()))
		if s.Now != "" {
			lipgloss.Fprintln(out, dimStyle.Render("    "+s.Now))
		}
		if s.Action != "" {
			lipgloss.Fprintln(out, "    → "+s.Action)
		}
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, boldStyle.Render(p.Summary()))
}

func changes(n int) string {
	if n == 1 {
		return "1 change"
	}
	return fmt.Sprintf("%d changes", n)
}

// newLogCmd is `aps-tools log`: show the end of the activity log.
func newLogCmd() *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show recent changes the tool made (the activity log)",
		Long: `Shows the most recent lines of the activity log: every change the tool
made in an outside system, and every attempt that failed. The full log is a
plain text file you can open in any editor; its location is printed below.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			log, err := activity.Default()
			if err != nil {
				return err
			}
			lines, err := log.Recent(n)
			if err != nil {
				return err
			}
			if len(lines) == 0 {
				lipgloss.Fprintln(out, "No changes recorded yet.")
			}
			for _, l := range lines {
				f := strings.Split(l, "\t")
				if len(f) < 5 {
					lipgloss.Fprintln(out, l)
					continue
				}
				mark := okStyle.Render("✓")
				if f[3] != "ok" {
					mark = failStyle.Render("✗")
				}
				lipgloss.Fprintln(out, dimStyle.Render(f[0])+"  "+mark+" "+f[1]+"  "+boldStyle.Render(f[2])+"  "+strings.Join(f[4:], " "))
			}
			lipgloss.Fprintln(out, dimStyle.Render("Full log: "+log.Path()))
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "lines", "n", 20, "how many recent lines to show")
	return cmd
}
