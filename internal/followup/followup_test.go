package followup

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/activity"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/progress"
	"github.com/yippieskippie24/aps-tools/internal/roster"
	"github.com/yippieskippie24/aps-tools/internal/welcome"
)

var (
	ok      = connectors.MemberStatus{State: connectors.StateOK, Summary: "Fine."}
	missing = connectors.MemberStatus{State: connectors.StateMissing, Summary: "Not there."}
	pending = connectors.MemberStatus{State: connectors.StatePending, Summary: "Hasn't clicked."}
)

// The made-up members:
//   - Amy: all set up.
//   - Ben: hasn't verified Cloudflare's email.
//   - Cat: no forum account (and hasn't verified either).
//   - Dan: hasn't used their forum invite.
//   - Eve: not vetted yet (never in a follow-up).
var people = []connectors.DirectoryEntry{
	{Person: connectors.Person{Email: "amy@example.com", FirstName: "Amy", LastName: "Able"}, Status: ok},
	{Person: connectors.Person{Email: "ben@example.com", FirstName: "Ben", LastName: "Bell"}, Status: ok},
	{Person: connectors.Person{Email: "cat@example.com", FirstName: "Cat", LastName: "Cole"}, Status: ok},
	{Person: connectors.Person{Email: "dan@example.com", FirstName: "Dan", LastName: "Dale"}, Status: ok},
	{Person: connectors.Person{Email: "eve@example.com", FirstName: "Eve", LastName: "Etch"}, Status: pending},
}

// sheet is the membership sheet.
type sheet struct{}

func (sheet) ID() string                           { return "sheet" }
func (sheet) Name() string                         { return "Membership sheet" }
func (sheet) Missing() []string                    { return nil }
func (sheet) TestConnection(context.Context) error { return nil }
func (s sheet) CheckMember(ctx context.Context, email string) (connectors.MemberStatus, error) {
	_, st, err := s.LookupMember(ctx, email)
	return st, err
}
func (sheet) LookupMember(_ context.Context, email string) (connectors.Person, connectors.MemberStatus, error) {
	for _, e := range people {
		if e.Person.Email == email {
			return e.Person, e.Status, nil
		}
	}
	return connectors.Person{Email: email}, missing, nil
}
func (sheet) AllMembers(context.Context) ([]connectors.DirectoryEntry, error) { return people, nil }

// mail is Cloudflare: Ben and Cat haven't verified. It records re-sends.
type mail struct {
	resent *[]string
	fail   map[string]bool
}

func (mail) ID() string                           { return "cloudflare" }
func (mail) Name() string                         { return "Mailing list (Cloudflare)" }
func (mail) Missing() []string                    { return nil }
func (mail) TestConnection(context.Context) error { return nil }
func (mail) CheckMember(_ context.Context, email string) (connectors.MemberStatus, error) {
	if email == "ben@example.com" || email == "cat@example.com" {
		return pending, nil
	}
	return ok, nil
}
func (m mail) ResendVerification(_ context.Context, email string) (string, error) {
	if m.fail[email] {
		return "", errors.New("Cloudflare said no")
	}
	*m.resent = append(*m.resent, email)
	return "Cloudflare is sending them a new verification email.", nil
}

// forum: Cat has nothing (the tool can invite her), Dan has an invite. It
// records invites.
type forum struct{ invited *[]string }

func (forum) ID() string                           { return "discourse" }
func (forum) Name() string                         { return "Discourse forum" }
func (forum) Missing() []string                    { return nil }
func (forum) TestConnection(context.Context) error { return nil }
func (f forum) CheckMember(_ context.Context, email string) (connectors.MemberStatus, error) {
	for _, e := range *f.invited {
		if e == email {
			return pending, nil
		}
	}
	switch email {
	case "cat@example.com":
		return missing, nil
	case "dan@example.com":
		return pending, nil
	}
	return ok, nil
}
func (forum) PlanOnboard(_ context.Context, _ connectors.Person, st connectors.MemberStatus) (string, error) {
	if st.State == connectors.StateMissing {
		return "Create a personal forum invite", nil
	}
	return "", nil
}
func (f forum) Onboard(_ context.Context, p connectors.Person) (string, error) {
	*f.invited = append(*f.invited, p.Email)
	return "Created a personal forum invite.", nil
}

func setup(t *testing.T) (conns []connectors.Connector, resent, invited *[]string, log *activity.Log) {
	resent, invited = new([]string), new([]string)
	conns = []connectors.Connector{sheet{}, mail{resent: resent, fail: map[string]bool{"cat@example.com": true}}, forum{invited}}
	return conns, resent, invited, activity.Open(filepath.Join(t.TempDir(), "activity.log"))
}

