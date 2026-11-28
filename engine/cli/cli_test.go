package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/Sepiolina/migo/engine/workflow"
)

// run is migo without a database connection.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return Main(ctx, args, stdout, stderr, Options{})
}

func TestExampleWorkflow(t *testing.T) {
	out := filepath.Join(t.TempDir(), "summary.xlsx")
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"-in", "parts=../../examples/parts.xlsx", "-check", "../../examples/parts-summary.yaml"}, &stdout, &stderr); code != 0 {
		t.Fatalf("check code %d: %s", code, &stderr)
	}
	if !strings.Contains(stdout.String(), "no output kept") {
		t.Fatalf("stdout=%s", &stdout)
	}
	// Flags may also follow the workflow path.
	if code := run(context.Background(), []string{"../../examples/parts-summary.yaml", "-in", "parts=../../examples/parts.xlsx", "-out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("run code %d: %s", code, &stderr)
	}
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, _ := f.GetRows("Parts")
	if len(rows) < 2 || !reflect.DeepEqual(rows[0], []string{"Part Number", "Description", "Quantity"}) || !reflect.DeepEqual(rows[1], []string{"00123", "Resistor", "5"}) {
		t.Fatalf("rows=%v", rows)
	}
}

func TestBOMWorkflow(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bom.xlsx")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"-in", "bom=../../testdata/bom_basic.xlsx", "-in", "priority=../../testdata/prio_basic.xlsx", "-out", out, "../../workflows/bom.yaml"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code %d: %s", code, &stderr)
	}
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := f.GetSheetList(); !reflect.DeepEqual(got, []string{"BOM", "debug"}) {
		t.Fatalf("sheets=%v", got)
	}
}

func TestUsageErrors(t *testing.T) {
	wf := "../../examples/parts-summary.yaml"
	for _, args := range [][]string{
		{},
		{"-check", wf, "extra.yaml"},
		{"-in", "parts=../../examples/parts.xlsx", wf}, // neither -out nor -check
		{"-check", wf},                                    // missing input
		{"-in", "nope", "-check", wf},                     // bad -in
		{"-in", "parts=x", "-in", "more=y", "-check", wf}, // unknown input
		{"-in", "parts=../../examples/parts.xlsx", "-out", "../../examples/parts.xlsx", wf}, // overwrite input
		{"-cfg", "bad-json", "-check", wf},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), args, &stdout, &stderr); code != 2 || stderr.Len() == 0 {
				t.Fatalf("code=%d stderr=%s", code, &stderr)
			}
		})
	}
	if _, err := os.Stat("../../examples/parts.xlsx"); err != nil {
		t.Fatal("input damaged")
	}
}

// The files generated from the step descriptors must match the engine.
func migo(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

// Every starter template is created by `migo new`, validates and runs on
// its sample files.
func TestTemplatesRun(t *testing.T) {
	for _, tpl := range workflow.Templates() {
		dir := t.TempDir()
		if code, out := migo(t, "new", "my-flow", "-template", tpl.ID, "-dir", dir); code != 0 {
			t.Fatalf("new %s: %s", tpl.ID, out)
		}
		file := filepath.Join(dir, "my-flow.yaml")
		if code, out := migo(t, "validate", file); code != 0 || !strings.HasPrefix(out, "ok ") {
			t.Fatalf("validate %s: %s", tpl.ID, out)
		}
		wf, err := workflow.Load(file)
		if err != nil || wf.Name != "My flow" {
			t.Fatalf("%s: name %q, err %v", tpl.ID, wf.Name, err)
		}
		if len(tpl.Samples) == 0 {
			continue // blank: nothing to run on
		}
		args := []string{"run", "-out", filepath.Join(dir, "out"+wf.Ext()), file}
		for name, sample := range tpl.Samples {
			args = append(args, "-in", name+"="+filepath.Join("../..", sample))
		}
		if code, out := migo(t, args...); code != 0 {
			t.Errorf("run %s: %s", tpl.ID, out)
		}
	}
}

func TestNewAndValidateRefuseMistakes(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"new", "Bad_Name", "-dir", dir},
		{"new", "ok-name", "-template", "nope", "-dir", dir},
		{"new", "-dir", dir},
	} {
		if code, out := migo(t, args...); code != 2 || !strings.Contains(out, "ERR") {
			t.Errorf("%v: code %d: %s", args, code, out)
		}
	}
	if code, _ := migo(t, "new", "twice", "-dir", dir); code != 0 {
		t.Fatal("first new failed")
	}
	if code, out := migo(t, "new", "twice", "-dir", dir); code != 2 || !strings.Contains(out, "already exists") {
		t.Errorf("second new: code %d: %s", code, out)
	}
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("name: X\ninput: {a: {}}\nsteps:\n  - sorted: 1\noutput: {sheets: {A: a}}\n"), 0o644)
	if code, out := migo(t, "validate", filepath.Join(dir, "twice.yaml"), bad); code != 1 || !strings.Contains(out, `unknown step "sorted"`) || !strings.Contains(out, "ok ") {
		t.Errorf("validate: code %d: %s", code, out)
	}
	if code, out := migo(t, "new", "-list"); code != 0 || !strings.Contains(out, "parts-summary") {
		t.Errorf("list: %s", out)
	}
	if code, out := migo(t, "help"); code != 0 || !strings.Contains(out, "migo test") {
		t.Errorf("help: %s", out)
	}
}

