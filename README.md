# Migo BOM workflow interpreter

This standalone Go module runs Migo’s original
[`workflows/bom.yaml`](workflows/bom.yaml), extracted from commit
`6294d2b87f7a756e69373ef551b650fa72e012fd`. It takes a customer BOM and a
Priority file and writes the styled `BOM` and `debug` sheets to an XLSX workbook.

The runtime implements the operations used by that YAML: `from`, `set`,
`filter`, `dedupe`, `fill`, `window`, `join`, `stack`, `pick`, `save`,
`if`, `log`, `fail`, `sql`, and `rest`. It preserves the workbook readers,
expression evaluation, SQL batching, SAP retries, and XLSX formatting used
by the original workflow.

## Embed it in Go

Use Go 1.22 or later. Clone the `yaml-interpreter` branch alongside your
application.

```sh
git clone --single-branch --branch yaml-interpreter https://github.com/Sepiolina/migo.git migo-yaml
```

From your application’s module directory, add the local dependency.

```sh
go mod edit -require=example.com/migo-yaml@v0.0.0
go mod edit -replace=example.com/migo-yaml=../migo-yaml
```

Copy `workflows/bom.yaml` to your application’s working directory as
`bom.yaml`. Add this program, run `go mod tidy`, and place the input files
at the paths shown below.

```go
package main

import (
	"context"
	"log"

	"example.com/migo-yaml/workflow"
)

func main() {
	bom, err := workflow.Load("bom.yaml")
	if err != nil {
		log.Fatal(err)
	}
	inputs := map[string]string{
		"bom":      "customer.xlsx",
		"priority": "priority.xlsx",
	}
	rt := workflow.Runtime{Log: func(line string) { log.Print(line) }}
	if err := bom.Run(context.Background(), rt, inputs, "annotated.xlsx"); err != nil {
		log.Fatal(err)
	}
}
```

Use `Parse` if your application already holds the YAML bytes. Reuse a
compiled workflow across jobs with independent output paths and
concurrency-safe runtime callbacks. Pass paths by input name, or use
`Assign` to apply the YAML filename matching rules to an ordered input list.

## Enable enrichment

The zero-value runtime runs the offline workflow. To match a configured
Migo deployment, supply the same effective configuration and data sources.

| Enrichment | Host integration |
| --- | --- |
| Material Master | Set `db_enabled` in `Runtime.Cfg`, and set `Runtime.SQL` to a callback using your SQL Server connection. The workflow passes connection name `db`, the query, and bound positional values for `@p1`, `@p2`, and so on. Execute the query with those arguments and return a `workflow.Table` with columns in result order and values aligned to those columns. |
| SAP | Set `sap_enabled`, `sap_host`, `sap_port`, and `sap_werks`. Supply `sap_tcode`, `api_batch_size`, and `api_retries` when configured in Migo. The runtime sends requests to the SAP endpoint defined in the YAML. Set `Runtime.HTTP` to use your own HTTP client. |

Use `workflow.Values` to supply the configuration map, or implement
`Value(key string) any`. The host manages connections, credentials, and job
lifecycle. Load trusted workflow definitions because steps can issue HTTP
requests and invoke the SQL callback.

## Verify the extraction

Run `go test ./...` from this checkout. The BOM tests compare both output
sheets with Migo’s golden workbook for stubbed SQL and SAP disabled. They
also check Priority workbook layouts and manufacturer part-number
normalization.
