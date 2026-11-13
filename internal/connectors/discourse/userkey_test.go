package discourse

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"strings"
	"testing"
)

// forumLocks does what Discourse does after "Authorize": read the public
// key from the approval link, lock the answer with it, and show it as
// base64 broken into 60-character lines (Ruby's Base64.encode64).
func forumLocks(t *testing.T, link, payload string, oaep bool) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(u.Query().Get("public_key")))
	if block == nil {
		t.Fatal("approval link has no PEM public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	var locked []byte
	if oaep {
		locked, err = rsa.EncryptOAEP(sha1.New(), rand.Reader, pub.(*rsa.PublicKey), []byte(payload), nil)
	} else {
		locked, err = rsa.EncryptPKCS1v15(rand.Reader, pub.(*rsa.PublicKey), []byte(payload))
	}
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(locked)
	var lines []string
	for len(b64) > 60 {
		lines, b64 = append(lines, b64[:60]), b64[60:]
	}
	return strings.Join(append(lines, b64), "\n") + "\n"
}

func TestUserKeyURL(t *testing.T) {
	req, err := NewUserKeyRequest("client-1")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(req.URL("forum.example.org/latest"))
	if u.Scheme+"://"+u.Host+u.Path != "https://forum.example.org/user-api-key/new" {
		t.Errorf("URL = %s", u)
	}
	q := u.Query()
	if q.Get("scopes") != "read" {
		t.Errorf("must ask only for read access, asked for %q", q.Get("scopes"))
	}
	if q.Get("client_id") != "client-1" || q.Get("nonce") == "" || !strings.Contains(q.Get("application_name"), "aps-tools") {
		t.Errorf("query = %v", q)
	}
	if q.Has("auth_redirect") {
		t.Error("no redirect: the forum should show the text for the webmaster to paste")
	}
}

func TestUnlock(t *testing.T) {
	for _, oaep := range []bool{false, true} {
		req, _ := NewUserKeyRequest("client-1")
		link := req.URL("forum.example.org")
		nonce, _ := url.Parse(link)
		payload := `{"key":"abc123","nonce":"` + nonce.Query().Get("nonce") + `","push":false,"api":4}`
		shown := forumLocks(t, link, payload, oaep)

		if key, err := req.Unlock(shown); err != nil || key != "abc123" {
			t.Errorf("oaep=%v: Unlock = %q, %v", oaep, key, err)
		}
		// Copied with a few words around it.
		if key, err := req.Unlock("Paste this into the app:\n" + shown + "Thanks"); err != nil || key != "abc123" {
			t.Errorf("oaep=%v: with extra words: %q, %v", oaep, key, err)
		}
	}
}

func TestUnlockRejects(t *testing.T) {
	req, _ := NewUserKeyRequest("client-1")
	link := req.URL("forum.example.org")

	if _, err := req.Unlock("hello"); err == nil {
		t.Error("accepted text that isn't from the forum")
	}

	stale := forumLocks(t, link, `{"key":"abc123","nonce":"old","api":4}`, false)
	if _, err := req.Unlock(stale); err == nil || !strings.Contains(err.Error(), "older approval") {
		t.Errorf("wrong nonce: err = %v", err)
	}

	other, _ := NewUserKeyRequest("client-1")
	nonce, _ := url.Parse(link)
	forOther := forumLocks(t, other.URL("forum.example.org"), `{"key":"abc123","nonce":"`+nonce.Query().Get("nonce")+`"}`, false)
	if _, err := req.Unlock(forOther); err == nil || !strings.Contains(err.Error(), "wasn't made for this approval") {
		t.Errorf("other key pair: err = %v", err)
	}
}

func TestExplainStoredKey(t *testing.T) {
	key := strings.Repeat("a1", 16)
	scrambled := strings.Repeat("aGVsbG8gd29ybGQh", 22) + "\n" + "Zm9v"
	cases := map[string]struct{ stored, want string }{
		"match":          {key, ""},
		"match, newline": {key + "\n", ""},
		"empty":          {"", "empty"},
		"forum text":     {scrambled, "scrambled text from the forum page"},
		"other field":    {"alex", "4 characters; the key is 32"},
		"older key":      {strings.Repeat("b2", 16), "different key"},
	}
	for name, tc := range cases {
		got := ExplainStoredKey(tc.stored, key)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want it to mention %q", name, got, tc.want)
		}
		if got != "" && strings.Contains(got, key) {
			t.Errorf("%s: message must never show the key", name)
		}
	}
}
