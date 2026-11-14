package discourse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// writableForum is a fake forum that answers GETs from routes and accepts
// one kind of change: creating an invite. Every invite body it receives is
// recorded.
func writableForum(t *testing.T, routes map[string]string, cfg config.Discourse) (*Connector, *[]map[string]any) {
	t.Helper()
	var created []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Api-Key") != "test-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			if r.URL.Path != "/invites.json" {
				t.Errorf("unexpected change: POST %s (the tool may only create invites)", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body)
			w.Write([]byte(`{"id":9,"link":"https://forum.example.org/invites/abc123","email":"new@example.com","expires_at":"2027-02-11T00:00:00Z"}`))
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s request", r.Method)
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
	store := secrets.NewMemory()
	_ = store.Set(secrets.DiscourseAPIKey, "test-key")
	cfg.URL, cfg.Username, cfg.KeyType, cfg.ClientID = "forum.example.org", "webmaster", config.DiscourseUserKey, "client-1"
	c := New(cfg, store)
	c.client.baseURL = srv.URL
	return c, &created
}

var noOne = map[string]string{
	"/admin/users.json":         `[]`,
	"/u/webmaster/invited.json": `{"invites":[]}`,
}

func TestPlanOnboard(t *testing.T) {
	missing := connectors.MemberStatus{State: connectors.StateMissing}
	readOnly, _ := writableForum(t, noOne, config.Discourse{})
	if a, _ := readOnly.PlanOnboard(context.Background(), connectors.Person{}, missing); a != "" {
		t.Errorf("a read-only setup planned %q", a)
	}
	c, _ := writableForum(t, noOne, config.Discourse{CreateInvites: true, WelcomeTopicURL: "https://forum.example.org/t/welcome/5"})
	if a, _ := c.PlanOnboard(context.Background(), connectors.Person{}, missing); !strings.Contains(a, "personal invite") || !strings.Contains(a, "welcome topic") {
		t.Errorf("plan = %q", a)
	}
	if a, _ := c.PlanOnboard(context.Background(), connectors.Person{}, connectors.MemberStatus{State: connectors.StatePending}); a != "" {
		t.Errorf("someone already invited got %q", a)
	}
}

func TestOnboardCreatesOneInvite(t *testing.T) {
	c, created := writableForum(t, noOne, config.Discourse{CreateInvites: true, WelcomeTopicURL: "https://forum.example.org/t/welcome/5"})
	did, err := c.Onboard(context.Background(), connectors.Person{Email: "new@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*created) != 1 || !strings.Contains(did, "Created a personal invite") {
		t.Fatalf("did %q, created %v", did, *created)
	}
	body := (*created)[0]
	if body["email"] != "new@example.com" || body["skip_email"] != true || body["max_redemptions_allowed"] != float64(1) || body["topic_id"] != float64(5) {
		t.Errorf("invite body = %v", body)
	}
}

func TestOnboardChecksFirst(t *testing.T) {
	hasAccount := map[string]string{
		"/admin/users.json":   `[{"id":7,"username":"newbie"}]`,
		"/admin/users/7.json": `{"id":7,"username":"newbie","active":true}`,
	}
	c, created := writableForum(t, hasAccount, config.Discourse{CreateInvites: true})
	if did, err := c.Onboard(context.Background(), connectors.Person{Email: "new@example.com"}); err != nil || !strings.Contains(did, "already have a forum account") {
		t.Errorf("did %q, %v", did, err)
	}
	invited := map[string]string{
		"/admin/users.json":         `[]`,
		"/u/webmaster/invited.json": `{"invites":[{"email":"new@example.com","link":"https://forum.example.org/invites/old","created_at":"2026-11-01T00:00:00Z","expires_at":"2099-01-01T00:00:00Z"}]}`,
	}
	c2, created2 := writableForum(t, invited, config.Discourse{CreateInvites: true})
	if did, err := c2.Onboard(context.Background(), connectors.Person{Email: "new@example.com"}); err != nil || !strings.Contains(did, "pending invite") {
		t.Errorf("did %q, %v", did, err)
	}
	if len(*created)+len(*created2) != 0 {
		t.Error("invited someone who didn't need it")
	}
	readOnly, created3 := writableForum(t, noOne, config.Discourse{})
	if _, err := readOnly.Onboard(context.Background(), connectors.Person{Email: "new@example.com"}); err == nil || len(*created3) != 0 {
		t.Error("a read-only setup created an invite")
	}
}

func TestInviteLink(t *testing.T) {
	invited := map[string]string{
		"/admin/users.json":         `[]`,
		"/u/webmaster/invited.json": `{"invites":[{"email":"new@example.com","link":"https://forum.example.org/invites/abc","created_at":"2026-11-01T00:00:00Z","expires_at":"2099-01-01T00:00:00Z"}]}`,
	}
	c, _ := writableForum(t, invited, config.Discourse{CreateInvites: true})
	if link, err := c.InviteLink(context.Background(), "new@example.com"); err != nil || link.URL != "https://forum.example.org/invites/abc" || link.HasAccount {
		t.Errorf("pending invite: %+v %v", link, err)
	}
	if link, _ := c.InviteLink(context.Background(), "other@example.com"); link.URL != "" {
		t.Errorf("no invite: %+v", link)
	}
	hasAccount := map[string]string{
		"/admin/users.json":   `[{"id":7,"username":"newbie"}]`,
		"/admin/users/7.json": `{"id":7,"username":"newbie","active":true}`,
	}
	c2, _ := writableForum(t, hasAccount, config.Discourse{})
	if link, _ := c2.InviteLink(context.Background(), "new@example.com"); link.URL != "https://forum.example.org" || !link.HasAccount {
		t.Errorf("has an account: %+v", link)
	}
}

func TestReadOnlyAdviceMentionsInvites(t *testing.T) {
	c, _ := writableForum(t, noOne, config.Discourse{})
	st, _ := c.CheckMember(context.Background(), "new@example.com")
	if st.State != connectors.StateMissing || !strings.Contains(st.Fix, "setup discourse") {
		t.Errorf("got %+v", st)
	}
}
