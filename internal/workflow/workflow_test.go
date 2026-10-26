package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// run parses doc, runs it on testdata/parts.xls (sheet Parts: ID, Quantity
// = 001/2, 002/3) and returns the output workbook's rows per sheet.
func run(t *testing.T, doc string, rt Runtime) (map[string][][]string, []string, error) {
	t.Helper()
	wf, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var logs []string
	var mu sync.Mutex
	rt.Log = func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() }
	out := filepath.Join(t.TempDir(), "o.xlsx")
	if err := wf.Run(context.Background(), rt, map[string]string{"parts": "testdata/parts.xls"}, out); err != nil {
		return nil, logs, err
	}
	return readAll(t, out), logs, nil
}

func doc(steps, output string) string {
	if output == "" {
		output = "  sheets: {Out: out}\n"
	}
	return "name: T\ninput: {parts: {}}\nsteps:\n" + steps + "output:\n" + output
}

func TestTableSteps(t *testing.T) {
	for name, c := range map[string]struct {
		steps string
		want  [][]string
	}{
		"set and prev": {`  - from: parts
  - set: {Twice: 'num(Quantity) * 2', Running: 'index == 0 ? num(Quantity) : prev("Running") + num(Quantity)'}
  - save: out
`, [][]string{{"ID", "Quantity", "Twice", "Running"}, {"001", "2", "4", "2"}, {"002", "3", "6", "5"}}},
		"filter select rename": {`  - from: parts[Parts]
  - filter: num(Quantity) > 2
  - select: {Q: Quantity, Id: ID}
  - rename: 'lower(name)'
  - save: out
`, [][]string{{"q", "id"}, {"3", "002"}}},
		"group sort": {`  - from: parts
  - set: {K: '"all"'}
  - group: {by: [K], sum: [Quantity], count: [ID]}
  - save: out
`, [][]string{{"K", "ID", "Quantity"}, {"all", "2", "5"}}},
		"window": {`  - from: parts
  - window: {set: {Share: 'num(Quantity) / sum(map(group, num(#.Quantity)))', Pos: 'str(index + 1) + "/" + str(size)'}}
  - save: out
`, [][]string{{"ID", "Quantity", "Share", "Pos"}, {"001", "2", "0.4", "1/2"}, {"002", "3", "0.6", "2/2"}}},
		"stack dedupe": {`  - from: parts
  - set: {A: ID, B: '"x"'}
  - stack: {columns: [A, B], into: V}
  - dedupe: {by: [V], keep: last}
  - save: out
`, [][]string{{"ID", "Quantity", "V"}, {"001", "2", "001"}, {"002", "3", "002"}, {"002", "3", "x"}}},
		"join take": {`  - from: parts
  - set: {Price: 'num(Quantity) * 10'}
  - save: prices
  - from: parts
  - join: {with: prices, on: [ID], take: {P: Price}}
  - save: out
`, [][]string{{"ID", "Quantity", "P"}, {"001", "2", "20"}, {"002", "3", "30"}}},
		"split": {`  - from: parts
  - split:
      where: num(Quantity) > 2
      then: [{set: {Size: '"big"'}}]
      else: [{set: {Size: '"small"', Note: '"x"'}}]
  - save: out
`, [][]string{{"ID", "Quantity", "Size", "Note"}, {"002", "3", "big"}, {"001", "2", "small", "x"}}},
		"pick": {`  - from: parts
  - set: {G: '1'}
  - pick:
      by: [G]
      mark: Win
      reason: Why
      rules:
        - {rows: 'num(Quantity) > 5', reason: never}
        - {rows: 'true', best: num(Quantity), reason: 'best {best} of {size} ({tied} tied): {chosen.ID}'}
  - save: out
`, [][]string{{"ID", "Quantity", "G", "Win", "Why"}, {"001", "2", "1", "FALSE"}, {"002", "3", "1", "TRUE", "best 3 of 2 (1 tied): 002"}}},
		"if else": {`  - from: parts
  - if: len(rows) > 5
    then: [{set: {Branch: '"then"'}}]
    else: [{set: {Branch: '"else"'}}]
  - save: out
`, [][]string{{"ID", "Quantity", "Branch"}, {"001", "2", "else"}, {"002", "3", "else"}}},
	} {
		t.Run(name, func(t *testing.T) {
			got, _, err := run(t, doc(c.steps, ""), Runtime{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got["Out"], c.want) {
				t.Errorf("got  %q\nwant %q", got["Out"], c.want)
			}
		})
	}
}

func TestFailAndLog(t *testing.T) {
	_, logs, err := run(t, doc(`  - from: parts
  - log: 'rows: {len(rows)}, db: {config("db") ?? "off"}'
  - fail: {when: 'any(rows, #.ID == "002")', message: 'bad ID {rows[1].ID}'}
  - save: out
`, ""), Runtime{})
	if err == nil || err.Error() != "bad ID 002" || len(logs) == 0 || logs[0] != "rows: 2, db: off" {
		t.Errorf("err=%v logs=%q", err, logs)
	}
}

func TestSQL(t *testing.T) {
	var calls []string
	sql := func(_ context.Context, conn, query string, args []any) (*Table, error) {
		calls = append(calls, fmt.Sprintf("%s|%s|%v", conn, query, args))
		if strings.Contains(query, "boom") {
			return nil, fmt.Errorf("down")
		}
		out := &Table{Columns: []string{"id", "name"}}
		for _, a := range args[:len(args)-1] {
			out.Rows = append(out.Rows, []any{a, "name " + text(a)})
		}
		return out, nil
	}
	steps := `  - from: parts
  - sql:
      query: SELECT id, name FROM t WHERE id IN (@ids) AND x = @x
      params: {ids: 'map(rows, #.ID)', x: '"X"'}
      batch: 1
      save: names
  - join: {with: names, on: [ID], right_on: [id], take: {Name: name}}
  - sql: {query: boom @x, params: {x: '1'}, columns: [a], on_error: 'lookup failed: {error}', save: none}
  - save: out
`
	got, logs, err := run(t, doc(steps, ""), Runtime{SQL: sql})
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"ID", "Quantity", "Name"}, {"001", "2", "name 001"}, {"002", "3", "name 002"}}; !reflect.DeepEqual(got["Out"], want) {
		t.Errorf("got %q", got["Out"])
	}
	if len(calls) != 3 || calls[0] != "db|SELECT id, name FROM t WHERE id IN (@p1) AND x = @p2|[001 X]" {
		t.Errorf("calls %q", calls)
	}
	if len(logs) != 1 || logs[0] != "lookup failed: down" {
		t.Errorf("logs %q", logs)
	}
	if _, _, err := run(t, doc("  - from: parts\n  - sql: {query: boom, save: x}\n  - save: out\n", ""), Runtime{SQL: sql}); err == nil {
		t.Error("an sql failure without on_error must stop the run")
	}
}

