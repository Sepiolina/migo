// Package workflow runs one YAML file per process: spreadsheet and file
// wrangling with REST and SQL steps, branching and inline rules. Nothing about
// a particular business process lives in Go; it is all in the file.
//
//	name: Parts summary
//	input:
//	  parts: {label: Parts}
//	steps:
//	  - from: parts[Parts]
//	  - filter: 'num(Quantity) > 0'
//	  - save: summary
//	output:
//	  sheets: {Parts: summary}
//
// Every step and expression is compiled when the file is loaded, so a typo
// fails at startup, not mid-request. docs/WORKFLOWS.md is the reference.
package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"miservice/internal/config"
	"miservice/internal/logx"
)

// Workflow is a loaded, compiled workflow file.
type Workflow struct {
	Name   string
	Inputs []Input // upload order
	Sheets []*SheetSpec
	steps  []step
}

// Input is one uploaded file. Match (a regexp on the uploaded filename) lets
// an input claim its file regardless of upload order.
type Input struct {
	Name  string
	Label string
	Match *regexp.Regexp
}

// Runtime is what a run may use outside the workflow file: configuration,
// logging and the connections for sql and rest steps.
type Runtime struct {
	Cfg   *config.Config
	Log   logx.Logger
	SQL   SQLFunc      // nil: sql steps fail
	HTTP  *http.Client // nil: a client with a 30 s timeout
	Sleep func(time.Duration)
}

// SQLFunc runs query with positional parameters (@p1, @p2...) on the
// connection named in the step and returns the result as a table.
type SQLFunc func(ctx context.Context, connection, query string, args []any) (*Table, error)

// State is what steps share during one run.
type State struct {
	rt     Runtime
	Inputs map[string]string // input name -> file path
	Tables map[string]*Table // saved datasets
	Cur    *Table            // the table the steps work on
}

// Runner executes one compiled step.
type Runner func(ctx context.Context, s *State) error

type step struct {
	name string
	line int
	run  Runner
}

// scope is what compilers check names against.
type scope struct {
	inputs map[string]bool
	saved  map[string]bool
}

type compiler func(arg *yaml.Node, sc *scope) (Runner, error)

var ops map[string]compiler

func init() {
	ops = map[string]compiler{
		"from": compileFrom, "save": compileSave, "log": compileLog, "fail": compileFail,
		"select": tableStep(compileSelect), "rename": tableStep(compileRename),
		"filter": tableStep(compileFilter), "set": tableStep(compileSet),
		"fill": tableStep(compileFill), "dedupe": tableStep(compileDedupe),
		"group": tableStep(compileGroup), "sort": tableStep(compileSort),
		"join": tableStep(compileJoin), "append": tableStep(compileAppend),
		"compare": tableStep(compileCompare), "stack": tableStep(compileStack),
		"window": tableStep(compileWindow), "pick": tableStep(compilePick),
		"split": compileSplit, "sql": compileSQL, "rest": compileREST,
	}
}

// Load reads and compiles a workflow file.
func Load(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	wf, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return wf, nil
}

type document struct {
	Name   string    `yaml:"name"`
	Input  yaml.Node `yaml:"input"`
	Steps  yaml.Node `yaml:"steps"`
	Output struct {
		Sheets yaml.Node `yaml:"sheets"`
	} `yaml:"output"`
}

// Parse compiles a workflow document. YAML aliases, merge keys, duplicate
// keys and unknown fields are rejected so every file reads exactly as it runs.
func Parse(data []byte) (*Workflow, error) {
	root, err := readNode(data)
	if err != nil {
		return nil, err
	}
	var doc document
	if err := strictDecode(root, &doc); err != nil {
		return nil, err
	}
	wf := &Workflow{Name: strings.TrimSpace(doc.Name)}
	if wf.Name == "" {
		return nil, fmt.Errorf("name must not be blank")
	}
	if wf.Inputs, err = parseInputs(&doc.Input); err != nil {
		return nil, err
	}
	sc := &scope{inputs: map[string]bool{}, saved: map[string]bool{}}
	for _, in := range wf.Inputs {
		sc.inputs[in.Name] = true
	}
	if doc.Steps.Kind != yaml.SequenceNode || len(doc.Steps.Content) == 0 {
		return nil, fmt.Errorf("steps must be a non-empty list")
	}
	if wf.steps, err = compileSteps(&doc.Steps, sc, ""); err != nil {
		return nil, err
	}
	if wf.Sheets, err = parseSheets(&doc.Output.Sheets); err != nil {
		return nil, err
	}
	if len(wf.Sheets) == 0 {
		return nil, fmt.Errorf("output.sheets must name at least one sheet")
	}
	for _, sh := range wf.Sheets {
		if !sc.saved[sh.From] {
			return nil, fmt.Errorf("output sheet %q: no step saves dataset %q", sh.Name, sh.From)
		}
	}
	return wf, nil
}

