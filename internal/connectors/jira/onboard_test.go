package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// writableJira is a fake Jira. customers is the desk's customer search
// answer; accountExists makes creating a customer fail as Jira does when
// the email already has an Atlassian account. Every change is recorded.
func writableJira(t *testing.T, customers string, accountExists bool) (*Connector, *[]string) {
	t.Helper()
	var changes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "webmaster@example.com" || p != "test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-ExperimentalApi") != "opt-in" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/servicedeskapi/servicedesk/7/customer":
			w.Write([]byte(customers))
		case "POST /rest/servicedeskapi/customer":
			var b map[string]string
			_ = json.Unmarshal(body, &b)
			changes = append(changes, "create "+b["email"]+" ("+b["displayName"]+")")
			if accountExists {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"errorMessage":"An account already exists for this email"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"accountId":"new-1","emailAddress":"` + b["email"] + `"}`))
		case "GET /rest/api/3/user/search":
			w.Write([]byte(`[{"accountId":"old-9","emailAddress":"robin@example.com"}]`))
		case "POST /rest/servicedeskapi/servicedesk/7/customer":
			changes = append(changes, "add to desk "+strings.TrimSpace(string(body)))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	store := secrets.NewMemory()
	_ = store.Set(secrets.JiraAPIToken, "test-token")
	c := New(config.Jira{Site: "example.atlassian.net", Email: "webmaster@example.com", ServiceDeskID: "7", ServiceDeskName: "APS Help"}, store)
	c.baseURL = srv.URL
	return c, &changes
}

var newMember = connectors.Person{Email: "robin@example.com", FirstName: "Robin", LastName: "Example"}

func TestPlanOnboardJira(t *testing.T) {
	c, _ := writableJira(t, `{"values":[]}`, false)
	if a, _ := c.PlanOnboard(context.Background(), newMember, connectors.MemberStatus{State: connectors.StateMissing}); !strings.Contains(a, "customer on the APS Help service desk") {
		t.Errorf("plan = %q", a)
	}
	if a, _ := c.PlanOnboard(context.Background(), newMember, connectors.MemberStatus{State: connectors.StateProblem}); a != "" {
		t.Errorf("a deactivated customer should be sorted out by hand, planned %q", a)
	}
}

func TestOnboardCreatesAndAddsCustomer(t *testing.T) {
	c, changes := writableJira(t, `{"values":[]}`, false)
	did, err := c.Onboard(context.Background(), newMember)
	if err != nil || !strings.Contains(did, "Created their customer account") {
		t.Fatalf("did %q, %v", did, err)
	}
	want := `create robin@example.com (Robin Example)|add to desk {"accountIds":["new-1"]}`
	if strings.Join(*changes, "|") != want {
		t.Errorf("changes = %v", *changes)
	}
}

func TestOnboardUsesAnExistingAccount(t *testing.T) {
	c, changes := writableJira(t, `{"values":[]}`, true)
	did, err := c.Onboard(context.Background(), newMember)
	if err != nil || !strings.Contains(did, "existing Atlassian account") {
		t.Fatalf("did %q, %v", did, err)
	}
	if last := (*changes)[len(*changes)-1]; last != `add to desk {"accountIds":["old-9"]}` {
		t.Errorf("changes = %v", *changes)
	}
}

func TestOnboardSkipsExistingCustomer(t *testing.T) {
	c, changes := writableJira(t, `{"values":[{"accountId":"1","emailAddress":"robin@example.com","displayName":"Robin","active":true}]}`, false)
	did, err := c.Onboard(context.Background(), newMember)
	if err != nil || !strings.Contains(did, "nothing to do") || len(*changes) != 0 {
		t.Errorf("did %q, %v, changes %v", did, err, *changes)
	}
}