func TestREST(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			http.Error(w, "busy", 503) // first attempt fails: retried
			return
		}
		bodies = append(bodies, r.Header.Get("X-Key")+" "+string(b))
		var in struct{ Codes []string }
		json.Unmarshal(b, &in)
		var out []map[string]any
		for _, c := range in.Codes {
			out = append(out, map[string]any{" CODE ": c, "Qty": 7})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": out})
	}))
	defer srv.Close()
	steps := `  - from: parts
  - rest:
      url: '{config("url")}/lookup'
      method: post
      each: 'map(rows, #.ID)'
      batch: '1'
      body: '{codes: batch}'
      headers: {X-Key: 'k{batch_number}'}
      retries: '2'
      rows: response.items
      keys: lower
      save: answers
  - join: {with: answers, on: [ID], right_on: [code], take: {Q: qty}}
  - save: out
`
	slept := 0
	got, _, err := run(t, doc(steps, ""), Runtime{Cfg: cfg(t, `{"url": "`+srv.URL+`"}`), Sleep: func(time.Duration) { slept++ }})
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"ID", "Quantity", "Q"}, {"001", "2", "7"}, {"002", "3", "7"}}; !reflect.DeepEqual(got["Out"], want) {
		t.Errorf("got %q", got["Out"])
	}
	if slept != 1 || !reflect.DeepEqual(bodies, []string{`k1 {"codes":["001"]}`, `k2 {"codes":["002"]}`}) {
		t.Errorf("slept=%d bodies=%q", slept, bodies)
	}
}

func TestOutputStyles(t *testing.T) {
	wf, err := Parse([]byte(doc("  - from: parts\n  - save: out\n", `  sheets:
    - name: A
      from: out
      freeze: true
      columns:
        - {header: Code, column: ID, width: 12}
        - {header: Qty, value: num(Quantity), number_format: "0.00", id: q}
      styles:
        - {when: num(Quantity) > 2, columns: [q], fill: "#FF0000", border: {bottom: thick}}
    - {name: B, like: A, columns: [{header: Extra, value: '"x"'}]}
`)))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "o.xlsx")
	if err := wf.Run(context.Background(), Runtime{}, map[string]string{"parts": "testdata/parts.xls"}, out); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a, _ := f.GetRows("A")
	b, _ := f.GetRows("B")
	if !reflect.DeepEqual(a, [][]string{{"Code", "Qty"}, {"001", "2.00"}, {"002", "3.00"}}) || len(b[0]) != 3 {
		t.Errorf("A=%q B=%q", a, b)
	}
	id, _ := f.GetCellStyle("A", "B3")
	st, _ := f.GetStyle(id)
	if st.Fill.Color[0] != "FF0000" || len(st.Border) != 1 {
		t.Errorf("B3 style %+v", st)
	}
	if w, _ := f.GetColWidth("A", "A"); w != 12 {
		t.Errorf("width %v", w)
	}
}

