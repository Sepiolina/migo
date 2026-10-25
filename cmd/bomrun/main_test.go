package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWorkflowCLI(t *testing.T) {
	dir := t.TempDir()
	f := excelize.NewFile()
	for axis, value := range map[string]string{"A1": "Part", "B1": "Quantity", "A2": "001", "B2": "2", "A3": "001", "B3": "3"} {
		if err := f.SetCellStr("Sheet1", axis, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SetSheetDimension("Sheet1", "A1:B3"); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(filepath.Join(dir, "parts.xlsx")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	path := filepath.Join(dir, "recipe.yaml")
	recipe := `workflow: CLI integration
input: {parts: parts.xlsx}
process:
  - from: 'parts["Sheet1"]'
    as: totals
    do:
      - group:
          by: [Part]
          aggregate: {Quantity: sum}
output:
  - file: result.xlsx
    sheets: {BOM: totals}
`
	if err := os.WriteFile(path, []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"-workflow", path, "-check"}, &stdout, &stderr); code != 0 {
		t.Fatalf("check code %d: %s", code, &stderr)
	}
	out := filepath.Join(dir, "result.xlsx")
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("check published output: %v", err)
	}
	if code := run(context.Background(), []string{"-workflow", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("run code %d: %s", code, &stderr)
	}
	got, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	rows, err := got.GetRows("BOM")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"Part", "Quantity"}, {"001", "5"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows=%v want=%v", rows, want)
	}
	if !strings.Contains(stdout.String(), "no outputs published") {
		t.Fatalf("stdout=%s", &stdout)
	}
}

func TestLegacyCLI(t *testing.T) {
	out := filepath.Join(t.TempDir(), "legacy.xlsx")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"-bom", "../../testdata/bom_basic.xlsx", "-prio", "../../testdata/prio_basic.xlsx", "-out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("legacy code %d: %s", code, &stderr)
	}
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := f.GetSheetList(); !reflect.DeepEqual(got, []string{"BOM", "debug"}) {
		t.Fatalf("sheets=%v", got)
	}
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil || len(rows) < 2 || len(rows[0]) < 15 || rows[0][1] != "Part Number" {
			t.Fatalf("legacy %s contents %v: %v", name, rows, err)
		}
	}
}

func TestCLIUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"-check"}, {"-workflow", "x", "-bom", "y"}, {"-workflow", "x", "-out", "y"}, {"unexpected"}, {"-cfg", "bad-json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), args, &stdout, &stderr); code != 2 || stderr.Len() == 0 {
				t.Fatalf("code=%d stderr=%s", code, &stderr)
			}
		})
	}
}
