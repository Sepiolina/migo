// Package mock stands in for the systems a workflow's sql and rest steps
// call, so a workflow runs the same way in tests, in Mai Studio and on a
// laptop with no network. A mocks file is YAML:
//
//	config: {db_enabled: true}          # config.json keys for the run
//	sql:
//	  - query: Material_Master          # text the query contains (omit: any query)
//	    columns: [FPN, Status]
//	    rows:
//	      - [A-1, Active]
//	rest:
//	  - url: /materials                 # text the URL contains (omit: any URL)
//	    method: POST                    # optional
//	    status: 200                     # default 200; 4xx and 5xx make the step fail
//	    body: {results: [{id: 1}]}      # the JSON response
//
// A call uses the first entry that matches it and has not been used yet;
// when all matching entries are used, the last one answers again. So one
// entry serves every batch, and several entries answer batches in order.
// A call that matches nothing fails with a message that names it.
//
// Record writes such a file from a real run (migo run -record).
package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"miservice/internal/config"
	"miservice/internal/workflow"
)

// Set is the content of a mocks file.
type Set struct {
	Config map[string]any `yaml:"config,omitempty"`
	SQL    []SQL          `yaml:"sql,omitempty"`
	REST   []REST         `yaml:"rest,omitempty"`
}

// SQL answers sql steps whose query contains Query.
type SQL struct {
	Query   string   `yaml:"query,omitempty"`
	Columns []string `yaml:"columns,flow"`
	Rows    [][]any  `yaml:"rows"`
}

// REST answers rest requests whose URL contains URL (and use Method, if set).
type REST struct {
	URL    string `yaml:"url,omitempty"`
	Method string `yaml:"method,omitempty"`
	Status int    `yaml:"status,omitempty"`
	Body   any    `yaml:"body"`
}

// Load reads a mocks file.
func Load(path string) (*Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Parse reads mocks from YAML; unknown keys are errors.
func Parse(data []byte) (*Set, error) {
	s := &Set{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(s); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	for i, r := range s.REST {
		if r.Status != 0 && (r.Status < 100 || r.Status > 599) {
			return nil, fmt.Errorf("rest entry %d: status %d is not an HTTP status", i+1, r.Status)
		}
	}
	return s, nil
}

// Save writes the set as a mocks file.
func (s *Set) Save(path string) error {
	var buf bytes.Buffer
	buf.WriteString("# Recorded responses for sql and rest steps (docs/CLI.md, \"Mocks\").\n" +
		"# Check that nothing confidential is in here before you commit it.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Runtime returns a runtime that answers from the set and reaches nothing
// else. extra config keys win over the set's own.
func (s *Set) Runtime(extra map[string]any) (workflow.Runtime, error) {
	merged := map[string]any{}
	for k, v := range s.Config {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	cfg, err := Config(merged)
	if err != nil {
		return workflow.Runtime{}, err
	}
	p := &player{set: s, sqlUsed: make([]bool, len(s.SQL)), restUsed: make([]bool, len(s.REST))}
	return workflow.Runtime{
		Cfg:   cfg,
		SQL:   p.sql,
		HTTP:  &http.Client{Transport: p},
		Sleep: func(time.Duration) {}, // retries need not wait for a mock
	}, nil
}

// Config builds a run configuration from config.json keys.
func Config(keys map[string]any) (*config.Config, error) {
	if keys == nil {
		keys = map[string]any{}
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	tmp, err := os.CreateTemp("", "cfg*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return nil, err
	}
	return config.Load(tmp.Name())
}

type player struct {
	mu       sync.Mutex
	set      *Set
	sqlUsed  []bool
	restUsed []bool
}

// pick returns the entry to answer with: the first unused match, else the
// last match; -1 when nothing matches.
func pick(used []bool, matches func(i int) bool) int {
	last := -1
	for i := range used {
		if !matches(i) {
			continue
		}
		if !used[i] {
			used[i] = true
			return i
		}
		last = i
	}
	return last
}

func (p *player) sql(_ context.Context, _ string, query string, _ []any) (*workflow.Table, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := pick(p.sqlUsed, func(i int) bool { return strings.Contains(query, p.set.SQL[i].Query) })
	if i < 0 {
		return nil, fmt.Errorf("no mock answers this query; add a sql entry whose query: is text it contains. The query: %s", oneLine(query))
	}
	m := p.set.SQL[i]
	t := &workflow.Table{Columns: append([]string{}, m.Columns...)}
	for n, row := range m.Rows {
		if len(row) != len(m.Columns) {
			return nil, fmt.Errorf("sql mock %d: row %d has %d value(s) for %d column(s)", i+1, n+1, len(row), len(m.Columns))
		}
		t.Rows = append(t.Rows, append([]any{}, row...))
	}
	return t, nil
}

// RoundTrip answers a rest step's request.
func (p *player) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	url := r.URL.String()
	i := pick(p.restUsed, func(i int) bool {
		m := p.set.REST[i]
		return strings.Contains(url, m.URL) && (m.Method == "" || strings.EqualFold(m.Method, r.Method))
	})
	if i < 0 {
		return nil, fmt.Errorf("no mock answers %s %s; add a rest entry whose url: is text it contains", r.Method, url)
	}
	m := p.set.REST[i]
	status := m.Status
	if status == 0 {
		status = http.StatusOK
	}
	var body []byte
	switch b := m.Body.(type) {
	case nil:
	case string:
		body = []byte(b)
	default:
		var err error
		if body, err = json.Marshal(b); err != nil {
			return nil, fmt.Errorf("rest mock %d: body: %w", i+1, err)
		}
	}
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r,
	}, nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// Record wraps the runtime's connections so every sql result and rest
// response of a run is kept, and returns the set they are collected in.
// Request headers and bodies are never recorded (they may carry credentials).
func Record(rt *workflow.Runtime) *Set {
	s := &Set{}
	var mu sync.Mutex
	if query := rt.SQL; query != nil {
		rt.SQL = func(ctx context.Context, connection, q string, args []any) (*workflow.Table, error) {
			t, err := query(ctx, connection, q, args)
			if err == nil && t != nil {
				mu.Lock()
				s.SQL = append(s.SQL, SQL{Query: oneLine(q), Columns: t.Columns, Rows: t.Rows})
				mu.Unlock()
			}
			return t, err
		}
	}
	client := rt.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	inner := client.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	recording := *client
	recording.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		resp, err := inner.RoundTrip(r)
		if err != nil {
			return resp, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(data))
		var body any
		if json.Unmarshal(data, &body) != nil {
			body = string(data)
		}
		m := REST{URL: r.URL.RequestURI(), Method: r.Method, Body: body} // no host: it comes from config
		if resp.StatusCode != http.StatusOK {
			m.Status = resp.StatusCode
		}
		mu.Lock()
		s.REST = append(s.REST, m)
		mu.Unlock()
		return resp, nil
	})
	rt.HTTP = &recording
	return s
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
