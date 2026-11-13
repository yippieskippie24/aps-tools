package sheet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// This file is the low-level conversation with the Google Sheets API.
// Every call is a read-only GET. API reference:
// https://developers.google.com/workspace/sheets/api/reference/rest

const defaultSheetsURL = "https://sheets.googleapis.com"

// Client reads one Google account's view of spreadsheets, signed in as a
// service account.
type Client struct {
	key        ServiceAccountKey
	tokens     *tokenSource
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a client from a parsed service account key.
func NewClient(key ServiceAccountKey) *Client {
	hc := &http.Client{Timeout: 30 * time.Second}
	return &Client{key: key, tokens: newTokenSource(key, hc), baseURL: defaultSheetsURL, httpClient: hc}
}

// ServiceAccount is the robot's email address (share the sheet with it).
func (c *Client) ServiceAccount() string { return c.key.ClientEmail }

var idInLink = regexp.MustCompile(`/spreadsheets/d/([A-Za-z0-9_-]+)`)

// SpreadsheetID pulls the ID out of a pasted link, or returns the input if
// it already looks like an ID.
func SpreadsheetID(s string) string {
	s = strings.TrimSpace(s)
	if m := idInLink.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

// get performs a GET and decodes the JSON answer into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("couldn't build a request for Google Sheets: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.New("couldn't reach Google Sheets — check your internet connection")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err := c.explainStatus(resp.StatusCode, body); err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("Google Sheets sent back something unexpected")
	}
	return nil
}

func (c *Client) explainStatus(code int, body []byte) error {
	if code >= 200 && code < 300 {
		return nil
	}
	var e struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	msg := strings.ToLower(e.Error.Message)
	switch {
	case strings.Contains(msg, "has not been used") || strings.Contains(msg, "is disabled") || strings.Contains(msg, "service_disabled"):
		return errors.New("the Google Sheets API isn't turned on for the tool's Google Cloud project — in Google Cloud: APIs & Services → Library → Google Sheets API → Enable (docs/setup/google-sheet.md)")
	case code == http.StatusForbidden:
		return fmt.Errorf("the sheet isn't shared with the tool (or the share is still waiting for the owner's approval) — in the sheet, click Share and add %s as an Editor", c.key.ClientEmail)
	case code == http.StatusNotFound:
		return errors.New("Google couldn't find that spreadsheet — check the link, or that it's shared with the tool")
	case code == http.StatusBadRequest && strings.Contains(msg, "unable to parse range"):
		return errors.New("that tab doesn't exist in the sheet any more (was it renamed?) — run: aps-tools setup sheet")
	case code == http.StatusTooManyRequests:
		return errors.New("Google is limiting how fast the sheet can be read — wait a minute and try again")
	case code >= 500:
		return fmt.Errorf("Google Sheets is having problems right now (error %d) — try again later", code)
	case e.Error.Message != "":
		return fmt.Errorf("Google Sheets: %s", e.Error.Message)
	}
	return fmt.Errorf("Google Sheets returned an unexpected error (HTTP %d)", code)
}

// Spreadsheet is a sheet's name and its tabs.
type Spreadsheet struct {
	Title string
	Tabs  []string
}

// GetSpreadsheet reads a spreadsheet's title and tab names (no cell data).
func (c *Client) GetSpreadsheet(ctx context.Context, id string) (Spreadsheet, error) {
	var r struct {
		Properties struct {
			Title string `json:"title"`
		} `json:"properties"`
		Sheets []struct {
			Properties struct {
				Title     string `json:"title"`
				SheetType string `json:"sheetType"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	q := url.Values{"fields": {"properties.title,sheets.properties(title,sheetType)"}}
	if err := c.get(ctx, "/v4/spreadsheets/"+url.PathEscape(id), q, &r); err != nil {
		return Spreadsheet{}, err
	}
	s := Spreadsheet{Title: r.Properties.Title}
	for _, sh := range r.Sheets {
		if sh.Properties.SheetType == "" || sh.Properties.SheetType == "GRID" {
			s.Tabs = append(s.Tabs, sh.Properties.Title)
		}
	}
	return s, nil
}

// Rows reads every cell on a tab, as the sheet displays them (so dates look
// the way they do on screen and ticked checkboxes read "TRUE").
func (c *Client) Rows(ctx context.Context, id, tab string) ([][]string, error) {
	var r struct {
		Values [][]any `json:"values"`
	}
	rng := "'" + strings.ReplaceAll(tab, "'", "''") + "'"
	q := url.Values{"valueRenderOption": {"FORMATTED_VALUE"}, "majorDimension": {"ROWS"}}
	if err := c.get(ctx, "/v4/spreadsheets/"+url.PathEscape(id)+"/values/"+url.PathEscape(rng), q, &r); err != nil {
		return nil, err
	}
	rows := make([][]string, len(r.Values))
	for i, row := range r.Values {
		rows[i] = make([]string, len(row))
		for j, v := range row {
			rows[i][j] = strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return rows, nil
}