func TestParseRejects(t *testing.T) {
	const ok = "name: T\ninput: {a: {}}\nsteps:\n  - from: a\n  - save: out\noutput:\n  sheets: {Out: out}\n"
	if _, err := Parse([]byte(ok)); err != nil {
		t.Fatalf("valid document: %v", err)
	}
	for name, d := range map[string]string{
		"unknown root key":     ok + "surprise: 1\n",
		"duplicate key":        ok + "name: again\n",
		"alias":                strings.Replace(ok, "input: {a: {}}", "input: &x {a: {}}\nother: *x", 1),
		"merge key":            ok + "x: {<<: {a: b}}\n",
		"two documents":        ok + "---\n{}\n",
		"blank name":           strings.Replace(ok, "name: T", "name: ' '", 1),
		"no input":             strings.Replace(ok, "input: {a: {}}", "input: {}", 1),
		"bad match":            strings.Replace(ok, "{a: {}}", "{a: {match: '('}}", 1),
		"unknown step":         strings.Replace(ok, "  - save: out", "  - nope\n  - save: out", 1),
		"bad expression":       strings.Replace(ok, "  - save: out", "  - filter: 'ID =='\n  - save: out", 1),
		"unknown table name":   strings.Replace(ok, "  - save: out", "  - log: '{nope}'\n  - save: out", 1),
		"unknown input":        strings.Replace(ok, "  - from: a", "  - from: b", 1),
		"unsaved output":       strings.Replace(ok, "{Out: out}", "{Out: other}", 1),
		"unsaved join":         strings.Replace(ok, "  - save: out", "  - join: {with: x, on: [ID]}\n  - save: out", 1),
		"save hides input":     strings.Replace(ok, "  - save: out", "  - save: a\n  - save: out", 1),
		"if without then":      strings.Replace(ok, "  - save: out", "  - if: 'true'\n  - save: out", 1),
		"if unknown key":       strings.Replace(ok, "  - save: out", "  - {if: 'true', then: [], otherwise: []}\n  - save: out", 1),
		"bad color":            strings.Replace(ok, "{Out: out}", "[{name: Out, from: out, styles: [{fill: red}]}]", 1),
		"unknown style column": strings.Replace(ok, "{Out: out}", "[{name: Out, from: out, columns: [{header: A}], styles: [{columns: [B], fill: '#000000'}]}]", 1),
		"bad sheet name":       strings.Replace(ok, "{Out: out}", "{'a/b': out}", 1),
		"case-equal sheets":    strings.Replace(ok, "{Out: out}", "{Out: out, out: out}", 1),
		"sql param unused":     strings.Replace(ok, "  - save: out", "  - sql: {query: SELECT 1, params: {x: '1'}, save: s}\n  - save: out", 1),
		"rest without save":    strings.Replace(ok, "  - save: out", "  - rest: {url: http://x}\n  - save: out", 1),
	} {
		if _, err := Parse([]byte(d)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestErrorsNameTheColumn(t *testing.T) {
	_, _, err := run(t, doc("  - from: parts\n  - filter: Qty > 1\n  - save: out\n", ""), Runtime{})
	if err == nil || !strings.Contains(err.Error(), `unknown column "Qty"`) {
		t.Errorf("err=%v", err)
	}
}

func TestAssign(t *testing.T) {
	wf, err := Parse([]byte("name: T\ninput:\n  bom: {}\n  priority: {match: '(?i)prio|shortage'}\nsteps: [{from: bom}, {save: x}]\noutput: {sheets: {X: x}}\n"))
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
	} {
		if got := wf.Assign(c.names); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v want %v", c.names, got, c.want)
		}
	}
}

// One compiled workflow serves concurrent requests (run with -race).
func TestConcurrentRuns(t *testing.T) {
	wf, err := Parse([]byte(doc("  - from: parts\n  - filter: num(Quantity) > 2\n  - set: {X: 'ID + \"x\"'}\n  - save: out\n", "")))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			errs <- wf.Run(context.Background(), Runtime{}, map[string]string{"parts": "testdata/parts.xls"}, filepath.Join(dir, fmt.Sprintf("o%d.xlsx", i)))
		}(i)
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
