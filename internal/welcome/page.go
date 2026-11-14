package welcome

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"slices"
	"sort"
	"strings"

	"github.com/yippieskippie24/aps-tools/internal/browser"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/pageserver"
)

//go:embed page.html
var pageHTML string

var pageTmpl = template.Must(template.New("page").Parse(pageHTML))

// pageData is what the copy page shows.
type pageData struct {
	Title   string
	Notes   []string
	Who     string
	Email   Email
	Body    template.HTML // the email's own HTML, already escaped by Render
	Missing []missingValue
}

type missingValue struct{ Name, What string }

// Page renders the copy page for one email. who is the member's name and
// address, for the page title.
func Page(e Email, who string) ([]byte, error) {
	title := e.Kind.Name
	if title == "" {
		title = Welcome.Name
	}
	d := pageData{Title: title, Who: who, Email: e, Body: template.HTML(e.HTML), Notes: e.Notes}
	for _, m := range e.Missing {
		what, ok := Placeholders[m]
		if !ok {
			what = "not something the tool knows about: check the spelling in your template"
		}
		d.Missing = append(d.Missing, missingValue{Name: m, What: what})
	}
	sort.Slice(d.Missing, func(i, j int) bool { return d.Missing[i].Name < d.Missing[j].Name })
	var b bytes.Buffer
	if err := pageTmpl.Execute(&b, d); err != nil {
		return nil, fmt.Errorf("couldn't build the welcome email page: %w", err)
	}
	return b.Bytes(), nil
}

// ForMember builds one member's email of kind k from the webmaster's
// template (creating it from the example the first time), their sheet
// entry, and what the forum says about them (from InviteLink). created
// reports that the template was just created.
func (k Kind) ForMember(cfg *config.Config, p connectors.Person, link connectors.ForumLink) (e Email, created bool, err error) {
	tmpl, created, err := k.LoadTemplate()
	if err != nil {
		return Email{}, false, err
	}
	values := Values(cfg, p)
	if link.URL != "" {
		values["FORUM_INVITE_LINK"] = link.URL
	}
	if link.Username != "" {
		values["FORUM_USERNAME"] = link.Username
	}
	e, err = Render(tmpl, p.Email, values)
	if err != nil {
		return e, created, err
	}
	e.Kind = k
	switch {
	case k.NeedsInvite && link.HasAccount:
		e.Notes = append(e.Notes, "They already have a forum account, so no invite was made: the invitation link in this email is just the forum's address. If they're an existing member who can't get in, send "+ForumAccess.Name+" instead (f on the Members screen).")
	case k.ID == ForumAccess.ID && !link.HasAccount:
		e.Notes = append(e.Notes, "The forum says they don't have an account yet, so this email doesn't fit: onboard them and send the "+strings.ToLower(Welcome.Name)+" instead.")
	}
	return e, created, err
}

// ErrNoInvite means the member has neither a forum account nor a pending
// invite, so there's no link for their welcome email yet.
var ErrNoInvite = errors.New("they don't have a forum invite yet")

// Personal are the placeholders that are different for every member and
// can't be shared, e.g. a single-use forum invite link. An email whose
// template uses any of them must go to each member separately.
var Personal = []string{"FORUM_INVITE_LINK", "FORUM_USERNAME", "MEMBER_EMAIL"}

// GroupGreeting replaces {{FIRST_NAME}} in an email to several members,
// e.g. "Hello everyone,".
const GroupGreeting = "everyone"

// ErrNotGroupable means an email has something personal in it, so it
// can't go to several members at once.
var ErrNotGroupable = errors.New("this email has something personal in it")

// PersonalIn lists the personal placeholders (see Personal) that a
// template uses.
func PersonalIn(tmpl string) []string {
	var lines []string
	for _, l := range strings.Split(tmpl, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") { // skip notes to the webmaster
			lines = append(lines, l)
		}
	}
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(strings.Join(lines, "\n"), -1) {
		if slices.Contains(Personal, m[1]) && !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// ForGroup builds ONE email of kind k for several members, to send with
// everyone in Bcc (so nobody sees anyone else's address) and the webmaster
// in To. It works only for an email with nothing personal in it (see
// Personal): for anything else it returns an error wrapping
// ErrNotGroupable, and each member needs their own email. {{FIRST_NAME}}
// becomes "everyone".
func (k Kind) ForGroup(cfg *config.Config, people []connectors.Person) (e Email, created bool, err error) {
	tmpl, created, err := k.LoadTemplate()
	if err != nil {
		return Email{}, false, err
	}
	if personal := PersonalIn(tmpl); len(personal) > 0 {
		return Email{}, created, fmt.Errorf("%w ({{%s}}), so it has to go to each member separately", ErrNotGroupable, strings.Join(personal, "}}, {{"))
	}
	values := Values(cfg, connectors.Person{})
	values["FIRST_NAME"] = GroupGreeting
	e, err = Render(tmpl, cfg.Webmaster.Email, values)
	if err != nil {
		return e, created, err
	}
	e.Kind = k
	for _, p := range people {
		e.Bcc = append(e.Bcc, p.Email)
	}
	note := fmt.Sprintf("One email to %d members. Put the addresses in Bcc, not To, so they don't see each other's addresses (in Mail: View → Bcc Address Field).", len(people))
	if e.To == "" {
		note += " Put your own address in To (run aps-tools setup webmaster so the tool fills it in)."
	}
	e.Notes = append(e.Notes, note)
	return e, created, nil
}

// InviteLink asks the forum connector (if there is one among conns) for the
// link to put in the member's welcome email. It returns ErrNoInvite when
// there isn't one yet, and "" with no error when there's no forum connected.
func InviteLink(ctx context.Context, conns []connectors.Connector, email string) (connectors.ForumLink, error) {
	for _, c := range conns {
		l, ok := c.(connectors.InviteLinker)
		if !ok || len(c.Missing()) > 0 {
			continue
		}
		link, err := l.InviteLink(ctx, email)
		if err != nil {
			return connectors.ForumLink{}, err
		}
		if link.URL == "" {
			return link, ErrNoInvite
		}
		return link, nil
	}
	return connectors.ForumLink{}, nil
}

// Open shows the copy page for e in the default browser, served from the
// tool's own page server (see internal/pageserver), so nothing is saved to
// disk. It returns the page's address, so it can be shown if the browser
// doesn't open. The page stays available while the tool is running.
func Open(e Email, who string) (string, error) {
	page, err := Page(e, who)
	if err != nil {
		return "", err
	}
	srv, err := pageserver.Shared()
	if err != nil {
		return "", err
	}
	url := srv.Add(page)
	if err := browser.Open(url); err != nil {
		return url, fmt.Errorf("couldn't open your browser; open this address yourself while the tool is running: %s", url)
	}
	return url, nil
}
