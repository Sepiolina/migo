# Workflows

A workflow is one YAML file. Its name is its endpoint:

| File | Endpoints |
| --- | --- |
| `workflows/bom.yaml` | `POST /api/bom/upload`, `GET /api/bom/download` |
| `workflows/<name>.yaml` | `POST /api/<name>/upload`, `GET /api/<name>/download` |

All workflows use the same request and response. Upload sends multipart `files`
(one per input, in any order) plus `bam` (the employee number). The response is:

```json
{"status":"success","message":"<name> processed successfully.","fileName":"<first input>_<bam>.xlsx","downloadUrl":"http://host/api/<name>/download","bam":"113280"}
```

Download returns the latest result of that workflow only.

Where the files come from:
- The files in the repo folder `workflows/` are built into the binary.
- A file in `<config dir>/workflows/` replaces the built-in one with the same name, or adds a new endpoint.

Names use lowercase letters, digits and `-`. A file with a mistake stops startup with the file, step and reason.

Outputs go to `outputs/<name>/`. Each employee keeps their newest two files, and the 14-day retention applies.

## Shape

```yaml
name: Parts summary            # shown in the response message
input:                         # one entry per uploaded file
  parts: {label: Parts list}   # label: used in "Expected 1 file (Parts list)"
steps:                         # run top to bottom
  - from: parts[Parts]
  - filter: 'num("Quantity") > 0'
  - save: summary
output:
  sheets: {Parts: summary}     # sheet name -> saved dataset, in this order
```

**Inputs.** Files are matched to inputs in upload order. An input with
`match: "<regexp>"` takes the first uploaded file whose name matches, wherever
it sits. For example, `match: "(?i)prio|shortage"` lets the Priority file come
first or second.

**Steps.** Each step is either a bare name (`- group_rows`) or one `name: argument` pair.

**Conditions.** Conditions are expressions inside the step that uses them. Nothing is imported from another file.

**Output.** A workflow either lists `output.sheets` or ends in a step that writes the workbook itself, such as `write_bom`.

Everything is checked when the file loads: input and dataset names, step order and arguments, and expressions. A bad file stops startup; it never fails a request halfway.

## Table steps

These work on the *current table*. `from` loads it and `save` stores a copy under a name.

| Step | Argument | Effect |
| --- | --- | --- |
| `from` | `input`, `input[Sheet]` or `dataset` | load the first sheet, a named sheet, or a saved dataset |
| `save` | `name` | keep a snapshot for `output.sheets`, `join`, `append` or `compare` |
| `filter` | `'<expression>'` | keep the rows where it is true |
| `set` | `{Col: '<expression>', ...}` | compute columns in order; new ones are appended |
| `select` | `{New: Source, ...}` | keep only these columns, renamed and in this order |
| `rename` | `{Old: New, ...}` | rename and keep everything |
| `fill` | `[Col, ...]` | fill blank cells from the row above |
| `dedupe` | `[Col, ...]` | keep the first row per key |
| `group` | `{by: [Key], sum: [..], count: [..], mean/min/max/first/last/join: [..]}` | one row per key; without `by`, one row in total |
| `sort` | `{by: [Col, ...], desc: false, numeric: false}` | stable sort |
| `join` | `{with: dataset, on: [Key], right_on: [Key], how: left\|inner, normalize: false, suffix: _right}` | add the matching columns |
| `append` | `[dataset, ...]` | add rows; columns are matched by name |
| `compare` | `{with: baseline, by: [Key], columns: [..], normalize: false, status: status}` | add `added` / `removed` / `changed` / `unchanged` |
| `style` | `{header: {bold, background: "#RRGGBB", color}, auto_width, freeze_header, number_formats: {Col: "0.00"}}` | formatting used when written |

Cells are text. A column listed in `number_formats` is written as numbers.

## Expressions

