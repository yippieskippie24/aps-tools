package cloudflare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// fakeCloudflare serves canned answers by path, checks every request is a
// read-only GET with the right token, and returns a client pointed at it.
func fakeCloudflare(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("made a %s request — the connector must be read-only", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"success":false,"errors":[{"code":6003,"message":"Invalid request headers"}]}`))
			return
		}
		key := r.URL.Path
		if p := r.URL.Query().Get("page"); p != "" && p != "1" {
			key += "?page=" + p
		}
		body, ok := routes[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"success":false,"errors":[{"code":7003,"message":"Could not route"}]}`))
			return
		}
		if strings.HasPrefix(body, "MULTIPART:") {
			w.Header().Set("Content-Type", "multipart/form-data; boundary=XYZ")
			body = strings.TrimPrefix(body, "MULTIPART:")
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-token")
	c.baseURL = srv.URL
	return c
}

const addrPage1 = `{"success":true,"result":[
	{"email":"verified@example.com","created":"2026-10-01T00:00:00Z","verified":"2026-10-02T00:00:00Z"},
	{"email":"pending@example.com","created":"2026-11-01T00:00:00Z","verified":null}],
	"result_info":{"page":1,"total_pages":2}}`
const addrPage2 = `{"success":true,"result":[
	{"email":"late@example.com","created":"2026-11-10T00:00:00Z","verified":"2026-11-11T00:00:00Z"}],
	"result_info":{"page":2,"total_pages":2}}`

func TestListAddressesFollowsPages(t *testing.T) {
	c := fakeCloudflare(t, map[string]string{
		"/accounts/acc/email/routing/addresses":        addrPage1,
		"/accounts/acc/email/routing/addresses?page=2": addrPage2,
	})
	addrs, err := c.ListAddresses(context.Background(), "acc")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 3 || addrs[2].Email != "late@example.com" {
		t.Errorf("got %+v", addrs)
	}
}

func TestStatusFor(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	addrs := []Address{
		{Email: "Verified@Example.com", Created: "2026-10-01T00:00:00Z", Verified: "2026-10-02T00:00:00Z"},
		{Email: "pending@example.com", Created: "2026-11-01T00:00:00Z"},
	}
	if st := statusFor(addrs, "verified@example.com", now); st.State != connectors.StateOK {
		t.Errorf("verified (case-insensitive) = %+v", st)
	}
	st := statusFor(addrs, "pending@example.com", now)
	if st.State != connectors.StatePending || !strings.Contains(st.Summary, "12 days ago") || st.Fix == "" {
		t.Errorf("pending = %+v", st)
	}
	if st := statusFor(addrs, "nobody@example.com", now); st.State != connectors.StateMissing || st.Fix == "" {
		t.Errorf("missing = %+v", st)
	}
}

func TestBadTokenIsExplained(t *testing.T) {
	c := fakeCloudflare(t, nil)
	c.token = "wrong"
	err := c.VerifyToken(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "rejected the API token") {
		t.Errorf("err = %v", err)
	}
}

func TestVerifyAcceptsAccountTokens(t *testing.T) {
	c := fakeCloudflare(t, map[string]string{
		// A user-token check fails for an account token...
		"/user/tokens/verify": `{"success":false,"errors":[{"code":1000,"message":"Invalid API Token"}]}`,
		// ...but the account-token check succeeds.
		"/accounts/acc/tokens/verify": `{"success":true,"result":{"status":"active"}}`,
	})
	if err := c.VerifyToken(context.Background(), "acc"); err != nil {
		t.Errorf("an active account token should verify, got %v", err)
	}
}

func TestMissingPermissionIsExplained(t *testing.T) {
	c := fakeCloudflare(t, map[string]string{
		"/zones": `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`,
	})
	_, err := c.ListZones(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing a permission") {
		t.Errorf("err = %v", err)
	}
}

func TestRedactKeepsAPSAddresses(t *testing.T) {
	got := Redact(`const LIST = ["alex@example.com", "sam@example.net"]; // sent to members@aps.example.org`, "aps.example.org")
	want := `const LIST = ["[email hidden]", "[email hidden]"]; // sent to members@aps.example.org`
	if got != want {
		t.Errorf("Redact =\n %s\nwant\n %s", got, want)
	}
}

