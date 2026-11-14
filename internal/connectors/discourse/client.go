package discourse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/progress"
)

// This file is the low-level conversation with a Discourse forum's API.
// Every call is a read-only GET. API reference: https://docs.discourse.org

// Client makes read-only calls to one Discourse forum. It can use either
// kind of Discourse key (see docs/decisions/0008-discourse-user-api-key.md):
//   - a user API key, which the webmaster approves in the browser and which
//     works on every Discourse plan (the normal choice), or
//   - an admin API key, which only Pro and higher plans can create.
type Client struct {
	baseURL    string // e.g. "https://forum.example.org"
	username   string // the key's owner (needed for the invites page)
	key        string
	clientID   string // set only for user API keys
	httpClient *http.Client
}

// NewClient creates a client that uses an ADMIN API key (Pro plans and
// higher). siteURL may be pasted with or without https:// and with a
// trailing path; it is cleaned up.
func NewClient(siteURL, username, key string) *Client {
	return &Client{
		baseURL:    CleanURL(siteURL),
		username:   cleanUsername(username),
		key:        strings.TrimSpace(key),
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// NewUserKeyClient creates a client that uses a USER API key (any plan).
// username may be empty during setup; Me fills it in.
func NewUserKeyClient(siteURL, username, key, clientID string) *Client {
	c := NewClient(siteURL, username, key)
	c.clientID = strings.TrimSpace(clientID)
	return c
}

// usesUserKey reports which headers to send.
func (c *Client) usesUserKey() bool { return c.clientID != "" }

func cleanUsername(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@"))
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
	code, body, err := c.do(ctx, http.MethodGet, u, nil, 20<<20)
	if err != nil {
		return err
	}
	if err := explainStatus(code, body); err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("Discourse sent back something unexpected (is %s really the forum address?)", c.baseURL)
	}
	return nil
}

// post sends a JSON POST request (a change on the forum) and decodes the
// answer into out. Only CreateInvite uses it: the tool's one write.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	code, answer, err := c.do(ctx, http.MethodPost, c.baseURL+path, data, 1<<20)
	if err != nil {
		return err
	}
	if code == http.StatusForbidden || code == http.StatusUnauthorized {
		return errors.New("the forum refused to make the change: the key may be read-only. Run: aps-tools setup discourse, and allow the tool to create invites")
	}
	if err := explainStatus(code, answer); err != nil {
		return err
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("Discourse sent back something unexpected (is %s really the forum address?)", c.baseURL)
	}
	return nil
}

// maxWaits is how many times one request waits out the forum's rate limit
// before giving up.
const maxWaits = 4

// do sends one request (data is the JSON body, or nil) and returns the
// status code and up to limit bytes of the answer.
//
// The forum allows a user API key only about 20 requests a minute. When it
// answers "too many requests" (HTTP 429), nothing was done, so it's safe to
// wait as long as it asks (up to a minute), saying so on the app's progress
// line, and send the same request again. That lets bulk work (like
// following up on a dozen members) finish rather than fail part-way.
func (c *Client) do(ctx context.Context, method, u string, data []byte, limit int64) (int, []byte, error) {
	for waits := 0; ; waits++ {
		var reader io.Reader
		if data != nil {
			reader = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, reader)
		if err != nil {
			return 0, nil, fmt.Errorf("couldn't build a request for Discourse: %w", err)
		}
		c.authorize(req)
		if data != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			return 0, nil, fmt.Errorf("couldn't reach %s — check your internet connection and the forum address", c.baseURL)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
		resp.Body.Close()
		if resp.StatusCode != http.StatusTooManyRequests || waits == maxWaits {
			return resp.StatusCode, body, nil
		}
		wait := retryAfter(resp.Header.Get("Retry-After"), body)
		progress.Reportf(ctx, "The forum asked the tool to slow down: waiting %d seconds", int(wait.Round(time.Second)/time.Second))
		if err := pause(ctx, wait); err != nil {
			return 0, nil, errors.New("Discourse is limiting how fast requests can be made — wait a minute and try again")
		}
	}
}

// retryAfter is how long the forum asked us to wait: its Retry-After
// header, or the wait_seconds in its answer, kept between 1 and 60 seconds.
func retryAfter(header string, body []byte) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil {
		var e struct {
			Extras struct {
				WaitSeconds int `json:"wait_seconds"`
			} `json:"extras"`
		}
		_ = json.Unmarshal(body, &e)
		secs = e.Extras.WaitSeconds
	}
	return time.Duration(min(max(secs, 1), 60)) * time.Second
}

