package workflow

import (
	"fmt"
	"strings"
	"testing"
)

// Every step a workflow may use has a valid descriptor, so editors can
// offer it. A new step without one fails here.
func TestEveryStepIsDescribed(t *testing.T) {
	described := map[string]bool{}
	items := itemTypes()
	for _, s := range Describe().Steps {
		if described[s.Op] {
			t.Errorf("step %q is described twice", s.Op)
		}
		described[s.Op] = true
		if err := checkStepInfo(s, items); err != nil {
			t.Error(err)
		}
		if s.Op != "if" && ops[s.Op] == nil {
			t.Errorf("descriptor for %q, which is not a registered step", s.Op)
		}
	}
	for _, name := range StepNames() {
		if !described[name] {
			t.Errorf("step %q has no descriptor: add a StepInfo to builtinSteps (steps_info.go) or call DescribeStep", name)
		}
	}
	for _, it := range builtinItems {
		fields := it.Fields
		if it.Shape == "entry" {
			if it.EntryKey == nil || it.EntryValue == nil {
				t.Errorf("item %q needs EntryKey and EntryValue", it.Type)
				continue
			}
			fields = []FieldInfo{*it.EntryKey, *it.EntryValue}
		}
		if err := checkFields(fields, items); err != nil {
			t.Errorf("item %q: %v", it.Type, err)
		}
	}
}

// The example a new step starts with is accepted by the step's compiler.
func TestStepExamplesCompile(t *testing.T) {
	for _, s := range Describe().Steps {
		if s.Example == "" {
			continue // inline forms (if) start empty
		}
		doc := fmt.Sprintf("name: T\ninput:\n  input: {label: In}\nsteps:\n"+
			"  - from: input\n  - save: other\n  - save: baseline\n  - %s: %s\n  - save: out\n"+
			"output:\n  sheets: {Out: out}\n", s.Op, s.Example)
		if _, err := Parse([]byte(doc)); err != nil {
			t.Errorf("%s example %q: %v", s.Op, s.Example, err)
		}
	}
}

func TestFunctionsDescribed(t *testing.T) {
	var names []string
	for _, f := range Describe().Functions {
		names = append(names, f.Name)
		if !strings.HasPrefix(f.Signature, f.Name+"(") || f.Doc == "" {
			t.Errorf("function %q needs a signature and a doc line", f.Name)
		}
	}
	if got, want := strings.Join(names, " "), strings.Join(globalVars, " "); got != want {
		t.Errorf("functions (describe.go) list %q, the expression helpers are %q", got, want)
	}
}

func TestDescribeStepRejectsMistakes(t *testing.T) {
	for name, info := range map[string]StepInfo{
		"repeated": {Op: "filter"},
		"category": {Op: "x1", Title: "X", Description: "X.", Category: "Nope", Form: "map", Fields: []FieldInfo{{Kind: "text"}}},
		"kind":     {Op: "x2", Title: "X", Description: "X.", Category: "Rows", Form: "map", Fields: []FieldInfo{{Kind: "slider"}}},
		"short":    {Op: "x3", Title: "X", Description: "X.", Category: "Rows", Form: "map", List: short("by"), Fields: []FieldInfo{{Kind: "list", Name: "A"}}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: DescribeStep accepted an invalid descriptor", name)
				}
			}()
			DescribeStep(info)
		}()
	}
}
