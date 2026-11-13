package sheet

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// testKey makes a throwaway service account key file.
func testKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	file, _ := json.MarshalIndent(map[string]string{
		"type": "service_account", "project_id": "aps-tools-test", "private_key_id": "kid1",
		"private_key": pemKey, "client_email": "reader@aps-tools-test.iam.gserviceaccount.com",
		"client_id": "123", "auth_uri": "https://accounts.google.com/o/oauth2/auth",
	}, "", "  ")
	return string(file), priv
}

// The user's real headers, with the vetting person's name replaced.
var header = []string{"Start Date:", "Paid:", "Paid Date:", "Due Date:", "Member Status", "New", "App vetted (Pat)",
	"Last Name:", "First Name:", "e-Mail Address:", "Address:", "City:", "State:", "Zipcode:", "Phone Number:", "2nd Phone Number:"}

func row(start, paid, paidDate, due, status, isNew, vetted, last, first, email string) []string {
	return []string{start, paid, paidDate, due, status, isNew, vetted, last, first, email, "1 Example St", "Townsville", "MN", "55555", "555-0100", ""}
}

var sampleRows = [][]string{
	header,
	row("1/5/2024", "Y", "1/5/2026", "1/5/2027", "Active", "", "Y", "Example", "Alex", "Alex@Example.com"),
	row("10/1/2026", "Y", "10/1/2026", "10/1/2027", "Active", "Y", "", "Sample", "Sam", "sam@example.com"),
	row("", "", "", "1/1/2025", "Inactive", "", "Y", "Old", "Olive", "olive@example.com; olive2@example.com"),
	{},
	row("", "", "", "", "Active", "", "", "Nobody", "Nora", ""),
	row("3/3/2023", "Y", "", "3/3/2024", "Active", "", "", "Example", "Alex", "alex@example.com"),
}

func TestFindColumnsOnRealHeaders(t *testing.T) {
	cols, ok := FindColumns(sampleRows)
	if !ok {
		t.Fatal("no email column found")
	}
	want := map[Role]string{RoleStartDate: "A", RolePaid: "B", RolePaidDate: "C", RoleDueDate: "D", RoleStatus: "E",
		RoleNew: "F", RoleVetted: "G", RoleLastName: "H", RoleFirstName: "I", RoleEmail: "J"}
	for r, col := range want {
		if j, ok := cols.Index[r]; !ok || ColumnLetter(j) != col {
			t.Errorf("%s: got column %v, want %s", r, ColumnLetter(j), col)
		}
	}
	if len(cols.Index) != len(want) {
		t.Errorf("found extra roles (address/state/phone must be ignored): %v", cols.Headers)
	}
}

func TestHeaderBelowTitleRow(t *testing.T) {
	rows := append([][]string{{"APS Membership 2026"}, {}}, sampleRows...)
	cols, ok := FindColumns(rows)
	if !ok || cols.HeaderRow != 2 {
		t.Errorf("header row = %d, ok=%v", cols.HeaderRow, ok)
	}
	members, _ := Members(rows, cols)
	if members[0].Row != 4 {
		t.Errorf("first member row = %d, want 4 (as numbered in Google Sheets)", members[0].Row)
	}
}

func TestMembers(t *testing.T) {
	cols, _ := FindColumns(sampleRows)
	members, noEmail := Members(sampleRows, cols)
	if len(members) != 4 || noEmail != 1 {
		t.Fatalf("got %d members, %d without email", len(members), noEmail)
	}
	if got := Find(members, " OLIVE2@example.com "); len(got) != 1 || got[0].Name() != "Olive Old" {
		t.Errorf("second address in a cell not found: %+v", got)
	}
	if got := Find(members, "alex@example.com"); len(got) != 2 {
		t.Errorf("duplicates: got %d rows", len(got))
	}
}

