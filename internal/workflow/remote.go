package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"miservice/internal/jsx"
)

// remoteResult is what sql and rest share: the dataset they save, its shape
// when nothing comes back, and what happens on failure.
type remoteResult struct {
	save    string
	columns []string
	onError *template // nil: a failure stops the run
}

func compileRemote(save string, columns []string, onError string, sc *scope, vars ...string) (remoteResult, error) {
	r := remoteResult{save: save, columns: columns}
	if strings.TrimSpace(save) == "" {
		return r, fmt.Errorf("needs save: the dataset that receives the result")
	}
	if sc.inputs[save] {
		return r, fmt.Errorf("dataset %q would hide the input of the same name", save)
	}
	sc.saved[save] = true
	if onError != "" {
		t, err := compileTemplate(onError, false, append(append(vars, "error"), tableVars...)...)
		if err != nil {
			return r, fmt.Errorf("on_error: %w", err)
		}
		r.onError = t
	}
	return r, nil
}

// failed logs a failure and returns nil when on_error is set, else err.
func (r remoteResult) failed(s *State, err error, extra map[string]any) error {
	if r.onError == nil {
		return err
	}
	extra["error"] = err.Error()
	line, rerr := r.onError.render(s.tableEnv(extra))
	if rerr != nil {
		return rerr
	}
	s.rt.Log.Print(line)
	return nil
}

func (r remoteResult) store(s *State, t *Table) {
	for _, c := range r.columns {
		t.addColumn(c)
	}
	s.Tables[r.save] = t
}

