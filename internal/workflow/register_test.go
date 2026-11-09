package workflow_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"miservice/internal/workflow"
)

func init() {
	workflow.RegisterTableStep("upper_test", func(arg *yaml.Node) (workflow.TableFunc, error) {
		var cols []string
		if err := arg.Decode(&cols); err != nil || len(cols) == 0 {
			return nil, err
		}
		return func(t *workflow.Table) (*workflow.Table, error) {
			for _, c := range cols {
				i := -1
				for j, name := range t.Columns {
					if name == c {
						i = j
					}
				}
				for _, r := range t.Rows {
					if s, ok := r[i].(string); ok {
						r[i] = strings.ToUpper(s)
					}
				}
			}
			return t, nil
		}, nil
	})
}

func TestRegisteredTableStep(t *testing.T) {
	wf, err := workflow.Parse([]byte(`name: Upper
input: {parts: {}}
steps:
  - from: parts
  - upper_test: [Description]
  - save: out
output: {sheets: {Out: out}}
`))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.xlsx")
	if err := wf.Run(context.Background(), workflow.Runtime{}, map[string]string{"parts": "../../examples/parts.xlsx"}, out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range workflow.StepNames() {
		found = found || n == "upper_test"
	}
	if !found {
		t.Error("StepNames misses the registered step")
	}
	defer func() {
		if recover() == nil {
			t.Error("registering a name twice must panic")
		}
	}()
	workflow.RegisterTableStep("filter", nil)
}

func TestServiceDefaults(t *testing.T) {
	wf, err := workflow.Parse([]byte("name: X\ninput: {a: {}}\nsteps: [{from: a}, {save: t}]\noutput: {sheets: {T: t}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s := wf.Service; s.Route != "" || s.OwnerField != "bam" || !s.OwnerRequired || s.MaxUploadMB != 0 {
		t.Errorf("defaults %+v", s)
	}
	if _, err := workflow.Parse([]byte("name: X\nservice: {nope: 1}\ninput: {a: {}}\nsteps: [{from: a}, {save: t}]\noutput: {sheets: {T: t}}\n")); err == nil {
		t.Error("unknown service keys must be rejected")
	}
}
