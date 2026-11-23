// Package wftest runs workflow test files: sample inputs in, the expected
// output out, with no network. A test file is YAML, by convention
// tests/<workflow>.test.yaml beside the workflow:
//
//	workflow: ../parts-summary.yaml     # relative to this file
//	tests:
//	  - name: totals per part
//	    in: {parts: ../parts.xlsx}      # one file per workflow input
//	    expect: parts-summary.xlsx      # the expected output; cell values are compared
//	  - name: empty file is refused
//	    in: {parts: empty.xlsx}
//	    error: is empty                 # the run must fail with this text
//
// Optional per test: config (config.json keys) and sql_stub (a JSON table
// every sql step returns), and rest_stub (a JSON response body every rest
// request returns). Tests cannot reach the network.
package wftest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"miservice/internal/config"
	"miservice/internal/workflow"
)

// Suffix is the file name ending of a workflow test file.
const Suffix = ".test.yaml"

// File is one test file.
type File struct {
	Path     string `yaml:"-"`
	Workflow string `yaml:"workflow"`
	Tests    []Case `yaml:"tests"`
}

// Case is one run of the workflow and what it must produce.
type Case struct {
	Name     string            `yaml:"name"`
	In       map[string]string `yaml:"in"`
	Config   map[string]any    `yaml:"config"`
	SQLStub  string            `yaml:"sql_stub"`
	RESTStub string            `yaml:"rest_stub"`
	Expect   string            `yaml:"expect"`
	Error    string            `yaml:"error"`
}

// Result is the outcome of one case.
type Result struct {
	Name          string
	Err           error // nil: passed
	Updated       bool  // the expected file was (re)written
	Duration      time.Duration
	Difference    *Difference
	ExpectedError string
	ActualError   string
}

// Find returns the test files at the paths: a path is a test file or a
// directory searched for *.test.yaml (hidden folders and node_modules skipped).
func Find(paths []string) ([]string, error) {
	return FindContext(context.Background(), paths)
}

// FindContext discovers test files and stops walking when ctx is canceled.
func FindContext(ctx context.Context, paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() && path != p && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(name, Suffix) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Load reads and checks a test file.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(path, data)
}

func parse(path string, data []byte) (*File, error) {
	f := &File{Path: path}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Workflow == "" || len(f.Tests) == 0 {
		return nil, fmt.Errorf("%s: give workflow: and at least one entry under tests:", path)
	}
	seen := map[string]bool{}
	for i, c := range f.Tests {
		switch {
		case c.Name == "" || seen[c.Name]:
			return nil, fmt.Errorf("%s: test %d needs a unique name", path, i+1)
		case (c.Expect == "") == (c.Error == ""):
			return nil, fmt.Errorf("%s: test %q: give either expect: (a file) or error: (text)", path, c.Name)
		}
		seen[c.Name] = true
	}
	return f, nil
}

// Run runs every case of the file. With update, a case whose output differs
// from its expected file (or has none yet) rewrites that file instead of failing.
func (f *File) Run(ctx context.Context, update bool) []Result {
	resolve := func(path string) (string, error) {
		if filepath.IsAbs(path) {
			return path, nil
		}
		return filepath.Join(filepath.Dir(f.Path), filepath.FromSlash(path)), nil
	}
	return f.runFile(ctx, resolve, update)
}

// RunWorkspace runs a suite without updating its expected files.
func (f *File) RunWorkspace(ctx context.Context, root string) []Result {
	return f.runFile(ctx, f.workspaceResolver(root), false)
}

// RunWorkflow runs a suite against an already parsed workflow revision, as
// when the caller needs to identify exactly which saved version was tested.
func (f *File) RunWorkflow(ctx context.Context, root string, wf *workflow.Workflow) []Result {
	resolve := f.workspaceResolver(root)
	_, err := resolve(f.Workflow)
	return f.run(ctx, wf, err, resolve, false)
}

type resolver func(string) (string, error)

func (f *File) runFile(ctx context.Context, resolve resolver, update bool) []Result {
	wfPath, err := resolve(f.Workflow)
	var wf *workflow.Workflow
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		wf, err = workflow.Load(wfPath)
	}
	return f.run(ctx, wf, err, resolve, update)
}

