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
// Optional per test: config (config.json keys), mocks (a file or inline SQL
// and REST answers, see package mock), sql_stub (a JSON table), and rest_stub
// (a JSON response body). Tests cannot reach the network.
package wftest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Sepiolina/migo/engine/mock"
	"github.com/Sepiolina/migo/engine/workflow"
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
	Mocks    yaml.Node         `yaml:"mocks"`
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

type noRESTMock struct {
	http.RoundTripper
}

func (t noRESTMock) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	_, err := t.RoundTripper.RoundTrip(r)
	return nil, fmt.Errorf("rest steps have no network in tests: %w", err)
}

// runtime uses the case's configuration and offline responses.
func runtime(c Case, resolve resolver) (workflow.Runtime, error) {
	set := &mock.Set{}
	var err error
	switch c.Mocks.Kind {
	case 0:
	case yaml.ScalarNode:
		path, err := resolve(c.Mocks.Value)
		if err != nil {
			return workflow.Runtime{}, err
		}
		if set, err = mock.Load(path); err != nil {
			return workflow.Runtime{}, err
		}
	default:
		data, err := yaml.Marshal(&c.Mocks)
		if err != nil {
			return workflow.Runtime{}, err
		}
		if set, err = mock.Parse(data); err != nil {
			return workflow.Runtime{}, fmt.Errorf("mocks: %w", err)
		}
	}
	if c.SQLStub != "" {
		path, err := resolve(c.SQLStub)
		if err != nil {
			return workflow.Runtime{}, err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return workflow.Runtime{}, err
		}
		var t workflow.Table
		if err := json.Unmarshal(b, &t); err != nil {
			return workflow.Runtime{}, fmt.Errorf("%s: %w", c.SQLStub, err)
		}
		set.SQL = append(set.SQL, mock.SQL{Columns: t.Columns, Rows: t.Rows})
	}
	if c.RESTStub != "" {
		path, err := resolve(c.RESTStub)
		if err != nil {
			return workflow.Runtime{}, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return workflow.Runtime{}, err
		}
		if !json.Valid(data) {
			return workflow.Runtime{}, fmt.Errorf("%s: rest_stub must contain a JSON response body", c.RESTStub)
		}
		set.REST = append(set.REST, mock.REST{Body: string(data)})
	}
	rt, err := set.Runtime(c.Config)
	if err != nil {
		return workflow.Runtime{}, err
	}
	if len(set.REST) == 0 {
		rt.HTTP.Transport = noRESTMock{rt.HTTP.Transport}
	}
	return rt, nil
}

var slugRun = regexp.MustCompile(`[^a-z0-9]+`)

// Add runs the workflow on the inputs and records the result as a new case
// in the test file at path, which is created when missing: the output
// becomes the case's expected file, beside the test file. workflowPath, the
// inputs and mocks (optional) are files; the test file refers to them
// relative to itself. It returns the expected file's path.
func Add(ctx context.Context, path, workflowPath, name string, inputs map[string]string, mocks string) (string, error) {
	name = strings.TrimSpace(name)
	slug := strings.Trim(slugRun.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		return "", errors.New("give the test a name with letters or digits")
	}
	dir := filepath.Dir(path)
	rel := func(p string) (string, error) {
		a, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		d, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		r, err := filepath.Rel(d, a)
		return filepath.ToSlash(r), err
	}
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		return "", err
	}
	c := Case{Name: name, In: map[string]string{}}
	c.Expect = strings.TrimSuffix(filepath.Base(path), Suffix) + "." + slug + wf.Ext()
	for input, file := range inputs {
		if c.In[input], err = rel(file); err != nil {
			return "", err
		}
	}
	if mocks != "" {
		m, err := rel(mocks)
		if err != nil {
			return "", err
		}
		c.Mocks = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: m}
	}

	var doc yaml.Node
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		wfRel, err := rel(workflowPath)
		if err != nil {
			return "", err
		}
		header := fmt.Sprintf("# Tests for %s. Run them with: migo test %s\n# After a deliberate change to the output: migo test -update %s\nworkflow: %s\ntests: []\n",
			filepath.Base(workflowPath), filepath.ToSlash(dir), filepath.ToSlash(dir), wfRel)
		data = []byte(header)
	case err != nil:
		return "", err
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	var tests *yaml.Node
	if len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		for i := 0; i+1 < len(doc.Content[0].Content); i += 2 {
			if doc.Content[0].Content[i].Value == "tests" {
				tests = doc.Content[0].Content[i+1]
			}
		}
	}
	if tests == nil || tests.Kind != yaml.SequenceNode {
		return "", fmt.Errorf("%s: no tests: list to add to", path)
	}
	for _, t := range tests.Content {
		var existing Case
		if t.Decode(&existing) == nil && (existing.Name == name || existing.Expect == c.Expect) {
			return "", fmt.Errorf("%s already has a test named %q", filepath.Base(path), existing.Name)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	here := func(p string) (string, error) {
		return absolutePath(dir, p)
	}
	if err := runCase(ctx, wf, c, here, true, &Result{}); err != nil {
		return "", err
	}
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(key string, value *yaml.Node) {
		entry.Content = append(entry.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	add("name", str(name))
	in := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
	for _, input := range wf.Inputs {
		in.Content = append(in.Content, str(input.Name), str(c.In[input.Name]))
	}
	add("in", in)
	if mocks != "" {
		add("mocks", str(c.Mocks.Value))
	}
	add("expect", str(c.Expect))
	tests.Content = append(tests.Content, entry)
	tests.Style = 0
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return here(c.Expect)
}
