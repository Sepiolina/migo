# Mai workflow engine

The part of MaiService that reads workflow YAML and runs it: the parser,
every step, the expression language, Excel/CSV reading and writing, and the
SQL and REST steps. It is its own Go module so other services can use it as
a library:

```sh
go get github.com/Sepiolina/migo/engine@main
```

MaiService, the `migo` CLI and Mai Studio use this same code through a
`replace` directive in the repository's root `go.mod`, so there is one copy
of the engine.

## Packages

| Package | What it does |
| --- | --- |
| `workflow` | Parse or load a workflow file, validate it, run it, trace it step by step, list the steps it supports. |
| `config` | The values that `config("key")` reads in expressions. `config.New(map)` builds them in code, and `config.Load(path)` reads a `config.json` file. |
| `mock` | Answers SQL and REST steps from a `.mocks.yaml` file, for tests and offline runs. |
| `wftest` | Runs `.test.yaml` workflow tests that compare a run with an expected output. |
| `sheet` | Reads `.xlsx`, `.xls` and `.csv` files into grids of text. |
| `cli`, `cmd/migo` | The `migo` command line. This build has no SQL Server driver; the repository's `cmd/migo` adds it through `cli.Options`. |
| `jsx`, `logx` | Number, text and truthiness rules shared with the Node service, and the logger interface. |

## Run a workflow

```go
import (
	"context"

	"github.com/Sepiolina/migo/engine/config"
	"github.com/Sepiolina/migo/engine/workflow"
)

wf, err := workflow.Load("orders-totals.yaml") // or workflow.Parse(yamlBytes)
if err != nil {
	return err // the error names the step and line
}
rt := workflow.Runtime{
	Cfg: config.New(map[string]any{"stock_url": "https://stock.example"}),
	// SQL:  your func(ctx, connection, query, args) (*workflow.Table, error); nil makes sql steps fail
	// HTTP: an *http.Client for rest steps; nil uses one with a 30-second timeout
	// Log:  a logx.Logger for log steps; nil discards
}
err = wf.Run(context.Background(), rt,
	map[string]string{"orders": "/path/to/orders.xlsx"}, // one file per declared input
	"/path/to/output"+wf.Ext())                           // .xlsx or .csv, as the workflow says
```

`wf.Inputs` lists the files a workflow expects. `wf.Service` holds its
endpoint settings, and `wf.ContentType()` gives the MIME type of its output.
`wf.Trace(...)` runs the workflow step by step and returns each step's
rows and column profiles; Mai Studio's preview uses it.

To run without a database or network, use a mocks file:

```go
// import "github.com/Sepiolina/migo/engine/mock"
set, err := mock.Load("tests/stock-lookup.mocks.yaml")
rt, err := set.Runtime(nil)
```

## Hand it to another project

`make engine-kit` in the repository root writes `dist/mai-engine-<version>.zip`.
CI also attaches the zip to every run as `mai-engine-kit`. The zip contains:

- this module;
- the conformance suite (`conformance/`): workflows, inputs and the exact
  outputs `migo` writes;
- the workflow documentation;
- `AGENTS.md`, which tells a coding agent how to integrate the engine and
  prove that its output matches `migo`.

## Develop

The engine's tests use fixtures from the repository (`../workflows`,
`../testdata`, `../examples`). Run them from this folder:

```sh
go test ./...
```

From the repository root, `make check` checks both modules. After changing a
step descriptor, run `make describe` there to regenerate the Studio step
catalog, the JSON Schema and [the step reference](../docs/STEP_REFERENCE.md).
The workflow file format is described in [docs/WORKFLOWS.md](../docs/WORKFLOWS.md).

Versions: tag releases of this module as `engine/vX.Y.Z` (for example
`git tag engine/v0.1.0`), then `go get github.com/Sepiolina/migo/engine@v0.1.0`.
