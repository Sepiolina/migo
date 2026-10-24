// bomrun runs the pipeline on two files without the HTTP server (batch
// testing of real BOMs, and differential testing against the Node service).
// -stubdb replaces SQL Server with a JSON map {"NORMALISED MPN": {"materialNo":"..","zBlock":".."}}.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"miservice/internal/clients/sap"
	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/modules/bom"
)

func main() {
	bomF := flag.String("bom", "", "customer BOM (.xls/.xlsx)")
	prioF := flag.String("prio", "", "priority workbook")
	out := flag.String("out", "out.xlsx", "output .xlsx")
	cfgJSON := flag.String("cfg", "{}", "config as JSON (keys like config.json)")
	stub := flag.String("stubdb", "", "JSON file standing in for Material_Master")
	flag.Parse()

	tmp, _ := os.CreateTemp("", "cfg*.json")
	tmp.WriteString(*cfgJSON)
	tmp.Close()
	defer os.Remove(tmp.Name())
	cfg, err := config.Load(tmp.Name())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	deps := bom.Deps{SAP: &sap.Client{}}
	if *stub != "" {
		b, err := os.ReadFile(*stub)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		var m map[string]struct{ MaterialNo, ZBlock string }
		if err := json.Unmarshal(b, &m); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		deps.Lookup = func(_ context.Context, mpns []string) (map[string]bom.Match, error) {
			res := map[string]bom.Match{}
			for _, x := range mpns {
				if v, ok := m[jsx.Norm(x)]; ok {
					res[jsx.Norm(x)] = bom.Match{MaterialNo: v.MaterialNo, ZBlock: v.ZBlock}
				}
			}
			return res, nil
		}
	}
	p := bom.NewPipeline(cfg, func(s string) { fmt.Println("LOG:", s) }, deps)
	if err := p.Process(context.Background(), *bomF, *prioF, *out); err != nil {
		fmt.Fprintln(os.Stderr, "ERR", err.Error())
		os.Exit(1)
	}
	fmt.Println("done")
}
