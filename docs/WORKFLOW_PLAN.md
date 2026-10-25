# Declarative workflow implementation plan

## Findings

`cmd/bomrun/main.go` calls `bom.NewPipeline`, whose nine `DefaultStages`
(`internal/modules/bom/stages.go`) read, group, scope, reformat, apply vendor
policy, enrich from SQL/SAP, choose priorities, and write BOM/debug sheets.
There is no existing declarative workflow parser. Shared JSON configuration
controls service and external systems, not spreadsheet operations.
`ReadBom` selects the first sheet and detects known headers; `ReadPriority`
requires Shortage and starts at row 168. `ProcessGrouping` combines domain
forward filling, find-number grouping, and quantity repair. These rules must
remain intact rather than be mistaken for generic aggregation.

Reuse `internal/sheet.Open` and its XLS/XLSX string grids, `jsx.Norm` for
part matching, and Excelize v2.8.1 for writing. Existing readers intentionally
return cached values and declared-range-relative grids, not formulas/styles.
The BOM writer recreates domain formatting. Existing golden fixtures,
unit/HTTP tests, and Makefile vet/test targets establish compatibility.
No AGENTS.md, CI workflow, or contribution template exists. Origin's default
branch is main; the initial local work branch is clean at the same commit.

## Schema and architecture

Add `internal/workflow` with a typed model, strict YAML parser, preset
resolution, table operations, and an ordered Excel runner. Add `-workflow`
to bomrun; keep the old CLI and HTTP pipeline unchanged. YAML v3 is the only
new dependency. Workflow paths resolve relative to the workflow file.

```yaml
workflow: Process BOM
input:
  parts: parts.xlsx
process:
  - from: parts["Parts"]
    as: cleaned
    do:
      - forward_fill:
          columns: [Category, Manufacturer]
      - filter:
          column: Quantity
          greater_than: 0
      - group:
          by: [Part Number]
          aggregate: {Quantity: sum}
output:
  - file: result.xlsx
    sheets: {BOM: cleaned}
```

Processes and steps execute in list order. Named intermediate results are
immutable snapshots; branches implement splitting and multiple sheet/file
outputs implement combining. Dataset references must precede use. Headers
are exact names; `@A` explicitly addresses the first table column without
ambiguity with a header named A. `header_row` defaults to one, relative to
the reader's declared-range origin. Output sheet mappings use sorted names
for deterministic workbook order. `enabled: false` disables a step.

Presets share operation schemas: filters, forward_fills, groups,
aggregations, mappings, styles, sorts, transforms, and pipelines. Imports
resolve relative to the declaring YAML file. Duplicate imported/local names
are rejected; explicit inheritance uses `preset`. Nested maps merge;
sequences and scalars replace; `override` wins over inherited properties.
Resolve missing references and cycles before running. Pipeline presets
expand to the same steps, not another execution engine.

Operations cover ordered projection/copy/rename/move mapping; recursive
all/any filtering; forward fill; grouping/aggregation; stable sorting;
deduplication; arithmetic calculation; text transforms; joins; dataset
merge; keyed BOM comparison; and styling. Mapping defines desired output
columns explicitly. Normalize matching only when requested. Validate
references and operation properties before exporting anything.

## Compatibility and limits

Preserve every existing Stage/Job API, CLI option, service JSON key, HTTP
route, domain rule and golden fixture. YAML is an opt-in generic workflow;
SAP/vendor decisions remain in the existing pipeline. No legacy YAML needs
migration because none exists. Document converting customization in Go to
recipes without claiming generic grouping duplicates BOM-specific rules.

XLS and XLSX inputs are values-only. XLSX output may start from a template
to retain untouched sheets, formulas and formatting. Replaced data regions
do not promise arbitrary formula translation or source format fidelity.
Export is XLSX only. Numeric calculations reject invalid/non-finite data.
No expression evaluator, code execution, or hidden external enrichment.

## Work breakdown and dependencies

1. ASTRA freezes shared typed interfaces, saves this plan, and owns CLI/CI.
2. Sol 6.1 Medium implements parser, resolver and validation in its own
   worktree, with strictness, import, inheritance and override tests.
3. Sol 6.1 Medium implements table operations and unit tests separately.
4. Sol 6.1 Medium implements runner, Excel I/O, template/style support and
   workbook integration tests separately.
5. Sol 6.1 Light writes reference documentation, runnable examples and
   README links against the frozen schema.
6. ASTRA integrates isolated commits; shared model changes require explicit
   coordination. Luna independently reviews edge cases and test quality.
7. ASTRA fixes findings, verifies, commits, pushes, creates the requested
   detailed PR, monitors CI, and repairs implementation failures. Do not merge.

Expected files: `internal/workflow/*.go`, `cmd/bomrun/main.go` and tests,
`docs/WORKFLOWS.md`, `examples/workflows/*.yaml`, README, go.mod/go.sum,
and `.github/workflows/go.yml`. Keep operations, parser, and I/O ownership
separate to minimize conflicts. Main risks are schema drift, table column
ordering, preset aliasing, numeric coercion, duplicate matching keys, source
range offsets, template overwrites, and Go toolchain compatibility.

## Validation and acceptance

Run gofmt, go vet ./..., go build ./..., go test ./..., and race detection.
Check minimum supported Go compatibility as well as the prepared toolchain.
Test parser errors, missing/duplicate references, preset cycles and import
collisions, list replacement, nested overrides and preset immutability.
Exercise every operation including invalid numbers, empty/header-only
tables, stable grouping/sorting, joins and comparison duplicates.
Integration tests must reopen workbooks and assert cells, sheet order,
styles, template formulas, XLS inputs and multiple output files. Ensure
processing errors produce no outputs and cancellation is respected.
Keep all legacy golden, domain, HTTP, storage and client tests passing.
Examples must match the implemented schema. Review final diff for secrets
and generated files. Acceptance requires working documented operations,
determinism, actionable contextual errors, unchanged legacy outputs, a
reviewable PR, and verified CI status (or a concrete external blocker).
