package workflow

import (
	"math"
	"strings"
	"testing"
)

func TestAllOperations(t *testing.T) {
	steps := `      - map: {columns: [{from: Part, to: Number}, {from: '@B', to: Qty, move: true}], keep: true}
      - filter: {all: [{column: Qty, greater_than: 0}, {any: [{column: Number, contains: ABC}, {column: Number, in: [X, Y]}]}]}
      - forward_fill: {columns: [Category]}
      - group: {by: [Number], aggregate: {Qty: sum}}
      - aggregate: {columns: {Qty: mean}}
      - sort: {by: [Qty], order: descending, numeric: true}
      - deduplicate: {columns: [Number]}
      - calculate: {column: Total, columns: [Qty, Price], operation: multiply, value: 2}
      - transform: {columns: [Number], operation: replace, old: '-', new: ''}
      - join: {with: previous, on: [Number], right_on: [Part], how: inner, normalize: true}
      - merge: {with: [previous]}
      - compare: {with: previous, by: [Number], columns: [Qty], status_column: Changed}
      - style: {header: {bold: true, background: AABBCC, color: '000000'}, auto_width: true, freeze_header: true, number_formats: {Qty: '0.00'}}`
	text := strings.Replace(recipeWithSteps(steps), "process:\n", `process:
  - from: parts["Parts"]
    as: previous
    do: []
`, 1)
	c := mustParse(t, text)
	names := []string{"map", "filter", "forward_fill", "group", "aggregate", "sort", "deduplicate", "calculate", "transform", "join", "merge", "compare", "style"}
	for i, step := range c.Process[1].Do {
		if StepName(step) != names[i] {
			t.Fatalf("step %d: %s", i, StepName(step))
		}
	}
}
func TestInvalidOperationStructures(t *testing.T) {
	tests := map[string]string{
		"empty map":             "map: {columns: []}",
		"duplicate target":      "map: {columns: [{from: A, to: B}, {from: C, to: B}]}",
		"bad index":             "forward_fill: {columns: ['@0']}",
		"duplicate columns":     "deduplicate: {columns: [A, A]}",
		"empty group":           "group: {by: []}",
		"bad aggregation":       "aggregate: {columns: {Qty: execute}}",
		"empty aggregate":       "aggregate: {columns: {}}",
		"empty sort":            "sort: {by: []}",
		"bad calculation":       "calculate: {column: T, columns: [A], operation: eval}",
		"no operands":           "calculate: {column: T, operation: add}",
		"nonfinite calculation": "calculate: {column: T, columns: [A], operation: add, value: .inf}",
		"bad transform":         "transform: {columns: [A], operation: eval}",
		"bad join":              "join: {with: result, on: [A], how: outer}",
		"join key lengths":      "join: {with: result, on: [A], right_on: [A, B]}",
		"empty merge":           "merge: {with: []}",
		"empty compare":         "compare: {with: result, by: []}",
		"bad style column":      "style: {number_formats: {'@': '0'}}",
		"empty filter":          "filter: {}",
		"mixed filter":          "filter: {column: A, equal: x, any: [{column: B, blank: true}]}",
		"empty filter all":      "filter: {all: []}",
		"empty filter in":       "filter: {column: A, in: []}",
		"nonfinite filter":      "filter: {column: A, greater_than: .nan}",
	}
	for name, op := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(recipeWithSteps("      - "+op)), t.TempDir()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := validateStep(Step{Calculate: &CalculateSpec{Column: "Total", Columns: []string{"Qty"}, Operation: "add", Value: floatPtr(math.NaN())}}); err == nil {
		t.Fatal("NaN was allowed")
	}
}
func floatPtr(v float64) *float64 { return &v }
func TestParseSource(t *testing.T) {
	for _, v := range []struct{ source, alias, sheet string }{{`parts["My Sheet"]`, "parts", "My Sheet"}, {`parts["A\"B"]`, "parts", `A"B`}, {"dataset", "", ""}} {
		alias, sheet, err := ParseSource(v.source)
		if err != nil || alias != v.alias || sheet != v.sheet {
			t.Fatalf("%q: %q %q %v", v.source, alias, sheet, err)
		}
	}
	for _, s := range []string{"", " ", `parts[Sheet]`, `parts[""]`, `["Sheet"]`, `parts["Sheet"]tail`, ` parts["Sheet"]`, `   ["Sheet"]`, `data]`, `parts["x"]["y"]`} {
		if _, _, err := ParseSource(s); err == nil {
			t.Errorf("expected invalid source %q", s)
		}
	}
}
func TestReferencesAndOutputs(t *testing.T) {
	tests := map[string]string{
		"unknown input":   strings.Replace(basicRecipe, `parts["Parts"]`, `unknown["Parts"]`, 1),
		"forward process": strings.Replace(basicRecipe, `parts["Parts"]`, "result", 1),
		"duplicate dataset": strings.Replace(basicRecipe, "output:", `  - from: result
    as: result
    do: []
output:`, 1),
		"alias collision":       strings.Replace(basicRecipe, "as: result", "as: parts", 1),
		"negative header":       strings.Replace(basicRecipe, "as: result", "as: result\n    header_row: -1", 1),
		"unknown join":          recipeWithSteps("      - join: {with: result, on: [A]}"),
		"unknown merge":         recipeWithSteps("      - merge: {with: [missing]}"),
		"unknown compare":       recipeWithSteps("      - compare: {with: missing, by: [A]}"),
		"unknown output":        strings.Replace(basicRecipe, "{Result: result}", "{Result: missing}", 1),
		"bad extension":         strings.Replace(basicRecipe, "file: result.xlsx", "file: result.xls", 1),
		"bad sheet":             strings.Replace(basicRecipe, "{Result: result}", "{'Invalid/Sheet': result}", 1),
		"case duplicate sheets": strings.Replace(basicRecipe, "{Result: result}", "{Result: result, result: result}", 1),
		"long sheet":            strings.Replace(basicRecipe, "{Result: result}", "{'"+strings.Repeat("A", 32)+"': result}", 1),
		"duplicate file":        basicRecipe + "  - file: ./result.xlsx\n    sheets: {Other: result}\n",
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(text), t.TempDir()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if err := Validate(nil); err == nil {
		t.Fatal("nil config accepted")
	}
}
