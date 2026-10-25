package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func workbookFixture(t *testing.T, path string, sheets map[string][][]string) {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for name, rows := range sheets {
		if name != "Sheet1" {
			if _, err := f.NewSheet(name); err != nil {
				t.Fatal(err)
			}
		}
		maxCols := 0
		for _, row := range rows {
			if len(row) > maxCols {
				maxCols = len(row)
			}
		}
		if maxCols > 0 && len(rows) > 0 {
			last, _ := excelize.CoordinatesToCellName(maxCols, len(rows))
			if err := f.SetSheetDimension(name, "A1:"+last); err != nil {
				t.Fatal(err)
			}
		}
		for r, row := range rows {
			for c, v := range row {
				cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
				if err := f.SetCellStr(name, cell, v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if _, ok := sheets["Sheet1"]; !ok {
		if err := f.DeleteSheet("Sheet1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
}
func openResult(t *testing.T, path string) *excelize.File {
	t.Helper()
	f, e := excelize.OpenFile(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
func value(t *testing.T, f *excelize.File, s, c string) string {
	t.Helper()
	v, e := f.GetCellValue(s, c)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestGridHeadersAndRows(t *testing.T) {
	good := [][]string{{"title"}, {"ID", "Value", ""}, {"001"}, {}, {"002", "x", ""}}
	got, err := tableFromGrid(good, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := &Table{Columns: []string{"ID", "Value"}, Rows: [][]string{{"001", ""}, {"", ""}, {"002", "x"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	for _, grid := range [][][]string{nil, {{""}}, {{"A", "", "C"}}, {{"A", "A"}}, {{"A", ""}, {"x", "extra"}}} {
		if _, e := tableFromGrid(grid, 0); e == nil {
			t.Fatalf("accepted bad headers/grid %#v", grid)
		}
	}
	if got, e := tableFromGrid([][]string{{"ID"}}, 0); e != nil || len(got.Rows) != 0 {
		t.Fatalf("header only: %v %v", got, e)
	}
}

func TestRunMultipleSourcesImmutableAndCheck(t *testing.T) {
	dir := t.TempDir()
	workbookFixture(t, filepath.Join(dir, "a.xlsx"), map[string][][]string{"Parts": {{"ID", "Value"}, {"001", "=literal"}, {}, {"002", "2"}}, "Other": {{"Name"}, {"other"}}})
	workbookFixture(t, filepath.Join(dir, "b.xlsx"), map[string][][]string{"Data": {{"ID"}, {"second"}}})
	cfg := &Config{BaseDir: dir, Input: map[string]string{"a": "a.xlsx", "b": "b.xlsx"}, Process: []Process{{From: `a["Parts"]`, As: "base"}, {From: "base", As: "branch", Do: []Step{{Transform: &TransformSpec{Columns: []string{"Value"}, Operation: "upper"}}}}, {From: `a["Other"]`, As: "other"}, {From: `b["Data"]`, As: "second"}}, Output: []Output{{File: "one.xlsx", Sheets: map[string]string{"Z": "other", "A": "base", "B": "branch"}}, {File: "two.xlsx", Sheets: map[string]string{"Second": "second"}}}}
	if err := Check(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "one.xlsx")); !os.IsNotExist(err) {
		t.Fatalf("Check published: %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, ".workflow-*")); len(files) != 0 {
		t.Fatal(files)
	}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	f := openResult(t, filepath.Join(dir, "one.xlsx"))
	if got := f.GetSheetList(); !reflect.DeepEqual(got, []string{"A", "B", "Z"}) {
		t.Fatal(got)
	}
	if dim, err := f.GetSheetDimension("A"); err != nil || dim != "A1:B4" {
		t.Fatal(dim, err)
	}
	if got := value(t, f, "A", "A2"); got != "001" {
		t.Fatal(got)
	}
	if got := value(t, f, "A", "B2"); got != "=literal" {
		t.Fatal(got)
	}
	if got := value(t, f, "B", "B2"); got != "=LITERAL" {
		t.Fatal(got)
	}
	if formula, _ := f.GetCellFormula("A", "B2"); formula != "" {
		t.Fatal(formula)
	}
	if got := value(t, f, "A", "A4"); got != "002" {
		t.Fatal(got)
	}
	if got := value(t, openResult(t, filepath.Join(dir, "two.xlsx")), "Second", "A2"); got != "second" {
		t.Fatal(got)
	}
}

func TestTemplateStylesNumericAndReplacement(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "template.xlsx")
	workbookFixture(t, template, map[string][][]string{"Keep": {{"keep"}}, "Data": {{"stale"}, {"stale"}, {"stale"}}})
	f := openResult(t, template)
	if err := f.SetCellStr("Data", "D10", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellFormula("Keep", "B2", "Data!B2+1"); err != nil {
		t.Fatal(err)
	}
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.SetCellStyle("Keep", "A1", "A1", style); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BaseDir: dir, Output: []Output{{File: "result.xlsx", Template: "template.xlsx", Sheets: map[string]string{"Data": "data"}}}}
	table := &Table{Columns: []string{"ID", "Amount"}, Rows: [][]string{{"001", "12.5"}, {"=literal", ""}}, Style: &StyleSpec{Header: &HeaderStyle{Bold: true, Color: "#FFFFFF", Background: "#112233"}, AutoWidth: true, FreezeHeader: true, NumberFormats: map[string]string{"Amount": "0.00"}}}
	if err = exportWorkbooks(context.Background(), cfg, map[string]*Table{"data": table}, true); err != nil {
		t.Fatal(err)
	}
	out := openResult(t, filepath.Join(dir, "result.xlsx"))
	if got := value(t, out, "Data", "A2"); got != "001" {
		t.Fatal(got)
	}
	if got := value(t, out, "Data", "B2"); got != "12.50" {
		t.Fatal(got)
	}
	if typ, _ := out.GetCellType("Data", "B2"); typ != excelize.CellTypeNumber && typ != excelize.CellTypeUnset {
		t.Fatalf("not numeric: %v", typ)
	}
	if got := value(t, out, "Data", "D10"); got != "" {
		t.Fatal("stale distant cell", got)
	}
	if got := value(t, out, "Data", "A4"); got != "" {
		t.Fatal("stale", got)
	}
	if formula, _ := out.GetCellFormula("Keep", "B2"); formula != "Data!B2+1" {
		t.Fatal(formula)
	}
	if id, _ := out.GetCellStyle("Keep", "A1"); id != style {
		t.Fatal(id, style)
	}
	id, _ := out.GetCellStyle("Data", "A1")
	st, err := out.GetStyle(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Font == nil || !st.Font.Bold || st.Font.Color != "FFFFFF" || len(st.Fill.Color) == 0 || st.Fill.Color[0] != "112233" {
		t.Fatalf("style: %#v", st)
	}
	panes, err := out.GetPanes("Data")
	if err != nil || !panes.Freeze || panes.YSplit != 1 || panes.TopLeftCell != "A2" {
		t.Fatalf("panes: %#v %v", panes, err)
	}
	width, _ := out.GetColWidth("Data", "A")
	if width < 8 || width > 60 {
		t.Fatal(width)
	}
}

func TestExportErrorsLeaveNoOutputs(t *testing.T) {
	for _, bad := range []string{"oops", "NaN", "Inf", "1e999"} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			cfg := &Config{BaseDir: dir, Output: []Output{{File: "first.xlsx", Sheets: map[string]string{"S": "good"}}, {File: "last.xlsx", Sheets: map[string]string{"S": "bad"}}}}
			tables := map[string]*Table{"good": {Columns: []string{"N"}, Rows: [][]string{{"1"}}}, "bad": {Columns: []string{"N"}, Rows: [][]string{{bad}}, Style: &StyleSpec{NumberFormats: map[string]string{"N": "0.00"}}}}
			if err := exportWorkbooks(context.Background(), cfg, tables, true); err == nil || !strings.Contains(err.Error(), "row 2 column") {
				t.Fatal(err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal(entries)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, &Config{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestOutputPathProtection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.xlsx")
	workbookFixture(t, input, map[string][][]string{"S": {{"ID"}, {"1"}}})
	if err := os.Symlink(input, filepath.Join(dir, "alias.xlsx")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(input, filepath.Join(dir, "hard.xlsx")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"input.xlsx", "alias.xlsx", "hard.xlsx"} {
		cfg := &Config{BaseDir: dir, Input: map[string]string{"in": "input.xlsx"}, Output: []Output{{File: target, Sheets: map[string]string{"S": "d"}}}}
		if err := exportWorkbooks(context.Background(), cfg, nil, true); err == nil || !strings.Contains(err.Error(), "collides") {
			t.Fatalf("%s: %v", target, err)
		}
	}
	for _, outputs := range [][]Output{{{File: "same.xlsx"}, {File: "./same.xlsx"}}, {{File: "input.xlsx", Template: "input.xlsx"}}} {
		if err := exportWorkbooks(context.Background(), &Config{BaseDir: dir, Output: outputs}, nil, true); err == nil || !strings.Contains(err.Error(), "collides") {
			t.Fatal(err)
		}
	}
}

func TestRunContextualErrors(t *testing.T) {
	dir := t.TempDir()
	workbookFixture(t, filepath.Join(dir, "bad.xlsx"), map[string][][]string{"Parts": {{"A", "A"}}})
	cfg := &Config{BaseDir: dir, Input: map[string]string{"in": "bad.xlsx"}, Process: []Process{{From: `in["Parts"]`, As: "result"}}, Output: []Output{{File: "out.xlsx", Sheets: map[string]string{"S": "result"}}}}
	err := Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("accepted duplicate header")
	}
	for _, text := range []string{"process 1", "result", "bad.xlsx", "Parts"} {
		if !strings.Contains(err.Error(), text) {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "out.xlsx")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// parts.xls is a BIFF8 fixture containing text identifiers and numeric quantities.
func TestRunXLSInput(t *testing.T) {
	dir := t.TempDir()
	fixture, err := filepath.Abs("testdata/parts.xls")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BaseDir: dir, Input: map[string]string{"legacy": fixture}, Process: []Process{{From: `legacy["Parts"]`, As: "data"}}, Output: []Output{{File: "result.xlsx", Sheets: map[string]string{"Data": "data"}}}}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	out := openResult(t, filepath.Join(dir, "result.xlsx"))
	if got := value(t, out, "Data", "A2"); got != "001" {
		t.Fatal(got)
	}
	if got := value(t, out, "Data", "B3"); got != "3" {
		t.Fatal(got)
	}
}

func TestHeaderRowRelativeToDeclaredOriginAndStepContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "offset.xlsx")
	f := excelize.NewFile()
	for cell, v := range map[string]string{"C3": "title", "C4": "ID", "D4": "Quantity", "C5": "001", "D5": "bad"} {
		if err := f.SetCellStr("Sheet1", cell, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SetSheetDimension("Sheet1", "C3:D5"); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	f.Close()
	cfg := &Config{BaseDir: dir, Input: map[string]string{"in": "offset.xlsx"}, Process: []Process{{From: `in["Sheet1"]`, As: "data", HeaderRow: 2}}, Output: []Output{{File: "out.xlsx", Sheets: map[string]string{"Data": "data"}}}}
	tables, err := Execute(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := tables["data"]; !reflect.DeepEqual(got.Columns, []string{"ID", "Quantity"}) || got.Rows[0][0] != "001" {
		t.Fatalf("%#v", got)
	}
	cfg.Process[0].Do = []Step{{Sort: &SortSpec{By: []string{"Quantity"}, Numeric: true}}}
	err = Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("invalid numeric sort accepted")
	}
	for _, part := range []string{"process 1", "from", "offset.xlsx", "Sheet1", "step 1", "sort"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("missing %q: %v", part, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "out.xlsx")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

type cancelAfterChecks struct {
	context.Context
	remaining int
}

func (c *cancelAfterChecks) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}
func TestCancellationDuringRenderingCleansStage(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{BaseDir: dir, Output: []Output{{File: "one.xlsx", Sheets: map[string]string{"Data": "d"}}, {File: "two.xlsx", Sheets: map[string]string{"Data": "d"}}}}
	table := &Table{Columns: []string{"ID"}, Rows: [][]string{{"1"}, {"2"}, {"3"}}}
	// Cancel while the second workbook is rendering, after the first was staged.
	ctx := &cancelAfterChecks{Context: context.Background(), remaining: 12}
	err := exportWorkbooks(ctx, cfg, map[string]*Table{"d": table}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
}

func TestStyleValidationAndBoundedWidths(t *testing.T) {
	for _, color := range []string{"red", "#123", "#GG1234"} {
		f := excelize.NewFile()
		err := writeTable(context.Background(), f, "Sheet1", &Table{Columns: []string{"ID"}, Style: &StyleSpec{Header: &HeaderStyle{Color: color}}})
		f.Close()
		if err == nil {
			t.Fatalf("accepted color %q", color)
		}
	}
	f := excelize.NewFile()
	defer f.Close()
	table := &Table{Columns: []string{"ID", "Wide"}, Rows: [][]string{{"1", strings.Repeat("x", 200)}}, Style: &StyleSpec{AutoWidth: true}}
	if err := writeTable(context.Background(), f, "Sheet1", table); err != nil {
		t.Fatal(err)
	}
	narrow, _ := f.GetColWidth("Sheet1", "A")
	wide, _ := f.GetColWidth("Sheet1", "B")
	if narrow != 8 || wide != 60 {
		t.Fatal(narrow, wide)
	}
}

func TestSymlinkDirectoryDuplicateTargets(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BaseDir: dir, Output: []Output{{File: "real/result.xlsx"}, {File: "alias/result.xlsx"}}}
	if err := exportWorkbooks(context.Background(), cfg, nil, true); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatal(err)
	}
}

func TestStagingErrorPreservesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	old := []byte("original destination bytes")
	path := filepath.Join(dir, "one.xlsx")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BaseDir: dir, Output: []Output{{File: "one.xlsx", Sheets: map[string]string{"Data": "d"}}, {File: "two.xlsx", Template: "missing.xlsx", Sheets: map[string]string{"Data": "d"}}}}
	err := exportWorkbooks(context.Background(), cfg, map[string]*Table{"d": {Columns: []string{"ID"}}}, true)
	if err == nil {
		t.Fatal("missing template accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatal(string(got), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatal(entries)
	}
}
