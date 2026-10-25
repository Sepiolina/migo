# Spreadsheet workflows

`bomrun -workflow` runs an ordered YAML recipe over spreadsheet values. The
existing BOM CLI flags and HTTP service keep their domain pipeline, including
vendor policy and optional SQL/SAP enrichment. Generic workflows do not apply
those rules automatically.

From the repository root:

```sh
go run ./cmd/bomrun -workflow examples/workflows/basic.yaml -check
go run ./cmd/bomrun -workflow examples/workflows/basic.yaml
go run ./cmd/bomrun -workflow examples/workflows/presets.yaml
```

`-check` reads inputs, processes every step, and validates and renders the
workbooks without publishing output files. The other commands create
`examples/workflows/generated/basic.xlsx` and `presets.xlsx`. Both recipes use
`parts.xlsx`, a small synthetic workbook bundled beside them: its `Parts` sheet
has `Part Number`, `Description`, and `Quantity` headers, two rows for `00123`
with quantities 2 and 3, a row for `00456` with quantity 1, and a zero-quantity
row. The result contains `00123` with quantity 5 and `00456` with quantity 1.
The padded first identifier demonstrates trimming and preservation of leading
zeros. This fixture is independent of the legacy BOM golden workbooks, whose
sparse headers are unsuitable for strict generic tables.

## Recipe structure

```yaml
workflow: My report
input:
  source: source.xlsx
process:
  - from: source["Parts"]
    as: cleaned
    header_row: 1
    do:
      - filter: {column: Quantity, greater_than: 0}
  - from: cleaned
    as: sorted
    do:
      - sort: {by: [Part Number], order: ascending}
output:
  - file: report.xlsx
    sheets: {Parts: sorted, Raw: cleaned}
```

Input aliases map to paths. `from` selects `alias["Sheet Name"]` (the sheet
name must be double quoted) or an earlier dataset. Each `as` creates a unique,
independent snapshot. Processes and their `do` lists execute in order;
branching from the same dataset does not alter it. Join, merge, and comparison
references must also name earlier datasets. `header_row` defaults to 1 and
counts from the input reader's declared-range origin. Headers must be unique
and nonblank; unused trailing blank headers are trimmed. Names are exact and
case sensitive. Use `@A`, `@B`, through `@AA`, etc. for positional references;
`A` means a literal header named `A`.

`header_row` applies only to file/sheet sources; it is rejected on references
to an earlier dataset.

Input, output, and template paths resolve relative to the recipe file, even
when the command runs elsewhere. Output is a list of workbooks, each with a
`file`, optional literal `template` path, and a `sheets` mapping from Excel sheet
names to datasets. Sheet names are processed in sorted order. Every process
and workbook is prepared before files are published. Each file replacement is
atomic; publication of multiple files is not a single transaction.
Output directories must already exist. The bundled examples include an ignored
`generated` directory for their results.

YAML is strictly decoded: unknown properties, duplicate keys, invalid operation
values, missing references, and preset cycles are errors. Each step contains
one operation and may add `enabled: false`. Disabled steps still need valid
syntax and operation properties. Quote string values such as `equal: "00123"`.

## Operations

The following are operation bodies under a `do` item, for example
`- forward_fill: {columns: [Category]}`. Column lists are required unless
explicitly described as optional.

| Operation | Properties and behavior |
|---|---|
| `map` | `columns: [{from: Old, to: New, move: true}]`, optional `keep: true`. Default projection emits exactly the listed destinations in list order. With `keep`, original columns stay in their original order and new destinations append. `move` removes a renamed source only with `keep`; otherwise the source is copied. Conflicting destinations fail. |
| `filter` | Leaf `column` plus one or more predicates: `equal`, `not_equal`, `contains`, `in: [a, b]`, `blank: true/false`, `greater_than`, `greater_or_equal`, `less_than`, `less_or_equal`. Predicates on a leaf are ANDed. Alternatively use recursive `all: [...]` or `any: [...]`; do not combine these modes on one node. String comparisons are exact; blank checks trim whitespace. |
| `forward_fill` | `columns: [Category]`. Replace blank or whitespace cells with the preceding nonblank value in that column; initial blanks stay blank. |
| `group` | `by: [Part Number]`, optional `aggregate: {Quantity: sum}`. Retains group keys followed by explicitly aggregated columns in source column order. Groups retain first encounter order. Other columns are dropped. Group keys cannot also be aggregated. |
| `aggregate` | `columns: {Quantity: sum}`. Aggregate the whole table into one row. |
| `sort` | `by: [Category, Part Number]`, optional `order: ascending` (default) or `descending`, optional `numeric: true`. Stable sort; numeric applies to every sort key. |
| `deduplicate` | `columns: [Part Number]`. Keep the first row for each exact key tuple. |
| `calculate` | `column: Total`, `operation: add/subtract/multiply/divide`, `columns: [Quantity, Unit Cost]`, optional numeric `value`. Fold source operands left to right, then apply `value` as the last operand. Adds or replaces the destination. Division by zero fails. |
| `transform` | `columns: [Part Number]`, `operation: trim/upper/lower/normalize/replace`. `replace` uses literal `old` and `new` strings, replacing every occurrence. `normalize` trims and uppercases using the shared part-number normalizer. |
| `join` | `with: earlier_dataset`, `on: [Part Number]`, optional `right_on` list, `how: left` (default) or `inner`, `normalize: true`, and `suffix: _right`. Right key columns are omitted; colliding nonkey names get the suffix. Duplicate right matches produce multiple rows. |
| `merge` | `with: [earlier_dataset, another_dataset]`. Append rows in listed dataset order. Column sets must match; right columns are reordered to the left table. |
| `compare` | `with: baseline`, `by: [Part Number]`, optional `columns: [Quantity]`, `normalize: true`, `status_column: status`. Current rows precede baseline-only rows. Emits the union of columns and `added`, `removed`, `changed`, or `unchanged`. Matched rows carry current values; removed rows carry baseline values. Default comparison checks the union of nonkey columns, treating missing cells as blank; explicit comparison columns must exist on both sides. Duplicate keys on either side fail; normalization affects keys only. |
| `style` | Optional `header: {bold: true, background: "#D9EAF7", color: "#17365D"}`, `auto_width: true`, `freeze_header: true`, `number_formats: {Quantity: "0.00"}`. Replaces the dataset's style settings. |

