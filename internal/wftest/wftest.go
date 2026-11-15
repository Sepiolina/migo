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
// every sql step returns). rest steps cannot reach the network in a test.
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
	"strings"

	"github.com/xuri/excelize/v2"
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
	Name    string            `yaml:"name"`
	In      map[string]string `yaml:"in"`
	Config  map[string]any    `yaml:"config"`
	SQLStub string            `yaml:"sql_stub"`
	Expect  string            `yaml:"expect"`
	Error   string            `yaml:"error"`
}

// Result is the outcome of one case.
type Result struct {
	Name    string
	Err     error // nil: passed
	Updated bool  // the expected file was (re)written
}

// Find returns the test files at the paths: a path is a test file or a
// directory searched for *.test.yaml (hidden folders and node_modules skipped).
func Find(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
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
	rel := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(filepath.Dir(f.Path), filepath.FromSlash(p))
	}
	wf, loadErr := workflow.Load(rel(f.Workflow))
	results := make([]Result, len(f.Tests))
	for i, c := range f.Tests {
		results[i].Name = c.Name
		if loadErr != nil {
			results[i].Err = loadErr
			continue
		}
		results[i].Updated, results[i].Err = runCase(ctx, wf, c, rel, update)
	}
	return results
}

func runCase(ctx context.Context, wf *workflow.Workflow, c Case, rel func(string) string, update bool) (bool, error) {
	inputs := map[string]string{}
	for _, in := range wf.Inputs {
		if c.In[in.Name] == "" {
			return false, fmt.Errorf("in: has no file for input %q", in.Name)
		}
		inputs[in.Name] = rel(c.In[in.Name])
	}
	if len(c.In) != len(wf.Inputs) {
		return false, fmt.Errorf("in: names %d file(s), the workflow has %d input(s)", len(c.In), len(wf.Inputs))
	}
	rt, err := runtime(c, rel)
	if err != nil {
		return false, err
	}
	dir, err := os.MkdirTemp("", "migo-test-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	got := filepath.Join(dir, "output"+wf.Ext())
	runErr := wf.Run(ctx, rt, inputs, got)
	if c.Error != "" {
		switch {
		case runErr == nil:
			return false, fmt.Errorf("the run succeeded; expected an error containing %q", c.Error)
		case !strings.Contains(runErr.Error(), c.Error):
			return false, fmt.Errorf("the error is %q; expected it to contain %q", runErr, c.Error)
		}
		return false, nil
	}
	if runErr != nil {
		return false, runErr
	}
	want := rel(c.Expect)
	if !strings.EqualFold(filepath.Ext(want), wf.Ext()) {
		return false, fmt.Errorf("expect: %s must be a %s file, like the workflow's output", c.Expect, wf.Ext())
	}
	diff := compare(got, want)
	if diff == nil || !update {
		if errors.Is(diff, os.ErrNotExist) {
			return false, fmt.Errorf("%s does not exist yet: run with -update to create it, then check it", c.Expect)
		}
		return false, diff
	}
	data, err := os.ReadFile(got)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(want, data, 0o644)
}

type blocked struct{}

func (blocked) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("a test cannot call %s: rest steps have no network in tests", r.URL.Host)
}

// runtime is a test's runtime: the case's config, its SQL stub, no network.
func runtime(c Case, rel func(string) string) (workflow.Runtime, error) {
	rt := workflow.Runtime{HTTP: &http.Client{Transport: blocked{}}}
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
		b, err := os.ReadFile(rel(c.SQLStub))
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

// compare reports the first difference between two outputs: for workbooks
// the sheet names and every cell's value (not styles), for CSV the text.
func compare(got, want string) error {
	if !strings.EqualFold(filepath.Ext(want), ".xlsx") {
		g, err := os.ReadFile(got)
		if err != nil {
			return err
		}
		w, err := os.ReadFile(want)
		if err != nil {
			return err
		}
		gl, wl := lines(g), lines(w)
		for i := 0; i < len(gl) || i < len(wl); i++ {
			if a, b := at(gl, i), at(wl, i); a != b {
				return fmt.Errorf("line %d: got %q, expected %q", i+1, a, b)
			}
		}
		return nil
	}
	w, err := excelize.OpenFile(want)
	if err != nil {
		return err
	}
	defer w.Close()
	g, err := excelize.OpenFile(got)
	if err != nil {
		return err
	}
	defer g.Close()
	if a, b := g.GetSheetList(), w.GetSheetList(); strings.Join(a, "\x00") != strings.Join(b, "\x00") {
		return fmt.Errorf("sheets: got %q, expected %q", a, b)
	}
	for _, sheet := range w.GetSheetList() {
		gr, err := g.GetRows(sheet)
		if err != nil {
			return err
		}
		wr, err := w.GetRows(sheet)
		if err != nil {
			return err
		}
		for r := 0; r < len(gr) || r < len(wr); r++ {
			var grow, wrow []string
			if r < len(gr) {
				grow = gr[r]
			}
			if r < len(wr) {
				wrow = wr[r]
			}
			for c := 0; c < len(grow) || c < len(wrow); c++ {
				if a, b := at(grow, c), at(wrow, c); a != b {
					cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
					return fmt.Errorf("sheet %q cell %s: got %q, expected %q", sheet, cell, a, b)
				}
			}
		}
	}
	return nil
}

func lines(b []byte) []string {
	return strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
}

func at(list []string, i int) string {
	if i < len(list) {
		return list[i]
	}
	return ""
}