// The repository's own workflow tests pass, and the runner reports a
// difference, creates expected files with -update and checks error cases.
func TestWorkflowTests(t *testing.T) {
	if code, out := migo(t, "test", "../../examples"); code != 0 || !strings.Contains(out, "5 passed, 0 failed") {
		t.Fatalf("examples: code %d: %s", code, out)
	}
	dir := t.TempDir()
	abs, _ := filepath.Abs("../../examples")
	spec := "workflow: " + filepath.ToSlash(abs) + "/parts-summary.yaml\ntests:\n" +
		"  - name: summary\n    in: {parts: " + filepath.ToSlash(abs) + "/parts.xlsx}\n    expect: want.xlsx\n" +
		"  - name: wrong error\n    in: {parts: " + filepath.ToSlash(abs) + "/orders.xlsx}\n    error: no such text\n"
	file := filepath.Join(dir, "parts.test.yaml")
	os.WriteFile(file, []byte(spec), 0o644)
	if code, out := migo(t, "test", dir); code != 1 || !strings.Contains(out, "run with -update") || !strings.Contains(out, "expected it to contain") {
		t.Fatalf("before update: code %d: %s", code, out)
	}
	if code, out := migo(t, "test", "-update", file); code != 1 || !strings.Contains(out, "1 written") {
		t.Fatalf("update: code %d: %s", code, out)
	}
	// A changed expectation is reported by sheet and cell.
	f, err := excelize.OpenFile(filepath.Join(dir, "want.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	f.SetCellValue("Parts", "A2", "changed")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if code, out := migo(t, "test", file); code != 1 || !strings.Contains(out, `sheet "Parts" cell A2: got "00123", expected "changed"`) {
		t.Fatalf("difference: code %d: %s", code, out)
	}
	if code, out := migo(t, "test", t.TempDir()); code != 2 || !strings.Contains(out, "no *.test.yaml files") {
		t.Fatalf("empty folder: code %d: %s", code, out)
	}
}

// A CSV workflow writes .csv; run names the temp and output file after it.
func TestRunWritesCSV(t *testing.T) {
	dir := t.TempDir()
	migo(t, "new", "to-csv", "-template", "excel-to-csv", "-dir", dir)
	file := filepath.Join(dir, "to-csv.yaml")
	if code, out := migo(t, "run", "-in", "sheet=../../examples/parts.xlsx", "-out", filepath.Join(dir, "o.xlsx"), file); code != 2 || !strings.Contains(out, ".csv file") {
		t.Fatalf("xlsx name for csv: code %d: %s", code, out)
	}
	if code, out := migo(t, "-in", "sheet=../../examples/parts.xlsx", "-out", filepath.Join(dir, "o.csv"), file); code != 0 {
		t.Fatalf("csv: %s", out)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "o.csv")); !bytes.Contains(data, []byte("Part Number")) {
		t.Fatalf("csv content: %q", data)
	}
}