func names(es []roster.Entry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.FirstName)
	}
	return strings.Join(out, ",")
}

func TestLists(t *testing.T) {
	conns, _, _, _ := setup(t)
	r, err := roster.Load(context.Background(), conns)
	if err != nil {
		t.Fatal(err)
	}
	lists := Lists(r)
	want := map[Kind]string{Unverified: "Ben,Cat", NeedsSetup: "Cat", InviteWaiting: "Dan"}
	if len(lists) != len(Kinds) {
		t.Fatalf("%d lists", len(lists))
	}
	for _, l := range lists {
		if got := names(l.Members); got != want[l.Kind] {
			t.Errorf("%s: %s, want %s", l.Kind.Title(), got, want[l.Kind])
		}
	}
	if d := NeedsSetup.Detail(lists[1].Members[0]); d != "Missing from the forum" {
		t.Errorf("detail = %q", d)
	}
	if d := Unverified.Detail(lists[0].Members[0]); d != "Hasn't clicked." {
		t.Errorf("detail = %q", d)
	}
}

// Only the people chosen are re-sent to; a failure doesn't stop the rest,
// and each one is logged.
func TestRunResends(t *testing.T) {
	conns, resent, _, log := setup(t)
	var steps []string
	ctx := progress.With(context.Background(), func(s string) { steps = append(steps, s) })
	who := []connectors.Person{people[2].Person, people[1].Person} // Cat fails, Ben works
	results := Run(ctx, Unverified, conns, who, log)
	if okN, failed := Count(results); okN != 1 || failed != 1 {
		t.Fatalf("results %+v", results)
	}
	if len(*resent) != 1 || (*resent)[0] != "ben@example.com" {
		t.Errorf("resent %v", *resent)
	}
	if !strings.Contains(results[0].What, "Cloudflare said no") {
		t.Errorf("Cat: %q", results[0].What)
	}
	if steps[0] != "1 of 2 · Cat Cole · Re-send Cloudflare's verification email" {
		t.Errorf("progress %v", steps)
	}
	lines, _ := log.Recent(10)
	if len(lines) != 2 {
		t.Errorf("log %v", lines)
	}
}

// Onboarding checks each member live and only does what's missing:
// running it twice changes nothing the second time.
func TestRunOnboardsAndIsSafeToRepeat(t *testing.T) {
	conns, _, invited, log := setup(t)
	cat := []connectors.Person{{Email: "cat@example.com"}}
	results := Run(context.Background(), NeedsSetup, conns, cat, log)
	if len(results) != 1 || !results[0].OK || len(*invited) != 1 {
		t.Fatalf("results %+v, invited %v", results, *invited)
	}
	if results[0].Person.FirstName != "Cat" || !strings.Contains(results[0].What, "Created a personal forum invite") {
		t.Errorf("result %+v", results[0])
	}
	again := Run(context.Background(), NeedsSetup, conns, cat, log)
	if len(*invited) != 1 || !again[0].OK || !strings.Contains(again[0].What, "Nothing for the tool to do") {
		t.Errorf("second run: %+v, invited %v", again, *invited)
	}

	// Someone the sheet doesn't pass is skipped, not onboarded.
	eve := Run(context.Background(), NeedsSetup, conns, []connectors.Person{{Email: "eve@example.com"}}, log)
	if eve[0].OK || !strings.HasPrefix(eve[0].What, "Skipped") || len(*invited) != 1 {
		t.Errorf("eve: %+v", eve)
	}
}

// Stopping stops before the next member.
func TestRunStops(t *testing.T) {
	conns, resent, _, log := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := Run(ctx, Unverified, conns, []connectors.Person{people[1].Person}, log); len(got) != 0 || len(*resent) != 0 {
		t.Errorf("ran %v after stopping", got)
	}
}

func TestEmailFor(t *testing.T) {
	st := map[string]connectors.State{"cloudflare": connectors.StatePending, "discourse": connectors.StatePending}
	if k := Unverified.Email(connectors.StateOK, st); k.ID != welcome.VerifyEmail.ID {
		t.Errorf("unverified → %s", k.ID)
	}
	if k := NeedsSetup.Email(connectors.StateOK, st); k.ID != welcome.Welcome.ID {
		t.Errorf("just onboarded → %s", k.ID)
	}
	st["cloudflare"] = connectors.StateOK
	if k := InviteWaiting.Email(connectors.StateOK, st); k.ID != welcome.ForumInvite.ID {
		t.Errorf("invite waiting → %s", k.ID)
	}
}
