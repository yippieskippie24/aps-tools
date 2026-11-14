package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// writableCloudflare answers GETs from routes and records every change
// (method and path, plus the body of a POST). failPost makes adding an
// address fail.
func writableCloudflare(t *testing.T, routes map[string]string, failPost bool) (*Connector, *[]string) {
	t.Helper()
	var changes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			body, ok := routes[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"success":false,"errors":[{"code":7003,"message":"Could not route"}]}`))
				return
			}
			w.Write([]byte(body))
		case http.MethodDelete:
			changes = append(changes, "DELETE "+r.URL.Path)
			w.Write([]byte(`{"success":true,"result":{}}`))
		case http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			var body map[string]string
			_ = json.Unmarshal(b, &body)
			changes = append(changes, "POST "+r.URL.Path+" "+body["email"])
			if failPost {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"success":false,"errors":[{"code":2020,"message":"Too many verification emails"}]}`))
				return
			}
			w.Write([]byte(`{"success":true,"result":{"id":"new1","email":"` + body["email"] + `"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	store := secrets.NewMemory()
	_ = store.Set(secrets.CloudflareAPIToken, "test-token")
	c := New(config.Cloudflare{AccountID: "acct", ZoneID: "zone", ZoneName: "example.org"}, store)
	c.client.baseURL = srv.URL
	return c, &changes
}

func resendRoutes(addresses, rules string) map[string]string {
	return map[string]string{
		"/accounts/acct/email/routing/addresses":    `{"success":true,"result":` + addresses + `,"result_info":{"page":1,"total_pages":1}}`,
		"/zones/zone/email/routing/rules":           `{"success":true,"result":` + rules + `,"result_info":{"page":1,"total_pages":1}}`,
		"/zones/zone/email/routing/rules/catch_all": `{"success":true,"result":{"name":"","actions":[{"type":"drop"}]}}`,
	}
}

const pendingAddr = `[{"id":"a1","email":"Pat@Example.com","created":"2026-09-28T00:00:00Z","verified":null}]`

func TestResendVerification(t *testing.T) {
	c, changes := writableCloudflare(t, resendRoutes(pendingAddr, `[]`), false)
	did, err := c.ResendVerification(context.Background(), "pat@example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := "DELETE /accounts/acct/email/routing/addresses/a1|POST /accounts/acct/email/routing/addresses Pat@Example.com"
	if strings.Join(*changes, "|") != want {
		t.Errorf("changes = %v, want remove then add", *changes)
	}
	if !strings.Contains(did, "new verification email") {
		t.Errorf("did = %q", did)
	}
}

func TestResendRefusesWhenUnsafe(t *testing.T) {
	cases := map[string]struct {
		addrs, rules, want string
	}{
		"already verified": {`[{"id":"a1","email":"pat@example.com","verified":"2026-10-01T00:00:00Z"}]`, `[]`, "already verified"},
		"not an address":   {`[]`, `[]`, "aren't a destination address"},
		"used by a rule":   {pendingAddr, `[{"name":"Treasurer","actions":[{"type":"forward","value":["pat@example.com"]}]}]`, `"Treasurer"`},
	}
	for name, tc := range cases {
		c, changes := writableCloudflare(t, resendRoutes(tc.addrs, tc.rules), false)
		_, err := c.ResendVerification(context.Background(), "pat@example.com")
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(*changes) != 0 {
			t.Errorf("%s: err %v, changes %v", name, err, *changes)
		}
	}
}

func TestResendExplainsAFailedReAdd(t *testing.T) {
	c, _ := writableCloudflare(t, resendRoutes(pendingAddr, `[]`), true)
	_, err := c.ResendVerification(context.Background(), "pat@example.com")
	if err == nil || !strings.Contains(err.Error(), "Add it back by hand") || !strings.Contains(err.Error(), "Too many verification emails") {
		t.Errorf("err = %v", err)
	}
}
