package discourse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// fakeForumURL serves canned answers by path (query ignored unless the key
// includes it) and checks every request is a read-only GET with a valid
// key: either an admin key (Api-Key + Api-Username) or a user key
// (User-Api-Key + User-Api-Client-Id), never both.
func fakeForumURL(t *testing.T, routes map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("made a %s request — the connector must be read-only", r.Method)
		}
		admin := r.Header.Get("Api-Key") == "test-key" && r.Header.Get("Api-Username") == "webmaster"
		user := r.Header.Get("User-Api-Key") == "test-key" && r.Header.Get("User-Api-Client-Id") == "client-1"
		if r.Header.Get("Api-Key") != "" && r.Header.Get("User-Api-Key") != "" {
			t.Error("sent both kinds of key headers")
		}
		if !admin && !user {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.RawQuery, "show_emails") && r.URL.Path != "/admin/users/list/all.json" {
			t.Error("must not ask Discourse to show emails except in the bulk list (it fills the staff action log)")
		}
		body, ok := routes[r.URL.Path+"?"+r.URL.RawQuery]
		if !ok {
			body, ok = routes[r.URL.Path]
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// fakeForum returns an admin-key client talking to a fake forum.
func fakeForum(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	url := fakeForumURL(t, routes)
	c := NewClient("forum.example.org", "@webmaster", "test-key")
	c.baseURL = url
	return c
}

var now = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func user(jsonText string) AdminUser {
	var u AdminUser
	if err := json.Unmarshal([]byte(jsonText), &u); err != nil {
		panic(err)
	}
	return u
}

func TestMemberStatus(t *testing.T) {
	cases := []struct {
		name    string
		u       string
		found   bool
		invites []Invite
		group   string
		want    connectors.State
		summary string
	}{
		{"active", `{"username":"alex","active":true,"approved":true,"last_seen_at":"2026-11-10T00:00:00Z","groups":[{"name":"members"},{"name":"trust_level_1","automatic":true}]}`, true, nil, "members", connectors.StateOK, "in the \"members\" group"},
		{"active, approved field missing", `{"username":"alex","active":true}`, true, nil, "", connectors.StateOK, "active account"},
		{"not activated", `{"username":"alex","active":false,"approved":true,"created_at":"2026-11-01T00:00:00Z"}`, true, nil, "", connectors.StatePending, "hasn't confirmed their email"},
		{"needs approval", `{"username":"alex","active":true,"approved":false}`, true, nil, "", connectors.StateProblem, "waiting for an admin to approve"},
		{"suspended", `{"username":"alex","active":true,"approved":true,"suspended_till":"2027-01-01T00:00:00Z"}`, true, nil, "", connectors.StateProblem, "suspended until 1 Jan 2027"},
		{"suspension over", `{"username":"alex","active":true,"approved":true,"suspended_till":"2026-01-01T00:00:00Z"}`, true, nil, "", connectors.StateOK, "active account"},
		{"wrong group", `{"username":"alex","active":true,"approved":true,"groups":[{"name":"guests"}]}`, true, nil, "members", connectors.StateProblem, "isn't in the \"members\" group"},
		{"pending invite", `{}`, false, []Invite{{Email: "Alex@example.com", CreatedAt: "2026-11-01T00:00:00Z", ExpiresAt: "2026-12-01T00:00:00Z"}}, "", connectors.StatePending, "Invited on 1 Nov 2026 (12 days ago)"},
		{"expired invite", `{}`, false, []Invite{{Email: "alex@example.com", CreatedAt: "2026-09-01T00:00:00Z", ExpiresAt: "2026-10-01T00:00:00Z", Expired: true}}, "", connectors.StateMissing, "expired before they accepted"},
		{"nothing", `{}`, false, nil, "", connectors.StateMissing, "No forum account, and no invite"},
		{"staged with invite", `{"username":"alex","staged":true}`, true, []Invite{{Email: "alex@example.com", CreatedAt: "2026-11-12T00:00:00Z"}}, "", connectors.StatePending, "hasn't accepted"},
	}
	for _, tc := range cases {
		st := memberStatus(user(tc.u), tc.found, tc.invites, "alex@example.com", tc.group, now)
		if st.State != tc.want || !strings.Contains(st.Summary, tc.summary) {
			t.Errorf("%s: got %v %q; want %v containing %q", tc.name, st.State, st.Summary, tc.want, tc.summary)
		}
		if st.State != connectors.StateOK && st.Fix == "" {
			t.Errorf("%s: a non-OK result must say what to do", tc.name)
		}
	}
}

func TestFindUserByEmail(t *testing.T) {
	c := fakeForum(t, map[string]string{
		"/admin/users.json?email=alex%40example.com":   `[{"id":42,"username":"alex"}]`,
		"/admin/users.json?email=nobody%40example.com": `[]`,
		"/admin/users/42.json":                         `{"id":42,"username":"alex","active":true,"approved":true,"groups":[{"name":"members"}]}`,
	})
	u, found, err := c.FindUserByEmail(context.Background(), "alex@example.com")
	if err != nil || !found || u.Username != "alex" || len(u.Groups) != 1 {
		t.Errorf("got %+v found=%v err=%v", u, found, err)
	}
	if _, found, err := c.FindUserByEmail(context.Background(), "nobody@example.com"); err != nil || found {
		t.Errorf("unknown email: found=%v err=%v", found, err)
	}
}

func TestMeRequiresAdmin(t *testing.T) {
	c := fakeForum(t, map[string]string{"/u/webmaster.json": `{"user":{"username":"webmaster","admin":false}}`})
	if _, err := c.Me(context.Background()); err == nil || !strings.Contains(err.Error(), "isn't a forum admin") {
		t.Errorf("err = %v", err)
	}
}

func TestBadKeyIsExplained(t *testing.T) {
	c := fakeForum(t, nil)
	c.key = "wrong"
	if _, err := c.Me(context.Background()); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("err = %v", err)
	}
}

