# Integrating the Mai workflow engine

Read this file in full before changing anything. It tells you how to add
this engine to a project so that it reads workflow YAML and writes output
exactly as `migo` and MaiService do. The bar is 1:1: same accepted and
rejected files, same error messages, same output values and sheets.

## What is in this folder

```
AGENTS.md          this guide (CLAUDE.md points here)
MANIFEST.json      version, source commit, and a SHA-256 for every file
engine/            the Go module github.com/Sepiolina/migo/engine: the engine itself
  workflow/        parse, validate, run and trace workflows; the step catalog
  sheet/           read .xlsx, .xls and .csv
  config/          the values config("key") reads
  mock/            answer sql and rest steps from a .mocks.yaml file
  wftest/          run .test.yaml workflow tests
  cli/, cmd/migo/  the migo command line (no SQL Server driver)
conformance/       the 1:1 test suite: workflows, inputs and the outputs migo writes
workflows/, examples/, testdata/   fixtures used by the engine's own tests
docs/              WORKFLOWS.md (file format), STEP_REFERENCE.md, CLI.md,
                   workflow.schema.json (JSON Schema for workflow files)
```

## Rules

1. **Do not edit anything under `engine/`.** Output identity depends on
   unchanged code, including the number formatting in `jsx`, the readers in
   `sheet` and the writer in `workflow/excel.go`. Do not reformat it or
   "modernize" it, and do not run code generators over it. If something in
   it looks wrong, report it rather than patching it.
2. **Do not reimplement the engine in another language** unless your user
   explicitly asks for a port. Use the Go module, or call the `migo`
   binary. A port is only acceptable when every case in `conformance/` passes
   unchanged (see "Prove it").
3. **Pass workflow YAML through as it is.** Do not parse and re-serialize it,
   convert it to JSON, or merge files. The parser is strict on purpose: it
   rejects duplicate keys, aliases, merge keys and unknown fields. Accept what
   `workflow.Parse` accepts and nothing else.
4. **Pass input files through as bytes.** Do not convert encodings, line
   endings or Excel formats. The reader sniffs the file type from its
   content, not its name, so any temporary file name works.
5. **Copy the output file as it is written.** Do not re-save the workbook
   with another library. Do not strip the UTF-8 byte order mark from CSV
   output: Excel needs it.
6. Keep `conformance/` and run it in the host project's CI (see below).

## Choose how to integrate

### A. The host is written in Go (preferred)

Copy the `engine/` folder into the host repository, for example to
`third_party/mai-engine/`, and point the host's `go.mod` at it:

```
require github.com/Sepiolina/migo/engine v0.0.0
replace github.com/Sepiolina/migo/engine => ./third_party/mai-engine
```

Then run `go mod tidy`. If the host can reach the GitHub repository, you can
use `go get github.com/Sepiolina/migo/engine@<version>` instead of copying.
In that case pin the version this folder's `MANIFEST.json` names. Requires
Go 1.22 or newer.

Load each workflow once; a parsed `*workflow.Workflow` is safe to run
concurrently, as MaiService does:

```go
import (
	"context"

	"github.com/Sepiolina/migo/engine/config"
	"github.com/Sepiolina/migo/engine/workflow"
)

wf, err := workflow.Parse(yamlBytes) // or workflow.Load(path); errors name the step and line
rt := workflow.Runtime{
	Cfg:  config.New(map[string]any{ /* the same keys as MaiService's config.json */ }),
	SQL:  mySQL,   // see "Runtime"; nil makes sql steps fail when they run
	HTTP: nil,     // *http.Client for rest steps; nil uses one with a 30 s timeout
	Log:  func(line string) { /* log steps write here */ },
}
inputs := map[string]string{"orders": "/tmp/upload-1"} // input name -> file path, one per wf.Inputs
out := "/tmp/result" + wf.Ext()                          // ".xlsx" or ".csv", as the workflow says
err = wf.Run(ctx, rt, inputs, out)
```

### B. The host is not written in Go

Build the command line once and call it as a subprocess:

```sh
cd engine && go build -trimpath -o migo ./cmd/migo
```

```sh
migo run -in orders=/path/in.xlsx -out /path/out.xlsx [-cfg '{"key":"value"}'] [-mocks answers.mocks.yaml] workflow.yaml
```

