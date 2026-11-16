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
// Optional per test: config (config.json keys), mocks (a mocks file, or the
// same content inline: the answers for sql and rest steps, see package mock)
// and sql_stub (a JSON table every sql step returns). A test never reaches
// the network: a sql or rest step with no mock fails.
package wftest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v3"

	"miservice/internal/mock"
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
	Mocks   yaml.Node         `yaml:"mocks"`
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

// runtime is a test's runtime: the case's mocks and config, no network.
func runtime(c Case, rel func(string) string) (workflow.Runtime, error) {
	set := &mock.Set{}
	var err error
	switch c.Mocks.Kind {
	case 0:
	case yaml.ScalarNode:
		if set, err = mock.Load(rel(c.Mocks.Value)); err != nil {
			return workflow.Runtime{}, err
		}
	default:
		data, merr := yaml.Marshal(&c.Mocks)
		if merr != nil {
			return workflow.Runtime{}, merr
		}
		if set, err = mock.Parse(data); err != nil {
			return workflow.Runtime{}, fmt.Errorf("mocks: %w", err)
		}
	}
	if c.SQLStub != "" {
		b, err := os.ReadFile(rel(c.SQLStub))
		if err != nil {
			return workflow.Runtime{}, err
		}
		var t workflow.Table
		if err := json.Unmarshal(b, &t); err != nil {
			return workflow.Runtime{}, fmt.Errorf("%s: %w", c.SQLStub, err)
		}
		set.SQL = append(set.SQL, mock.SQL{Columns: t.Columns, Rows: t.Rows})
	}
	return set.Runtime(c.Config)
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
	here := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, filepath.FromSlash(p))
	}
	if _, err := runCase(ctx, wf, c, here, true); err != nil {
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
	return here(c.Expect), nil
}