func (f *File) run(ctx context.Context, wf *workflow.Workflow, loadErr error, resolve resolver, update bool) []Result {
	results := make([]Result, len(f.Tests))
	for i, c := range f.Tests {
		start := time.Now()
		r := &results[i]
		r.Name, r.ExpectedError = c.Name, c.Error
		r.Err = ctx.Err()
		if r.Err == nil {
			r.Err = loadErr
		}
		if r.Err == nil {
			r.Err = runCase(ctx, wf, c, resolve, update, r)
		}
		r.Duration = time.Since(start)
	}
	return results
}

func runCase(ctx context.Context, wf *workflow.Workflow, c Case, resolve resolver, update bool, result *Result) error {
	inputs := map[string]string{}
	for _, in := range wf.Inputs {
		if c.In[in.Name] == "" {
			return fmt.Errorf("in: has no file for input %q", in.Name)
		}
		path, err := resolve(c.In[in.Name])
		if err != nil {
			return err
		}
		inputs[in.Name] = path
	}
	if len(c.In) != len(wf.Inputs) {
		return fmt.Errorf("in: names %d file(s), the workflow has %d input(s)", len(c.In), len(wf.Inputs))
	}
	rt, err := runtime(c, resolve)
	if err != nil {
		return err
	}
	var want string
	if c.Expect != "" {
		want, err = resolve(c.Expect)
		if err != nil {
			return err
		}
	}
	dir, err := os.MkdirTemp("", "migo-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	got := filepath.Join(dir, "output"+wf.Ext())
	runErr := wf.Run(ctx, rt, inputs, got)
	if runErr != nil {
		result.ActualError = runErr.Error()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Error != "" {
		switch {
		case runErr == nil:
			return fmt.Errorf("the run succeeded; expected an error containing %q", c.Error)
		case !strings.Contains(runErr.Error(), c.Error):
			return fmt.Errorf("the error is %q; expected it to contain %q", runErr, c.Error)
		}
		return nil
	}
	if runErr != nil {
		return runErr
	}
	if !strings.EqualFold(filepath.Ext(c.Expect), wf.Ext()) {
		return fmt.Errorf("expect: %s must be a %s file, like the workflow's output", c.Expect, wf.Ext())
	}
	diff, err := compare(ctx, got, want)
	result.Difference = diff
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if diff != nil {
		err = diff
	}
	if err == nil || !update {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s does not exist yet: run with -update to create it, then check it", c.Expect)
		}
		return err
	}
	data, err := os.ReadFile(got)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(want, data, 0o644); err != nil {
		return err
	}
	result.Updated = true
	return nil
}

type testTransport struct {
	body []byte
}

func (t testTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if t.body == nil {
		return nil, fmt.Errorf("a test cannot call %s: rest steps have no network in tests", r.URL.Host)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(t.body))}, nil
}

// runtime is a test's runtime: the case's config, its SQL stub, no network.
func runtime(c Case, resolve resolver) (workflow.Runtime, error) {
	transport := testTransport{}
	rt := workflow.Runtime{}
	if c.RESTStub != "" {
		path, err := resolve(c.RESTStub)
		if err != nil {
			return rt, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return rt, err
		}
		if !json.Valid(data) {
			return rt, fmt.Errorf("%s: rest_stub must contain a JSON response body", c.RESTStub)
		}
		transport.body = data
	}
	rt.HTTP = &http.Client{Transport: transport}
	cfg := c.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return rt, fmt.Errorf("config: %w", err)
	}
	tmp, err := os.CreateTemp("", "cfg*.json")
	if err != nil {
		return rt, err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return rt, err
	}
	if rt.Cfg, err = config.Load(tmp.Name()); err != nil {
		return rt, err
	}
	rt.SQL = func(context.Context, string, string, []any) (*workflow.Table, error) {
		return nil, errors.New("a sql step ran but the test has no sql_stub")
	}
	if c.SQLStub != "" {
		path, err := resolve(c.SQLStub)
		if err != nil {
			return rt, err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return rt, err
		}
		var t workflow.Table
		if err := json.Unmarshal(b, &t); err != nil {
			return rt, fmt.Errorf("%s: %w", c.SQLStub, err)
		}
		rt.SQL = func(context.Context, string, string, []any) (*workflow.Table, error) { return &t, nil }
	}
	return rt, nil
}