func TestMemberStatus(t *testing.T) {
	cols, _ := FindColumns(sampleRows)
	members, _ := Members(sampleRows, cols)
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		email   string
		want    connectors.State
		summary string
		detail  string
	}{
		{"nobody@example.com", connectors.StateMissing, "Not on the membership list", ""},
		{"sam@example.com", connectors.StatePending, "application is still in progress", "Sam Sample (row 3)"},
		{"alex@example.com", connectors.StateOK, "vetted", "Listed more than once: rows 2, 7"},
		{"olive@example.com", connectors.StateOK, "vetted", "not marked Active"},
	}
	for _, tc := range cases {
		st := memberStatus(Find(members, tc.email), now)
		all := strings.Join(st.Details, "\n")
		if st.State != tc.want || !strings.Contains(st.Summary, tc.summary) || !strings.Contains(all, tc.detail) {
			t.Errorf("%s: got %v %q %v", tc.email, st.State, st.Summary, st.Details)
		}
		for _, private := range []string{"Example St", "Townsville", "555-0100", "55555"} {
			if strings.Contains(all+st.Summary, private) {
				t.Errorf("%s: report shows private column %q", tc.email, private)
			}
		}
	}
	olive := memberStatus(Find(members, "olive@example.com"), now)
	if !strings.Contains(strings.Join(olive.Details, " "), "has passed") {
		t.Errorf("past due date not noted: %v", olive.Details)
	}
}

func TestIsYesAndDates(t *testing.T) {
	for _, s := range []string{"Y", "yes", "TRUE", "x", "✓"} {
		if !IsYes(s) {
			t.Errorf("IsYes(%q) = false", s)
		}
	}
	for _, s := range []string{"", "N", "FALSE", "no", "pending"} {
		if IsYes(s) {
			t.Errorf("IsYes(%q) = true", s)
		}
	}
	for _, s := range []string{"1/5/2027", "2027-01-05", "Jan 5, 2027", "1/5/27"} {
		if d, ok := ParseDate(s); !ok || d.Year() != 2027 {
			t.Errorf("ParseDate(%q) = %v %v", s, d, ok)
		}
	}
	if ColumnLetter(0) != "A" || ColumnLetter(25) != "Z" || ColumnLetter(26) != "AA" {
		t.Error("ColumnLetter")
	}
}

func TestSpreadsheetIDAndPaths(t *testing.T) {
	id := "1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-abcd"
	for _, in := range []string{"https://docs.google.com/spreadsheets/d/" + id + "/edit#gid=0", id, " " + id + " "} {
		if got := SpreadsheetID(in); got != id {
			t.Errorf("SpreadsheetID(%q) = %q", in, got)
		}
	}
	if got := CleanPath(`'/tmp/My\ Key.json'`); got != "/tmp/My Key.json" {
		t.Errorf("CleanPath = %q", got)
	}
}

