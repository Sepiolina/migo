# Migo YAML interpreter

A standalone Go library extracted from Migo at commit
`6294d2b87f7a756e69373ef551b650fa72e012fd`. It compiles workflow YAML and runs it
against local CSV, XLSX, or BIFF8 XLS files. Workflows can transform tables,
branch, retrieve data through SQL and HTTP, and write CSV or XLSX results.

## Run the example

Use Go 1.22 or later. From this directory, run the example and inspect
`orders-output.csv`.

```sh
go run ./examples/run orders-output.csv
go test ./...
```

The sample workflow reads `examples/orders.csv`, keeps open orders, and
computes their totals. The CSV starts with a UTF-8 byte order mark and contains the following rows.

```csv
Order,Total
A-101,25
A-103,12
```

## Embed in a Go application

For local development, place your application's module alongside this one.
From the application module directory, add a local module dependency.

```sh
go mod edit -require=example.com/migo-yaml@v0.0.0
go mod edit -replace=example.com/migo-yaml=../migo-yaml
```

Use `example.com/migo-yaml/workflow` in your Go source, then run `go mod tidy`
in your application. `example.com/migo-yaml` is the local module identifier.
If you publish the library, update it in `go.mod` and the Go imports to your
published path.

```go
package main

import (
	"context"
	"log"

	"example.com/migo-yaml/workflow"
)

func main() {
	wf, err := workflow.Load("orders.yaml")
	if err != nil {
		log.Fatal(err)
	}

	inputs := map[string]string{"orders": "orders.csv"}
	runtime := workflow.Runtime{
		Log: func(line string) { log.Print(line) },
	}
	if err := wf.Run(context.Background(), runtime, inputs, "report"+wf.Ext()); err != nil {
		log.Fatal(err)
	}
}
```

Run this host program with copies of `examples/orders.yaml` and
`examples/orders.csv` in its working directory. Supply the input paths keyed
by YAML input name and choose a writable output path for each run. Use
`Parse([]byte)` when your application already has the YAML in memory.
Compile and validate workflows before accepting jobs, then reuse the compiled
workflow across runs. Keep the compiled workflow and shared runtime
configuration read-only during concurrent runs.

## Connect the runtime

| Field | Host integration |
| --- | --- |
| `Cfg` | Supply `workflow.Values{"host": "example.org"}` or implement `Value(key string) any`. YAML expressions read values through `config("host")`. Missing keys return nil. |
| `Log` | Supply a function that accepts one log line. The zero value discards logs. |
| `SQL` | Supply a function that takes a context, connection name, query, and positional arguments and returns a `*workflow.Table`. Select the database by name, bind the arguments using your driver, and keep column and row order aligned. SQL steps without a handler follow their configured error behavior. |
| `HTTP` | Supply an `*http.Client` for REST steps. The default client has a 30-second timeout. |
| `Sleep` | Optionally supply the delay function used between HTTP retry attempts. The default is `time.Sleep`. |

Workflow definitions can issue outbound requests and invoke the SQL handler.
Use trusted YAML and scope the connections, HTTP client, and configuration
values to the operations your host permits.

`Trace` runs the steps and returns per-step summaries without writing an
output file. It still runs SQL and HTTP steps. A failing step reports its
error in the corresponding `StepTrace.Error`.

## How the interpreter works

1. `Load` reads a file and delegates to `Parse`. The parser validates the
   YAML structure and compiles step arguments and expressions into executable
   functions. Each step carries its source line number.
2. `Run` builds fresh state for each execution. `from` loads the current
   table, transformation steps update it, and `save` stores named datasets
   that later steps and output sheets can use.
3. Output writers resolve the saved datasets, apply column mappings and cell
   expressions, and write the requested format. XLSX output also applies
   workbook styles.

## Extend the interpreter

Register a custom table step once at startup, before loading workflows or
starting workers. `RegisterTableStep` takes a compiler that validates the
YAML argument and returns a function that transforms a copy of the current
table. The registry is shared across the process. For custom editor metadata,
register the matching descriptor through `DescribeStep`. Use `Schema()`,
`Describe()`, and `Reference()` to expose the language to editors or to
generate its reference. [Custom step example](workflow/register_test.go)

Existing `service` YAML remains valid and is available as `Workflow.Service`
for hosts that implement their own routing. The integration entry points
above run inside your application process.

- [YAML syntax, expressions, and output settings](docs/WORKFLOWS.md)
- [Step reference](docs/STEP_REFERENCE.md)
- [JSON Schema](docs/workflow.schema.json)
