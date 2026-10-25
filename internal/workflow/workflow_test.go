package workflow

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v3"
)

// testReg adds "load: name" (current table from test data) and "done" (a
// writer, so tests need no output.sheets) to the table steps.
func testReg(data map[string]*Table) Registry {
	return TableOps().With(Registry{
		"load": {Compile: func(arg *yaml.Node, _ *Scope) (Runner, error) {
			return func(_ context.Context, s *State) error { s.Cur = data[arg.Value].clone(); return nil }, nil
		}},
		"done": {Compile: func(*yaml.Node, *Scope) (Runner, error) {
			return func(context.Context, *State) error { return nil }, nil
		}, Writes: true},
	})
}

// apply runs steps on the "parts" table (datasets saved as given) and
// returns the current table.
func apply(t *testing.T, steps string, data map[string]*Table) (*Table, error) {
	t.Helper()
	doc := "name: T\ninput: {x: {}}\nsteps:\n  - load: parts\n"
	for name := range data {
		if name != "parts" {
			doc += "  - load: " + name + "\n  - save: " + name + "\n  - load: parts\n"
		}
	}
	doc += steps + "\n  - done\n"
	wf, err := Parse([]byte(doc), testReg(data))
	if err != nil {
		return nil, err
	}
	s := &State{Inputs: map[string]string{"x": "x"}, Tables: map[string]*Table{}, Data: map[string]any{}}
	for _, run := range wf.run {
		if err := run(context.Background(), s); err != nil {
			return nil, err
		}
	}
	return s.Cur, nil
}

var parts = &Table{
	Columns: []string{"Part", "Desc", "Qty"},
	Rows:    [][]string{{" A ", "x", "2"}, {"B", "", "1"}, {"A", "y", "3"}, {"", "z", "0"}},
}

