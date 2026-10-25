package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const basicRecipe = `workflow: Example
input: {parts: parts.xlsx}
process:
  - from: parts["Parts"]
    as: result
    do: []
output:
  - file: result.xlsx
    sheets: {Result: result}
`

func recipeWithSteps(steps string) string {
	return strings.Replace(basicRecipe, "    do: []", "    do:\n"+steps, 1)
}
func mustParse(t *testing.T, text string) *Config {
	t.Helper()
	c, err := Parse([]byte(text), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestParseBasic(t *testing.T) {
	c := mustParse(t, basicRecipe)
	if c.Workflow != "Example" || !filepath.IsAbs(c.BaseDir) || len(c.Process) != 1 {
		t.Fatalf("unexpected config: %+v", c)
	}
}
func TestStrictYAML(t *testing.T) {
	tests := map[string]string{
		"unknown root":            basicRecipe + "surprise: true\n",
		"unknown process":         strings.Replace(basicRecipe, "    as: result", "    as: result\n    surprise: true", 1),
		"unknown operation field": recipeWithSteps("      - sort: {by: [Part], surprise: true}"),
		"duplicate root":          basicRecipe + "workflow: Again\n",
		"duplicate nested":        recipeWithSteps("      - sort: {by: [Part], by: [Other]}"),
		"multiple documents":      basicRecipe + "---\n{}\n",
		"alias":                   strings.Replace(basicRecipe, "input: {parts: parts.xlsx}", "input: &inputs {parts: parts.xlsx}\noutput_alias: *inputs", 1),
		"merge":                   basicRecipe + "other: {<<: {x: y}}\n",
		"unknown operation":       recipeWithSteps("      - explode: {}"),
		"multiple operations":     recipeWithSteps("      - filter: {column: Part, blank: true}\n        sort: {by: [Part]}"),
		"invalid disabled":        recipeWithSteps("      - enabled: false\n        calculate: {column: Total, columns: [Qty], operation: shell}"),
		"invalid sort enum":       recipeWithSteps("      - sort: {by: [Part], order: sideways}"),
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(text), t.TempDir()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
func TestPresetResolution(t *testing.T) {
	text := recipeWithSteps(`      - style: {preset: child, override: {header: {color: '112233'}}}
      - style: {preset: base}
      - group: {by: [Part], aggregation_preset: totals, aggregate: {Price: max}}
      - pipeline: {preset: cleaning, override: {do: [{sort: {by: [Price], order: descending}}]}}
      - transform: {preset: text, override: {columns: [Description]}}`) + `presets:
  styles:
    base: {header: {bold: true, background: AABBCC}, number_formats: {Qty: '0'}}
    child: {preset: base, header: {background: DDEEFF}}
  aggregations:
    totals: {columns: {Qty: sum, Price: sum}}
  pipelines:
    cleaning: {do: [{forward_fill: {columns: [Part]}}]}
  transforms:
    text: {columns: [Part, Category], operation: trim}
`
	c := mustParse(t, text)
	steps := c.Process[0].Do
	if len(steps) != 5 {
		t.Fatalf("expanded steps: %d", len(steps))
	}
	h := steps[0].Style.Header
	if !h.Bold || h.Background != "DDEEFF" || h.Color != "112233" {
		t.Fatalf("deep merge: %+v", h)
	}
	if steps[1].Style.Header.Background != "AABBCC" || steps[1].Style.Header.Color != "" {
		t.Fatal("preset was mutated")
	}
	if steps[2].Group.Aggregate["Qty"] != "sum" || steps[2].Group.Aggregate["Price"] != "max" {
		t.Fatalf("aggregation expansion: %+v", steps[2])
	}
	if steps[3].Sort == nil || steps[3].Sort.By[0] != "Price" {
		t.Fatal("pipeline list override did not replace")
	}
	if len(steps[4].Transform.Columns) != 1 || steps[4].Transform.Columns[0] != "Description" {
		t.Fatal("list override did not replace")
	}
}
func TestPresetErrors(t *testing.T) {
	for name, presets := range map[string]string{
		"unknown category":        "  bananas: {}",
		"missing":                 "  filters: {a: {preset: absent}}",
		"cycle":                   "  filters: {a: {preset: b}, b: {preset: a}}",
		"pipeline cycle":          "  pipelines: {a: {do: [{pipeline: {preset: b}}]}, b: {do: [{pipeline: {preset: a}}]}}",
		"unused invalid":          "  filters: {unused: {column: X, typo: true}}",
		"bad aggregation":         "  groups: {g: {by: [X], aggregation_preset: missing}}",
		"unknown inherited field": "  styles: {s: {header: {bold: true, surprise: true}}}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(basicRecipe+"presets:\n"+presets+"\n"), t.TempDir())
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
func TestImports(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "lib"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("lib/base.yaml", "presets:\n  filters:\n    useful: {column: Part, blank: false}\n")
	write("lib/more.yaml", "imports: [base.yaml]\npresets:\n  filters:\n    inherited: {preset: useful, contains: ABC}\n")
	root := recipeWithSteps("      - filter: {preset: inherited}") + "imports: [lib/more.yaml]\n"
	write("workflow.yaml", root)
	c, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseDir != dir || c.Process[0].Do[0].Filter.Contains == nil {
		t.Fatal("import did not resolve")
	}
	write("workflow.yaml", root+"presets:\n  filters:\n    useful: {column: X, blank: true}\n")
	if _, err = Load(filepath.Join(dir, "workflow.yaml")); err == nil || !strings.Contains(err.Error(), "duplicate preset") {
		t.Fatalf("collision: %v", err)
	}
	write("lib/base.yaml", "imports: [../workflow.yaml]\n")
	write("workflow.yaml", root)
	if _, err = Load(filepath.Join(dir, "workflow.yaml")); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle: %v", err)
	}
}
func TestDisabledPipeline(t *testing.T) {
	c := mustParse(t, recipeWithSteps(`      - enabled: false
        pipeline:
          do:
            - join: {with: never_created, on: [Part]}
            - sort: {by: [Part], order: ascending}`))
	for _, s := range c.Process[0].Do {
		if s.Enabled == nil || *s.Enabled {
			t.Fatal("pipeline disabled flag was lost")
		}
	}
}

func TestNullEnabledPipelineDefaultsToEnabled(t *testing.T) {
	c := mustParse(t, recipeWithSteps(`      - enabled: null
        pipeline: {do: [{sort: {by: [Part]}}]}`))
	if c.Process[0].Do[0].Enabled != nil && !*c.Process[0].Do[0].Enabled {
		t.Fatal("null enabled unexpectedly disabled the step")
	}
}
func TestImportSymlinkCycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(basicRecipe+"imports: [alias.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(dir, "alias.yaml")); err != nil {
		t.Skip(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("symlink cycle: %v", err)
	}
}
