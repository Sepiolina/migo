// Command run demonstrates running the example from the module root.
package main

import (
	"context"
	"log"
	"os"

	"example.com/migo-yaml/workflow"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./examples/run OUTPUT.csv")
	}
	wf, err := workflow.Load("examples/orders.yaml")
	if err != nil {
		log.Fatal(err)
	}
	rt := workflow.Runtime{Log: func(line string) { log.Print(line) }}
	inputs := map[string]string{"orders": "examples/orders.csv"}
	if err := wf.Run(context.Background(), rt, inputs, os.Args[1]); err != nil {
		log.Fatal(err)
	}
}
