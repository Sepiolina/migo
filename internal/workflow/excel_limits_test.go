package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExcelCellLengthLimits(t *testing.T) {
	for _, header := range []bool{false, true} {
		t.Run(map[bool]string{false: "value", true: "header"}[header], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "first.xlsx")
			original := []byte("existing report")
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			cfg := &Config{BaseDir: dir, Output: []Output{{File: "first.xlsx", Sheets: map[string]string{"Good": "good"}}, {File: "last.xlsx", Sheets: map[string]string{"Report": "bad"}}}}
			bad := &Table{Columns: []string{"Text"}, Rows: [][]string{{strings.Repeat("界", excelize.TotalCellChars+1)}}}
			cell := "A2"
			if header {
				bad.Columns[0], bad.Rows[0][0] = bad.Rows[0][0], "ok"
				cell = "A1"
			}
			err := exportWorkbooks(context.Background(), cfg, map[string]*Table{"good": {Columns: []string{"Text"}}, "bad": bad}, true)
			if err == nil || !strings.Contains(err.Error(), "Report") || !strings.Contains(err.Error(), "cell "+cell) {
				t.Fatalf("error=%v", err)
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != string(original) {
				t.Fatalf("existing output changed: %q %v", got, err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatalf("published/staged files remain: %v", entries)
			}
		})
	}
	dir := t.TempDir()
	boundary := strings.Repeat("界", excelize.TotalCellChars)
	cfg := &Config{BaseDir: dir, Output: []Output{{File: "boundary.xlsx", Sheets: map[string]string{"Data": "data"}}}}
	if err := exportWorkbooks(context.Background(), cfg, map[string]*Table{"data": {Columns: []string{"Text"}, Rows: [][]string{{boundary}}}}, true); err != nil {
		t.Fatal(err)
	}
	f := openResult(t, filepath.Join(dir, "boundary.xlsx"))
	if got := value(t, f, "Data", "A2"); got != boundary {
		t.Fatalf("boundary string truncated to %d bytes", len(got))
	}
}

func TestScratchSheetNameCollision(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{BaseDir: dir, Output: []Output{{File: "result.xlsx", Sheets: map[string]string{"_WORKFLOW_0": "data", "_workflow_1": "data"}}}}
	if err := exportWorkbooks(context.Background(), cfg, map[string]*Table{"data": {Columns: []string{"Text"}}}, true); err != nil {
		t.Fatal(err)
	}
	f := openResult(t, filepath.Join(dir, "result.xlsx"))
	if len(f.GetSheetList()) != 2 {
		t.Fatalf("unexpected sheets: %v", f.GetSheetList())
	}
}
