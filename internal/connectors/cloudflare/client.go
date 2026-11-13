package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// This file is the low-level conversation with Cloudflare's API. Every call
// is a read-only GET. API reference: https://developers.cloudflare.com/api/

// apiBase is Cloudflare's API address. Tests point the client elsewhere.
const apiBase = "https://api.cloudflare.com/client/v4"

// Client makes read-only calls to the Cloudflare API with an API token.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a client for the given API token.
func NewClient(token string) *Client {
	return &Client{
		token:      strings.TrimSpace(token),
		baseURL:    apiBase,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// envelope is the wrapper Cloudflare puts around every JSON answer.
type envelope struct {
	Success    bool            `json:"success"`
	Errors     []apiMessage    `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int    `json:"page"`
		TotalPages int    `json:"total_pages"`
		Cursor     string `json:"cursor"`
	} `json:"result_info"`
}

type apiMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// get fetches one JSON page and decodes its "result" into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (envelope, error) {
	body, _, err := c.raw(ctx, path, query)
	if err != nil {
		return envelope{}, err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return envelope{}, errors.New("Cloudflare sent back something unexpected")
	}
	if !env.Success {
		return env, explainAPIErrors(env.Errors)
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return env, fmt.Errorf("couldn't understand Cloudflare's answer: %w", err)
		}
	}
	return env, nil
}

// raw performs a GET and returns the body and its content type. Non-2xx
// answers are turned into plain-English errors.
func (c *Client) raw(ctx context.Context, path string, query url.Values) ([]byte, string, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("couldn't build a request for Cloudflare: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", errors.New("couldn't reach Cloudflare — check your internet connection")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", errors.New("the connection to Cloudflare dropped — try again")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Cloudflare usually explains itself in a JSON envelope.
		var env envelope
		if json.Unmarshal(body, &env) == nil && len(env.Errors) > 0 {
			return nil, "", explainAPIErrors(env.Errors)
		}
		return nil, "", explainStatus(resp.StatusCode)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// explainAPIErrors turns Cloudflare's error list into one actionable message.
func explainAPIErrors(errs []apiMessage) error {
	if len(errs) == 0 {
		return errors.New("Cloudflare reported a problem without details")
	}
	e := errs[0]
	switch e.Code {
	case 1000, 6003, 6111:
		return errors.New("Cloudflare rejected the API token — it may be mistyped, expired, or deleted. See docs/setup/cloudflare.md, then run: aps-tools setup cloudflare")
	case 9109, 10000:
		return errors.New("the Cloudflare API token is missing a permission this needs — compare it with the list in docs/setup/cloudflare.md")
	}
	return fmt.Errorf("Cloudflare: %s (code %d)", e.Message, e.Code)
}

func explainStatus(code int) error {
	switch {
	case code == http.StatusUnauthorized:
		return errors.New("Cloudflare rejected the API token — see docs/setup/cloudflare.md, then run: aps-tools setup cloudflare")
	case code == http.StatusForbidden:
		return errors.New("the Cloudflare API token is missing a permission this needs — see docs/setup/cloudflare.md")
	case code == http.StatusNotFound:
		return errors.New("Cloudflare couldn't find that — check the account and zone (run: aps-tools setup cloudflare)")
	case code == http.StatusTooManyRequests:
		return errors.New("Cloudflare is limiting how fast requests can be made — wait a minute and try again")
	case code >= 500:
		return fmt.Errorf("Cloudflare is having problems right now (error %d) — try again later, or check cloudflarestatus.com", code)
	default:
		return fmt.Errorf("Cloudflare returned an unexpected error (HTTP %d)", code)
	}
}

// ---- Calls used by the tool ----

// VerifyToken confirms the token is valid and active.
//
// Cloudflare has two kinds of token: "user" tokens (My Profile → API Tokens)
// and "account" tokens (Manage Account → Account API Tokens). Each has its
// own verify address, so we try the user one first and then, if we know the
// account, the account one.
func (c *Client) VerifyToken(ctx context.Context, accountID string) error {
	var r struct {
		Status string `json:"status"`
	}
	_, err := c.get(ctx, "/user/tokens/verify", nil, &r)
	if err != nil && accountID != "" {
		r.Status = ""
		_, err = c.get(ctx, "/accounts/"+url.PathEscape(accountID)+"/tokens/verify", nil, &r)
	}
	if err != nil {
		return err
	}
	if r.Status != "active" {
		return fmt.Errorf("the Cloudflare API token is %q, not active — create a new one (docs/setup/cloudflare.md)", r.Status)
	}
	return nil
}

// Zone is one domain on Cloudflare.
type Zone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"account"`
}

// ListZones returns every domain the token can see.
func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	var all []Zone
	for page := 1; ; page++ {
		var zones []Zone
		env, err := c.get(ctx, "/zones", url.Values{"page": {fmt.Sprint(page)}, "per_page": {"50"}}, &zones)
		if err != nil {
			return nil, err
		}
		all = append(all, zones...)
		if page >= env.ResultInfo.TotalPages || len(zones) == 0 {
			return all, nil
		}
	}
}

// Address is an Email Routing destination address: an inbox Cloudflare is
// allowed to forward mail to, once its owner has verified it.
type Address struct {
	Email    string `json:"email"`
	Created  string `json:"created"`
	Verified string `json:"verified"` // empty = not verified yet
}

// IsVerified reports whether the owner clicked Cloudflare's verification link.
func (a Address) IsVerified() bool { return a.Verified != "" }

// ListAddresses returns every destination address in the account.
func (c *Client) ListAddresses(ctx context.Context, accountID string) ([]Address, error) {
	var all []Address
	for page := 1; ; page++ {
		var addrs []Address
		env, err := c.get(ctx, "/accounts/"+url.PathEscape(accountID)+"/email/routing/addresses",
			url.Values{"page": {fmt.Sprint(page)}, "per_page": {"50"}}, &addrs)
		if err != nil {
			return nil, err
		}
		all = append(all, addrs...)
		if page >= env.ResultInfo.TotalPages || len(addrs) == 0 {
			return all, nil
		}
	}
}

// RoutingSettings says whether Email Routing is switched on for a zone.
type RoutingSettings struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

// GetRoutingSettings fetches a zone's Email Routing status.
func (c *Client) GetRoutingSettings(ctx context.Context, zoneID string) (RoutingSettings, error) {
	var s RoutingSettings
	_, err := c.get(ctx, "/zones/"+url.PathEscape(zoneID)+"/email/routing", nil, &s)
	return s, err
}

// Rule is one Email Routing rule: "mail to X → do Y".
type Rule struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Matchers []struct {
		Type  string `json:"type"`  // "literal" or "all"
		Field string `json:"field"` // "to"
		Value string `json:"value"` // the address, for literal matchers
	} `json:"matchers"`
	Actions []struct {
		Type  string   `json:"type"` // "forward", "worker" or "drop"
		Value []string `json:"value"`
	} `json:"actions"`
}