func TestDiscover(t *testing.T) {
	c := fakeForum(t, map[string]string{
		"/u/webmaster.json": `{"user":{"username":"webmaster","admin":true}}`,
		"/admin/site_settings.json": `{"site_settings":[
			{"setting":"invite_only","value":true},{"setting":"must_approve_users","value":false},
			{"setting":"login_required","value":true},{"setting":"enable_local_logins","value":true},
			{"setting":"enable_google_oauth2_logins","value":true},{"setting":"invite_expiry_days","value":90}]}`,
		"/groups.json": `{"groups":[{"name":"members","user_count":30},{"name":"trust_level_0","automatic":true,"user_count":40}],"total_rows_groups":2}`,
		"/u/webmaster/invited.json?filter=pending": `{"invites":[{"email":"a@example.com"},{"email":"b@example.com"}]}`,
		"/u/webmaster/invited.json?filter=expired": `{"invites":[{"email":"c@example.com"}]}`,
	})
	d, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.PendingInvites != 2 || len(d.Groups) != 1 || d.Groups[0].Name != "members" {
		t.Errorf("got %+v", d)
	}
	if strings.Join(d.LoginMethods, ",") != "email and password,Google" {
		t.Errorf("login methods = %v", d.LoginMethods)
	}
	notes := strings.Join(d.Notes, "\n")
	for _, want := range []string{"invite-only", "private", "2 invite(s)", "1 of your invites expired", "members"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
		}
	}
	for _, st := range d.Settings {
		if st.Name == "enable_discourse_connect" {
			t.Error("a setting the forum didn't return should be left out, not shown as —")
		}
	}
	if strings.Contains(notes, "a@example.com") {
		t.Error("discover must never show members' emails")
	}
}