func TestParseKey(t *testing.T) {
	file, _ := testKey(t)
	k, err := ParseServiceAccountKey(file)
	if err != nil {
		t.Fatal(err)
	}
	compact := k.Compact()
	if strings.Contains(compact, "\n") || strings.Contains(compact, "auth_uri") {
		t.Errorf("Compact should be one line with only needed fields: %s", compact)
	}
	if _, err := ParseServiceAccountKey(compact); err != nil {
		t.Errorf("compact key doesn't parse: %v", err)
	}
	if ExplainStoredKey(compact, compact) != "" || ExplainStoredKey(file, compact) != "" {
		t.Error("same key should match, pretty or compact")
	}
	other, _ := testKey(t)
	for stored, want := range map[string]string{"": "empty", compact[:200]: "cut short", other: "different Google key"} {
		if got := ExplainStoredKey(stored, compact); !strings.Contains(got, want) {
			t.Errorf("ExplainStoredKey: got %q, want %q", got, want)
		}
	}
	for _, bad := range []string{"hello", `{"type":"authorized_user"}`} {
		if _, err := ParseServiceAccountKey(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// fakeGoogle is a token endpoint plus a Sheets API that checks the signed
// sign-in request and the access token, and only allows GET for Sheets.
func fakeGoogle(t *testing.T, pub *rsa.PublicKey, shared bool, tokenCalls *int32) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			atomic.AddInt32(tokenCalls, 1)
			r.ParseForm()
			parts := strings.Split(r.Form.Get("assertion"), ".")
			if len(parts) != 3 {
				w.WriteHeader(400)
				return
			}
			sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
			sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) != nil {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`))
				return
			}
			claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var c map[string]any
			json.Unmarshal(claims, &c)
			if c["scope"] != ReadOnlyScope || c["aud"] != srv.URL+"/token" {
				t.Errorf("bad sign-in claims: %v", c)
			}
			w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("made a %s request — the connector must be read-only", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		if !shared {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED"}}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/values/'Members'"):
			b, _ := json.Marshal(map[string]any{"values": sampleRows})
			w.Write(b)
		case strings.HasPrefix(r.URL.Path, "/v4/spreadsheets/"):
			w.Write([]byte(`{"properties":{"title":"Membership"},"sheets":[{"properties":{"title":"Members","sheetType":"GRID"}},{"properties":{"title":"Chart","sheetType":"OBJECT"}}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func connectorFor(t *testing.T, shared bool, calls *int32) *Connector {
	t.Helper()
	file, priv := testKey(t)
	srv := fakeGoogle(t, &priv.PublicKey, shared, calls)
	k, _ := ParseServiceAccountKey(file)
	k.TokenURI = srv.URL + "/token"
	store := secrets.NewMemory()
	store.Set(secrets.GoogleServiceAccountKey, k.Compact())
	c := New(config.Sheet{SpreadsheetID: "sheet-1", Tab: "Members"}, store)
	c.client.baseURL = srv.URL
	return c
}

func TestCheckMemberEndToEnd(t *testing.T) {
	var calls int32
	c := connectorFor(t, true, &calls)
	if m := c.Missing(); len(m) != 0 {
		t.Fatalf("Missing() = %v", m)
	}
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := c.CheckMember(context.Background(), "sam@example.com")
	if err != nil || st.State != connectors.StatePending {
		t.Errorf("got %+v, %v", st, err)
	}
	if calls != 1 {
		t.Errorf("signed in %d times; the access token should be reused", calls)
	}
	d, err := c.client.Discover(context.Background(), "sheet-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Tab != "Members" || len(d.Tabs) != 1 || d.Rows != 4 || d.Vetted != 2 || d.InProgress != 2 || d.Duplicates != 1 || d.NoEmail != 1 || d.Inactive != 1 {
		t.Errorf("discover = %+v", d)
	}
	notes := strings.Join(d.Notes, "\n")
	for _, private := range []string{"example.com", "Alex", "Example St"} {
		if strings.Contains(notes, private) {
			t.Errorf("discover notes show private data %q", private)
		}
	}
}

func TestNotSharedIsExplained(t *testing.T) {
	var calls int32
	c := connectorFor(t, false, &calls)
	err := c.TestConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Share") || !strings.Contains(err.Error(), "reader@aps-tools-test.iam.gserviceaccount.com") {
		t.Errorf("err = %v", err)
	}
}

func TestMissing(t *testing.T) {
	m := New(config.Sheet{}, secrets.NewMemory()).Missing()
	if len(m) != 2 || !strings.Contains(m[1], "aps-tools setup sheet") {
		t.Errorf("Missing() = %v", m)
	}
}

func TestTabs(t *testing.T) {
	tabs := []string{"Renewals", "Member Directory May 2026", "Member Directory Jan 2025", "Board 2026"}
	if got := SuggestTab(tabs, ""); got != "Member Directory May 2026" {
		t.Errorf("SuggestTab = %q", got)
	}
	if got := SuggestTab(tabs, "Renewals"); got != "Renewals" {
		t.Errorf("a saved tab should be kept, got %q", got)
	}
	if got := NewerTab(tabs, "Member Directory May 2026"); got != "" {
		t.Errorf("no newer tab, got %q", got)
	}
	tabs = append(tabs, "Member Directory November 2026")
	if got := NewerTab(tabs, "Member Directory May 2026"); got != "Member Directory November 2026" {
		t.Errorf("NewerTab = %q", got)
	}
	if got := SuggestTab(tabs, "Member Directory May 2026"); got != "Member Directory November 2026" {
		t.Errorf("SuggestTab should offer the newer copy, got %q", got)
	}
	if got := NewerTab([]string{"Board 2025", "Member Directory 2027"}, "Board 2025"); got != "" {
		t.Errorf("different list names must not match, got %q", got)
	}
}