func TestDiscoverFindsListInWorkerCode(t *testing.T) {
	c := fakeCloudflare(t, map[string]string{
		"/zones/z/email/routing":                 `{"success":true,"result":{"enabled":true,"status":"ready"}}`,
		"/zones/z/email/routing/rules":           `{"success":true,"result":[{"enabled":true,"matchers":[{"type":"literal","field":"to","value":"members@aps.example.org"}],"actions":[{"type":"worker","value":["list-worker"]}]},{"enabled":true,"matchers":[{"type":"literal","field":"to","value":"webmaster@aps.example.org"}],"actions":[{"type":"forward","value":["me@example.com"]}]}],"result_info":{"page":1,"total_pages":1}}`,
		"/zones/z/email/routing/rules/catch_all": `{"success":true,"result":{"enabled":true,"matchers":[{"type":"all"}],"actions":[{"type":"drop"}]}}`,
		"/accounts/acc/email/routing/addresses":  `{"success":true,"result":[{"email":"a@example.com","verified":"2026-01-01T00:00:00Z"},{"email":"b@example.com"}],"result_info":{"page":1,"total_pages":1}}`,
		"/accounts/acc/workers/scripts/list-worker/settings": `{"success":true,"result":{"bindings":[
			{"name":"MEMBERS","type":"kv_namespace","namespace_id":"ns1"},
			{"name":"ADMIN","type":"plain_text","text":"boss@example.com"},
			{"name":"KEY","type":"secret_text"}]}}`,
		"/accounts/acc/storage/kv/namespaces/ns1/keys": `{"success":true,"result":[{"name":"x@example.com"},{"name":"config"}],"result_info":{"cursor":""}}`,
		"/accounts/acc/workers/scripts/list-worker/content/v2": "MULTIPART:--XYZ\r\n" +
			"Content-Disposition: form-data; name=\"worker.js\"; filename=\"worker.js\"\r\n\r\n" +
			"export default { async email(message, env) { const list = ['m1@example.com','m2@example.com']; for (const to of list) await message.forward(to); env.MEMBERS.get('x'); } }\r\n" +
			"--XYZ--\r\n",
	})

	d, err := c.Discover(context.Background(), "acc", "APS", "z", "aps.example.org")
	if err != nil {
		t.Fatal(err)
	}
	if d.AddressesTotal != 2 || d.AddressesVerified != 1 {
		t.Errorf("addresses = %d/%d", d.AddressesVerified, d.AddressesTotal)
	}
	if len(d.Rules) != 2 || d.Rules[0].Worker != "list-worker" || d.Rules[0].Address != "members@aps.example.org" {
		t.Errorf("rules = %+v", d.Rules)
	}
	if len(d.Workers) != 1 {
		t.Fatalf("workers = %+v", d.Workers)
	}
	w := d.Workers[0]
	if w.Err != "" || !w.Forwards || len(w.Modules) != 1 || w.Modules[0].EmailCount != 2 {
		t.Errorf("worker = %+v", w)
	}
	if strings.Contains(w.RedactedCode, "m1@example.com") || !strings.Contains(w.RedactedCode, "[email hidden]") {
		t.Error("member emails must be hidden in the code shown")
	}
	all := strings.Join(d.Notes, "\n")
	for _, want := range []string{"written into its code", "KV storage \"MEMBERS\"", "setting \"ADMIN\" contains 1", "1 destination address(es) are still waiting"} {
		if !strings.Contains(all, want) {
			t.Errorf("notes missing %q:\n%s", want, all)
		}
	}
	// members@ goes to the Worker, which has no LIST_MEMBERS variable.
	if len(d.Lists) != 1 || d.Lists[0].Defined || !strings.Contains(all, "no LIST_MEMBERS variable") {
		t.Errorf("lists = %+v; notes:\n%s", d.Lists, all)
	}
	// Nothing in the report may contain a raw member email.
	for _, b := range w.Bindings {
		if strings.Contains(b.Detail, "x@example.com") || strings.Contains(b.Detail, "boss@example.com") {
			t.Errorf("binding detail leaks an email: %q", b.Detail)
		}
	}
}

func TestMissing(t *testing.T) {
	c := New(config.Cloudflare{}, secrets.NewMemory())
	m := c.Missing()
	if len(m) != 2 || !strings.Contains(m[1], "aps-tools setup cloudflare") {
		t.Errorf("Missing() = %v", m)
	}
}

func TestCheckMemberNotesWhenRoutingIsOff(t *testing.T) {
	client := fakeCloudflare(t, map[string]string{
		"/accounts/acc/email/routing/addresses": `{"success":true,"result":[{"email":"alex@example.com","created":"2026-10-01T00:00:00Z","verified":"2026-10-02T00:00:00Z"}],"result_info":{"page":1,"total_pages":1}}`,
		"/zones/z/email/routing":                `{"success":true,"result":{"enabled":false,"status":"unconfigured"}}`,
		"/zones/z/email/routing/rules":          `{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`,
	})
	c := &Connector{cfg: config.Cloudflare{AccountID: "acc", ZoneID: "z", ZoneName: "aps.example.org"}, client: client, token: "test-token"}
	st, err := c.CheckMember(context.Background(), "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != connectors.StateOK || !strings.Contains(strings.Join(st.Details, " "), "switched off") {
		t.Errorf("verified member with routing off should be OK with a note; got %+v", st)
	}
}

