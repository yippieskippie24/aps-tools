package tui

import (
	"context"
	"errors"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/followup"
	"github.com/yippieskippie24/aps-tools/internal/progress"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

// This file opens a member's email (e.g. their welcome email) in the
// browser, ready to copy into Mail. Choosing which email, and asking "Did
// you send it?", happen in popups (memberactions.go); building the email
// is done by internal/welcome.

// emailOpenedMsg reports that the email was opened (or why not).
type emailOpenedMsg struct {
	kind    welcome.Kind
	person  connectors.Person
	resent  string // what re-sending the verification email did, if asked
	notes   []string
	missing []string
	err     error
}

// resendVerification has the mailing list system send the member a new
// verification email, records the attempt in the activity log, and
// returns a line saying how it went. Nothing is changed if the log can't
// be written.
func resendVerification(ctx context.Context, conns []connectors.Connector, email string, log *activity.Log, logErr error) string {
	if logErr != nil {
		return "✗ Didn't re-send the verification email: the activity log can't be written (" + logErr.Error() + ")."
	}
	did, err := followup.ResendOne(ctx, conns, email, log)
	switch {
	case err != nil && did != "":
		return "! " + err.Error() // done, but not logged
	case err != nil:
		return "✗ Couldn't re-send the verification email: " + err.Error()
	}
	return "✓ " + did
}

// emailWork does the work of opening a member's email: optionally having
// Cloudflare re-send their verification email (the webmaster has said yes),
// reading their name from the sheet, finding their forum invite or
// account, and opening the filled-in email in the browser. Shared by the
// Email screen and the member popup.
func (a App) emailWork(ctx context.Context, k welcome.Kind, p connectors.Person, resend bool, log *activity.Log, logErr error) emailOpenedMsg {
	cfg, conns, err := a.connect(ctx)
	if err != nil {
		return emailOpenedMsg{kind: k, err: err}
	}
	resent := ""
	if resend {
		progress.Report(ctx, "Asking Cloudflare to send a new verification email")
		resent = resendVerification(ctx, conns, p.Email, log, logErr)
	}
	progress.Report(ctx, "Reading their name from the membership sheet")
	for _, c := range conns {
		if d, ok := c.(connectors.Directory); ok && len(c.Missing()) == 0 {
			if found, _, err := d.LookupMember(ctx, p.Email); err == nil && found.Name() != "" {
				p = found
			}
		}
	}
	progress.Report(ctx, "Looking them up on the forum")
	link, err := welcome.InviteLink(ctx, conns, p.Email)
	switch {
	case errors.Is(err, welcome.ErrNoInvite) && k.NeedsInvite:
		return emailOpenedMsg{kind: k, person: p, err: errors.New("they don't have a forum invite yet. Onboard them first (that creates it), then send this")}
	case errors.Is(err, welcome.ErrNoInvite):
		err = nil
	case err != nil:
		return emailOpenedMsg{kind: k, person: p, err: err}
	}
	who := p.Email
	if n := p.Name(); n != "" {
		who = n + " <" + p.Email + ">"
	}
	progress.Report(ctx, "Filling in the template and opening it in your browser")
	email, err := showEmail(k, cfg, p, who, link)
	return emailOpenedMsg{kind: k, person: p, resent: resent, notes: email.Notes, missing: email.Missing, err: err}
}

// showEmail builds a member's email of kind k and opens its copy page in
// the browser. Tests replace it so they don't open a browser.
var showEmail = func(k welcome.Kind, cfg *config.Config, p connectors.Person, who string, link connectors.ForumLink) (welcome.Email, error) {
	e, _, err := k.ForMember(cfg, p, link)
	if err != nil {
		return welcome.Email{}, err
	}
	_, err = welcome.Open(e, who)
	return e, err
}
