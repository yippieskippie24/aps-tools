// Package jira connects the tool to Jira Service Management (Cloud).
//
// APS members are *customers* on an APS service desk. Customers never use one
// of the free plan's 3 agent seats.
//
// How it authenticates: your Atlassian account email + an API token, sent as
// HTTP "basic auth". The token lives in the macOS Keychain. Setup steps are in
// docs/setup/jira.md.
//
// Atlassian API reference:
// https://developer.atlassian.com/cloud/jira/service-desk/rest/
package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// ID is this connector's machine name (used in `aps-tools setup jira`).
const ID = "jira"

// Connector talks to one Jira Cloud site. Create it with New (normal use)
// or NewForSetup (while the webmaster is entering settings).
type Connector struct {
	site     string // e.g. "example.atlassian.net"
	email    string // the webmaster's Atlassian account email
	token    string // API token from the Keychain
	tokenErr error  // why the token couldn't be loaded, if it couldn't
	deskID   string
	deskName string

	baseURL    string // "https://" + site; tests point this at a fake server
	httpClient *http.Client
}

// New builds the connector from saved settings and the Keychain.
func New(cfg config.Jira, store secrets.Store) *Connector {
	token, err := store.Get(secrets.JiraAPIToken)
	c := NewForSetup(cfg.Site, cfg.Email, token)
	c.tokenErr = err
	c.deskID = cfg.ServiceDeskID
	c.deskName = cfg.ServiceDeskName
	return c
}