// ListRules returns a zone's routing rules.
func (c *Client) ListRules(ctx context.Context, zoneID string) ([]Rule, error) {
	var all []Rule
	for page := 1; ; page++ {
		var rules []Rule
		env, err := c.get(ctx, "/zones/"+url.PathEscape(zoneID)+"/email/routing/rules",
			url.Values{"page": {fmt.Sprint(page)}, "per_page": {"50"}}, &rules)
		if err != nil {
			return nil, err
		}
		all = append(all, rules...)
		if page >= env.ResultInfo.TotalPages || len(rules) == 0 {
			return all, nil
		}
	}
}

// GetCatchAll returns the rule for mail to any address with no other rule.
func (c *Client) GetCatchAll(ctx context.Context, zoneID string) (Rule, error) {
	var r Rule
	_, err := c.get(ctx, "/zones/"+url.PathEscape(zoneID)+"/email/routing/rules/catch_all", nil, &r)
	return r, err
}

// Binding connects a Worker to storage or a setting (KV, D1, a variable…).
type Binding struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // e.g. "kv_namespace", "plain_text", "secret_text", "d1"
	NamespaceID string `json:"namespace_id"`
	Text        string `json:"text"` // value of a plain_text variable (never set for secrets)
	ID          string `json:"id"`   // e.g. D1 database id
}

// GetWorkerBindings returns what storage and settings a Worker can use.
func (c *Client) GetWorkerBindings(ctx context.Context, accountID, script string) ([]Binding, error) {
	var s struct {
		Bindings []Binding `json:"bindings"`
	}
	_, err := c.get(ctx, "/accounts/"+url.PathEscape(accountID)+"/workers/scripts/"+url.PathEscape(script)+"/settings", nil, &s)
	return s.Bindings, err
}

// Module is one file of a Worker's code.
type Module struct {
	Name string
	Code string
}

// GetWorkerCode downloads a Worker's code. Workers made of several files
// come back as a multipart message; each part is one file.
func (c *Client) GetWorkerCode(ctx context.Context, accountID, script string) ([]Module, error) {
	body, ctype, err := c.raw(ctx, "/accounts/"+url.PathEscape(accountID)+"/workers/scripts/"+url.PathEscape(script)+"/content/v2", nil)
	if err != nil {
		return nil, err
	}
	mediaType, params, _ := mime.ParseMediaType(ctype)
	if !strings.HasPrefix(mediaType, "multipart/") {
		return []Module{{Name: script, Code: string(body)}}, nil
	}
	var mods []Module
	r := multipart.NewReader(strings.NewReader(string(body)), params["boundary"])
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			return mods, nil
		}
		if err != nil {
			return mods, fmt.Errorf("couldn't read the Worker's code: %w", err)
		}
		b, _ := io.ReadAll(part)
		name := part.FileName()
		if name == "" {
			name = part.FormName()
		}
		mods = append(mods, Module{Name: name, Code: string(b)})
	}
}

// CountKVKeys counts the keys in a KV namespace (up to limit) and returns
// the first few key names, so discovery can show what's stored there.
func (c *Client) CountKVKeys(ctx context.Context, accountID, namespaceID string, limit int) (int, []string, error) {
	var keys []struct {
		Name string `json:"name"`
	}
	path := "/accounts/" + url.PathEscape(accountID) + "/storage/kv/namespaces/" + url.PathEscape(namespaceID) + "/keys"
	total, cursor := 0, ""
	var sample []string
	for total < limit {
		q := url.Values{"limit": {"1000"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		env, err := c.get(ctx, path, q, &keys)
		if err != nil {
			return total, sample, err
		}
		for _, k := range keys {
			if len(sample) < 5 {
				sample = append(sample, k.Name)
			}
		}
		total += len(keys)
		cursor = env.ResultInfo.Cursor
		if cursor == "" || len(keys) == 0 {
			break
		}
	}
	return total, sample, nil
}