// pause waits for d, or until ctx is cancelled. Tests replace it so they
// don't really wait.
var pause = func(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// authorize adds the key headers for whichever kind of key this is.
func (c *Client) authorize(req *http.Request) {
	if c.usesUserKey() {
		req.Header.Set("User-Api-Key", c.key)
		req.Header.Set("User-Api-Client-Id", c.clientID)
	} else {
		req.Header.Set("Api-Key", c.key)
		req.Header.Set("Api-Username", c.username)
	}
	req.Header.Set("Accept", "application/json")
}

// CreateInvite makes a personal, single-use invite for email WITHOUT the
// forum emailing them (skip_email): the link goes in the webmaster's
// welcome email instead. topicID, if not 0, is the topic they land on after
// signing up. The forum's own expiry setting applies. This CHANGES the
// forum: callers must preview it and check for an existing invite first.
func (c *Client) CreateInvite(ctx context.Context, email string, topicID int) (Invite, error) {
	body := map[string]any{"email": email, "skip_email": true, "max_redemptions_allowed": 1}
	if topicID > 0 {
		body["topic_id"] = topicID
	}
	var inv Invite
	if err := c.post(ctx, "/invites.json", body, &inv); err != nil {
		return Invite{}, err
	}
	if inv.Link == "" {
		return inv, errors.New("the forum created the invite but didn't send back its link — find it under your profile → Invites")
	}
	return inv, nil
}

func explainStatus(code int, body []byte) error {
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusForbidden, code == http.StatusUnauthorized:
		return errors.New("Discourse refused the key — it may have been revoked or expired. Run: aps-tools setup discourse (guide: docs/setup/discourse.md)")
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

// Me returns the key owner's profile and checks they're an admin.
// With a user API key the forum says who the key belongs to, so the
// client's username is filled in from the answer.
func (c *Client) Me(ctx context.Context) (Profile, error) {
	var p Profile
	if c.usesUserKey() {
		var r struct {
			CurrentUser *Profile `json:"current_user"`
		}
		if err := c.get(ctx, "/session/current.json", nil, &r); err != nil {
			return p, err
		}
		if r.CurrentUser == nil {
			return p, errors.New("Discourse didn't accept the key — run: aps-tools setup discourse")
		}
		p = *r.CurrentUser
		c.username = p.Username
	} else {
		var err error
		if p, err = c.GetProfile(ctx, c.username); err != nil {
			return p, err
		}
	}
	if !p.Admin {
		return p, fmt.Errorf("@%s isn't a forum admin — the key must belong to an admin account (sign in to the forum as an admin when you approve it)", p.Username)
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
	// Only in the bulk list (ListUsers):
	Email           string   `json:"email"`
	SecondaryEmails []string `json:"secondary_emails"`
	SuspendReason   string   `json:"suspend_reason"`
	SilenceReason   string   `json:"silence_reason"`
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

// maxPages stops a runaway loop if the forum keeps answering (100 users or
// about 50 invites a page, so far beyond APS's size).
const maxPages = 50

// ListUsers returns every real account on the forum, with email addresses,
// in as few requests as possible (100 a page), for checking many members at
// once. Showing emails adds one "show emails" line to the forum's staff
// action log per page, which is why single checks (FindUserByEmail) filter
// by email instead. List rows leave out groups and suspension dates: use
// the full record (GetUser) when those matter.
func (c *Client) ListUsers(ctx context.Context) ([]AdminUser, error) {
	var all []AdminUser
	for page := 1; page <= maxPages; page++ {
		var rows []AdminUser
		q := url.Values{"show_emails": {"true"}, "page": {strconv.Itoa(page)}}
		if err := c.get(ctx, "/admin/users/list/all.json", q, &rows); err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		all = append(all, rows...)
	}
	return all, nil
}

// GetUser reads one account's full admin record.
func (c *Client) GetUser(ctx context.Context, id int) (AdminUser, error) {
	var full AdminUser
	err := c.get(ctx, fmt.Sprintf("/admin/users/%d.json", id), nil, &full)
	return full, err
}

// AllInvites returns all of the key owner's invites with this filter
// ("pending" or "expired"), following the forum's pages.
func (c *Client) AllInvites(ctx context.Context, filter string) ([]Invite, error) {
	var all []Invite
	seen := map[string]bool{}
	for page := 0; page < maxPages; page++ {
		q := url.Values{"filter": {filter}}
		if len(all) > 0 {
			q.Set("offset", strconv.Itoa(len(all)))
		}
		var r struct {
			Invites []Invite `json:"invites"`
		}
		if err := c.get(ctx, "/u/"+url.PathEscape(c.username)+"/invited.json", q, &r); err != nil {
			return nil, err
		}
		added := 0
		for _, inv := range r.Invites {
			k := inv.Email + "|" + inv.CreatedAt
			if !seen[k] {
				seen[k] = true
				all = append(all, inv)
				added++
			}
		}
		if added == 0 {
			break // an empty page, or the forum ignored the offset
		}
	}
	return all, nil
}

// Invite is one email invitation.
type Invite struct {
	ID int `json:"id"`
	// Link is the invite's sign-up link. The forum shows it to the person
	// who made the invite (the key's owner).
	Link      string `json:"link"`
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