Expressions use [expr](https://expr-lang.org/docs/language-definition). Wrap
them in single quotes in YAML. Inside `filter` and `set` you can use:

| | |
| --- | --- |
| `col("Part Number")` | the cell as text |
| `num("Quantity")` | the cell as a number (a blank or non-number cell is an error) |
| `norm(s)` | trimmed upper case |
| `trim`, `upper`, `lower`, `replace(s, old, new)`, `len` | text helpers |
| `a startsWith "X"`, `contains`, `endsWith`, `matches "regexp"` | text tests, each with a `not` form: `a not startsWith "X"` |
| `&&`, `\|\|`, `!`, `==`, `<`, `+`, `*`, `x ? a : b` | the usual operators |

## BOM steps

`workflows/bom.yaml` is the BOM processor. Each BOM step appears at most once. Its output is cell-for-cell
identical to the original service; `TestOutputMatchesGolden` checks this.

| Step | Does | Needs earlier |
| --- | --- | --- |
| `read_bom: bom` | read the customer BOM (first sheet, header detection) | |
| `read_priority: priority` | read the Priority `Shortage` sheet | `read_bom` |
| `group_rows` | forward fill, find-number groups, quantity repair | `read_bom` |
| `pcb_scope` | rows after the last PCB row are out of scope | `group_rows` |
| `pcb_reformat` | PCB MPN `_REV_` reformat | `pcb_scope` |
| `vendor_policy` | CFT-001 (argument below) | `pcb_reformat` |
| `material_master` | Forth P/N from SQL Server (`db_enabled`) | `vendor_policy` |
| `sap_enrich` | block status, vendor, stock (`sap_enabled`) | `material_master` |
| `select_priority` | CFT-002 MAIN / SUBn | `read_priority`, `pcb_scope`, `vendor_policy` (plus `material_master` and `sap_enrich` when present) |
| `write_bom` | BOM + debug sheets | `read_bom` |

The steps that make business decisions carry their rules as their argument;
`workflows/bom.yaml` has the full set with comments.

### Row fields

Every BOM rule is an expression over one row and can read these fields:

| Field | Meaning |
| --- | --- |
| `brand`, `mpn`, `desc` | manufacturer, MPN (leading punctuation removed), effective description; trimmed, upper case |
| `in_scope`, `level0`, `has_ijk`, `pcb` | above the last PCB row; the assembly row; has manufacturer/MPN data; describes a PCB |
| `forbidden`, `exempt` | CFT-001 verdict; allowed by a `vendor_policy` exception |
| `z1` | blocked (Z1) by Material_Master or SAP |
| `forth_pn` | the Forth P/N (`CREATE P/N` when none exists yet) |
| `priority` | value from the Priority workbook, or `nil` |
| `vendor`, `stock` | SAP vendor name (else vendor code), trimmed; SAP stock |

`norm(s)` gives trimmed upper case.

### `vendor_policy` (CFT-001)

```yaml
- vendor_policy:
    banned: [YAGEO CORPORATION, WALSIN TECHNOLOGY CORPORATION, KDS]
    exceptions:                              # first matching brand wins
      - name: TDK
        brand: 'brand matches "\\bTDK\\b" || brand startsWith "TDK"'
        allow: 'desc not startsWith "CAP" || mpn startsWith "CGA"'
        log: "TDK Exception: {count} TDK CAP (CGA series) row(s) confirmed valid."
```

A row whose brand matches an exception is forbidden unless `allow` is true. Any other row is forbidden only if its brand is in `banned`.

### `material_master`

```yaml
- material_master:
    rows: 'in_scope && !level0 && has_ijk'   # rows that get a Forth P/N
    query: |                                 # returns MaterialNo, ZBlock, Maker (the MPN)
      SELECT MaterialNo, ISNULL(ZBlock, '') AS ZBlock, Maker
      FROM SAP_ForthFacDb.dbo.Material_Master
      WHERE ZBlock <> 'D' AND Project = 'FNS - Finisar'
        AND UPPER(LTRIM(RTRIM(Maker))) IN (@mpns)
    retry: {rows: pcb, suffix: _SANMINA}     # misses retried with the suffix
```

How the lookup works:
- `@mpns` becomes the normalised MPNs, sent as SQL parameters in batches.
- A covered row gets the matching MaterialNo. With no match it gets `CREATE P/N`, or a blank if the row is forbidden.
- A ZBlock of `Z1` blocks the row.
- With `db_enabled` off, or when the lookup fails, every covered row gets `CREATE P/N`. Forbidden and Z1 rows stay blank.

### `sap_enrich`

```yaml
- sap_enrich:
    rows: 'in_scope && !level0 && has_ijk && (!forbidden || exempt) && forth_pn not in ["", "CREATE P/N"]'
    employee_id: "106049"
    fields:                                  # SAP keys, in order of preference
      status: [mmsta, mstae, zblock, status]
      vendor_name: [vname]
      vendor_code: [lifnr]
      stock: [labst, stock]
```

How the fields are read:
- The step asks SAP about the Forth P/Ns of the covered rows.
- `status`, `vendor_name` and `vendor_code` each take the first non-empty key.
- `stock` takes the first key that is present.
- A `Z1` status blocks the row.

Connection settings stay in `config.json`: `sap_enabled`, host, port, tcode, werks, `api_batch_size` and `api_retries`.

### `select_priority` (CFT-002)

```yaml
- select_priority:
    ineligible:                              # first match wins; reason goes to the debug sheet
      - {when: level0, reason: Ineligible (Level 0)}
    duplicate_key: 'norm(forth_pn) == "CREATE P/N" ? "" : norm(forth_pn)'
    main:                                    # first rule that applies picks MAIN
      - rows: priority == 1                  # the candidates this rule looks at
        when: count == 1                     # default: count > 0
        pick: top                            # top (default) or most_stock
        reason: "MAIN (Cond A: Single P1 match)"
```

This runs once per find-number group:
1. Rows matching an `ineligible` rule are left out.
2. Among the eligible rows, a repeated `duplicate_key` keeps only its top-most row; a blank key never repeats.
3. The `main` rules are tried in order. A rule applies when its `rows` match at least one candidate and its `when` holds. `when` can read `count` (rows matched), `stock` (their highest stock) and `tied` (rows sharing that stock).
4. The rule picks the top-most row, or with `most_stock` the top-most of the highest-stock rows.
5. Its `reason` (or `reason_tied` when `tied > 1`) goes to the debug sheet, and can use `{stock}`, `{tied}`, `{count}` and `{vendor}`.
6. The other candidates become SUB1, SUB2… in file order.

## Run locally

```sh
go run ./cmd/migo -in bom=testdata/bom_basic.xlsx -in priority=testdata/prio_basic.xlsx -out out.xlsx workflows/bom.yaml
go run ./cmd/migo -in parts=examples/parts.xlsx -check examples/parts-summary.yaml
```

The flags:
- `-check` runs everything and keeps no file.
- `-cfg '<json>'` takes `config.json` keys. The default `{}` turns off DB and SAP.
- `-stubdb file.json` stands in for Material_Master, for example `{"ABC123":{"MaterialNo":"100001","ZBlock":""}}`.