// - sql: runs a query on a connection from config.json.
//
//	sql:
//	  connection: db              # config keys db_server, db_user... (default db)
//	  query: SELECT a, b FROM t WHERE k IN (@keys) AND p = @project
//	  params:                     # expressions; a list expands to @p1, @p2, ...
//	    keys: 'uniq(map(rows, norm(#.PN)))'
//	    project: '"FNS"'
//	  batch: 1900                 # list values per query (results are appended)
//	  columns: [a, b]             # shape of the result when it has no rows
//	  on_error: "lookup failed: {error}"   # log and continue with no rows
//	  save: lookup                # the dataset that receives the result
func compileSQL(arg *yaml.Node, sc *scope) (Runner, error) {
	var spec struct {
		Connection string    `yaml:"connection"`
		Query      string    `yaml:"query"`
		Params     yaml.Node `yaml:"params"`
		Batch      int       `yaml:"batch"`
		Columns    []string  `yaml:"columns"`
		OnError    string    `yaml:"on_error"`
		Save       string    `yaml:"save"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.Query) == "" {
		return nil, fmt.Errorf("needs a query")
	}
	if spec.Connection == "" {
		spec.Connection = "db"
	}
	if spec.Batch <= 0 {
		spec.Batch = 1900
	}
	params := map[string]*program{}
	if spec.Params.Kind != 0 {
		pairs, err := orderedPairs(&spec.Params)
		if err != nil {
			return nil, fmt.Errorf("params: %w", err)
		}
		for _, p := range pairs {
			if params[p[0]], err = compileExpr(p[1], false, tableVars...); err != nil {
				return nil, fmt.Errorf("params.%s: %w", p[0], err)
			}
			if !strings.Contains(spec.Query, "@"+p[0]) {
				return nil, fmt.Errorf("params.%s: the query does not use @%s", p[0], p[0])
			}
		}
	}
	res, err := compileRemote(spec.Save, spec.Columns, spec.OnError, sc)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *State) error {
		env := s.tableEnv(nil)
		values := map[string]any{}
		listParam := ""
		for name, p := range params {
			v, err := p.run(env)
			if err != nil {
				return err
			}
			if list, ok := v.([]any); ok {
				if listParam != "" {
					return fmt.Errorf("only one list parameter is supported (@%s and @%s)", listParam, name)
				}
				listParam = name
				v = list
			}
			values[name] = v
		}
		batches := [][]any{nil}
		if listParam != "" {
			list := values[listParam].([]any)
			if len(list) == 0 {
				res.store(s, &Table{})
				return nil
			}
			batches = nil
			for i := 0; i < len(list); i += spec.Batch {
				batches = append(batches, list[i:min(i+spec.Batch, len(list))])
			}
		}
		if s.rt.SQL == nil {
			return res.failed(s, fmt.Errorf("no database connection is configured"), map[string]any{})
		}
		out := &Table{}
		for _, b := range batches {
			query, args := bindParams(spec.Query, values, listParam, b)
			t, err := s.rt.SQL(ctx, spec.Connection, query, args)
			if err != nil {
				if ferr := res.failed(s, err, map[string]any{}); ferr != nil {
					return ferr
				}
				out = &Table{}
				break
			}
			out = unionRows(out, t)
		}
		res.store(s, out)
		return nil
	}, nil
}

var paramRef = regexp.MustCompile(`@([A-Za-z_][A-Za-z0-9_]*)`)

// bindParams turns @name references into positional @p1, @p2... parameters.
func bindParams(query string, values map[string]any, listParam string, batch []any) (string, []any) {
	var args []any
	q := paramRef.ReplaceAllStringFunc(query, func(ref string) string {
		name := ref[1:]
		v, ok := values[name]
		if !ok {
			return ref
		}
		if name == listParam {
			ph := make([]string, len(batch))
			for i, x := range batch {
				args = append(args, x)
				ph[i] = fmt.Sprintf("@p%d", len(args))
			}
			return strings.Join(ph, ", ")
		}
		args = append(args, v)
		return fmt.Sprintf("@p%d", len(args))
	})
	return q, args
}

// - rest: calls an HTTP API, optionally in batches, and saves the rows of
// the responses.
//
//	rest:
//	  url: 'http://{config("host")}/api/items'    # text with {expressions}
//	  method: POST                                 # default GET
//	  each: 'uniq(map(rows, #.Code))'              # values to send, in batches
//	  batch: '50'                                  # values per request (expression)
//	  body: '{codes: batch}'                       # JSON body; sees batch, batch_number
//	  headers: {Authorization: 'Bearer {config("token")}'}
//	  retries: '3'                                 # extra attempts (expression)
//	  rows: 'response.items'                       # list of objects in the response
//	  keys: lower                                  # trim and lower-case (or upper-case) object keys
//	  columns: [code, name]                        # shape when nothing comes back
//	  on_error: "batch {batch_number}: {error}"    # log and skip a failed request
//	  save: items
func compileREST(arg *yaml.Node, sc *scope) (Runner, error) {
	var spec struct {
		URL     string            `yaml:"url"`
		Method  string            `yaml:"method"`
		Each    string            `yaml:"each"`
		Batch   string            `yaml:"batch"`
		Body    string            `yaml:"body"`
		Headers map[string]string `yaml:"headers"`
		Retries string            `yaml:"retries"`
		Rows    string            `yaml:"rows"`
		Keys    string            `yaml:"keys"`
		Columns []string          `yaml:"columns"`
		OnError string            `yaml:"on_error"`
		Save    string            `yaml:"save"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if spec.Method == "" {
		spec.Method = http.MethodGet
	}
	var keyCase func(string) string
	switch spec.Keys {
	case "":
	case "lower":
		keyCase = func(k string) string { return strings.ToLower(jsx.Trim(k)) }
	case "upper":
		keyCase = func(k string) string { return strings.ToUpper(jsx.Trim(k)) }
	default:
		return nil, fmt.Errorf("keys must be lower or upper")
	}
	spec.Method = strings.ToUpper(spec.Method)
	reqVars := append([]string{"batch", "batch_number"}, tableVars...)
	url, err := compileTemplate(spec.URL, false, reqVars...)
	if err != nil || strings.TrimSpace(spec.URL) == "" {
		return nil, fmt.Errorf("url: %v", err)
	}
	headers := map[string]*template{}
	for k, v := range spec.Headers {
		if headers[k], err = compileTemplate(v, false, reqVars...); err != nil {
			return nil, fmt.Errorf("headers.%s: %w", k, err)
		}
	}
	opt := func(src, def string, vars ...string) (*program, error) {
		if src == "" {
			src = def
		}
		return compileExpr(src, false, vars...)
	}
	var each, batch, body *program
	if spec.Each != "" {
		if each, err = opt(spec.Each, "", tableVars...); err != nil {
			return nil, fmt.Errorf("each: %w", err)
		}
	}
	if batch, err = opt(spec.Batch, "0", tableVars...); err != nil {
		return nil, fmt.Errorf("batch: %w", err)
	}
	if spec.Body != "" {
		if body, err = opt(spec.Body, "", reqVars...); err != nil {
			return nil, fmt.Errorf("body: %w", err)
		}
	}
	retries, err := opt(spec.Retries, "0", tableVars...)
	if err != nil {
		return nil, fmt.Errorf("retries: %w", err)
	}
	rows, err := opt(spec.Rows, "response", append([]string{"response"}, reqVars...)...)
	if err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	res, err := compileRemote(spec.Save, spec.Columns, spec.OnError, sc, "batch", "batch_number", "retries")
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *State) error {
		env := s.tableEnv(nil)
		batches := [][]any{nil}
		if each != nil {
			v, err := each.run(env)
			if err != nil {
				return err
			}
			list, ok := v.([]any)
			if !ok {
				return fmt.Errorf("each: want a list")
			}
			size := 0
			if b, err := batch.run(env); err != nil {
				return err
			} else {
				size = int(toNumber(cell(b), 0))
			}
			if size <= 0 {
				size = max(len(list), 1)
			}
			batches = nil
			for i := 0; i < len(list); i += size {
				batches = append(batches, list[i:min(i+size, len(list))])
			}
		}
		r, err := retries.run(env)
		if err != nil {
			return err
		}
		attempts := int(toNumber(cell(r), 0))
		out := &Table{}
		for n, b := range batches {
			benv := s.tableEnv(map[string]any{"batch": b, "batch_number": n + 1, "retries": attempts})
			resp, err := s.request(ctx, spec.Method, url, headers, body, benv, attempts)
			if err == nil {
				benv["response"] = resp
				var v any
				if v, err = rows.run(benv); err == nil {
					err = appendObjects(out, v, keyCase)
				}
			}
			if err != nil {
				if ferr := res.failed(s, err, map[string]any{"batch": b, "batch_number": n + 1, "retries": attempts}); ferr != nil {
					return ferr
				}
			}
		}
		res.store(s, out)
		return nil
	}, nil
}