// compileSteps compiles a step list; prefix names nested steps in errors.
func compileSteps(list *yaml.Node, sc *scope, prefix string) ([]step, error) {
	if list.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: expects a list of steps", list.Line)
	}
	var out []step
	for i, n := range list.Content {
		where := fmt.Sprintf("%sstep %d", prefix, i+1)
		name, run, err := compileStep(n, sc, where+".")
		if err != nil {
			if name != "" {
				return nil, fmt.Errorf("%s (%s, line %d): %w", where, name, n.Line, err)
			}
			return nil, fmt.Errorf("%s (line %d): %w", where, n.Line, err)
		}
		out = append(out, step{name: name, line: n.Line, run: run})
	}
	return out, nil
}

// compileStep reads "- name", "- name: arg" or "- if: ... then: ... else: ...".
func compileStep(n *yaml.Node, sc *scope, prefix string) (string, Runner, error) {
	switch {
	case n.Kind == yaml.ScalarNode && n.Value != "":
		c, ok := ops[n.Value]
		if !ok {
			return n.Value, nil, fmt.Errorf("unknown step %q", n.Value)
		}
		run, err := c(nil, sc)
		return n.Value, run, err
	case n.Kind == yaml.MappingNode && field(n, "if") != nil:
		run, err := compileIf(n, sc, prefix)
		return "if", run, err
	case n.Kind == yaml.MappingNode && len(n.Content) == 2:
		name := n.Content[0].Value
		c, ok := ops[name]
		if !ok {
			return name, nil, fmt.Errorf("unknown step %q", name)
		}
		run, err := c(n.Content[1], sc)
		return name, run, err
	}
	return "", nil, fmt.Errorf("a step is a name, one 'name: argument' pair, or if/then/else")
}

func runSteps(ctx context.Context, s *State, steps []step) error {
	for _, st := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := st.run(ctx, s); err != nil {
			// The error text stays the step's own: it is the API's error message.
			s.rt.Log.Printf("step %s (line %d) failed: %v", st.name, st.line, err)
			return err
		}
	}
	return nil
}

// Run executes the steps and writes the output workbook to out. inputs maps
// every declared input name to a file path.
func (wf *Workflow) Run(ctx context.Context, rt Runtime, inputs map[string]string, out string) error {
	if rt.Cfg == nil {
		rt.Cfg = &config.Config{}
	}
	if rt.Log == nil {
		rt.Log = logx.Nop
	}
	for _, in := range wf.Inputs {
		if inputs[in.Name] == "" {
			return fmt.Errorf("missing input %q", in.Name)
		}
	}
	s := &State{rt: rt, Inputs: inputs, Tables: map[string]*Table{}}
	if err := runSteps(ctx, s, wf.steps); err != nil {
		return err
	}
	return writeWorkbook(ctx, out, wf.Sheets, s)
}

// Assign pairs uploaded file names with the inputs: inputs with a Match
// claim the first unclaimed matching file, the rest take the remaining files
// in upload order. It returns, per input, the index of its file.
func (wf *Workflow) Assign(names []string) []int {
	pick := make([]int, len(wf.Inputs))
	used := make([]bool, len(names))
	for i, in := range wf.Inputs {
		pick[i] = -1
		if in.Match == nil {
			continue
		}
		for j, n := range names {
			if !used[j] && in.Match.MatchString(n) {
				pick[i], used[j] = j, true
				break
			}
		}
	}
	next := 0
	for i := range pick {
		for pick[i] < 0 && next < len(names) {
			if !used[next] {
				pick[i], used[next] = next, true
			}
			next++
		}
	}
	return pick
}

