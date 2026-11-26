# Workflows

A workflow is one YAML file. It holds everything about a process: which files
come in, the steps (sheet wrangling, SQL, REST, branching), every rule as an
inline expression, and the workbook that goes out. Go code knows nothing
about any particular process.

## Shape

```yaml
name: Parts summary
input:                         # one entry per input file
  parts: {label: Parts list}   # label: display name for the input
steps:                         # run top to bottom on "the current table"
  - from: parts[Parts]
  - filter: num(Quantity) > 0
  - save: summary
output:
  sheets: {Parts: summary}     # sheet name -> saved dataset
```

**Inputs.** Pass paths keyed by input name to `Run`. If your host receives only an ordered list of files, call `Assign` first. An input with `match: "<regexp>"` claims the first available matching filename. The remaining inputs are assigned in order.

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
| `config("key")` | a value provided by `Runtime.Cfg` |
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

**Inputs** may be `.xlsx`, `.xls` (Excel 97–2003) or CSV. A CSV file is a
workbook with one sheet, `Sheet1`; its delimiter (comma, semicolon, tab or `|`)
is detected from the first line, and text that is not UTF-8 is read as
Windows-1252.

## Editor support

`docs/workflow.schema.json` is a JSON Schema for workflow files. With it an
editor completes step names and keys, shows each one's help on hover and
underlines unknown keys while you type.

Point your YAML editor at the bundled schema. For a workflow in the
`examples` directory, add this first line.

```yaml
# yaml-language-server: $schema=../docs/workflow.schema.json
```

Use `workflow.Schema()` to generate a schema for your build. Use `Load` or
`Parse` to compile the expressions and check dataset references. Column
references against input data are resolved during execution.

## Steps

The step reference covers each operation and its settings. [Step reference](STEP_REFERENCE.md)

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
| `drop` | `[Col, ...]` | remove these columns |
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
- fail: {when: 'len(rows) == 0', message: The file is empty.}   # stops the run; message is the returned error
```

### SQL

```yaml
- sql:
    connection: db                   # connection name passed to Runtime.SQL
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
          # a side may name its own color, which wins over color: {left: "medium #2C3E50"}
    - name: Details
      like: Report                   # Report's columns and styles, plus these
      columns: [{header: Note, column: Note}]
```

The short form `sheets: {Name: dataset}` writes every column as is.

`output.format: csv` writes the one named sheet as a UTF-8 CSV file instead
of a workbook. Column mappings, headers and `value` expressions still apply.

