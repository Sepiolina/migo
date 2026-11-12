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

// Exports can turn the dashes of a part number into "?" or Unicode dashes;
// the BOM workflow writes a plain "-" and looks the part up with it.
func TestBOMFixesDashesInMfrPN(t *testing.T) {
	dir := t.TempDir()
	f, err := excelize.OpenFile("../../testdata/bom_basic.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	sheet := f.GetSheetList()[0]
	for cell, v := range map[string]string{
		"BA3": "ADM6?60?01.5?L?4?2?A-TR",
		"BA4": "ADM6‐60‑01.5–L‒4—2−A-TR",
	} {
		if err := f.SetCellStr(sheet, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	bom := filepath.Join(dir, "bom.xlsx")
	if err := f.SaveAs(bom); err != nil {
		t.Fatal(err)
	}
	f.Close()

	master := &Table{Columns: []string{"MaterialNo", "ZBlock", "Maker"}, Rows: [][]any{{"F9001", "", "ADM6-60-01.5-L-4-2-A-TR"}}}
	var logs []string
	rt := Runtime{
		Cfg: cfg(t, `{"db_enabled": true}`),
		SQL: func(context.Context, string, string, []any) (*Table, error) { return master, nil },
		Log: func(s string) { logs = append(logs, s) },
	}
	out := filepath.Join(dir, "out.xlsx")
	inputs := map[string]string{"bom": bom, "priority": "../../testdata/prio_basic.xlsx"}
	if err := loadBOM(t).Run(context.Background(), rt, inputs, out); err != nil {
		t.Fatalf("%v\n%v", err, logs)
	}
	rows := readAll(t, out)["BOM"]
	for _, r := range []int{2, 3} { // output rows 3 and 4 (index 0 is the header)
		if got := rows[r][10]; got != "ADM6-60-01.5-L-4-2-A-TR" {
			t.Errorf("row %d Mfr. Part Number = %q", r+1, got)
		}
		if got := rows[r][13]; got != "F9001" { // Forth P/N from the lookup
			t.Errorf("row %d Forth P/N = %q", r+1, got)
		}
	}
	found := false
	for _, l := range logs {
		found = found || l == "Fixed dashes in Mfr. Part Number for 2 row(s)."
	}
	if !found {
		t.Errorf("no dash log line in %q", logs)
	}
}

// The Priority workbook comes in different layouts and sheet names; the FPN
// and Priority columns are found by their titles.
func TestBOMPriorityLayouts(t *testing.T) {
	dir := t.TempDir()
	// write saves rows on sheet; with first, a junk sheet named first comes before it.
	write := func(name, sheet string, rows map[string][]any, first ...string) string {
		f := excelize.NewFile()
		if len(first) > 0 {
			f.SetSheetName("Sheet1", first[0])
			f.SetCellValue(first[0], "A1", "junk")
			f.NewSheet(sheet)
		} else {
			f.SetSheetName("Sheet1", sheet)
		}
		for cell, row := range rows {
			if err := f.SetSheetRow(sheet, cell, &row); err != nil {
				t.Fatal(err)
			}
		}
		// The reader honours the stored dimension, which excelize leaves at A1.
		if err := f.SetSheetDimension(sheet, "A1:M10"); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := f.SaveAs(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	long := make([]any, 13)
	long[0], long[1], long[12] = "FPN", "customer P/N", "Priority"
	longHdr := make([]any, 13)
	longHdr[0], longHdr[12] = "FPN", "Priority"
	model := make([]any, 13)
	model[9], model[10] = "1441229", "20563-0107-00001"
	cases := map[string]string{
		// short export, any sheet name: header in row 3, Priority in B
		"short": write("short.xlsx", "Data", map[string][]any{
			"A2": {"MMDATA"}, "A3": {"FPN", "Priority"}, "A4": {"FP-1", 1}, "A5": {"FP-2", 0}, "A6": {"FP-3", "2"},
		}),
		// long export: header in row 1 with Priority in M, model rows, then
		// MMDATA and the header again before the data
		"long": write("long.xlsx", "Shortage", map[string][]any{
			"A1": long, "A2": model, "A3": {"MMDATA"}, "A4": longHdr,
			"A5": append([]any{"FP-1"}, append(make([]any, 11), 1)...),
			"A6": append([]any{"FP-2"}, append(make([]any, 11), 0)...),
			"A7": append([]any{"FP-3"}, append(make([]any, 11), "2")...),
		}),
		// a named Shortage sheet wins over the first sheet
		"named": write("named.xlsx", "Shortage", map[string][]any{
			"A1": {"FPN", "Priority"}, "A2": {"FP-1", 1}, "A3": {"FP-2", 0}, "A4": {"FP-3", 2},
		}, "Notes"),
	}
	for name, prio := range cases {
		var logs []string
		rt := Runtime{Cfg: cfg(t, `{}`), Log: func(s string) { logs = append(logs, s) }}
		inputs := map[string]string{"bom": "../../testdata/bom_basic.xlsx", "priority": prio}
		if err := loadBOM(t).Run(context.Background(), rt, inputs, filepath.Join(dir, name+"-out.xlsx")); err != nil {
			t.Errorf("%s: %v\n%v", name, err, logs)
			continue
		}
		if !contains(logs, "Loaded 3 Priority entries.") {
			t.Errorf("%s: want 3 entries (FPN/MMDATA lines and model rows dropped), logs %v", name, logs)
		}
	}
	// A workbook without FPN/Priority columns is refused with a clear message.
	rt := Runtime{Cfg: cfg(t, `{}`), Log: func(string) {}}
	bad := write("bad.xlsx", "Sheet", map[string][]any{"A1": {"Part", "Qty"}, "A2": {"X", 1}})
	err := loadBOM(t).Run(context.Background(), rt, map[string]string{"bom": "../../testdata/bom_basic.xlsx", "priority": bad}, filepath.Join(dir, "bad-out.xlsx"))
	if err == nil || err.Error() != "Priority file has no priorities. Expected FPN and Priority columns on its Shortage sheet or, without one, its first sheet." {
		t.Errorf("bad priority: %v", err)
	}
}