func TestListVariableMatchesTheWorker(t *testing.T) {
	cases := map[string]string{
		"news@aps.example.org":          "LIST_NEWS",
		"board-members@aps.example.org": "LIST_BOARD_MEMBERS",
		"News+Weekly@aps.example.org":   "LIST_NEWS",
		"photo.club@aps.example.org":    "LIST_PHOTO_CLUB",
	}
	for in, want := range cases {
		if got := ListVariable(in); got != want {
			t.Errorf("ListVariable(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMembersMatchesTheWorker(t *testing.T) {
	got := parseMembers(" A@example.com, b@example.com;c@example.com\n a@example.com ,, ")
	want := []string{"a@example.com", "b@example.com", "c@example.com"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("parseMembers = %v, want %v", got, want)
	}
}

// listsServer fakes an account with three group addresses on one Worker.
func listsServer(t *testing.T) *Client {
	return fakeCloudflare(t, map[string]string{
		"/zones/z/email/routing/rules": `{"success":true,"result":[
			{"enabled":true,"matchers":[{"type":"literal","field":"to","value":"news@aps.example.org"}],"actions":[{"type":"worker","value":["lists"]}]},
			{"enabled":true,"matchers":[{"type":"literal","field":"to","value":"photos@aps.example.org"}],"actions":[{"type":"worker","value":["lists"]}]},
			{"enabled":true,"matchers":[{"type":"literal","field":"to","value":"treasurer@aps.example.org"}],"actions":[{"type":"worker","value":["lists"]}]},
			{"enabled":true,"matchers":[{"type":"literal","field":"to","value":"jira@aps.example.org"}],"actions":[{"type":"forward","value":["x@example.com"]}]}],
			"result_info":{"page":1,"total_pages":1}}`,
		"/accounts/acc/workers/scripts/lists/settings": `{"success":true,"result":{"bindings":[
			{"name":"LIST_NEWS","type":"plain_text","text":"alex@example.com, sam@example.com, pat@example.com, gone@example.com"},
			{"name":"LIST_PHOTOS","type":"plain_text","text":"alex@example.com"},
			{"name":"LIST_TREASURER","type":"secret_text"}]}}`,
	})
}

func TestLoadLists(t *testing.T) {
	lists, err := listsServer(t).LoadLists(context.Background(), "acc", "z")
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 3 {
		t.Fatalf("want 3 Worker lists (the forward rule isn't a list), got %+v", lists)
	}
	news, ok := FindList(lists, "news@aps.example.org")
	if !ok || len(news.Members) != 4 || !news.Has("ALEX@example.com") {
		t.Errorf("news = %+v", news)
	}
	if tr, _ := FindList(lists, "treasurer"); !tr.Defined || !tr.Secret {
		t.Errorf("treasurer should be a Secret: %+v", tr)
	}
}

func TestMemberStatusWithLists(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	lists, _ := listsServer(t).LoadLists(context.Background(), "acc", "z")
	addrs := []Address{
		{Email: "alex@example.com", Created: "2026-10-01T00:00:00Z", Verified: "2026-10-02T00:00:00Z"},
		{Email: "sam@example.com", Created: "2026-11-01T00:00:00Z"},
		{Email: "lee@example.com", Created: "2026-10-01T00:00:00Z", Verified: "2026-10-02T00:00:00Z"},
	}
	cases := []struct {
		email   string
		want    connectors.State
		summary string
	}{
		{"alex@example.com", connectors.StateOK, "On the news@ list and verified"},
		{"sam@example.com", connectors.StatePending, "haven't clicked"},
		{"pat@example.com", connectors.StateProblem, "not a destination address"},
		{"lee@example.com", connectors.StateMissing, "Verified with Cloudflare, but not on the news@ list"},
		{"nobody@example.com", connectors.StateMissing, "Not on the news@ list, and not a destination address"},
	}
	for _, tc := range cases {
		st := memberStatus(addrs, lists, "news", tc.email, now)
		if st.State != tc.want || !strings.Contains(st.Summary, tc.summary) {
			t.Errorf("%s: got %v %q, want %v containing %q", tc.email, st.State, st.Summary, tc.want, tc.summary)
		}
	}
	alex := memberStatus(addrs, lists, "news", "alex@example.com", now)
	if !strings.Contains(strings.Join(alex.Details, " "), "Also on: photos@") {
		t.Errorf("should list the other lists they're on: %v", alex.Details)
	}
	lee := memberStatus(addrs, lists, "news", "lee@example.com", now)
	if !strings.Contains(lee.Fix, "LIST_NEWS") {
		t.Errorf("fix should name the variable to edit: %q", lee.Fix)
	}
}

func TestMemberStatusWithoutListWorker(t *testing.T) {
	st := memberStatus([]Address{{Email: "alex@example.com", Verified: "2026-10-02T00:00:00Z"}}, nil, "news", "alex@example.com", time.Now())
	if st.State != connectors.StateOK || !strings.Contains(strings.Join(st.Details, " "), "isn't checked") {
		t.Errorf("before a list Worker exists, report the address only and say so: %+v", st)
	}
}

func TestSummarizeListsCountsDeliveryProblems(t *testing.T) {
	lists := []MailingList{{Address: "news@aps.example.org", Defined: true,
		Members: []string{"ok@example.com", "wait@example.com", "none@example.com"}}}
	addrs := []Address{{Email: "ok@example.com", Verified: "2026-01-01T00:00:00Z"}, {Email: "wait@example.com"}}
	got := summarizeLists(lists, addrs)[0]
	if got.Members != 3 || got.Unverified != 1 || got.NotDestination != 1 {
		t.Errorf("got %+v", got)
	}
}