Aggregation functions are `sum`, `first`, `last`, `min`, `max`, `count`, `mean`,
and `join`. `min` and `max` are numeric. `count` counts rows, including blanks;
`join` combines distinct nonblank values in encounter order with `, `.
Arithmetic, numeric predicates, numeric sorting, and numeric aggregation reject
blank, invalid, and nonfinite values rather than silently converting them to
zero. Calculations also reject nonfinite results.
All conditions in a filter are evaluated, even after another condition decides
the match. To exclude blank quantities before a numeric comparison, use a
separate `filter: {column: Quantity, blank: false}` step first.

## Presets and imports

Preset categories are `filters`, `forward_fills`, `groups`, `aggregations`,
`mappings`, `styles`, `sorts`, `transforms`, and `pipelines`. Use an operation's
singular YAML name with `preset`:

```yaml
imports: [shared-presets.yaml]
presets:
  styles:
    local_report:
      preset: report
      header: {bold: true}
      override:
        header: {color: "#17365D"}
```

This inherits the imported `styles.report`. Maps merge recursively; lists and
scalars replace their inherited values. Local properties override inherited
ones, and `override` wins last. This also works directly on a step:

```yaml
- filter:
    preset: positive
    override: {greater_than: 10}
- group:
    by: [Part Number]
    aggregation_preset: quantities
    aggregate: {Description: first}
- style: {preset: local_report}
- pipeline: {preset: clean}
```

An aggregation preset has an `aggregate` operation body (`columns: {...}`);
`group.aggregation_preset` imports those functions and explicit `aggregate`
entries win. Pipeline presets have `do: [...]` and expand into ordinary steps.
See the executable `presets.yaml` and its `shared-presets.yaml` import.

Imports contain only `imports` and `presets`; they cannot supply input,
process, or output sections. Nested imports resolve relative to the file that
declares them. Imported and local preset names must be unique within their
category; use a new name plus `preset` to inherit instead of redefining a name.
Missing presets, import cycles, inheritance cycles, and pipeline cycles fail.

## Workbooks and templates

Inputs support XLSX and Excel 97–2003 BIFF8 XLS. Output is XLSX. Reading is
values-only: formulas use their cached values, with no formula recalculation
and no transfer of source formatting. General cells are written as strings,
including identifiers with leading zeros and strings starting with `=`.
`style.number_formats` explicitly opts a column into numeric Excel cells:
nonblank values must be finite numbers; blanks remain blank. Do not use it on
identifier columns whose leading zeros matter.
Cells longer than Excel's 32,767-character limit are rejected instead of
silently truncated. Calculations use finite 64-bit floating-point numbers;
they do not provide arbitrary-precision decimal arithmetic.

For a template, set `template: report-template.xlsx` beside `file`. Untouched
sheets retain their formulas and styles. An output sheet that matches an
existing template sheet is regenerated completely, including its data and
formatting; formulas and custom layout on that replaced sheet are not retained.
There is no formula translation, expression evaluation, or implicit enrichment.

## Advanced example and migration

`advanced.yaml` is a recipe for user-supplied workbooks placed beside the YAML:

| File / sheet | Required headers |
|---|---|
| `current.xlsx` / `Parts` | `Part Number`, `Description`, `Quantity`, `Category` |
| `previous.xlsx` / `Parts` | `Part Number`, `Description`, `Quantity` |
| `catalog.xlsx` / `Catalog` | `Part Number`, `Unit Cost` |

All headers are on the first row. Quantities and costs must contain finite
numbers. Baseline part numbers must be unique; current duplicates are grouped.
The catalog keeps its first row for duplicate part numbers. The example joins
only catalog matches before calculating cost and writes separate Changes and
Purchasing sheets. Run it with `-check` after supplying the files, then remove
`-check` to publish `generated/purchasing.xlsx`.

To move a customization from Go into YAML, identify the generic projection,
filtering, grouping, or reporting steps and express each as an ordered recipe
step. Keep using the legacy BOM pipeline for find-number grouping, quantity
repair, vendor/SAP decisions, and other domain rules: generic `group` only
performs the requested key grouping and aggregate functions. No existing YAML
configuration requires migration; workflow YAML is a separate opt-in interface.
