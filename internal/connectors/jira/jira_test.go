package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// fakeJira starts a pretend Jira server. handler receives each request after
// the auth and header checks every real call must pass.
func fakeJira(t *testing.T, handler http.HandlerFunc) *Connector {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("connector made a %s request — it must be read-only", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "webmaster@example.com" || pass != "test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-ExperimentalApi") != "opt-in" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	store := secrets.NewMemory()
	_ = store.Set(secrets.JiraAPIToken, "test-token")
	c := New(config.Jira{
		Site: "example.atlassian.net", Email: "webmaster@example.com",
		ServiceDeskID: "7", ServiceDeskName: "APS Help",
	}, store)
	c.baseURL = srv.URL
	return c
}

func customersHandler(t *testing.T, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/servicedeskapi/servicedesk/7/customer" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "alex@example.com" {
			t.Errorf("query = %q", r.URL.Query().Get("query"))
		}
		w.Write([]byte(body))
	}
}

func TestCheckMemberFound(t *testing.T) {
	c := fakeJira(t, customersHandler(t, `{"values":[
		{"accountId":"1","emailAddress":"alexandra@example.com","displayName":"Alexandra","active":true},
		{"accountId":"2","emailAddress":"Alex@Example.com","displayName":"Alex Example","active":true}]}`))
	st, err := c.CheckMember(context.Background(), "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != connectors.StateOK || !strings.Contains(st.Summary, "APS Help") {
		t.Errorf("got %+v", st)
	}
	if len(st.Details) == 0 || st.Details[0] != "Name in Jira: Alex Example" {
		t.Errorf("should report the matched person, got %v", st.Details)
	}
}

func TestCheckMemberNotFound(t *testing.T) {
	c := fakeJira(t, customersHandler(t, `{"values":[
		{"accountId":"1","emailAddress":"alexandra@example.com","displayName":"Alexandra","active":true}]}`))
	st, err := c.CheckMember(context.Background(), "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != connectors.StateMissing || st.Fix == "" {
		t.Errorf("a near-miss must not count as a match; got %+v", st)
	}
}

func TestCheckMemberDeactivated(t *testing.T) {
	c := fakeJira(t, customersHandler(t, `{"values":[
		{"accountId":"2","emailAddress":"alex@example.com","displayName":"Alex","active":false}]}`))
	st, _ := c.CheckMember(context.Background(), "alex@example.com")
	if st.State != connectors.StateProblem {
		t.Errorf("deactivated account should be a Problem, got %+v", st)
	}
}

func TestCheckMemberHiddenEmailSingleMatch(t *testing.T) {
	c := fakeJira(t, customersHandler(t, `{"values":[
		{"accountId":"2","emailAddress":"","displayName":"Alex","active":true}]}`))
	st, _ := c.CheckMember(context.Background(), "alex@example.com")
	if st.State != connectors.StateOK || !strings.Contains(strings.Join(st.Details, " "), "hides") {
		t.Errorf("single hidden-email match should be OK with a note, got %+v", st)
	}
}

func TestErrorsAreActionable(t *testing.T) {
	cases := map[int]string{
		http.StatusUnauthorized:        "aps-tools setup jira",
		http.StatusForbidden:           "agent or admin",
		http.StatusNotFound:            "check the site address",
		http.StatusTooManyRequests:     "wait a minute",
		http.StatusInternalServerError: "status.atlassian.com",
	}
	for code, want := range cases {
		c := fakeJira(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		_, err := c.CheckMember(context.Background(), "alex@example.com")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("HTTP %d: error %v should mention %q", code, err, want)
		}
	}
}

func TestWrongTokenIsRejected(t *testing.T) {
	c := fakeJira(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	c.token = "wrong"
	if _, err := c.WhoAmI(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("err = %v", err)
	}
}

func TestListServiceDesksFollowsPages(t *testing.T) {
	c := fakeJira(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start") == "0" {
			w.Write([]byte(`{"isLastPage":false,"values":[{"id":"1","projectName":"IT","projectKey":"IT"}]}`))
			return
		}
		w.Write([]byte(`{"isLastPage":true,"values":[{"id":"7","projectName":"APS Help","projectKey":"APS"}]}`))
	})
	desks, err := c.ListServiceDesks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(desks) != 2 || desks[1].ProjectName != "APS Help" {
		t.Errorf("got %+v", desks)
	}
}

func TestMissing(t *testing.T) {
	c := New(config.Jira{}, secrets.NewMemory())
	m := c.Missing()
	if len(m) != 4 {
		t.Fatalf("want 4 missing items, got %v", m)
	}
	if !strings.Contains(m[len(m)-1], "aps-tools setup jira") {
		t.Errorf("last item should say how to fix it: %v", m)
	}
}

func TestCleanSite(t *testing.T) {
	for in, want := range map[string]string{
		"example.atlassian.net":                            "example.atlassian.net",
		" https://Example.atlassian.net/ ":                 "example.atlassian.net",
		"https://example.atlassian.net/jira/servicedesk/x": "example.atlassian.net",
	} {
		if got := CleanSite(in); got != want {
			t.Errorf("CleanSite(%q) = %q, want %q", in, got, want)
		}
	}
}
