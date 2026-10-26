package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"miservice/internal/config"
)

// stubSQL answers every query with the table in path ({"columns": [...],
// "rows": [[...]]}); the workflow's joins pick the rows it asked for.
func stubSQL(t *testing.T, path string) SQLFunc {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tb Table
	if err := json.Unmarshal(raw, &tb); err != nil {
		t.Fatal(err)
	}
	return func(context.Context, string, string, []any) (*Table, error) { return &tb, nil }
}

func loadBOM(t *testing.T) *Workflow {
	t.Helper()
	wf, err := Load("../../workflows/bom.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return wf
}

func cfg(t *testing.T, js string) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// testdata/golden_random7_nosap.xlsx is the output of the former Go BOM
// implementation for the same inputs (stubbed Material_Master, SAP off).
// workflows/bom.yaml must reproduce every cell value of both sheets.
func TestBOMMatchesGolden(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.xlsx")
	var logs []string
	rt := Runtime{Cfg: cfg(t, `{"db_enabled": true}`), SQL: stubSQL(t, "../../testdata/stub_master.json"), Log: func(s string) { logs = append(logs, s) }}
	inputs := map[string]string{"bom": "../../testdata/bom_random7.xls", "priority": "../../testdata/prio_random7.xls"}
	if err := loadBOM(t).Run(context.Background(), rt, inputs, out); err != nil {
		t.Fatalf("%v\n%v", err, logs)
	}
	got, want := readAll(t, out), readAll(t, "../../testdata/golden_random7_nosap.xlsx")
	for sheet, w := range want {
		g := got[sheet]
		if len(g) != len(w) {
			t.Errorf("sheet %q: %d rows, want %d", sheet, len(g), len(w))
		}
		for i := range w {
			if i < len(g) && !reflect.DeepEqual(g[i], w[i]) {
				t.Errorf("sheet %q row %d:\n got  %q\n want %q", sheet, i+1, g[i], w[i])
			}
		}
	}
	t.Log(logs)
}

func readAll(t *testing.T, path string) map[string][][]string {
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	res := map[string][][]string{}
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil {
			t.Fatal(err)
		}
		res[name] = rows
	}
	return res
}