- Exit code `0`: success. The last stdout line is `done: <out>`.
- Exit code `1`: the workflow failed. stderr has `ERR <message>`, which is
  the same message the Go API returns.
- Exit code `2`: a usage error, such as a missing input or a wrong output
  extension.
- Each `log` step prints a `LOG: <line>` line on stdout.

`migo validate <file>` checks files without running them. `migo test <dir>`
runs `.test.yaml` suites. See `docs/CLI.md` for every command. This build
has no SQL Server driver, so sql steps need `-mocks` or `-sql-stub`.
Otherwise use option A, or build your own `main` with `cli.Main` and
`cli.Options{SQL: ...}`.

## Runtime: what the host provides

- **Config.** Workflows read `config("key")`. Give them the same keys and
  values that MaiService's `config.json` has, for example `db_enabled` or a
  base URL. `config.New(map)` builds a config from keys, and
  `config.Load(path)` reads a `config.json` file.
- **SQL.** The function has the signature `func(ctx, connection, query
  string, args []any) (*workflow.Table, error)`. To match MaiService:
  - Accept connection `"db"` and refuse any other name.
  - Run the query with positional parameters `@p1`, `@p2`… and send string
    arguments as VARCHAR.
  - Return the column names and each row's values as the database driver
    scanned them, converting only `[]byte` to `string`.
- **HTTP.** `rest` steps use `Runtime.HTTP`. Put company proxies, auth
  headers and TLS settings in that client's transport. Do not rewrite
  bodies.
- **Tests and offline runs.** `mock.Load("x.mocks.yaml")` followed by
  `set.Runtime(nil)` gives a runtime that answers sql and rest steps from
  the file and reaches nothing else.

## Serving workflows over HTTP like MaiService (only if asked)

MaiService serves each workflow at `POST /api/<route>/upload` and
`GET /api/<route>/download`. If your user wants the same API, copy these
rules exactly:

- **Route.** Use `wf.Service.Route` if it is set. Otherwise use the
  workflow's file name without `.yaml`; it must satisfy
  `workflow.ValidRoute`.
- **Uploads.** Accept multipart field `files`, one file per input in
  `wf.Inputs`. Each file can be at most `wf.Service.MaxUploadMB` MiB, or
  25 MiB when that is 0. Assign files to inputs with
  `pick := wf.Assign(originalFileNames)`; input `i` gets file `pick[i]`.
  Inputs with a `match` pattern claim matching names first, and the rest
  follow upload order.
- **Owner.** Read the owner from form field `wf.Service.OwnerField`. It may
  contain only letters, digits, `-` and `_`. It is optional only when
  `wf.Service.OwnerRequired` is false.
- **Download.** Send the file with `Content-Type: wf.ContentType()`. Build
  the name from the first assigned upload's name: drop the last extension,
  replace `\ / : * ? " < > |` and control characters with `_`, and trim it
  (an empty name becomes `output`). Then add `_<owner>` and `wf.Ext()`.

## Prove it: the 1:1 checks

Run these from `engine/`. All of them must pass before you call the
integration done, and after every engine update:

```sh
go vet ./...
```

```sh
go test ./...
```

```sh
go run ./cmd/migo test ../conformance
```

`conformance/` holds one `.test.yaml` per workflow. Each case gives the
inputs (and mocks) and either the exact output `migo` wrote or the error it
must stop with. Outputs are compared by sheet names and every cell value, or
by text for CSV. One BOM case is checked against an independent golden file.

To check the **host's own code path** (your HTTP handler, queue worker or
subprocess wrapper), add a test in the host project that feeds each
conformance case through that path. The test should compare the result
with the case's `expect` file, or with its `error` text. Add the three
commands above, and that host test, to the host's CI.

## Updating the engine later

Replace the copied `engine/` folder (and `conformance/`) with the ones from
a newer zip. Do not merge files by hand. Then run the checks again. If an
expected output changed on purpose, the new zip's `conformance/` already
contains it.

## When you are done, report

- How the host uses the engine: option A or B, and where the code lives.
- What the host supplies for config, SQL and HTTP.
- The output of the three checks and of the host-level conformance test.
- Anything you could not match 1:1, and why.
