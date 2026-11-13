package discourse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// This file is the low-level conversation with a Discourse forum's API.
// Every call is a read-only GET. API reference: https://docs.discourse.org

// Client makes read-only calls to one Discourse forum with an admin API key.
type Client struct {
	baseURL    string // e.g. "https://forum.example.org"
	username   string // the API key's owner (Api-Username header)
	key        string
	httpClient *http.Client
}

// NewClient creates a client. siteURL may be pasted with or without
// https:// and with a trailing path; it is cleaned up.
func NewClient(siteURL, username, key string) *Client {
	return &Client{
		baseURL:    CleanURL(siteURL),
		username:   strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(username), "@")),
		key:        strings.TrimSpace(key),
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// CleanURL turns "forum.example.org/latest" into "https://forum.example.org".
func CleanURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimRight(s, "/")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host)
}

// get performs a GET and decodes the JSON answer into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("couldn't build a request for Discourse: %w", err)
	}
	req.Header.Set("Api-Key", c.key)
	req.Header.Set("Api-Username", c.username)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s — check your internet connection and the forum address", c.baseURL)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err := explainStatus(resp.StatusCode, body); err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("Discourse sent back something unexpected (is %s really the forum address?)", c.baseURL)
	}
	return nil
}

func explainStatus(code int, body []byte) error {
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusForbidden, code == http.StatusUnauthorized:
		return errors.New("Discourse refused the API key — check it was made for your admin username with the \"Read-only\" scope (docs/setup/discourse.md), then run: aps-tools setup discourse")
	case code == http.StatusNotFound:
		// Discourse answers 404 (not 403) for admin pages the key can't see.
		return errors.New("Discourse couldn't find that — check the forum address, and that the API key belongs to an ADMIN account (run: aps-tools setup discourse)")
	case code == http.StatusTooManyRequests:
		return errors.New("Discourse is limiting how fast requests can be made — wait a minute and try again")
	case code >= 500:
		return fmt.Errorf("the forum is having problems right now (error %d) — try again later", code)
	default:
		var e struct {
			Errors []string `json:"errors"`
		}
		if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 {
			return fmt.Errorf("Discourse: %s", strings.Join(e.Errors, "; "))
		}
		return fmt.Errorf("Discourse returned an unexpected error (HTTP %d)", code)
	}
}

// ---- Calls used by the tool ----

// Profile is the public part of a user's profile.
type Profile struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Admin    bool   `json:"admin"`
}

// GetProfile reads a user's profile by username.
func (c *Client) GetProfile(ctx context.Context, username string) (Profile, error) {
	var r struct {
		User Profile `json:"user"`
	}
	err := c.get(ctx, "/u/"+url.PathEscape(username)+".json", nil, &r)
	return r.User, err
}

// Me returns the API key owner's profile and checks they're an admin.
func (c *Client) Me(ctx context.Context) (Profile, error) {
	p, err := c.GetProfile(ctx, c.username)
	if err != nil {
		return p, err
	}
	if !p.Admin {
		return p, fmt.Errorf("%s isn't a forum admin — the API key must belong to an admin account", c.username)
	}
	return p, nil
}

// AdminUser is a user as seen from the admin pages.
type AdminUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	// Active and Approved are pointers so a field the forum leaves out
	// counts as "yes" rather than wrongly flagging every member.
	Active        *bool  `json:"active"`   // false = email address not confirmed yet
	Approved      *bool  `json:"approved"` // only matters if new users need approval
	Staged        bool   `json:"staged"`   // placeholder account, never signed up
	CreatedAt     string `json:"created_at"`
	LastSeenAt    string `json:"last_seen_at"`
	SuspendedTill string `json:"suspended_till"`
	SilencedTill  string `json:"silenced_till"`
	Groups        []struct {
		Name      string `json:"name"`
		Automatic bool   `json:"automatic"`
	} `json:"groups"`
}

// IsActive reports whether the user confirmed their email (missing = yes).
func (u AdminUser) IsActive() bool { return u.Active == nil || *u.Active }

// IsApproved reports whether the account is approved (missing = yes).
func (u AdminUser) IsApproved() bool { return u.Approved == nil || *u.Approved }

// FindUserByEmail returns the account with this email, or ok=false.
// We deliberately don't ask Discourse to *show* emails (that would add an
// entry to the staff action log on every check); filtering is enough.
func (c *Client) FindUserByEmail(ctx context.Context, email string) (AdminUser, bool, error) {
	var rows []AdminUser
	if err := c.get(ctx, "/admin/users.json", url.Values{"email": {email}}, &rows); err != nil {
		return AdminUser{}, false, err
	}
	if len(rows) == 0 {
		return AdminUser{}, false, nil
	}
	// The list row lacks some fields (approval, groups); fetch the full record.
	var full AdminUser
	if err := c.get(ctx, fmt.Sprintf("/admin/users/%d.json", rows[0].ID), nil, &full); err != nil {
		return AdminUser{}, false, err
	}
	return full, true, nil
}

// Invite is one email invitation.
type Invite struct {
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	Expired   bool   `json:"expired"`
}

// Invites lists the API key owner's invites. filter is "pending" or "expired";
// search narrows by email (empty = all).
func (c *Client) Invites(ctx context.Context, filter, search string) ([]Invite, error) {
	q := url.Values{"filter": {filter}}
	if search != "" {
		q.Set("search", search)
	}
	var r struct {
		Invites []Invite `json:"invites"`
	}
	err := c.get(ctx, "/u/"+url.PathEscape(c.username)+"/invited.json", q, &r)
	return r.Invites, err
}

// Group is one forum group.
type Group struct {
	Name      string `json:"name"`
	FullName  string `json:"full_name"`
	UserCount int    `json:"user_count"`
	Automatic bool   `json:"automatic"` // built-in groups like trust_level_0
}

// Groups lists the forum's groups.
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	var all []Group
	for page := 0; page < 20; page++ {
		var r struct {
			Groups          []Group `json:"groups"`
			TotalRowsGroups int     `json:"total_rows_groups"`
			LoadMoreGroups  string  `json:"load_more_groups"`
		}
		if err := c.get(ctx, "/groups.json", url.Values{"page": {fmt.Sprint(page)}}, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Groups...)
		if len(r.Groups) == 0 || len(all) >= r.TotalRowsGroups {
			break
		}
	}
	return all, nil
}

// SiteSettings returns the forum's settings as name → value text.
func (c *Client) SiteSettings(ctx context.Context) (map[string]string, error) {
	var r struct {
		SiteSettings []struct {
			Setting string          `json:"setting"`
			Value   json.RawMessage `json:"value"`
		} `json:"site_settings"`
	}
	if err := c.get(ctx, "/admin/site_settings.json", nil, &r); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range r.SiteSettings {
		var v any
		_ = json.Unmarshal(s.Value, &v)
		out[s.Setting] = strings.TrimSpace(fmt.Sprint(v))
	}
	return out, nil
}
