package workflow_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sepiolina/migo/engine/config"
	"github.com/Sepiolina/migo/engine/workflow"
)

// A workflow from YAML, run on a CSV file: the engine reads the inputs,
// runs the steps and writes the output file.
func Example() {
	wf, err := workflow.Parse([]byte(`
name: Totals
input:
  orders: {label: Orders}
steps:
  - from: orders
  - set: {Amount: 'num(Qty) * num(Price)'}
  - group: {by: [Customer], sum: [Amount]}
  - sort: {by: [Customer]}
  - save: totals
output:
  format: csv
  sheets: {Totals: totals}
`))
	if err != nil {
		fmt.Println(err)
		return
	}
	dir, _ := os.MkdirTemp("", "example")
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "orders.csv")
	os.WriteFile(in, []byte("Customer,Qty,Price\nBeta,1,5\nAcme,2,10\nAcme,3,10\n"), 0o644)
	out := filepath.Join(dir, "totals"+wf.Ext())

	// config("key") in expressions reads Cfg; SQL and HTTP are optional.
	rt := workflow.Runtime{Cfg: config.New(map[string]any{"region": "EU"})}
	if err := wf.Run(context.Background(), rt, map[string]string{"orders": in}, out); err != nil {
		fmt.Println(err)
		return
	}
	data, _ := os.ReadFile(out)
	// CSV output starts with a UTF-8 byte order mark, so Excel reads it right.
	fmt.Print(strings.TrimPrefix(string(data), "\uFEFF"))
	// Output:
	// Customer,Amount
	// Acme,50
	// Beta,5
}
