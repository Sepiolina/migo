package workflow_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miservice/internal/sheet"
	"miservice/internal/workflow"
)

const csvFlow = `name: Orders
input:
  orders: {label: Orders}
steps:
  - from: orders
  - set: {Qty: 'toNumber(Qty, 0)'}
  - filter: Qty > 0
  - drop: [Note]
  - log: 'kept {len(rows)} rows'
  - save: result
output:
  format: csv
  sheets: {Orders: result}
`

func writeOrders(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "orders")
	if err := os.WriteFile(path, []byte("ID;Qty;Note\n1;3;a\n2;0;b\n3;5;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCSVInDropAndCSVOut(t *testing.T) {
	wf, err := workflow.Parse([]byte(csvFlow))
	if err != nil {
		t.Fatal(err)
	}
	if wf.Ext() != ".csv" || !strings.HasPrefix(wf.ContentType(), "text/csv") {
		t.Errorf("format %s %s", wf.Ext(), wf.ContentType())
	}
	out := filepath.Join(t.TempDir(), "out.csv")
	if err := wf.Run(context.Background(), workflow.Runtime{}, map[string]string{"orders": writeOrders(t)}, out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	rows, err := sheet.ParseCSV(data)
	if err != nil || len(rows) != 3 || strings.Join(rows[0], ",") != "ID,Qty" || strings.Join(rows[2], ",") != "3,5" {
		t.Errorf("csv %q %v", data, err)
	}
	if _, err := workflow.Parse([]byte(strings.Replace(csvFlow, "format: csv", "format: pdf", 1))); err == nil {
		t.Error("unknown format accepted")
	}
	two := strings.Replace(csvFlow, "sheets: {Orders: result}", "sheets: {A: result, B: result}", 1)
	if _, err := workflow.Parse([]byte(two)); err == nil {
		t.Error("csv with two sheets accepted")
	}
}

func TestTrace(t *testing.T) {
	wf, err := workflow.Parse([]byte(csvFlow))
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]string{"orders": writeOrders(t)}
	tr, err := wf.Trace(context.Background(), workflow.Runtime{}, in, workflow.TraceOptions{Focus: 2, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(tr) != 6 || tr[0].Total != 3 || tr[2].Total != 2 || len(tr[3].Columns) != 2 {
		t.Fatalf("trace %+v", tr)
	}
	if tr[2].Rows == nil || tr[1].Rows != nil || tr[2].Rows[1][1] != "5" {
		t.Errorf("rows only for the focused step: %+v", tr[2].Rows)
	}
	qty := tr[1].Columns[1]
	if qty.Kind != "number" || qty.Min != 0 || qty.Max != 5 || len(qty.Bins) != 8 || tr[0].Columns[2].Filled != 2 {
		t.Errorf("profile %+v %+v", qty, tr[0].Columns[2])
	}
	if len(tr[4].Log) != 1 || tr[4].Log[0] != "kept 2 rows" {
		t.Errorf("log %v", tr[4].Log)
	}
	// Skipping the filter previews the workflow without it.
	tr, _ = wf.Trace(context.Background(), workflow.Runtime{}, in, workflow.TraceOptions{Focus: -1, Skip: []int{2}})
	if !tr[2].Skipped || tr[5].Total != 3 {
		t.Errorf("skip %+v", tr[2])
	}
	// A failing step ends the trace and carries the error.
	bad, _ := workflow.Parse([]byte(strings.Replace(csvFlow, "drop: [Note]", "drop: [Nope]", 1)))
	tr, _ = bad.Trace(context.Background(), workflow.Runtime{}, in, workflow.TraceOptions{Focus: -1})
	if len(tr) != 4 || !strings.Contains(tr[3].Error, "Nope") {
		t.Errorf("error %+v", tr)
	}
}
