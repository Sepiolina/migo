package bom

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"miservice/internal/config"
	"miservice/internal/logx"
)

func TestRecipeDefaultCompilesOriginalStages(t *testing.T) {
	r, err := DefaultRecipe()
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Build(&config.Config{}, logx.Nop, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	original := DefaultStages(Deps{})
	if len(p.Stages) != len(original) {
		t.Fatalf("got %d stages", len(p.Stages))
	}
	for i := range original {
		if p.Stages[i].Name != original[i].Name {
			t.Fatalf("stage %d: %q", i, p.Stages[i].Name)
		}
	}
}

func TestRecipeStrictParsing(t *testing.T) {
	valid := string(defaultRecipeYAML)
	tests := []struct{ name, data, want string }{
		{"unknown top", valid + "extra: nope\n", "field extra"},
		{"unknown step", strings.Replace(valid, "stage: group-rows", "stage: group-rows\n    extra: nope", 1), "field extra"},
		{"unknown output", valid + "  extra: nope\n", "field extra"},
		{"unknown input", strings.Replace(valid, "  bom: bom.xls", "  other: bom.xls", 1), "exactly"},
		{"duplicate", valid + "engine: bom\n", "duplicate key"},
		{"duplicate nested", strings.Replace(valid, "  bom: bom.xls", "  bom: bom.xls\n  bom: other.xls", 1), "duplicate key"},
		{"documents", valid + "---\nworkflow: second\n", "multiple YAML documents"},
		{"aliases", strings.Replace(strings.Replace(valid, "  bom: bom.xls", "  bom: &path bom.xls", 1), "  priority: priority.xls", "  priority: *path", 1), "aliases"},
		{"merge", valid + "<<: {}\n", "merge keys"},
		{"engine", strings.Replace(valid, "engine: bom", "engine: table", 1), "engine must be bom"},
		{"output", strings.Replace(valid, "output.xlsx", "output.xls", 1), ".xlsx"},
		{"blank input", strings.Replace(valid, "bom: bom.xls", "bom: ' '", 1), "nonblank"},
		{"unknown disabled", strings.Replace(valid, "stage: group-rows", "stage: missing\n    enabled: false", 1), "unknown stage"},
		{"duplicate disabled", strings.Replace(valid, "stage: group-rows", "stage: read-inputs\n    enabled: false", 1), "duplicate stage"},
		{"disabled read", strings.Replace(valid, "stage: read-inputs", "stage: read-inputs\n    enabled: false", 1), "first"},
		{"disabled write", strings.Replace(valid, "stage: write-workbook", "stage: write-workbook\n    enabled: false", 1), "last"},
		{"group ordering", strings.Replace(valid, "- stage: group-rows\n  - stage: pcb-scope", "- stage: pcb-scope\n  - stage: group-rows", 1), "requires enabled group-rows"},
		{"optional enrichment ordering", strings.Replace(valid, "- stage: sap-enrich\n  - stage: cft-002-priority", "- stage: cft-002-priority\n  - stage: sap-enrich", 1), "requires enabled sap-enrich"},
		{"disabled dependency", strings.Replace(valid, "stage: material-master", "stage: material-master\n    enabled: false", 1), "requires enabled material-master"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRecipe([]byte(tt.data), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v; want %s", err, tt.want)
			}
		})
	}
}

