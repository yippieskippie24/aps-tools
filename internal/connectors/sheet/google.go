package sheet

// This file signs in to Google as a "service account": a robot Google
// account that belongs to a free Google Cloud project. The membership sheet
// is shared with the robot's email address (as Viewer), so the tool can read
// it without anyone's personal password. Why: docs/decisions/0009-google-service-account.md.
//
// How the sign-in works (Google's "OAuth 2.0 for server to server"):
//  1. The tool writes a short note saying "I am <robot email>, I want
//     read-only access to spreadsheets, valid for one hour", and signs it
//     with the robot's private key (from the JSON key file).
//  2. Google checks the signature and hands back an access token that
//     lasts an hour.
//  3. Every Sheets request carries that token.
//
// Only the standard library is used, so there are no extra dependencies.

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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ReadOnlyScope lets the tool read spreadsheets, never change them.
const ReadOnlyScope = "https://www.googleapis.com/auth/spreadsheets.readonly"

const defaultTokenURL = "https://oauth2.googleapis.com/token"

// ServiceAccountKey is the part of Google's JSON key file the tool needs.
type ServiceAccountKey struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id,omitempty"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri,omitempty"`

	signer *rsa.PrivateKey
}

// ParseServiceAccountKey reads a key file's contents and checks it's usable.
func ParseServiceAccountKey(data string) (ServiceAccountKey, error) {
	var k ServiceAccountKey
	if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &k); err != nil {
		return k, errors.New("that isn't a Google key file (it should be a .json file that starts with {)")
	}
	if k.Type != "service_account" || k.ClientEmail == "" || k.PrivateKey == "" {
		return k, errors.New("that JSON file isn't a service account key — in Google Cloud, open the service account → Keys → Add key → Create new key → JSON")
	}
	block, _ := pem.Decode([]byte(k.PrivateKey))
	if block == nil {
		return k, errors.New("the key file's private key is damaged — create a new key in Google Cloud")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if k1, err1 := x509.ParsePKCS1PrivateKey(block.Bytes); err1 == nil {
			parsed = k1
		} else {
			return k, errors.New("the key file's private key can't be read — create a new key in Google Cloud")
		}
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return k, errors.New("the key file has an unexpected kind of private key — create a new JSON key in Google Cloud")
	}
	k.signer = rsaKey
	return k, nil
}

// Compact returns the key as one line of JSON with only the fields the tool
// needs. That's what gets stored, because it's smaller (the Keychain has a
// size limit) and pastes cleanly into a single 1Password field.
func (k ServiceAccountKey) Compact() string {
	b, _ := json.Marshal(ServiceAccountKey{
		Type: k.Type, ProjectID: k.ProjectID, PrivateKeyID: k.PrivateKeyID,
		PrivateKey: k.PrivateKey, ClientEmail: k.ClientEmail, TokenURI: k.TokenURI,
	})
	return string(b)
}

// tokenSource gets and caches access tokens for one service account.
type tokenSource struct {
	key        ServiceAccountKey
	tokenURL   string
	httpClient *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func newTokenSource(key ServiceAccountKey, httpClient *http.Client) *tokenSource {
	u := key.TokenURI
	if u == "" {
		u = defaultTokenURL
	}
	return &tokenSource{key: key, tokenURL: u, httpClient: httpClient}
}

// Token returns a valid access token, signing in again when needed.
func (t *tokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" && time.Now().Before(t.expires.Add(-time.Minute)) {
		return t.token, nil
	}
	assertion, err := t.signedAssertion(time.Now())
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", errors.New("couldn't reach Google to sign in — check your internet connection")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &r)
	if resp.StatusCode != http.StatusOK || r.AccessToken == "" {
		return "", explainTokenError(resp.StatusCode, r.Error, r.Description)
	}
	t.token = r.AccessToken
	t.expires = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	return t.token, nil
}

func explainTokenError(code int, errCode, desc string) error {
	d := strings.ToLower(desc)
	switch {
	case strings.Contains(d, "invalid jwt signature"), strings.Contains(d, "account not found"), errCode == "invalid_client":
		return errors.New("Google no longer accepts this key (it may have been deleted, or the service account removed) — create a new key and run: aps-tools setup sheet")
	case strings.Contains(d, "short-lived"), strings.Contains(d, "expired"), strings.Contains(d, "clock"):
		return errors.New("Google rejected the sign-in because this Mac's clock looks wrong — check the date and time in System Settings")
	case desc != "":
		return fmt.Errorf("Google wouldn't sign in the service account: %s", desc)
	}
	return fmt.Errorf("Google wouldn't sign in the service account (HTTP %d)", code)
}

// signedAssertion builds the signed note (a "JWT") described above.
func (t *tokenSource) signedAssertion(now time.Time) (string, error) {
	if t.key.signer == nil {
		return "", errors.New("the service account key hasn't been loaded")
	}
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": t.key.PrivateKeyID})
	claims, _ := json.Marshal(map[string]any{
		"iss":   t.key.ClientEmail,
		"scope": ReadOnlyScope,
		"aud":   t.tokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	unsigned := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, t.key.signer, crypto.SHA256, sum[:])
	if err != nil {
		return "", errors.New("couldn't sign the Google sign-in request: " + err.Error())
	}
	return unsigned + "." + enc.EncodeToString(sig), nil
}

// ExplainStoredKey says, in plain English, why the value read back from
// 1Password isn't the key setup is saving, without showing either.
// Returns "" when they're the same key.
func ExplainStoredKey(stored, want string) string {
	w, _ := ParseServiceAccountKey(want)
	s, err := ParseServiceAccountKey(stored)
	switch {
	case strings.TrimSpace(stored) == "":
		return "That 1Password field is empty. Paste the key into the item's credential field and save it"
	case err != nil:
		return "That field doesn't hold the whole key (it may have been cut short, or it's a different field). Paste the key from your clipboard over it"
	case s.ClientEmail != w.ClientEmail || s.PrivateKeyID != w.PrivateKeyID || s.PrivateKey != w.PrivateKey:
		return "That item holds a different Google key, probably from an earlier try. Paste the one on your clipboard over it"
	}
	return ""
}
