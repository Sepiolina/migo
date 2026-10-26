# Workflows

A workflow is one YAML file. It holds everything about a process: which files
come in, the steps (sheet wrangling, SQL, REST, branching), every rule as an
inline expression, and the workbook that goes out. Go code knows nothing
about any particular process.

## Files and endpoints

| File | Endpoints |
| --- | --- |
| `workflows/<name>.yaml` | `POST /api/<name>/upload`, `GET /api/<name>/download` |

All workflows use the same request and response. Upload sends multipart
`files` (one per input, in any order) plus `bam` (the employee number). The
response is:

```json
{"status":"success","message":"<name> processed successfully.","fileName":"<first input>_<bam>.xlsx","downloadUrl":"http://host/api/<name>/download","bam":"113280"}
```

Download returns the latest result of that workflow.

Where the files come from:
- The files in the repo folder `workflows/` are built into the binary.
- A file in `<config dir>/workflows/` replaces the built-in one with the same name, or adds a new endpoint.

Names use lowercase letters, digits and `-`. Results go to `outputs/<name>/`.

Everything is checked when the file loads: step arguments, expressions, and
input and dataset names. A bad file stops startup; it never fails a request
halfway.

## Shape

```yaml
name: Parts summary            # shown in the response message
input:                         # one entry per uploaded file
  parts: {label: Parts list}   # label: used in "Expected 1 file (Parts list)"
steps:                         # run top to bottom on "the current table"
  - from: parts[Parts]
  - filter: num(Quantity) > 0
  - save: summary
output:
  sheets: {Parts: summary}     # sheet name -> saved dataset
```

**Inputs.** Files are matched to inputs in upload order. An input with
`match: "<regexp>"` takes the first uploaded file whose name matches, wherever
it sits.

**Steps.** Each step is one `name: argument` pair (or `if:` / `then:` /
`else:`). Most steps work on the current table: `from` loads it, `save` keeps
a named copy, and `join` / `append` / `compare` / `output` use saved copies.

**YAML.** Quote an expression that contains `#` or starts with `!`, `{`, `[`
or `*`: in YAML, ` #` starts a comment.

## Expressions

