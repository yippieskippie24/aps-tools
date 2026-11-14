package roster

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

var (
	ok      = connectors.MemberStatus{State: connectors.StateOK, Summary: "Fine."}
	missing = connectors.MemberStatus{State: connectors.StateMissing, Summary: "Not there."}
	pending = connectors.MemberStatus{State: connectors.StatePending, Summary: "Hasn't clicked."}
)

// fakeSheet lists members.
type fakeSheet struct{ entries []connectors.DirectoryEntry }

func (fakeSheet) ID() string                           { return "sheet" }
func (fakeSheet) Name() string                         { return "Membership sheet" }
func (fakeSheet) Missing() []string                    { return nil }
func (fakeSheet) TestConnection(context.Context) error { return nil }
func (f fakeSheet) CheckMember(_ context.Context, email string) (connectors.MemberStatus, error) {
	_, st, err := f.LookupMember(context.Background(), email)
	return st, err
}
func (f fakeSheet) LookupMember(_ context.Context, email string) (connectors.Person, connectors.MemberStatus, error) {
	for _, e := range f.entries {
		if e.Person.Email == email {
			return e.Person, e.Status, nil
		}
	}
	return connectors.Person{Email: email}, missing, nil
}
func (f fakeSheet) AllMembers(context.Context) ([]connectors.DirectoryEntry, error) {
	return f.entries, nil
}

// fakeSystem answers from a table, counting how it was asked.
type fakeSystem struct {
	name    string
	bulk    bool
	missing []string
	fail    error
	states  map[string]connectors.MemberStatus
	single  *int32
	bulks   *int32
}

func (f fakeSystem) ID() string                           { return strings.ToLower(f.name) }
func (f fakeSystem) Name() string                         { return f.name }
func (f fakeSystem) Missing() []string                    { return f.missing }
func (f fakeSystem) TestConnection(context.Context) error { return nil }
func (f fakeSystem) CheckMember(_ context.Context, email string) (connectors.MemberStatus, error) {
	atomic.AddInt32(f.single, 1)
	if f.fail != nil {
		return connectors.MemberStatus{}, f.fail
	}
	if st, ok := f.states[email]; ok {
		return st, nil
	}
	return ok, nil
}

// bulkSystem is a fakeSystem that can check in bulk.
type bulkSystem struct{ fakeSystem }

func (f bulkSystem) CheckMembers(_ context.Context, emails []string) (map[string]connectors.MemberStatus, error) {
	atomic.AddInt32(f.bulks, 1)
	if f.fail != nil {
		return nil, f.fail
	}
	out := map[string]connectors.MemberStatus{}
	for _, e := range emails {
		out[e] = ok
		if st, has := f.states[e]; has {
			out[e] = st
		}
	}
	return out, nil
}

func person(first, last string) connectors.Person {
	return connectors.Person{FirstName: first, LastName: last, Email: strings.ToLower(first) + "@example.com"}
}

func TestLoadGroupsEveryone(t *testing.T) {
	sheet := fakeSheet{entries: []connectors.DirectoryEntry{
		{Person: person("Zoe", "Zed"), Status: ok},
		{Person: person("Amy", "Able"), Status: ok},
		{Person: person("Ned", "New"), Status: pending},
		{Person: person("Wes", "Wait"), Status: connectors.MemberStatus{State: connectors.StateOK, Warning: "Member Status is \"Not Active\"."}},
	}}
	var single, bulks int32
	mail := bulkSystem{fakeSystem{name: "Mail", single: &single, bulks: &bulks,
		states: map[string]connectors.MemberStatus{"amy@example.com": missing, "wes@example.com": pending}}}
	desk := fakeSystem{name: "Desk", single: &single, bulks: &bulks}

	r, err := Load(context.Background(), []connectors.Connector{sheet, mail, desk})
	if err != nil {
		t.Fatal(err)
	}
	if bulks != 1 || single != 4 {
		t.Errorf("bulk system asked %d times, one-by-one system %d times; want 1 and 4", bulks, single)
	}
	if strings.Join(r.Systems, ",") != "Mail,Desk" {
		t.Errorf("systems = %v", r.Systems)
	}
	var order []string
	for _, e := range r.Entries {
		order = append(order, e.FirstName)
	}
	if strings.Join(order, ",") != "Amy,Ned,Wes,Zoe" {
		t.Errorf("not sorted by last name: %v", order)
	}
	want := map[string]Group{"amy@example.com": NeedsSetup, "ned@example.com": NotReady, "wes@example.com": Waiting, "zoe@example.com": SetUp}
	for _, e := range r.Entries {
		if e.Group != want[e.Email] {
			t.Errorf("%s: group %v, want %v", e.Email, e.Group, want[e.Email])
		}
	}
	amy := r.In(NeedsSetup)[0]
	if amy.Reason() != "Mail: Not there." {
		t.Errorf("reason = %q", amy.Reason())
	}
	if w := r.In(Waiting)[0].Warning(); !strings.Contains(w, "Not Active") {
		t.Errorf("warning = %q", w)
	}
	if r.Count(SetUp) != 1 || r.Count(CantCheck) != 0 {
		t.Errorf("counts: set up %d, can't check %d", r.Count(SetUp), r.Count(CantCheck))
	}
}

func TestSystemsThatCantBeChecked(t *testing.T) {
	sheet := fakeSheet{entries: []connectors.DirectoryEntry{{Person: person("Amy", "Able"), Status: ok}}}
	var n int32
	down := bulkSystem{fakeSystem{name: "Mail", single: &n, bulks: &n, fail: errors.New("Cloudflare said no")}}
	offline := fakeSystem{name: "Forum", single: &n, bulks: &n, missing: []string{"a key"}}
	r, err := Load(context.Background(), []connectors.Connector{sheet, down, offline})
	if err != nil {
		t.Fatal(err)
	}
	if r.Entries[0].Group != CantCheck || len(r.Problems) != 2 {
		t.Errorf("group %v, problems %v", r.Entries[0].Group, r.Problems)
	}
	if !strings.Contains(strings.Join(r.Problems, "\n"), "Mail: Cloudflare said no") {
		t.Errorf("problems = %v", r.Problems)
	}
}

func TestNoSheet(t *testing.T) {
	if _, err := Load(context.Background(), nil); !errors.Is(err, ErrNoSheet) {
		t.Errorf("err = %v", err)
	}
}

func TestRefreshOneMember(t *testing.T) {
	sheet := fakeSheet{entries: []connectors.DirectoryEntry{{Person: person("Amy", "Able"), Status: ok}}}
	var n int32
	sys := &fakeSystem{name: "Mail", single: &n, bulks: &n, states: map[string]connectors.MemberStatus{"amy@example.com": missing}}
	r, _ := Load(context.Background(), []connectors.Connector{sheet, *sys})
	if r.Entries[0].Group != NeedsSetup {
		t.Fatalf("group = %v", r.Entries[0].Group)
	}
	sys.states["amy@example.com"] = ok // e.g. after onboarding
	if err := r.Refresh(context.Background(), []connectors.Connector{sheet, *sys}, "amy@example.com"); err != nil {
		t.Fatal(err)
	}
	if r.Entries[0].Group != SetUp {
		t.Errorf("after refresh: %v", r.Entries[0].Group)
	}
}
