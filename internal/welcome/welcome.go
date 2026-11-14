// Package welcome turns the webmaster's email templates into ready-to-send
// emails for one member, and into a "copy page": a local web page with Copy
// To / Copy subject / Copy body buttons, for pasting into Mail. The tool
// never sends email itself (decision 0004).
//
// There's one template per kind of email (see Kinds): the welcome email for
// new members, and "forum access help" for members who already have a forum
// account but have asked how to get in. Each is a plain text file next to
// the settings file (e.g. ~/.config/aps-tools/welcome-email.md), so it can
// be edited in TextEdit and stays out of the public repository. The first
// time one is needed, it's created from the example built into the tool
// (the .md files in this folder). The format is explained at the top of
// each.
package welcome

import (
	_ "embed"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// Kind is one kind of email the tool can write.
type Kind struct {
	// ID is its name on the command line, e.g. "welcome".
	ID string
	// Name is how it's shown, e.g. "Welcome email".
	Name string
	// FileName is the template's name in the settings folder.
	FileName string
	// Example is the template the tool starts you off with.
	Example string
	// For says who it's for, for help text.
	For string
	// NeedsInvite means the email links to the member's personal forum
	// invite, which must exist (onboarding creates it) before it's opened.
	NeedsInvite bool
}

var (
	//go:embed welcome-email.md
	welcomeExample string
	//go:embed forum-access-email.md
	forumAccessExample string
	//go:embed forum-invite-email.md
	forumInviteExample string
	//go:embed verify-email-email.md
	verifyEmailExample string
	//go:embed not-getting-emails-email.md
	notGettingEmailsExample string
	//go:embed application-in-progress-email.md
	applicationExample string
	//go:embed email-change-email.md
	emailChangeExample string
)

// The kinds of email.
var (
	// Welcome is for a newly onboarded member: verify your email, join the
	// forum with your personal invite. The draft agreed in the Phase 2 plan.
	Welcome = Kind{ID: "welcome", Name: "Welcome email", FileName: "welcome-email.md", Example: welcomeExample, NeedsInvite: true,
		For: "a new member: verifying their email and joining the forum with their invite"}
	// ForumAccess is for a member who already has a forum account but has
	// asked how to get in: their username, the sign-in page, and how to
	// reset their password.
	ForumAccess = Kind{ID: "forum-access", Name: "Forum access help", FileName: "forum-access-email.md", Example: forumAccessExample,
		For: "a member who already has a forum account: how to sign in and reset their password"}
	// ForumInvite is a fresh personal invite for someone whose invite
	// expired or was never used.
	ForumInvite = Kind{ID: "forum-invite", Name: "New forum invite", FileName: "forum-invite-email.md", Example: forumInviteExample, NeedsInvite: true,
		For: "a member whose forum invite expired or was never used: a fresh personal invite"}
	// VerifyEmail reminds a member to click Cloudflare's verification email.
	VerifyEmail = Kind{ID: "verify-email", Name: "Verify your email", FileName: "verify-email-email.md", Example: verifyEmailExample,
		For: "a member who hasn't clicked Cloudflare's verification email, so APS emails can't reach them"}
	// NotGettingEmails helps a verified member whose APS emails aren't
	// arriving (usually spam filtering).
	NotGettingEmails = Kind{ID: "not-getting-emails", Name: "Not getting APS emails", FileName: "not-getting-emails-email.md", Example: notGettingEmailsExample,
		For: "a member whose address is verified but says APS emails aren't arriving"}
	// ApplicationInProgress answers someone who asks about access before
	// the membership chair has finished their application.
	ApplicationInProgress = Kind{ID: "application-in-progress", Name: "Application in progress", FileName: "application-in-progress-email.md", Example: applicationExample,
		For: "someone asking about access before their application is complete"}
	// EmailChange explains what happens when a member changes their email
	// address.
	EmailChange = Kind{ID: "email-change", Name: "Changing your email address", FileName: "email-change-email.md", Example: emailChangeExample,
		For: "a member who wants APS to use a different email address"}
)

// Kinds lists every kind of email, in the order they're offered.
var Kinds = []Kind{Welcome, ForumAccess, ForumInvite, VerifyEmail, NotGettingEmails, ApplicationInProgress, EmailChange}

// KindByID finds a kind by its command-line name.
func KindByID(id string) (Kind, bool) {
	for _, k := range Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// Example is the welcome email's starting template.
var Example = welcomeExample

// Placeholders the tool knows how to fill in, and what each is, for the
// "fill these in" warning.
var Placeholders = map[string]string{
	"FIRST_NAME":           "their first name (it isn't on the membership sheet)",
	"NEWS_ADDRESS":         "the all-members list address (run: aps-tools setup cloudflare)",
	"FORUM_ADDRESS":        "the forum's address (run: aps-tools setup discourse)",
	"FORUM_INVITE_LINK":    "their forum invite link: onboard them first so the tool creates it, or let the tool create invites (run: aps-tools setup discourse)",
	"FORUM_USERNAME":       "their forum username (they don't have a forum account, or the forum couldn't be checked)",
	"MEMBER_EMAIL":         "their email address",
	"HELP_DESK_NAME":       "the APS help desk's name in Jira (run: aps-tools setup jira)",
	"ELEVATOR_ACCESS_LINK": "the forum topic on requesting Elevator access (run: aps-tools setup email)",
	"FORUM_LOGIN_LINK":     "the forum's sign-in page (run: aps-tools setup discourse)",
	"PASSWORD_RESET_LINK":  "the forum's \"forgot password\" page (run: aps-tools setup discourse)",
	"GETTING_STARTED_LINK": "the forum's Getting Started link (run: aps-tools setup email)",
	"WEBMASTER_NAME":       "your name (run: aps-tools setup webmaster)",
}

// Path is where the webmaster's template for this kind of email lives.
func (k Kind) Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, k.FileName), nil
}

