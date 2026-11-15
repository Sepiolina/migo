# The migo command line

`migo` creates, checks, runs and tests workflow files on your computer, with no
server. It uses the same engine as MaiService and Mai Studio, so a workflow
that passes here behaves the same when it is served.

Build it once (`go build -o migo ./cmd/migo`, or `migo.exe` on Windows), or
replace `migo` with `go run ./cmd/migo` in every command below.

| Command | What it does |
| --- | --- |
| [`migo new`](#create-a-workflow) | Creates a workflow from a starter template |
| [`migo validate`](#check-a-workflow) | Checks workflow files without running them |
| [`migo run`](#run-a-workflow) | Runs a workflow on local files |
| [`migo test`](#test-a-workflow) | Runs a workflow's tests: sample files in, expected output out |
| [`migo describe`, `schema`, `docs`](#generated-files) | Writes the step catalog, the editor schema and the step reference |

`migo help` prints this list, and `migo <command> -h` a command's flags.

## Five-minute walkthrough

From the repository folder:

```sh
migo new order-totals -template parts-summary -dir my-workflows
```

```sh
migo validate my-workflows/order-totals.yaml
```

```sh
migo run -in parts=examples/parts.xlsx -out out.xlsx my-workflows/order-totals.yaml
```

Open `out.xlsx`, then edit `my-workflows/order-totals.yaml` (the
[step reference](STEP_REFERENCE.md) lists every step, and
[editor support](WORKFLOWS.md#editor-support) gives completion while you type)
and run it again. When the output is right, [keep it as a test](#test-a-workflow).

## Create a workflow

```sh
migo new <name> [-template id] [-dir workflows]
```

Writes `<dir>/<name>.yaml` and prints the commands to check and run it. The
name becomes the route (`/api/<name>`), so it uses lowercase letters, digits
and hyphens. An existing file is never overwritten.

The default folder is `workflows`, where Mai Studio and MaiService look for
workflows in a project folder. Inside this repository that folder holds the
built-in workflows compiled into the service, so the walkthrough above uses
`-dir my-workflows` instead.

`migo new -list` shows the templates:

| Template | Starts you with |
| --- | --- |
| `blank` (default) | One source, one output |
| `clean-sheet` | Trim cells, drop blank and duplicate rows |
| `parts-summary` | Filter rows, then total a column per group |
| `lookup-enrich` | Join an Excel sheet with a CSV list, then total per group |
| `orders-totals` | The tutorial: calculated column and totals per customer |
| `excel-to-csv` | Convert a sheet to a CSV file |

Mai Studio's start page offers the same templates. They live in
`internal/workflow/templates/`; see
[Extending](EXTENDING.md#6-add-a-starter-template) to add one.

## Check a workflow

```sh
migo validate workflows/*.yaml
```

Compiles each file with the production parser and runs nothing. It catches
unknown steps and settings, expression mistakes, and names that are not
defined. One line per file; the exit code is 1 when any file fails:

```
ok    workflows/bom.yaml  (inputs: bom, priority; xlsx output: BOM, debug)
FAIL  workflows/draft.yaml
      draft.yaml: step 1 (sorted, line 4): unknown step "sorted"
```

Problems that depend on the data (a missing sheet or column) only show when
the workflow runs: use `migo run -check` or a test.

## Run a workflow

```sh
migo run -in <input>=<file> [-in ...] -out <file> <workflow.yaml>
```

| Flag | Meaning |
| --- | --- |
| `-in name=path` | The file for one workflow input; repeat it for each input |
| `-out file` | Where to write the result: `.xlsx`, or `.csv` for a workflow with `output.format: csv` |
| `-check` | Run everything but keep no output (instead of `-out`) |
| `-cfg '<json>'` | `config.json` keys as JSON. The default `{}` turns the database and SAP off |
| `-sql-stub file.json` | Give every `sql` step this table, `{"columns": [...], "rows": [[...]]}`. See `testdata/stub_master.json` |

`migo [flags] workflow.yaml`, without `run`, does the same.

## Test a workflow

A test file lists runs of a workflow and what each must produce. By
convention it is `tests/<workflow>.test.yaml` beside the workflow. (Keep test
files out of the `workflows/` folder itself: every `.yaml` file there is
served as a workflow.)

```yaml
workflow: ../parts-summary.yaml     # paths are relative to this file
tests:
  - name: totals per part
    in: {parts: ../parts.xlsx}      # one file per workflow input
    expect: parts-summary.xlsx      # the expected output
  - name: an empty file is refused
    in: {parts: empty.xlsx}
    error: is empty                 # the run must fail with this text
```

Run every test under a folder, or one file:

```sh
migo test examples
```

```
examples/tests/parts-summary.test.yaml
  ok    totals per part

1 passed, 0 failed
```

A failure names the first difference:
`sheet "Parts" cell B2: got "5", expected "6"`.

**Creating and updating expected files.** Write the test without the expected
file, then run:

```sh
migo test -update examples
```

`-update` writes the current output as the expected file for every test that
has none or differs. Open the file and check it before you commit it: from
then on it is the definition of correct.

**What is compared.** For a workbook: the sheet names and every cell's value.
Styles, widths and colours are not compared. For CSV: the text, line by line.

**Per-test settings.**

| Key | Meaning |
| --- | --- |
| `name` | Required, unique in the file |
| `in` | Required: a file for every workflow input |
| `expect` | The expected output file, or |
| `error` | text the run's error must contain |
| `config` | `config.json` keys for this run, e.g. `{db_enabled: true}` |
| `sql_stub` | A JSON table that every `sql` step returns |

Tests never use the network: a `sql` step needs `sql_stub`, and a `rest` step
fails if it runs, so test the branch where it is switched off by `config`.

The repository's own tests are in `examples/tests/`; `go test ./...` and
`make workflows` run them.

## Generated files

`make describe` runs all three after a step descriptor or template changes.

| Command | Writes | Used by |
| --- | --- | --- |
| `migo describe -out file.json` | Steps, nested forms, helpers and templates as JSON | Mai Studio's forms and start page |
| `migo schema -out file.json` | A JSON Schema for workflow files | [Editor completion](WORKFLOWS.md#editor-support) |
| `migo docs -out file.md` | The [step reference](STEP_REFERENCE.md) | Readers |

Without `-out`, each prints to standard output.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | The workflow failed: a validation, run or test failure |
| 2 | The command was used wrongly: a bad flag, a missing file or name |