const enrichWorkflow = `name: Enrich
input:
  parts: {label: Parts}
steps:
  - from: {input: parts, sheet: Parts, trim: true}
  - rest:
      url: '{config("url")}/stock'
      method: post
      each: 'map(rows, #["Part Number"])'
      batch: '2'
      body: '{codes: batch}'
      rows: response.items
      save: stock
  - sql: {query: 'SELECT Code, Vendor FROM Vendors', save: vendors}
  - join: {with: stock, on: [Part Number], right_on: [code], take: {Stock: qty}}
  - join: {with: vendors, on: [Part Number], right_on: [Code], take: {Vendor: Vendor}}
  - save: out
output:
  sheets: {Out: out}
`

// A run against real systems is recorded once; afterwards the workflow runs
// and is tested from the recording, with the systems gone.
func TestRecordThenReplayMocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Codes []string }
		json.NewDecoder(r.Body).Decode(&req)
		items := []map[string]any{}
		for _, c := range req.Codes {
			items = append(items, map[string]any{"code": c, "qty": len(c) * 10})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	dir := t.TempDir()
	path := func(name string) string { return filepath.Join(dir, name) }
	os.WriteFile(path("enrich.yaml"), []byte(enrichWorkflow), 0o644)
	os.WriteFile(path("vendors.json"), []byte(`{"columns": ["Code", "Vendor"], "rows": [["00123", "Acme"]]}`), 0o644)
	cfg := `{"url": "` + srv.URL + `"}`
	parts := "parts=../../examples/parts.xlsx"

	code, out := migo(t, "run", "-in", parts, "-cfg", cfg, "-sql-stub", path("vendors.json"), "-record", path("enrich.mocks.yaml"), "-out", path("live.xlsx"), path("enrich.yaml"))
	if code != 0 || !strings.Contains(out, "recorded 1 sql result(s)") {
		t.Fatalf("record: code %d: %s", code, out)
	}
	srv.Close() // from here on nothing real answers
	recording, _ := os.ReadFile(path("enrich.mocks.yaml"))
	if strings.Contains(string(recording), srv.URL) || !strings.Contains(string(recording), "url: /stock") {
		t.Fatalf("the recording should hold the path, not the host:\n%s", recording)
	}

	if code, out := migo(t, "run", "-in", parts, "-mocks", path("enrich.mocks.yaml"), "-out", path("replay.xlsx"), path("enrich.yaml")); code != 0 {
		t.Fatalf("replay: code %d: %s", code, out)
	}
	rows := func(file string) [][]string {
		f, err := excelize.OpenFile(path(file))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		r, _ := f.GetRows("Out")
		return r
	}
	live := rows("live.xlsx")
	if len(live) < 2 || !reflect.DeepEqual(live, rows("replay.xlsx")) || !strings.Contains(strings.Join(live[1], "|"), "Acme") {
		t.Fatalf("live %v\nreplay %v", live, rows("replay.xlsx"))
	}

	// The same recording drives a workflow test; inline mocks work too, and a
	// failing status or a call nothing answers fails the run with a clear message.
	abs, _ := filepath.Abs("../../examples/parts.xlsx")
	in := "    in: {parts: " + filepath.ToSlash(abs) + "}\n"
	os.WriteFile(path("enrich.test.yaml"), []byte("workflow: enrich.yaml\ntests:\n"+
		"  - name: recorded\n"+in+"    mocks: enrich.mocks.yaml\n    expect: replay.xlsx\n"+
		"  - name: service down\n"+in+"    mocks: {rest: [{status: 503}], sql: [{columns: [Code, Vendor], rows: []}]}\n    error: status code 503\n"+
		"  - name: nothing answers\n"+in+"    error: no mock answers POST\n"), 0o644)
	if code, out := migo(t, "test", path("enrich.test.yaml")); code != 0 || !strings.Contains(out, "3 passed, 0 failed") {
		t.Fatalf("test: code %d: %s", code, out)
	}
	if code, out := migo(t, "run", "-in", parts, "-mocks", path("enrich.mocks.yaml"), "-record", path("x.yaml"), "-check", path("enrich.yaml")); code != 2 {
		t.Fatalf("-mocks with -record: code %d: %s", code, out)
	}
}