// LoadTemplate reads the webmaster's template for this kind of email,
// creating it from the example the first time. created is true when it was
// just created.
func (k Kind) LoadTemplate() (text string, created bool, err error) {
	path, err := k.Path()
	if err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", false, err
		}
		if err := os.WriteFile(path, []byte(k.Example), 0o600); err != nil {
			return "", false, fmt.Errorf("couldn't create the %s template: %w", strings.ToLower(k.Name), err)
		}
		return k.Example, true, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("couldn't read the %s template: %w", strings.ToLower(k.Name), err)
	}
	return string(b), false, nil
}

// Values are the placeholder values for one member, from their sheet entry
// and the settings. A value that isn't known is left out, so it stays
// highlighted in the email.
func Values(cfg *config.Config, p connectors.Person) map[string]string {
	v := map[string]string{}
	set := func(k, val string) {
		if val = strings.TrimSpace(val); val != "" {
			v[k] = val
		}
	}
	set("FIRST_NAME", p.FirstName)
	set("MEMBER_EMAIL", p.Email)
	if cfg.Cloudflare.ZoneName != "" {
		set("NEWS_ADDRESS", cfg.Cloudflare.MembersListName()+"@"+cfg.Cloudflare.ZoneName)
	}
	set("FORUM_ADDRESS", cfg.Discourse.URL)
	if forum := strings.TrimRight(cfg.Discourse.URL, "/"); forum != "" {
		set("FORUM_LOGIN_LINK", forum+"/login")
		set("PASSWORD_RESET_LINK", forum+"/password-reset")
	}
	set("GETTING_STARTED_LINK", cfg.Discourse.GettingStartedURL)
	set("ELEVATOR_ACCESS_LINK", cfg.Discourse.ElevatorAccessURL)
	set("WEBMASTER_NAME", cfg.Webmaster.Name)
	set("HELP_DESK_NAME", cfg.Jira.ServiceDeskName)
	return v
}

// Email is one member's welcome email, ready to copy.
type Email struct {
	To string
	// Bcc is set for an email to several members at once (see ForGroup):
	// they go in Bcc so they don't see each other's addresses.
	Bcc     []string
	Subject string
	// HTML is the body with inline styles (what Mail keeps when pasting).
	HTML string
	// Text is the plain-text body.
	Text string
	// Missing lists placeholders that couldn't be filled in, sorted.
	Missing []string
	// Notes are things to check before sending, e.g. that the member
	// already has a forum account.
	Notes []string
	// Kind is which kind of email this is (set by Kind.ForMember).
	Kind Kind
}