func TestRecipeDisabledStagesAreNotExecuted(t *testing.T) {
	r, err := DefaultRecipe()
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	for i := range r.Process {
		if r.Process[i].Stage == "material-master" || r.Process[i].Stage == "sap-enrich" {
			r.Process[i].Enabled = &disabled
		}
	}
	called := false
	p, err := r.Build(&config.Config{DB: config.DB{Enabled: true}}, logx.Nop, Deps{Lookup: func(context.Context, []string) (map[string]Match, error) {
		called = true
		return nil, errors.New("must not run")
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Stages) != 7 {
		t.Fatalf("got %d stages", len(p.Stages))
	}
	for _, s := range p.Stages {
		if s.Name == "material-master" || s.Name == "sap-enrich" {
			t.Fatalf("disabled stage compiled: %s", s.Name)
		}
	}
	if err := p.Process(context.Background(), "../../../testdata/bom_random7.xls", "../../../testdata/prio_random7.xls", filepath.Join(t.TempDir(), "out.xlsx")); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("disabled lookup ran")
	}
	// A custom read/write recipe is executable when no domain stage requires others.
	r.Process = []RecipeStep{{Stage: "read-inputs"}, {Stage: "write-workbook"}}
	if _, err := r.Build(&config.Config{}, logx.Nop, Deps{}); err != nil {
		t.Fatal(err)
	}
}

func TestRecipeRunAndCheck(t *testing.T) {
	base := t.TempDir()
	r, err := DefaultRecipe()
	if err != nil {
		t.Fatal(err)
	}
	r.BaseDir = base
	for key, source := range map[string]string{"bom": "bom_random7.xls", "priority": "prio_random7.xls"} {
		data, err := os.ReadFile(filepath.Join("../../../testdata", source))
		if err != nil {
			t.Fatal(err)
		}
		r.Input[key] = source
		if err := os.WriteFile(filepath.Join(base, source), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{}
	target := filepath.Join(base, r.Output.File)
	original := []byte("existing output")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.Check(context.Background(), cfg, logx.Nop, Deps{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(original) {
		t.Fatalf("Check published output: %q %v", got, err)
	}
	if err := r.Run(context.Background(), cfg, logx.Nop, Deps{}); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(base, "expected.xlsx")
	if err := NewPipeline(cfg, logx.Nop, Deps{}).Process(context.Background(), filepath.Join(base, r.Input["bom"]), filepath.Join(base, r.Input["priority"]), expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readAll(t, target), readAll(t, expected)) {
		t.Fatal("recipe differs from original pipeline")
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".bom-recipe-") {
			t.Fatal("temporary workbook leaked")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx, cfg, logx.Nop, Deps{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestRecipeProtectsInputsAndDoesNotCreateDirectories(t *testing.T) {
	for _, kind := range []string{"same path", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			input := filepath.Join(base, "input.xlsx")
			if err := os.WriteFile(input, []byte("preserve me"), 0600); err != nil {
				t.Fatal(err)
			}
			r, err := DefaultRecipe()
			if err != nil {
				t.Fatal(err)
			}
			r.BaseDir = base
			r.Input = map[string]string{"bom": "input.xlsx", "priority": "input.xlsx"}
			r.Output.File = "out.xlsx"
			switch kind {
			case "same path":
				r.Output.File = "input.xlsx"
			case "symlink":
				err = os.Symlink(input, filepath.Join(base, "out.xlsx"))
			case "hardlink":
				err = os.Link(input, filepath.Join(base, "out.xlsx"))
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, run := range []func(context.Context, *config.Config, logx.Logger, Deps) error{r.Run, r.Check} {
				if err := run(context.Background(), &config.Config{}, logx.Nop, Deps{}); err == nil || !strings.Contains(err.Error(), "overwrite input") {
					t.Fatalf("got %v", err)
				}
			}
			got, err := os.ReadFile(input)
			if err != nil || string(got) != "preserve me" {
				t.Fatalf("input modified: %v", err)
			}
			r.Output.File = "missing/out.xlsx"
			if err := r.Run(context.Background(), &config.Config{}, logx.Nop, Deps{}); err == nil || !strings.Contains(err.Error(), "directory") {
				t.Fatalf("got %v", err)
			}
			if _, err := os.Stat(filepath.Join(base, "missing")); !os.IsNotExist(err) {
				t.Fatal("output directory created")
			}
		})
	}
}

func TestRecipeStepErrorContext(t *testing.T) {
	r, err := DefaultRecipe()
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Build(&config.Config{}, logx.Nop, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	err = p.Process(context.Background(), "does-not-exist.xls", "does-not-exist.xls", filepath.Join(t.TempDir(), "out.xlsx"))
	if err == nil || !strings.Contains(err.Error(), "process 1 (read-inputs)") {
		t.Fatalf("got %v", err)
	}
}