func (s *State) request(ctx context.Context, method string, url *template, headers map[string]*template, body *program, env map[string]any, retries int) (any, error) {
	u, err := url.render(env)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if body != nil {
		v, err := body.run(env)
		if err != nil {
			return nil, err
		}
		if payload, err = jsx.Stringify(v); err != nil {
			return nil, err
		}
	}
	hdr := http.Header{"Accept": {"application/json, text/plain, */*"}}
	if payload != nil {
		hdr.Set("Content-Type", "application/json")
	}
	for k, t := range headers {
		v, err := t.render(env)
		if err != nil {
			return nil, err
		}
		hdr.Set(k, v)
	}
	client := s.rt.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	sleep := s.rt.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for attempt := 0; ; attempt++ {
		v, err := doRequest(ctx, client, method, u, hdr, payload)
		if err == nil {
			return v, nil
		}
		if attempt >= retries || ctx.Err() != nil {
			return nil, err
		}
		sleep(time.Duration(min(1<<(attempt+1), 10)) * time.Second)
	}
}

func doRequest(ctx context.Context, client *http.Client, method, url string, hdr http.Header, payload []byte) (any, error) {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	req.Header = hdr.Clone()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Request failed with status code %d", resp.StatusCode)
	}
	var v any
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b), nil
	}
	return v, nil
}

// appendObjects adds a list of objects to t as rows (keys become columns).
func appendObjects(t *Table, v any, keyCase func(string) string) error {
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("rows: want a list of objects, got %s", text(cell(v)))
	}
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		vals := map[string]any{}
		for _, k := range keys {
			name := k
			if keyCase != nil {
				name = keyCase(k)
			}
			t.addColumn(name)
			vals[name] = cell(obj[k])
		}
		row := make([]any, len(t.Columns))
		for i, c := range t.Columns {
			row[i] = vals[c]
		}
		t.Rows = append(t.Rows, row)
	}
	return nil
}