Expressions are [expr](https://expr-lang.org/docs/language-definition).

**Row expressions** (`filter`, `set`, `window`, `pick`, `split`, output
`value` and `when`) see the row's columns as variables: `Qty`, or
`col("Part Number")` for names that are not identifiers. They also see:

| | |
| --- | --- |
| `index` | the row's position (in `window`: within its group), from 0 |
| `prev("Col")` | the previous row's value; with `set`, values already computed for this column, so running totals and counters work |
| `group`, `size` | in `window`: the group's rows (as maps) and their number |

**Table expressions** (`if`, `log`, `fail`, the `sql` and `rest` arguments)
see `rows` (the current table as a list of maps), `table("name")` (a saved
dataset) and `sheets("input")` (an input's sheet names).

**Helpers**, on top of expr's built-ins (`len`, `map`, `filter`, `any`,
`first`, `uniq`, `sort`, `max`, `matches`, `startsWith`, `contains`, `?:`,
`??`, `let`...):

| | |
| --- | --- |
| `config("key")` | a `config.json` value |
| `norm(v)`, `trim(v)`, `str(v)` | trimmed upper case; trimmed (JavaScript whitespace); text |
| `num(v)` | a number; error if it is not one |
| `toNumber(v, d)` | JavaScript `parseFloat` (commas ignored), else `d` |
| `parseFloat(v)` | JavaScript `parseFloat`, `nil` when not a number |
| `blank(v)`, `truthy(v)` | nil or whitespace; JavaScript truthiness |
| `coalesce(a, b, ...)` | the first truthy value |
| `fields(v)`, `splitAny(v, ",;")` | whitespace-separated words; pieces between any of the characters |
| `replaceRe(v, "^0+", "")` | regexp replace |

**Text with expressions.** `log`, `fail`, `reason`, `url`, `headers` and
`on_error` are text with `{expression}` parts: `"Loaded {len(rows)} rows"`.

Cells hold text, numbers, `true`/`false` or nothing (`nil`). Sheets are read
as text.

## Steps

### Reading

```yaml
- from: parts                 # first sheet of an input, row 1 as header (or a saved dataset)
- from: parts[Parts]          # a named sheet
- from:
    input: bom
    sheet: 1                  # 1-based index or name (default: first)
    header:                   # find the header row by accepted titles
      columns: {Level: [level, lvl], Qty: [qty, quantity]}
      search: 15              # within the first 15 rows
      min: 4                  # where at least 4 columns match
      fallback: {Level: A, Qty: Z}   # positions of unmatched columns, or when no row matches
    columns: {PN: A, Value: M}       # or: fixed positions, no header
    start: 168                # first data row (default: after the header, else 2)
    trim: true                # trim cells
    skip_blank: true          # drop rows whose read cells are all blank
    row_number: Row           # add the sheet row number as a column
    empty: The file is empty. # fail with this message when the sheet has no cells
```

### Table steps

| Step | Argument | Effect |
| --- | --- | --- |
| `save` | `name` | keep a copy of the current table |
| `filter` | `'<row condition>'` | keep the rows where it is true |
| `set` | `{Col: '<expression>', ...}` | compute columns in order, row by row; new ones are appended |
| `select` | `{New: Source, ...}` | keep only these columns, renamed and in this order |
| `rename` | `{Old: New, ...}` or `'<expression of name>'` | rename columns, e.g. `'lower(name)'` |
| `fill` | `[Col, ...]` | fill blank cells from the row above |
| `dedupe` | `[Col, ...]` or `{by: [..], keep: first\|last, normalize: false}` | one row per key |
| `sort` | `{by: [Col, ...], desc: false, numeric: false}` | stable sort |
| `group` | `{by: [Key], sum: [..], count / mean / min / max / first / last / join: [..]}` | one row per key; without `by`, one row in total |
| `window` | `{by: [Key], where: '<cond>', set: {Col: '<expr>'}}` | compute columns per group, keeping every row (expressions see `group`, `index`, `size`) |
| `pick` | see below | choose one row per group by ordered rules |
| `join` | `{with: ds, on: [K], right_on: [K], how: left\|inner, normalize: false, take: {New: RightCol}}` | add matching columns (`take` chooses and names them) |
| `append` | `[ds, ...]` | add rows; columns matched by name |
| `stack` | `{columns: [A, B], into: Key}` | one row per listed column, its value in `Key` |
| `compare` | `{with: baseline, by: [K], columns: [..], status: status}` | add `added` / `removed` / `changed` / `unchanged` |

`pick` runs ordered rules inside each group. The first rule that applies
chooses the row:

```yaml
- pick:
    by: [Group]
    where: Eligible                  # rows that take part
    mark: IsMain                     # true on the chosen row, false on the others
    reason: Reason                   # column receiving the rule's reason
    rules:
      - rows: Priority == 1          # the rule's candidates
        when: size == 1              # default size > 0; sees size, best, tied
        reason: "Single P1"
      - rows: 'true'
        best: Stock                  # highest value wins; ties go to the first row
        when: best > 0
        reason: "Highest stock {best} ({tied} tied): {chosen.Vendor}"
```

### Branching

```yaml
- if: truthy(config("db_enabled"))   # any table expression
  then: [steps]
  else: [steps]                      # optional

- split:                             # route rows
    where: '<row condition>'
    then: [steps]                    # run on the matching rows
    else: [steps]                    # run on the others; results appended, then first

- log: "Loaded {len(rows)} rows"     # or {when: ..., message: ...}
- fail: {when: 'len(rows) == 0', message: The file is empty.}   # stops the run; message is the API error
```

### SQL

```yaml
- sql:
    connection: db                   # db_* keys of config.json (the default)
    query: SELECT Code, Name FROM Items WHERE Code IN (@codes) AND Site = @site
    params:                          # table expressions; a list expands to @p1, @p2, ...
      codes: 'uniq(map(rows, norm(#.Code)))'
      site: '"S1"'
    batch: 1900                      # list values per query; results are appended
    columns: [Code, Name]            # shape when nothing comes back
    on_error: 'lookup failed: {error}'   # log and continue with no rows (default: stop)
    save: items                      # the result; the current table is unchanged
```

### REST

```yaml
- rest:
    url: 'http://{config("host")}:{config("port")}/api/items'
    method: POST                     # default GET
    each: 'uniq(map(rows, #.Code))'  # values to send
    batch: '50'                      # values per request (expression)
    body: '{codes: batch}'           # JSON body (expression); sees batch, batch_number
    headers: {Authorization: 'Bearer {config("token")}'}
    retries: '3'                     # extra attempts, waiting 2, 4, 8, 10 s
    rows: response.items             # the list of objects in the response
    keys: lower                      # trim and lower-case object keys (or upper)
    columns: [code, name]            # shape when nothing comes back
    on_error: 'batch {batch_number} failed: {error}'   # log and skip (default: stop)
    save: items
```

Inside `map(...)`, write a map literal in parentheses, e.g.
`map(batch, ({CODE: #}))`, because `{...}` alone is a closure there.

## Output

```yaml
output:
  sheets:
    - name: Report
      from: result                   # a saved dataset
      freeze: true                   # keep the header row in view
      header_height: 28
      header: {fill: "#D9EAF7"}      # header cells (bold by default)
      style: {align: center}         # every cell
      columns:                       # default: every column as is
        - {header: Code, column: ItemCode, width: 12}            # column: a dataset column (default: header)
        - {header: Total, id: total, value: 'num(Qty) * num(Price)', number_format: "#,##0.00"}
      styles:                        # in order; later ones win
        - when: Status == "late"     # row condition over the dataset's columns
          columns: [total]           # column ids (default: the header); omit for all columns
          fill: "#FF0000"
          font: {bold: true, color: "#FFFFFF", size: 10}
          border: {bottom: thick, color: "#000000"}   # thin, medium, thick, dashed, dotted, double
    - name: Details
      like: Report                   # Report's columns and styles, plus these
      columns: [{header: Note, column: Note}]
```

The short form `sheets: {Name: dataset}` writes every column as is.

## The BOM workflow

`workflows/bom.yaml` reads the customer BOM (with header detection) and the
Priority workbook. It then works out:
- groups by find number, forward-filled values and the quantity fix;
- PCB scope and the `_REV_` reformat;
- CFT-001 vendor rules;
- Forth P/N from Material_Master (`sql`, when `db_enabled`);
- block status, vendor and stock from SAP (`rest`, when `sap_enabled`);
- CFT-002 MAIN / SUBn (`pick`).

Finally it writes the BOM and debug sheets. Every rule is in that file; read it top to bottom.

`TestBOMMatchesGolden` checks that its cell values match the output of the
former Go implementation. A differential run over 300 random BOMs (with SQL
and SAP stubs) gave identical values.

## Run locally

```sh
go run ./cmd/migo -in bom=testdata/bom_basic.xlsx -in priority=testdata/prio_basic.xlsx -out out.xlsx workflows/bom.yaml
go run ./cmd/migo -in parts=examples/parts.xlsx -check examples/parts-summary.yaml
```

The flags:
- `-check` runs everything and keeps no file.
- `-cfg '<json>'` takes `config.json` keys. The default `{}` turns the database and SAP off.
- `-sql-stub file.json` gives every `sql` step a fixed table, `{"columns": [...], "rows": [[...]]}`. See `testdata/stub_master.json`.