var placeholder = regexp.MustCompile(`\{\{\s*([A-Z_]+)\s*\}\}`)

// Render fills in the template for one member. An error means the template
// itself can't be used (e.g. no subject line).
func Render(tmpl, to string, values map[string]string) (Email, error) {
	e := Email{To: to}
	missing := map[string]bool{}
	var body []string
	for _, line := range strings.Split(strings.ReplaceAll(tmpl, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue // a note to the webmaster
		}
		if e.Subject == "" && trimmed != "" {
			subject, ok := strings.CutPrefix(trimmed, "Subject:")
			if !ok {
				return Email{}, errors.New(`the welcome email template must start with a "Subject: ..." line (after any # notes)`)
			}
			e.Subject = fill(strings.TrimSpace(subject), values, missing, false)
			continue
		}
		if e.Subject != "" {
			body = append(body, strings.TrimRight(line, " \t"))
		}
	}
	if e.Subject == "" {
		return Email{}, errors.New(`the welcome email template has no "Subject: ..." line`)
	}
	blocks := splitBlocks(body)
	if len(blocks) == 0 {
		return Email{}, errors.New("the welcome email template has a subject but no message")
	}
	var h, t strings.Builder
	for _, b := range blocks {
		renderBlock(&h, &t, b, values, missing)
	}
	e.HTML = strings.TrimSpace(h.String())
	e.Text = strings.TrimSpace(t.String())
	for k := range missing {
		e.Missing = append(e.Missing, k)
	}
	sort.Strings(e.Missing)
	return e, nil
}

// splitBlocks groups lines into blocks separated by blank lines.
func splitBlocks(lines []string) [][]string {
	var blocks [][]string
	var cur []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	return blocks
}

// Inline styles, so the formatting survives pasting into Mail.
const (
	styleP       = "margin:0 0 12px;"
	styleList    = "margin:0 0 12px;padding-left:24px;"
	styleItem    = "margin-bottom:4px;"
	styleLink    = "color:#1a5fb4;"
	styleBox     = "border:2px solid #1a73e8;border-radius:8px;padding:12px 16px;margin:0 0 12px;background:#e8f0fe;color:#111;"
	styleMissing = "background:#fff3a3;"
)

var (
	bulletItem = regexp.MustCompile(`^\s*[-*•]\s+`)
	numberItem = regexp.MustCompile(`^\s*\d+[.)]\s+`)
)

// renderBlock writes one block (a paragraph, lists, or a "> " box).
func renderBlock(h, t *strings.Builder, lines []string, values map[string]string, missing map[string]bool) {
	if allQuoted(lines) {
		inner := make([]string, len(lines))
		for i, l := range lines {
			inner[i] = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(l), ">"), " ")
		}
		h.WriteString(`<div style="` + styleBox + `">`)
		var ih strings.Builder
		for _, b := range splitBlocks(inner) {
			renderBlock(&ih, t, b, values, missing)
		}
		// The last paragraph's margin would leave a gap inside the box.
		h.WriteString(strings.TrimSpace(ih.String()))
		h.WriteString("</div>\n")
		return
	}

	// Lines in a block: runs of list items become lists, the rest are a
	// paragraph with line breaks.
	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		var parts []string
		for _, l := range para {
			parts = append(parts, inline(strings.TrimSpace(l), values, missing))
			t.WriteString(plain(strings.TrimSpace(l), values) + "\n")
		}
		h.WriteString(`<p style="` + styleP + `">` + strings.Join(parts, "<br>") + "</p>\n")
		t.WriteString("\n")
		para = nil
	}
	for i := 0; i < len(lines); {
		tag, marker := "", bulletItem
		switch {
		case bulletItem.MatchString(lines[i]):
			tag = "ul"
		case numberItem.MatchString(lines[i]):
			tag, marker = "ol", numberItem
		}
		if tag == "" {
			para = append(para, lines[i])
			i++
			continue
		}
		flushPara()
		h.WriteString("<" + tag + ` style="` + styleList + `">`)
		n := 1
		for ; i < len(lines) && marker.MatchString(lines[i]); i++ {
			item := marker.ReplaceAllString(lines[i], "")
			h.WriteString(`<li style="` + styleItem + `">` + inline(item, values, missing) + "</li>")
			prefix := "- "
			if tag == "ol" {
				prefix = fmt.Sprintf("%d. ", n)
			}
			t.WriteString(prefix + plain(item, values) + "\n")
			n++
		}
		h.WriteString("</" + tag + ">\n")
		t.WriteString("\n")
	}
	flushPara()
}