// ---- control steps ------------------------------------------------------------

// tableEnv is the environment of if/log/fail and the rest/sql expressions.
func (s *State) tableEnv(extra map[string]any) map[string]any {
	env := map[string]any{
		"config": s.config,
		"table": func(name string) ([]any, error) {
			t, err := s.dataset(name)
			if err != nil {
				return nil, err
			}
			return rowMaps(t, allRows(t)), nil
		},
		"sheets": func(input string) ([]any, error) {
			names, err := sheetNames(s.Inputs[input])
			out := make([]any, len(names))
			for i, n := range names {
				out[i] = n
			}
			return out, err
		},
		"rows": []any{},
	}
	if s.Cur != nil {
		env["rows"] = rowMaps(s.Cur, allRows(s.Cur))
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func (s *State) config(key string) any { return s.rt.Cfg.Value(key) }

// - if: 'config("db_enabled")'
//
//	then: [steps]
//	else: [steps]
func compileIf(n *yaml.Node, sc *scope, prefix string) (Runner, error) {
	if err := onlyKeys(n, "if", "then", "else"); err != nil {
		return nil, err
	}
	cond, err := compileExpr(field(n, "if").Value, false, tableVars...)
	if err != nil {
		return nil, err
	}
	if field(n, "then") == nil {
		return nil, fmt.Errorf("if needs then")
	}
	then, err := compileSteps(field(n, "then"), sc, prefix+"then.")
	if err != nil {
		return nil, err
	}
	var els []step
	if e := field(n, "else"); e != nil {
		if els, err = compileSteps(e, sc, prefix+"else."); err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context, s *State) error {
		ok, err := cond.test(s.tableEnv(nil))
		if err != nil {
			return err
		}
		if ok {
			return runSteps(ctx, s, then)
		}
		return runSteps(ctx, s, els)
	}, nil
}

// onlyKeys rejects keys of mapping n other than the given ones.
func onlyKeys(n *yaml.Node, keys ...string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expects a mapping", n.Line)
	}
	for i := 0; i < len(n.Content); i += 2 {
		if !contains(keys, n.Content[i].Value) {
			return fmt.Errorf("line %d: unknown key %q (use %s)", n.Content[i].Line, n.Content[i].Value, strings.Join(keys, ", "))
		}
	}
	return nil
}

// - split: {where: '<row condition>', then: [steps], else: [steps]}
// Matching rows go through then, the others through else; the results are
// appended (then first), with the union of their columns.
func compileSplit(arg *yaml.Node, sc *scope) (Runner, error) {
	if arg == nil {
		return nil, fmt.Errorf("needs where, then and/or else")
	}
	if err := onlyKeys(arg, "where", "then", "else"); err != nil {
		return nil, err
	}
	if field(arg, "where") == nil {
		return nil, fmt.Errorf("needs where")
	}
	where, err := compileExpr(field(arg, "where").Value, true, rowVars...)
	if err != nil {
		return nil, err
	}
	var then, els []step
	if n := field(arg, "then"); n != nil {
		if then, err = compileSteps(n, sc, "split.then."); err != nil {
			return nil, err
		}
	}
	if n := field(arg, "else"); n != nil {
		if els, err = compileSteps(n, sc, "split.else."); err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context, s *State) error {
		t, err := s.current()
		if err != nil {
			return err
		}
		if err := where.check(t); err != nil {
			return err
		}
		yes, no := &Table{Columns: t.Columns}, &Table{Columns: t.Columns}
		for i := range t.Rows {
			ok, err := where.test(rowEnv(t, i, nil))
			if err != nil {
				return fmt.Errorf("row %d: %w", i+1, err)
			}
			if ok {
				yes.Rows = append(yes.Rows, t.Rows[i])
			} else {
				no.Rows = append(no.Rows, t.Rows[i])
			}
		}
		var parts []*Table
		for _, branch := range []struct {
			t     *Table
			steps []step
		}{{yes.clone(), then}, {no.clone(), els}} {
			s.Cur = branch.t
			if err := runSteps(ctx, s, branch.steps); err != nil {
				return err
			}
			parts = append(parts, s.Cur)
		}
		s.Cur = unionRows(parts...)
		return nil
	}, nil
}

// - log: "Loaded {len(rows)} rows"            (or {when: <condition>, message: ...})
func compileLog(arg *yaml.Node, _ *scope) (Runner, error) {
	when, msg, err := compileMessage(arg)
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, s *State) error {
		env := s.tableEnv(nil)
		if when != nil {
			if ok, err := when.test(env); err != nil || !ok {
				return err
			}
		}
		line, err := msg.render(env)
		if err != nil {
			return err
		}
		s.rt.Log.Print(line)
		return nil
	}, nil
}

