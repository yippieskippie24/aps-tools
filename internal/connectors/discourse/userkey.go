package discourse

// This file creates a Discourse "user API key": a key the webmaster
// approves in their web browser, like letting a phone app sign in. Unlike
// admin API keys (Pro plans and higher), user API keys work on every plan.
// Why we use it: docs/decisions/0008-discourse-user-api-key.md.
//
// How the hand-shake works (Discourse's "User API keys specification"):
//  1. The tool makes a fresh key pair: a private half it keeps in memory
//     and a public half it sends to the forum.
//  2. The webmaster opens a forum page (URL below), checks it asks only for
//     "read" access, and clicks Authorize.
//  3. The forum locks the new key with the public half and shows the locked
//     text. The webmaster pastes it back into the tool.
//  4. Only the tool's private half can unlock it, so the key is never shown
//     in readable form anywhere along the way.

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"hash"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// UserKeyScopes is what the tool asks for: "read" lets it look (GET
// requests) with the webmaster's own permissions, and nothing else.
const UserKeyScopes = "read"

// UserKeyAppName is how the tool appears on the forum's approval page and
// in the webmaster's list of authorized apps (where it can be revoked).
const UserKeyAppName = "aps-tools (APS Webmaster tool, read-only)"

// NewClientID returns a random ID that names this computer's copy of the
// tool to the forum. Setup saves it, so approving again later replaces the
// old key instead of piling up new ones.
func NewClientID() string { return randomHex(16) }

// UserKeyRequest is one in-progress approval. Make one with NewUserKeyRequest,
// send the webmaster to URL, then pass what they paste to Unlock.
type UserKeyRequest struct {
	ClientID string
	nonce    string
	private  *rsa.PrivateKey
}

// NewUserKeyRequest creates a fresh key pair for one approval.
func NewUserKeyRequest(clientID string) (*UserKeyRequest, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errors.New("couldn't create a key pair: " + err.Error())
	}
	return &UserKeyRequest{ClientID: clientID, nonce: randomHex(12), private: priv}, nil
}

// URL is the forum page where the webmaster approves the key.
func (r *UserKeyRequest) URL(siteURL string) string {
	der, _ := x509.MarshalPKIXPublicKey(&r.private.PublicKey)
	pub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	q := url.Values{
		"application_name": {UserKeyAppName},
		"client_id":        {r.ClientID},
		"scopes":           {UserKeyScopes},
		"public_key":       {string(pub)},
		"nonce":            {r.nonce},
	}
	return CleanURL(siteURL) + "/user-api-key/new?" + q.Encode()
}

// base64Word matches one piece of base64 text.
var base64Word = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

// lockedCandidates finds the locked key in what was pasted. The forum
// shows it as several lines of base64; pasting may turn the line breaks
// into spaces, and the webmaster may have copied a few words around it.
// So: split into words and try each run of consecutive base64-looking
// words that decodes to exactly one locked block.
func (r *UserKeyRequest) lockedCandidates(pasted string) [][]byte {
	words := strings.Fields(pasted)
	var out [][]byte
	for start := range words {
		joined := ""
		for end := start; end < len(words) && base64Word.MatchString(words[end]); end++ {
			joined += words[end]
			if len(joined) > 4*r.private.Size()/3+4 {
				break // already longer than one locked block
			}
			if b, err := base64.StdEncoding.DecodeString(joined); err == nil && len(b) == r.private.Size() {
				out = append(out, b)
			}
		}
	}
	return out
}

// Unlock decodes the text the forum showed and returns the user API key.
func (r *UserKeyRequest) Unlock(pasted string) (string, error) {
	for _, locked := range r.lockedCandidates(pasted) {
		plain, err := r.decrypt(locked)
		if err != nil {
			continue
		}
		var p struct {
			Key   string `json:"key"`
			Nonce string `json:"nonce"`
		}
		if err := json.Unmarshal(plain, &p); err != nil || p.Key == "" {
			return "", errors.New("the forum's answer didn't contain a key — try approving again")
		}
		if p.Nonce != r.nonce {
			return "", errors.New("that text is from an older approval — paste the text from the page you just approved")
		}
		return p.Key, nil
	}
	if len(r.lockedCandidates(pasted)) > 0 {
		return "", errors.New("that text wasn't made for this approval — paste the text from the page you just approved (if you approved twice, use the newest)")
	}
	return "", errors.New("that doesn't look like the forum's text — copy the whole block shown after you click Authorize")
}

// decrypt tries each padding style Discourse may use (it has changed over
// the years, and newer forums let the app choose).
func (r *UserKeyRequest) decrypt(locked []byte) ([]byte, error) {
	if plain, err := rsa.DecryptPKCS1v15(nil, r.private, locked); err == nil {
		return plain, nil
	}
	for _, h := range []func() hash.Hash{sha1.New, sha256.New} {
		if plain, err := rsa.DecryptOAEP(h(), nil, r.private, locked, nil); err == nil {
			return plain, nil
		}
	}
	return nil, errors.New("couldn't unlock")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ExplainStoredKey says, in plain English, why the value read back from
// 1Password isn't the key setup just received, without showing either.
// Returns "" when they match.
func ExplainStoredKey(stored, want string) string {
	stored = strings.TrimSpace(stored)
	switch {
	case stored == want:
		return ""
	case stored == "":
		return "That 1Password field is empty. Paste the key into the item's credential field and save it"
	case len(stored) > 100 && base64Word.MatchString(strings.Join(strings.Fields(stored), "")):
		return "That item holds the scrambled text from the forum page, not the key. The key is the short code the tool just put on your clipboard. Paste that over it"
	case len(stored) != len(want):
		return "That field holds something else (" + strconv.Itoa(len(stored)) + " characters; the key is " + strconv.Itoa(len(want)) + "). Check you copied the reference for the field you pasted the key into"
	default:
		return "That item holds a different key, probably from an earlier try. Each approval makes a new key, so paste the one on your clipboard now over it"
	}
}