func allQuoted(lines []string) bool {
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), ">") {
			return false
		}
	}
	return true
}

var (
	boldRun = regexp.MustCompile(`\*\*(.+?)\*\*`)
	webLink = regexp.MustCompile(`https?://[^\s<>"]+`)
)

// inline formats one line as HTML: escaped text, filled-in placeholders,
// bold, and links.
func inline(s string, values map[string]string, missing map[string]bool) string {
	return linkify(boldRun.ReplaceAllString(fill(s, values, missing, true), "<strong>$1</strong>"))
}

// fill escapes s (when asHTML) and replaces placeholders with their values.
// Unknown ones are kept, highlighted in HTML, and recorded as missing.
func fill(s string, values map[string]string, missing map[string]bool, asHTML bool) string {
	esc := func(x string) string {
		if asHTML {
			return html.EscapeString(x)
		}
		return x
	}
	var b strings.Builder
	last := 0
	for _, m := range placeholder.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(esc(s[last:m[0]]))
		name := s[m[2]:m[3]]
		if v, ok := values[name]; ok {
			b.WriteString(esc(v))
		} else {
			missing[name] = true
			if asHTML {
				b.WriteString(`<span style="` + styleMissing + `">{{` + html.EscapeString(name) + `}}</span>`)
			} else {
				b.WriteString("{{" + name + "}}")
			}
		}
		last = m[1]
	}
	b.WriteString(esc(s[last:]))
	return b.String()
}

// linkify turns web addresses into links, leaving trailing punctuation out
// of the link.
func linkify(s string) string {
	return webLink.ReplaceAllStringFunc(s, func(u string) string {
		trail := ""
		for strings.ContainsAny(u[len(u)-1:], ".,;:!?)") {
			trail = u[len(u)-1:] + trail
			u = u[:len(u)-1]
		}
		return `<a href="` + u + `" style="` + styleLink + `">` + u + "</a>" + trail
	})
}

// plain is one line as plain text: placeholders filled, no ** markers.
func plain(s string, values map[string]string) string {
	return boldRun.ReplaceAllString(fill(s, values, map[string]bool{}, false), "$1")
}

// Suggest picks the email that best fits a member, from the membership
// sheet's verdict and each system's state, keyed by connector ID
// ("cloudflare", "discourse"). It's only a suggestion: the webmaster
// chooses.
//   - not passed by the sheet yet → Application in progress
//   - no forum account (perhaps an invite not used yet), and not set up on
//     the mailing list either → Welcome email (e.g. just onboarded)
//   - no forum account, but on the mailing list → New forum invite
//   - hasn't clicked Cloudflare's email → Verify your email
//   - otherwise (they have a forum account) → Forum access help
func Suggest(sheet connectors.State, systems map[string]connectors.State) Kind {
	mail, haveMail := systems["cloudflare"]
	forum, haveForum := systems["discourse"]
	switch {
	case sheet != connectors.StateOK:
		return ApplicationInProgress
	case haveForum && (forum == connectors.StateMissing || forum == connectors.StatePending):
		if haveMail && mail == connectors.StateOK {
			return ForumInvite
		}
		return Welcome
	case haveMail && mail == connectors.StatePending:
		return VerifyEmail
	case haveMail && mail == connectors.StateMissing:
		return Welcome
	}
	return ForumAccess
}
