package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

// This file is the Jira onboarding step: making a member a customer on the
// APS service desk, so their emails asking for help (and their Elevator
// requests) aren't blocked. Members never log in to Jira.
//
// Adding a customer makes Atlassian email them (an invitation to sign up,
// or a "verify your email" message, depending on the desk's "Customer
// invited" notification). The welcome email tells them to expect it
// (decision 0017).
//
// Two calls, both Jira Service Management "experimental" API:
//  1. POST /rest/servicedeskapi/customer creates the customer account. If
//     there's already an Atlassian account for the email, it's looked up
//     instead.
//  2. POST /rest/servicedeskapi/servicedesk/{id}/customer adds that
//     account to the desk.

// PlanOnboard implements connectors.Onboarder: someone who isn't a customer
// on the desk gets added. A deactivated customer is a problem to sort out
// by hand. It changes nothing.
func (c *Connector) PlanOnboard(_ context.Context, _ connectors.Person, current connectors.MemberStatus) (string, error) {
	if current.State != connectors.StateMissing {
		return "", nil
	}
	return "Add them as a customer on " + c.deskLabel() + " (Atlassian emails them about it; your welcome email explains)", nil
}

// Onboard implements connectors.Onboarder: add the member as a customer on
// the desk. It checks again first, so someone already on the desk isn't
// added twice. This CHANGES Jira.
func (c *Connector) Onboard(ctx context.Context, p connectors.Person) (string, error) {
	st, err := c.CheckMember(ctx, p.Email)
	if err != nil {
		return "", err
	}
	if st.State != connectors.StateMissing {
		return "Already a customer on " + c.deskLabel() + "; nothing to do.", nil
	}
	accountID, created, err := c.customerAccount(ctx, p)
	if err != nil {
		return "", err
	}
	path := "/rest/servicedeskapi/servicedesk/" + url.PathEscape(c.deskID) + "/customer"
	if err := c.send(ctx, http.MethodPost, path, map[string]any{"accountIds": []string{accountID}}, nil); err != nil {
		return "", errors.New("couldn't add them to " + c.deskLabel() + ": " + err.Error())
	}
	if created {
		return "Created their customer account and added them to " + c.deskLabel() + ".", nil
	}
	return "Added their existing Atlassian account to " + c.deskLabel() + ".", nil
}

// customerAccount creates a customer account for p, or finds the account
// that already exists for their email. created reports which.
func (c *Connector) customerAccount(ctx context.Context, p connectors.Person) (id string, created bool, err error) {
	name := p.Name()
	if name == "" {
		name = p.Email
	}
	var made struct {
		AccountID string `json:"accountId"`
	}
	err = c.send(ctx, http.MethodPost, "/rest/servicedeskapi/customer", map[string]string{"email": p.Email, "displayName": name}, &made)
	if err == nil && made.AccountID != "" {
		return made.AccountID, true, nil
	}
	// Most likely they already have an Atlassian account: find it.
	var users []struct {
		AccountID    string `json:"accountId"`
		EmailAddress string `json:"emailAddress"`
	}
	if serr := c.get(ctx, "/rest/api/3/user/search", url.Values{"query": {p.Email}}, &users); serr == nil {
		for _, u := range users {
			if strings.EqualFold(u.EmailAddress, p.Email) || (len(users) == 1 && u.EmailAddress == "") {
				return u.AccountID, false, nil
			}
		}
	}
	if err == nil {
		err = errors.New("Jira didn't return an account")
	}
	return "", false, fmt.Errorf("couldn't create or find their Atlassian account (%v); add them by hand: %s → Customers → Add customers", err, c.deskLabel())
}

// send makes a change in Jira (a POST with a JSON body) and decodes the
// answer into out, if not nil.
func (c *Connector) send(ctx context.Context, method, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("couldn't build a request for Jira: %w", err)
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ExperimentalApi", "opt-in")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s — check your internet connection", c.site)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Jira usually explains itself; prefer its words.
		var e struct {
			ErrorMessage string `json:"errorMessage"`
		}
		if json.Unmarshal(answer, &e) == nil && e.ErrorMessage != "" {
			return errors.New("Jira: " + e.ErrorMessage)
		}
		return explainStatus(resp.StatusCode)
	}
	if out != nil && len(answer) > 0 {
		if err := json.Unmarshal(answer, out); err != nil {
			return fmt.Errorf("Jira sent back something unexpected: %w", err)
		}
	}
	return nil
}