func TestTableSteps(t *testing.T) {
	baseline := &Table{Columns: []string{"Part", "Qty"}, Rows: [][]string{{"A", "5"}, {"C", "1"}}}
	prices := &Table{Columns: []string{"Part", "Price"}, Rows: [][]string{{"A", "1.5"}, {"B", "2"}}}
	data := map[string]*Table{"parts": parts, "baseline": baseline, "prices": prices}
	for name, c := range map[string]struct {
		steps string
		want  [][]string // header first
	}{
		"filter":  {`  - filter: 'num("Qty") > 1'`, [][]string{{"Part", "Desc", "Qty"}, {" A ", "x", "2"}, {"A", "y", "3"}}},
		"set":     {`  - set: {Part: 'trim(col("Part"))', Total: 'num("Qty") * 2'}` + "\n  - select: {Part: Part, Total: Total}", [][]string{{"Part", "Total"}, {"A", "4"}, {"B", "2"}, {"A", "6"}, {"", "0"}}},
		"select":  {`  - select: {Q: Qty, P: Part}`, [][]string{{"Q", "P"}, {"2", " A "}, {"1", "B"}, {"3", "A"}, {"0", ""}}},
		"rename":  {`  - rename: {Desc: Description}` + "\n  - filter: 'col(\"Part\") == \"B\"'", [][]string{{"Part", "Description", "Qty"}, {"B", "", "1"}}},
		"fill":    {"  - fill: [Desc]\n  - select: {D: Desc}", [][]string{{"D"}, {"x"}, {"x"}, {"y"}, {"z"}}},
		"dedupe":  {"  - set: {Part: 'trim(col(\"Part\"))'}\n  - dedupe: [Part]", [][]string{{"Part", "Desc", "Qty"}, {"A", "x", "2"}, {"B", "", "1"}, {"", "z", "0"}}},
		"group":   {"  - set: {Part: 'trim(col(\"Part\"))'}\n  - group: {by: [Part], sum: [Qty], join: [Desc]}", [][]string{{"Part", "Desc", "Qty"}, {"A", "x, y", "5"}, {"B", "", "1"}, {"", "z", "0"}}},
		"total":   {"  - group: {sum: [Qty], count: [Part], max: [Desc]}", nil},
		"sort":    {"  - sort: {by: [Qty], numeric: true, desc: true}\n  - select: {Q: Qty}", [][]string{{"Q"}, {"3"}, {"2"}, {"1"}, {"0"}}},
		"join":    {"  - set: {Part: 'trim(col(\"Part\"))'}\n  - join: {with: prices, on: [Part], how: inner}\n  - select: {P: Part, Price: Price}", [][]string{{"P", "Price"}, {"A", "1.5"}, {"B", "2"}, {"A", "1.5"}}},
		"append":  {"  - select: {Part: Part, Qty: Qty}\n  - filter: 'col(\"Part\") == \"B\"'\n  - append: [baseline]", [][]string{{"Part", "Qty"}, {"B", "1"}, {"A", "5"}, {"C", "1"}}},
		"compare": {"  - set: {Part: 'trim(col(\"Part\"))'}\n  - filter: 'col(\"Part\") != \"\"'\n  - group: {by: [Part], sum: [Qty]}\n  - compare: {with: baseline, by: [Part]}", [][]string{{"Part", "Qty", "status"}, {"A", "5", "unchanged"}, {"B", "1", "added"}, {"C", "1", "removed"}}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := apply(t, c.steps, data)
			if name == "total" {
				// max over a text column must fail; sum/count alone succeed.
				if err == nil || !strings.Contains(err.Error(), "invalid number") {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rows := append([][]string{got.Columns}, got.Rows...); !reflect.DeepEqual(rows, c.want) {
				t.Errorf("got  %v\nwant %v", rows, c.want)
			}
		})
	}
	if !reflect.DeepEqual(parts.Rows[0], []string{" A ", "x", "2"}) {
		t.Error("steps modified their source table")
	}
}

func TestStepErrors(t *testing.T) {
	for name, steps := range map[string]string{
		"unknown column":   `  - select: {X: Nope}`,
		"not a number":     `  - filter: 'num("Desc") > 1'`,
		"unknown dataset":  `  - join: {with: nope, on: [Part]}`,
		"duplicate header": `  - rename: {Part: Qty}`,
		"blank number":     `  - sort: {by: [Part], numeric: true}`,
	} {
		if _, err := apply(t, steps, map[string]*Table{"parts": parts}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestParseRejects(t *testing.T) {
	const ok = "name: T\ninput: {a: {}}\nsteps:\n  - load: parts\n  - save: out\noutput:\n  sheets: {Out: out}\n"
	if _, err := Parse([]byte(ok), testReg(nil)); err != nil {
		t.Fatalf("valid document: %v", err)
	}
	for name, doc := range map[string]string{
		"unknown root key":   ok + "surprise: 1\n",
		"duplicate key":      ok + "name: again\n",
		"alias":              strings.Replace(ok, "input: {a: {}}", "input: &x {a: {}}\nother: *x", 1),
		"merge key":          ok + "x: {<<: {a: b}}\n",
		"two documents":      ok + "---\n{}\n",
		"blank name":         strings.Replace(ok, "name: T", "name: ' '", 1),
		"no input":           strings.Replace(ok, "input: {a: {}}", "input: {}", 1),
		"bad input name":     strings.Replace(ok, "{a: {}}", "{'a b': {}}", 1),
		"bad match":          strings.Replace(ok, "{a: {}}", "{a: {match: '('}}", 1),
		"unknown input key":  strings.Replace(ok, "{a: {}}", "{a: {path: x}}", 1),
		"unknown step":       strings.Replace(ok, "  - save: out", "  - nope", 1),
		"two-key step":       strings.Replace(ok, "  - save: out", "  - {save: out, sort: x}", 1),
		"bad expression":     strings.Replace(ok, "  - save: out", "  - filter: 'col(\"A\") =='\n  - save: out", 1),
		"non-bool filter":    strings.Replace(ok, "  - save: out", "  - filter: '1 + 1'\n  - save: out", 1),
		"bad aggregate":      strings.Replace(ok, "  - save: out", "  - group: {by: [A], median: [B]}\n  - save: out", 1),
		"bad color":          strings.Replace(ok, "  - save: out", "  - style: {header: {color: red}}\n  - save: out", 1),
		"no output":          strings.Replace(ok, "output:\n  sheets: {Out: out}\n", "", 1),
		"output and writer":  strings.Replace(ok, "  - save: out", "  - save: out\n  - done", 1),
		"bad sheet name":     strings.Replace(ok, "{Out: out}", "{'a/b': out}", 1),
		"case-equal sheets":  strings.Replace(ok, "{Out: out}", "{Out: out, out: out}", 1),
		"step needs no args": strings.Replace(ok, "  - save: out", "  - save", 1),
	} {
		if _, err := Parse([]byte(doc), testReg(nil)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAssign(t *testing.T) {
	wf, err := Parse([]byte("name: T\ninput:\n  bom: {}\n  priority: {match: '(?i)prio|shortage'}\nsteps: [done]\n"), testReg(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		names []string
		want  []int
	}{
		{[]string{"Customer BOM.xlsx", "Priority.xlsx"}, []int{0, 1}},
		{[]string{"Priority.xlsx", "Customer BOM.xlsx"}, []int{1, 0}},
		{[]string{"Shortage.xlsx", "my.xls"}, []int{1, 0}},
		{[]string{"x.xlsx", "y.xlsx"}, []int{0, 1}},
		{[]string{"prio_a.xlsx", "PRIO_b.xlsx"}, []int{1, 0}},
	} {
		if got := wf.Assign(c.names); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v want %v", c.names, got, c.want)
		}
	}
}

// End to end through real files: read a sheet, style it, write it.
func TestRunWritesWorkbook(t *testing.T) {
	doc := `name: T
input: {parts: {}}
steps:
  - from: parts[Parts]
  - filter: 'num("Quantity") > 2'
  - style: {header: {bold: true}, freeze_header: true, number_formats: {Quantity: "0.00"}}
  - save: big
  - from: parts
  - group: {count: [ID]}
  - save: count
output:
  sheets: {Big: big, Count: count}
`
	wf, err := Parse([]byte(doc), TableOps())
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "o.xlsx")
	if err := wf.Run(context.Background(), nil, nil, map[string]string{"parts": "testdata/parts.xls"}, out); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := f.GetSheetList(); !reflect.DeepEqual(got, []string{"Big", "Count"}) {
		t.Fatalf("sheets %v", got)
	}
	big, _ := f.GetRows("Big")
	count, _ := f.GetRows("Count")
	if !reflect.DeepEqual(big, [][]string{{"ID", "Quantity"}, {"002", "3.00"}}) || !reflect.DeepEqual(count, [][]string{{"ID"}, {"2"}}) {
		t.Errorf("big=%v count=%v", big, count)
	}
	if err := wf.Run(context.Background(), nil, nil, map[string]string{}, out); err == nil {
		t.Error("missing input accepted")
	}
}

func TestParseChecksNames(t *testing.T) {
	const head = "name: T\ninput: {a: {}}\nsteps:\n"
	for name, doc := range map[string]string{
		"unknown from":     head + "  - from: b\n  - save: x\noutput: {sheets: {X: x}}\n",
		"unknown sheet":    head + "  - from: a\n  - save: x\noutput: {sheets: {X: y}}\n",
		"unknown join":     head + "  - from: a\n  - join: {with: y, on: [K]}\n  - save: x\noutput: {sheets: {X: x}}\n",
		"save hides input": head + "  - from: a\n  - save: a\noutput: {sheets: {X: a}}\n",
		"writer not last":  head + "  - done\n  - from: a\n",
	} {
		if _, err := Parse([]byte(doc), testReg(nil)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNumbersAreNotScientific(t *testing.T) {
	big := &Table{Columns: []string{"Qty"}, Rows: [][]string{{"600000"}, {"400000"}, {"0.00001"}}}
	got, err := apply(t, "  - set: {Tiny: 'num(\"Qty\") / 100000000000'}\n  - group: {sum: [Qty], min: [Tiny]}", map[string]*Table{"parts": big})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0][0] != "1000000.00001" || strings.ContainsAny(got.Rows[0][1], "eE") || !strings.HasPrefix(got.Rows[0][1], "0.0000000000000001") {
		t.Errorf("got %v", got.Rows[0])
	}
}

// One compiled workflow serves concurrent requests (run with -race).
func TestConcurrentRuns(t *testing.T) {
	wf, err := Parse([]byte("name: T\ninput: {parts: {}}\nsteps:\n  - from: parts\n  - filter: 'num(\"Quantity\") > 2'\n  - set: {ID2: 'col(\"ID\") + \"x\"'}\n  - save: s\noutput: {sheets: {S: s}}\n"), TableOps())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			errs <- wf.Run(context.Background(), nil, nil, map[string]string{"parts": "testdata/parts.xls"}, filepath.Join(dir, strings.Repeat("o", i+1)+".xlsx"))
		}(i)
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
