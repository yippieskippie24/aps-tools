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

// fakeForum serves canned answers by path (query ignored unless the key
// includes it) and checks every request is a read-only GET with the key.
func fakeForum(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("made a %s request — the connector must be read-only", r.Method)
		}
		if r.Header.Get("Api-Key") != "test-key" || r.Header.Get("Api-Username") != "webmaster" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.RawQuery, "show_emails") {
			t.Error("must not ask Discourse to show emails (it fills the staff action log)")
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
	c := NewClient("forum.example.org", "@webmaster", "test-key")
	c.baseURL = srv.URL
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
	if _, err := c.Me(context.Background()); err == nil || !strings.Contains(err.Error(), "Read-only") {
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
		"/u/webmaster/invited.json?filter=expired": `{"invites":[]}`,
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
	for _, want := range []string{"invite-only", "private", "2 invite(s)", "members"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
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
}