// - fail: {when: '<condition>', message: "..."} stops the run with message.
func compileFail(arg *yaml.Node, _ *scope) (Runner, error) {
	when, msg, err := compileMessage(arg)
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, s *State) error {
		env := s.tableEnv(nil)
		if when != nil {
			if ok, err := when.test(env); err != nil || !ok {
				return err
			}
		}
		line, err := msg.render(env)
		if err != nil {
			return err
		}
		return fmt.Errorf("%s", line)
	}, nil
}

func compileMessage(arg *yaml.Node) (*program, *template, error) {
	if arg == nil {
		return nil, nil, fmt.Errorf("needs a message")
	}
	var spec struct {
		When    string `yaml:"when"`
		Message string `yaml:"message"`
	}
	if arg.Kind == yaml.ScalarNode {
		spec.Message = arg.Value
	} else if err := strictDecode(arg, &spec); err != nil {
		return nil, nil, err
	}
	if spec.Message == "" {
		return nil, nil, fmt.Errorf("needs a message")
	}
	msg, err := compileTemplate(spec.Message, false, tableVars...)
	if err != nil {
		return nil, nil, err
	}
	var when *program
	if spec.When != "" {
		if when, err = compileExpr(spec.When, false, tableVars...); err != nil {
			return nil, nil, err
		}
	}
	return when, msg, nil
}

// ---- document helpers -----------------------------------------------------------

var inputName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

func parseInputs(n *yaml.Node) ([]Input, error) {
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return nil, fmt.Errorf("input must name at least one file")
	}
	var out []Input
	for i := 0; i < len(n.Content); i += 2 {
		name := n.Content[i].Value
		if !inputName.MatchString(name) {
			return nil, fmt.Errorf("input %q: use letters, digits, '_' or '-'", name)
		}
		var spec struct {
			Label string `yaml:"label"`
			Match string `yaml:"match"`
		}
		if err := strictDecode(n.Content[i+1], &spec); err != nil {
			return nil, fmt.Errorf("input %s: %w", name, err)
		}
		in := Input{Name: name, Label: spec.Label}
		if in.Label == "" {
			in.Label = name
		}
		if spec.Match != "" {
			re, err := regexp.Compile(spec.Match)
			if err != nil {
				return nil, fmt.Errorf("input %s: match: %w", name, err)
			}
			in.Match = re
		}
		out = append(out, in)
	}
	return out, nil
}

func field(n *yaml.Node, k string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == k {
			return n.Content[i+1]
		}
	}
	return nil
}

func readNode(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("multiple YAML documents are not supported")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("document must be a mapping")
	}
	if err := checkNode(doc.Content[0]); err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}

func checkNode(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("line %d: YAML aliases are not supported", n.Line)
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Value == "<<" {
				return fmt.Errorf("line %d: mapping keys must be plain names; merge keys are not supported", k.Line)
			}
			if seen[k.Value] {
				return fmt.Errorf("line %d: duplicate key %q", k.Line, k.Value)
			}
			seen[k.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := checkNode(child); err != nil {
			return err
		}
	}
	return nil
}

// strictDecode decodes n into v, rejecting unknown fields.
func strictDecode(n *yaml.Node, v any) error {
	if n == nil || n.Kind == 0 {
		return nil
	}
	b, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(v); err != nil && err != io.EOF {
		return err
	}
	return nil
}
