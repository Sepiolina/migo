// Package sap is the client for the SAP RFC gateway ("/api/sap-rfc/call").
// It knows the wire format, retries and the response shapes; what to ASK
// SAP and what the answers mean stays in the module that calls it.
package sap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"miservice/internal/jsx"
)

// Endpoint is one RFC gateway plus the transaction to call.
type Endpoint struct {
	Host, Port string
	Tcode      any // sent unchanged (string or number); omitted when nil
	EmployeeID string
}

func (e Endpoint) url() string { return fmt.Sprintf("http://%s:%s/api/sap-rfc/call", e.Host, e.Port) }

// Client carries the HTTP bits so tests can fake them. The zero value works.
type Client struct {
	HTTP  *http.Client
	Sleep func(time.Duration)
}

func (c *Client) client() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) sleep(d time.Duration) {
	if c != nil && c.Sleep != nil {
		c.Sleep(d)
		return
	}
	time.Sleep(d)
}

type payload struct {
	Tcode      any              `json:"tcode,omitempty"`
	EmployeeID string           `json:"employee_id"`
	Data       []map[string]any `json:"DATA"`
}

// CallRFC posts data (the "DATA" rows) to the gateway and returns the
// result rows with every key trimmed and lower-cased. A failed call is
// retried up to `retries` times with 2s, 4s, 8s, 10s, 10s... pauses; the
// error of the last attempt is returned if all fail.
func (c *Client) CallRFC(ctx context.Context, ep Endpoint, data []map[string]any, retries int) ([]map[string]any, error) {
	body, err := jsx.Stringify(payload{Tcode: ep.Tcode, EmployeeID: ep.EmployeeID, Data: data})
	if err != nil {
		return nil, err
	}
	resp, err := c.postWithRetry(ctx, ep.url(), body, retries)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	for _, ro := range extractRows(resp) {
		obj, ok := ro.(map[string]any)
		if !ok {
			continue
		}
		clean := make(map[string]any, len(obj))
		for k, v := range obj {
			clean[strings.ToLower(jsx.Trim(k))] = v
		}
		rows = append(rows, clean)
	}
	return rows, nil
}

func (c *Client) postWithRetry(ctx context.Context, url string, body []byte, retries int) (any, error) {
	for attempt := 0; ; attempt++ {
		data, err := c.post(ctx, url, body)
		if err == nil {
			return data, nil
		}
		if attempt >= retries || ctx.Err() != nil {
			return nil, err
		}
		pause := 1 << (attempt + 1)
		if pause > 10 {
			pause = 10
		}
		c.sleep(time.Duration(pause) * time.Second)
	}
}

func (c *Client) post(ctx context.Context, url string, body []byte) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // axios' default validateStatus
		return nil, fmt.Errorf("Request failed with status code %d", resp.StatusCode)
	}
	var data any
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, nil // axios would hand back the raw string; no rows either way
	}
	return data, nil
}

// extractRows finds the row list in the response shapes the gateway uses:
// a bare array, {tables:[{rows:[]}]}, or an object with DATA / data / rows.
func extractRows(data any) []any {
	switch d := data.(type) {
	case []any:
		return d
	case map[string]any:
		if tables, ok := d["tables"].([]any); ok {
			var out []any
			for _, t := range tables {
				if tm, ok := t.(map[string]any); ok {
					if rs, ok := tm["rows"].([]any); ok {
						out = append(out, rs...)
					}
				}
			}
			return out
		}
		for _, k := range []string{"DATA", "data", "rows"} {
			if rs, ok := d[k].([]any); ok {
				return rs
			}
		}
	}
	return nil
}
