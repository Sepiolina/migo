package workflow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

// Keep the recipes users copy executable, including imported preset pipelines.
func TestBundledExamples(t *testing.T) {
	for _, name := range []string{"basic.yaml", "presets.yaml"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(filepath.Join("../../examples/workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "result.xlsx")
			cfg.Output[0].File = out
			if err := Run(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			f := openResult(t, out)
			rows, err := f.GetRows("Parts", excelize.Options{RawCellValue: true})
			if err != nil {
				t.Fatal(err)
			}
			want := [][]string{{"Part Number", "Description", "Quantity"}, {"00123", "Resistor", "5"}, {"00456", "Capacitor", "1"}}
			if !reflect.DeepEqual(rows, want) {
				t.Fatalf("rows=%v want=%v", rows, want)
			}
			// The exported dimension must make all data visible to our shared reader.
			reread := &Config{Input: map[string]string{"out": out}, Process: []Process{{From: `out["Parts"]`, As: "data"}}, Output: []Output{{File: filepath.Join(t.TempDir(), "again.xlsx"), Sheets: map[string]string{"Parts": "data"}}}}
			tables, err := Execute(context.Background(), reread)
			if err != nil || !reflect.DeepEqual(tables["data"].Rows, want[1:]) {
				t.Fatalf("round trip: %v, %v", tables, err)
			}
		})
	}
}

func TestAdvancedExample(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "generated"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../examples/workflows/advanced.yaml")
	if err != nil {
		t.Fatal(err)
	}
	workbookFixture(t, filepath.Join(dir, "current.xlsx"), map[string][][]string{"Parts": {{"Part Number", "Description", "Quantity", "Category"}, {"001", "Resistor", "2", "Passive"}, {"001", "Resistor", "3", ""}, {"002", "Capacitor", "1", "Passive"}, {"003", "New", "4", "Other"}}})
	workbookFixture(t, filepath.Join(dir, "previous.xlsx"), map[string][][]string{"Parts": {{"Part Number", "Description", "Quantity"}, {"001", "Resistor", "4"}, {"002", "Capacitor", "1"}, {"004", "Old", "1"}}})
	workbookFixture(t, filepath.Join(dir, "catalog.xlsx"), map[string][][]string{"Catalog": {{"Part Number", "Unit Cost"}, {"001", "2.5"}, {"002", "1"}, {"001", "999"}}})
	cfg, err := Parse(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	f := openResult(t, filepath.Join(dir, "generated", "purchasing.xlsx"))
	changes, err := f.GetRows("Changes")
	if err != nil {
		t.Fatal(err)
	}
	wantChanges := [][]string{{"Part Number", "Description", "Quantity", "Category", "status"}, {"001", "Resistor", "5", "Passive", "changed"}, {"002", "Capacitor", "1", "Passive", "unchanged"}, {"003", "New", "4", "Other", "added"}, {"004", "Old", "1", "", "removed"}}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Fatalf("changes=%v want=%v", changes, wantChanges)
	}
	purchasing, err := f.GetRows("Purchasing")
	if err != nil {
		t.Fatal(err)
	}
	wantPurchasing := [][]string{{"Part Number", "Description", "Quantity", "Category", "Unit Cost", "Extended Cost"}, {"001", "Resistor", "5.00", "Passive", "2.50", "12.50"}, {"002", "Capacitor", "1.00", "Passive", "1.00", "1.00"}}
	if !reflect.DeepEqual(purchasing, wantPurchasing) {
		t.Fatalf("purchasing=%v want=%v", purchasing, wantPurchasing)
	}
}
