// Package workflow runs one YAML file per process. A workflow names its
// uploaded inputs, an ordered list of steps and the workbook it produces:
//
//	name: Parts summary
//	input:
//	  parts: {label: Parts}
//	steps:
//	  - from: parts[Parts]
//	  - filter: 'num("Quantity") > 0'
//	  - save: summary
//	output:
//	  sheets: {Parts: summary}
//
// Steps come from a Registry: TableOps for generic sheet work, plus any
// domain steps a module registers (for example the BOM steps). Every step is
// compiled when the file is loaded, so a typo fails at startup, not mid-request.
package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"miservice/internal/config"
	"miservice/internal/logx"
)

// Workflow is a loaded, compiled workflow file.
type Workflow struct {
	Name   string
	Inputs []Input  // upload order
	Sheets []Sheet  // output.sheets, in declared order (empty when a step writes)
	Steps  []string // step names, for logs and tests
	run    []Runner
}

// Input is one uploaded file. Match (a regexp on the uploaded filename) lets
// an input claim its file regardless of upload order.
type Input struct {
	Name  string
	Label string
	Match *regexp.Regexp
}

// Sheet maps an output sheet to a saved dataset.
type Sheet struct{ Name, Dataset string }

// State is what steps share during one run.
type State struct {
	Cfg    *config.Config
	Log    logx.Logger
	Inputs map[string]string // input name -> file path
	Out    string            // output workbook path
	Tables map[string]*Table // saved datasets
	Cur    *Table            // the table the generic steps work on
	Data   map[string]any    // domain state, keyed by module (e.g. "bom")
}

// Runner executes one compiled step.
type Runner func(ctx context.Context, s *State) error

// Op describes a step type. Compile receives the step's argument (nil when
// the step is written as a bare name) and checks it before anything runs.
type Op struct {
	Compile func(arg *yaml.Node) (Runner, error)
	Needs   []string // steps that must appear earlier
	After   []string // steps that, when present, must appear earlier
	Writes  bool     // the step writes the output workbook itself
}

// Registry is the set of step types a workflow may use.
type Registry map[string]Op

// With returns a registry holding r's steps plus more.
func (r Registry) With(more Registry) Registry {
	out := Registry{}
	for k, v := range r {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}

// Load reads and compiles a workflow file.
func Load(path string, reg Registry) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	wf, err := Parse(data, reg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return wf, nil
}

type document struct {
	Name   string      `yaml:"name"`
	Input  yaml.Node   `yaml:"input"`
	Steps  []yaml.Node `yaml:"steps"`
	Output struct {
		Sheets yaml.Node `yaml:"sheets"`
	} `yaml:"output"`
}

// Parse compiles a workflow document. YAML aliases, merge keys, duplicate
// keys and unknown fields are rejected so every file reads exactly as it runs.
func Parse(data []byte, reg Registry) (*Workflow, error) {
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
	if wf.Sheets, err = parseSheets(&doc.Output.Sheets); err != nil {
		return nil, err
	}
	if len(doc.Steps) == 0 {
		return nil, fmt.Errorf("steps must not be empty")
	}
	seen := map[string]int{}
	writes := 0
	for i, n := range doc.Steps {
		name, arg, err := splitStep(&n)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		op, ok := reg[name]
		if !ok {
			return nil, fmt.Errorf("step %d: unknown step %q", i+1, name)
		}
		for _, need := range op.Needs {
			if _, ok := seen[need]; !ok {
				return nil, fmt.Errorf("step %d (%s): requires %s before it", i+1, name, need)
			}
		}
		for _, after := range op.After {
			for _, later := range doc.Steps[i+1:] {
				if n, _, _ := splitStep(&later); n == after {
					return nil, fmt.Errorf("step %d (%s): %s must come before it", i+1, name, after)
				}
			}
		}
		run, err := op.Compile(arg)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, name, err)
		}
		if op.Writes {
			writes++
		}
		seen[name] = i
		wf.Steps = append(wf.Steps, name)
		wf.run = append(wf.run, run)
	}
	switch {
	case writes > 1:
		return nil, fmt.Errorf("only one step may write the output")
	case writes == 1 && len(wf.Sheets) > 0:
		return nil, fmt.Errorf("output.sheets cannot be combined with a step that writes the output")
	case writes == 0 && len(wf.Sheets) == 0:
		return nil, fmt.Errorf("output.sheets must name at least one sheet")
	}
	return wf, nil
}

// Run executes the steps and writes the output workbook to out. inputs maps
// every declared input name to a file path.
func (wf *Workflow) Run(ctx context.Context, cfg *config.Config, log logx.Logger, inputs map[string]string, out string) error {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if log == nil {
		log = logx.Nop
	}
	for _, in := range wf.Inputs {
		if inputs[in.Name] == "" {
			return fmt.Errorf("missing input %q", in.Name)
		}
	}
	s := &State{Cfg: cfg, Log: log, Inputs: inputs, Out: out, Tables: map[string]*Table{}, Data: map[string]any{}}
	for i, run := range wf.run {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := run(ctx, s); err != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, wf.Steps[i], err)
		}
	}
	if len(wf.Sheets) == 0 {
		return nil
	}
	return writeWorkbook(ctx, out, wf.Sheets, s.Tables)
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

var sheetName = regexp.MustCompile(`^[^\[\]:*?/\\]{1,31}$`)

func parseSheets(n *yaml.Node) ([]Sheet, error) {
	if n.Kind == 0 {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("output.sheets must map sheet names to datasets")
	}
	var out []Sheet
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		name, ds := n.Content[i].Value, n.Content[i+1].Value
		if !sheetName.MatchString(name) || strings.TrimSpace(name) != name {
			return nil, fmt.Errorf("output sheet %q: invalid Excel sheet name", name)
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("output sheet %q: duplicate (sheet names ignore case)", name)
		}
		seen[strings.ToLower(name)] = true
		if n.Content[i+1].Kind != yaml.ScalarNode || ds == "" {
			return nil, fmt.Errorf("output sheet %q: must name a saved dataset", name)
		}
		out = append(out, Sheet{name, ds})
	}
	return out, nil
}

// splitStep reads "- name" or "- name: arg".
func splitStep(n *yaml.Node) (string, *yaml.Node, error) {
	switch {
	case n.Kind == yaml.ScalarNode && n.Value != "":
		return n.Value, nil, nil
	case n.Kind == yaml.MappingNode && len(n.Content) == 2:
		return n.Content[0].Value, n.Content[1], nil
	}
	return "", nil, fmt.Errorf("line %d: a step is a name or a single 'name: argument' pair", n.Line)
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

// Decode is strictDecode for step compilers in other packages.
func Decode(arg *yaml.Node, v any) error { return strictDecode(arg, v) }