func TestCleanURL(t *testing.T) {
	for in, want := range map[string]string{
		"forum.example.org":                "https://forum.example.org",
		"https://Forum.example.org/latest": "https://forum.example.org",
		" https://forum.example.org/ ":     "https://forum.example.org",
	} {
		if got := CleanURL(in); got != want {
			t.Errorf("CleanURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMissing(t *testing.T) {
	m := New(config.Discourse{}, secrets.NewMemory()).Missing()
	if len(m) != 3 || !strings.Contains(m[2], "aps-tools setup discourse") {
		t.Errorf("Missing() = %v", m)
	}
	store := secrets.NewMemory()
	store.Set(secrets.DiscourseAPIKey, "k")
	ok := config.Discourse{URL: "https://forum.example.org", Username: "webmaster", ClientID: "c"}
	if m := New(ok, store).Missing(); len(m) != 0 {
		t.Errorf("user key set up, Missing() = %v", m)
	}
	ok.ClientID = ""
	if m := New(ok, store).Missing(); len(m) != 1 {
		t.Errorf("user key without client ID should need setup, Missing() = %v", m)
	}
	ok.KeyType = config.DiscourseAdminKey
	if m := New(ok, store).Missing(); len(m) != 0 {
		t.Errorf("admin key set up, Missing() = %v", m)
	}
}

// A user key: the forum says who owns it, and later calls use that name.
func TestUserKeyClient(t *testing.T) {
	url := fakeForumURL(t, map[string]string{
		"/session/current.json":                    `{"current_user":{"username":"webmaster","admin":true}}`,
		"/u/webmaster/invited.json?filter=pending": `{"invites":[{"email":"a@example.com"}]}`,
	})
	cfg := config.Discourse{URL: url, ClientID: "client-1"}
	c := ClientFor(cfg, "test-key")
	c.baseURL = url
	me, err := c.Me(context.Background())
	if err != nil || me.Username != "webmaster" {
		t.Fatalf("Me = %+v, %v", me, err)
	}
	inv, err := c.Invites(context.Background(), "pending", "")
	if err != nil || len(inv) != 1 {
		t.Errorf("Invites = %v, %v", inv, err)
	}
}

func TestUserKeyNotAdmin(t *testing.T) {
	url := fakeForumURL(t, map[string]string{
		"/session/current.json": `{"current_user":{"username":"someone","admin":false}}`,
	})
	c := NewUserKeyClient(url, "", "test-key", "client-1")
	c.baseURL = url
	if _, err := c.Me(context.Background()); err == nil || !strings.Contains(err.Error(), "@someone isn't a forum admin") {
		t.Errorf("err = %v", err)
	}
}

func TestCheckMembersInBulk(t *testing.T) {
	url := fakeForumURL(t, map[string]string{
		"/admin/users/list/all.json?page=1&show_emails=true": `[
			{"id":1,"username":"alex","email":"alex@example.com","active":true},
			{"id":2,"username":"sam","email":"sam@example.com","secondary_emails":["Sam.Two@example.com"],"active":false},
			{"id":3,"username":"pat","email":"pat@example.com","active":true,"suspend_reason":"spam"}]`,
		"/admin/users/list/all.json?page=2&show_emails=true": `[]`,
		"/admin/users/3.json":                                `{"id":3,"username":"pat","active":true,"suspended_till":"2099-01-01T00:00:00Z"}`,
		"/u/webmaster/invited.json?filter=pending":           `{"invites":[{"email":"nora@example.com","created_at":"2026-11-01T00:00:00Z","expires_at":"2099-01-01T00:00:00Z"}]}`,
		"/u/webmaster/invited.json?filter=pending&offset=1":  `{"invites":[{"email":"nora@example.com","created_at":"2026-11-01T00:00:00Z","expires_at":"2099-01-01T00:00:00Z"}]}`,
		"/u/webmaster/invited.json?filter=expired":           `{"invites":[]}`,
	})
	store := secrets.NewMemory()
	_ = store.Set(secrets.DiscourseAPIKey, "test-key")
	c := New(config.Discourse{URL: "forum.example.org", Username: "webmaster", KeyType: config.DiscourseUserKey, ClientID: "client-1"}, store)
	c.client.baseURL = url

	got, err := c.CheckMembers(context.Background(), []string{"alex@example.com", "sam.two@example.com", "pat@example.com", "nora@example.com", "nobody@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]connectors.State{
		"alex@example.com":    connectors.StateOK,
		"sam.two@example.com": connectors.StatePending, // found by a secondary email; not confirmed yet
		"pat@example.com":     connectors.StateProblem, // suspended, from the full record
		"nora@example.com":    connectors.StatePending, // invited
		"nobody@example.com":  connectors.StateMissing,
	}
	for email, state := range want {
		if got[email].State != state {
			t.Errorf("%s: got %v %q, want %v", email, got[email].State, got[email].Summary, state)
		}
	}
}