// NewForSetup builds a connector from values the webmaster has just typed,
// before anything is saved, so setup can test them first.
func NewForSetup(site, email, token string) *Connector {
	site = CleanSite(site)
	return &Connector{
		site:       site,
		email:      strings.TrimSpace(email),
		token:      strings.TrimSpace(token),
		baseURL:    "https://" + site,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// CleanSite turns whatever was pasted ("https://example.atlassian.net/jira/…")
// into just the site address ("example.atlassian.net").
func CleanSite(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// ID implements connectors.Connector.
func (c *Connector) ID() string { return ID }

// Name implements connectors.Connector.
func (c *Connector) Name() string { return "Jira Service Management" }

// Missing implements connectors.Connector.
func (c *Connector) Missing() []string {
	var m []string
	if c.site == "" {
		m = append(m, "your Jira site address")
	}
	if c.email == "" {
		m = append(m, "your Atlassian account email")
	}
	if c.token == "" {
		if c.tokenErr != nil && !errors.Is(c.tokenErr, secrets.ErrNotFound) {
			m = append(m, "a readable Jira API token ("+c.tokenErr.Error()+")")
		} else {
			m = append(m, "a Jira API token")
		}
	}
	if c.deskID == "" {
		m = append(m, "which service desk members belong to")
	}
	if len(m) > 0 {
		m[len(m)-1] += " (run: aps-tools setup jira)"
	}
	return m
}

// Me describes the Atlassian account the API token belongs to.
type Me struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

// WhoAmI returns the account the token belongs to. It proves the site,
// email and token are all correct.
func (c *Connector) WhoAmI(ctx context.Context) (Me, error) {
	var me Me
	err := c.get(ctx, "/rest/api/3/myself", nil, &me)
	return me, err
}

// ServiceDesk is one service desk (a Jira Service Management project).
type ServiceDesk struct {
	ID          string `json:"id"`
	ProjectName string `json:"projectName"`
	ProjectKey  string `json:"projectKey"`
}

// ListServiceDesks returns every service desk the token can see. Setup uses
// this so the webmaster picks from a list instead of hunting for an ID.
func (c *Connector) ListServiceDesks(ctx context.Context) ([]ServiceDesk, error) {
	var all []ServiceDesk
	for start := 0; ; {
		var page struct {
			Values     []ServiceDesk `json:"values"`
			IsLastPage bool          `json:"isLastPage"`
		}
		q := url.Values{"start": {fmt.Sprint(start)}, "limit": {"50"}}
		if err := c.get(ctx, "/rest/servicedeskapi/servicedesk", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		if page.IsLastPage || len(page.Values) == 0 {
			return all, nil
		}
		start += len(page.Values)
	}
}

// TestConnection implements connectors.Connector: it checks the token works
// and that the chosen service desk can be read. Read-only.
func (c *Connector) TestConnection(ctx context.Context) error {
	if _, err := c.WhoAmI(ctx); err != nil {
		return err
	}
	var desk ServiceDesk
	if err := c.get(ctx, "/rest/servicedeskapi/servicedesk/"+url.PathEscape(c.deskID), nil, &desk); err != nil {
		return fmt.Errorf("the token works, but the service desk in your settings couldn't be read: %w", err)
	}
	return nil
}

// customer is one entry from Jira's customer list.
type customer struct {
	AccountID    string `json:"accountId"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
	Active       bool   `json:"active"`
}

// CheckMember implements connectors.Connector: is this email a customer on
// the APS service desk? Read-only.
func (c *Connector) CheckMember(ctx context.Context, email string) (connectors.MemberStatus, error) {
	var page struct {
		Values []customer `json:"values"`
	}
	path := "/rest/servicedeskapi/servicedesk/" + url.PathEscape(c.deskID) + "/customer"
	// Jira matches "query" against name and email, so the results can include
	// near-misses; we look for an exact email match below.
	q := url.Values{"query": {email}, "limit": {"50"}}
	if err := c.get(ctx, path, q, &page); err != nil {
		return connectors.MemberStatus{}, err
	}

	desk := c.deskLabel()
	for _, cu := range page.Values {
		if !strings.EqualFold(cu.EmailAddress, email) {
			continue
		}
		return statusFor(cu, desk, ""), nil
	}

	// Some Atlassian accounts hide their email address. If the search found
	// exactly one person whose email is hidden, that's almost certainly them.
	if len(page.Values) == 1 && page.Values[0].EmailAddress == "" {
		return statusFor(page.Values[0], desk, "Matched by search — Jira hides this person's email address."), nil
	}

	return notACustomer(desk), nil
}

// notACustomer is the result for someone who isn't on the desk.
func notACustomer(desk string) connectors.MemberStatus {
	return connectors.MemberStatus{
		State:   connectors.StateMissing,
		Summary: "Not a customer on " + desk + ".",
		Fix:     "In Jira, open " + desk + " → Customers → Add customers, and enter their email.",
	}
}

// maxCustomerPages stops a runaway loop if Jira never says "last page"
// (50 per page, so 100 pages is 5,000 customers: far more than APS has).
const maxCustomerPages = 100

// CheckMembers implements connectors.BulkChecker: it reads the desk's whole
// customer list, a page at a time, and matches every email against it.
// People whose email Jira hides can't be matched from the list, so anyone
// not found is double-checked with an ordinary search, but only when the
// list has hidden emails. Read-only.
func (c *Connector) CheckMembers(ctx context.Context, emails []string) (map[string]connectors.MemberStatus, error) {
	path := "/rest/servicedeskapi/servicedesk/" + url.PathEscape(c.deskID) + "/customer"
	byEmail := map[string]customer{}
	hidden := false
	for start, n := 0, 0; n < maxCustomerPages; n++ {
		var page struct {
			Values     []customer `json:"values"`
			IsLastPage bool       `json:"isLastPage"`
		}
		q := url.Values{"start": {strconv.Itoa(start)}, "limit": {"50"}}
		if err := c.get(ctx, path, q, &page); err != nil {
			return nil, err
		}
		for _, cu := range page.Values {
			if cu.EmailAddress == "" {
				hidden = true
				continue
			}
			byEmail[strings.ToLower(cu.EmailAddress)] = cu
		}
		if page.IsLastPage || len(page.Values) == 0 {
			break
		}
		start += len(page.Values)
	}

	desk := c.deskLabel()
	out := make(map[string]connectors.MemberStatus, len(emails))
	for _, email := range emails {
		if cu, ok := byEmail[strings.ToLower(email)]; ok {
			out[email] = statusFor(cu, desk, "")
			continue
		}
		if !hidden {
			out[email] = notACustomer(desk)
			continue
		}
		st, err := c.CheckMember(ctx, email)
		if err != nil {
			return nil, err
		}
		out[email] = st
	}
	return out, nil
}

func statusFor(cu customer, desk, note string) connectors.MemberStatus {
	var details []string
	if cu.DisplayName != "" {
		details = append(details, "Name in Jira: "+cu.DisplayName)
	}
	if note != "" {
		details = append(details, note)
	}
	if !cu.Active {
		return connectors.MemberStatus{
			State:   connectors.StateProblem,
			Summary: "Customer on " + desk + ", but their Atlassian account is deactivated.",
			Details: details,
			Fix:     "They won't be able to log in. Check the account in Jira's customer list or admin.atlassian.com.",
		}
	}
	return connectors.MemberStatus{
		State:   connectors.StateOK,
		Summary: "Customer on " + desk + ".",
		Details: details,
	}
}

func (c *Connector) deskLabel() string {
	if c.deskName != "" {
		return "the " + c.deskName + " service desk"
	}
	return "service desk " + c.deskID
}

// get performs a read-only GET request and decodes the JSON answer into out.
// It turns HTTP failures into plain-English errors with a suggested fix.
func (c *Connector) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("couldn't build a request for Jira: %w", err)
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")
	// Some Service Management endpoints (including the customer list) are
	// labelled "experimental" by Atlassian and refuse requests without this.
	req.Header.Set("X-ExperimentalApi", "opt-in")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s — check your internet connection and the site address", c.site)
	}
	defer resp.Body.Close()

	if err := explainStatus(resp.StatusCode); err != nil {
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("Jira sent back something unexpected (is %s really a Jira site?)", c.site)
	}
	return nil
}

// explainStatus converts an HTTP status code into an actionable message.
func explainStatus(code int) error {
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusUnauthorized:
		return errors.New("Jira rejected the email or API token. Tokens expire after at most a year — create a new one (docs/setup/jira.md) and run: aps-tools setup jira")
	case code == http.StatusForbidden:
		return errors.New("your Atlassian account doesn't have permission for this — it needs to be an agent or admin on the service desk")
	case code == http.StatusNotFound:
		return errors.New("Jira couldn't find that — check the site address and service desk (run: aps-tools setup jira)")
	case code == http.StatusTooManyRequests:
		return errors.New("Jira is limiting how fast requests can be made — wait a minute and try again")
	case code >= 500:
		return fmt.Errorf("Jira is having problems right now (error %d) — try again later, or check status.atlassian.com", code)
	default:
		return fmt.Errorf("Jira returned an unexpected error (HTTP %d)", code)
	}
}
