# BOM pipelines in YAML

The default `POST /api/bom/upload` route compiles the embedded
[`default.yaml`](../internal/modules/bom/default.yaml) at startup. Its ordered
steps call the original Go BOM stage functions on the same `Job`: reading,
find-number grouping and quantity repair, PCB handling, vendor policy,
Material_Master, SAP, priority selection, and workbook writing. This is the
BOM algorithm expressed as stage orchestration in YAML, rather than a generic
table workflow or a single opaque “run BOM” operation.

With all default stages in the order below and identical inputs, configuration,
vendor policy, database results and SAP responses, the result reproduces the
original Go BOM workbook 1:1: cell values and types, styles, formulas, column
widths, row heights and panes. XLSX ZIP bytes need not be identical. The writer
continues to produce the original BOM and debug sheets. Changing or disabling
stages can change that result.

## Run an equivalent recipe locally

The bundled recipe is ready to run from the repository root:

```sh
go run ./cmd/bomrun -workflow examples/workflows/legacy-bom.yaml -check
go run ./cmd/bomrun -workflow examples/workflows/legacy-bom.yaml
```

It writes `examples/workflows/generated/legacy-bom.xlsx` using the bundled
BOM and Priority fixtures, without requiring SQL Server or SAP.

From the repository root, create `pipelines/` and save this complete recipe as
`pipelines/custom.yaml`. The sample paths point to the bundled test inputs;
the output directory must already exist.

```yaml
workflow: Original BOM processing
engine: bom
input:
  bom: ../testdata/bom_basic.xlsx
  priority: ../testdata/prio_basic.xlsx
process:
  - stage: read-inputs
  - stage: group-rows
  - stage: pcb-scope
  - stage: pcb-reformat
  - stage: cft-001-vendor-policy
  - stage: material-master
  - stage: sap-enrich
  - stage: cft-002-priority
  - stage: write-workbook
output:
  file: ../out.xlsx
```

```sh
mkdir -p pipelines
# Save the YAML above to pipelines/custom.yaml, then:
go run ./cmd/bomrun -workflow pipelines/custom.yaml -cfg '{}' -check
go run ./cmd/bomrun -workflow pipelines/custom.yaml -cfg '{}'
```

CLI paths in `input` and `output` resolve relative to the YAML file, including
when the command runs from another directory. Inputs accept XLS/XLSX; output
must have an `.xlsx` extension. The runner protects input files against output
overwrites and publishes the completed workbook by renaming a temporary file.
It does not create output directories.

`-check` executes every enabled step, including configured external lookups,
and renders the workbook to a temporary file without publishing the output.
It is more than a schema check. `-cfg` supplies configuration keys as JSON;
`{}` skips DB/SAP enrichment. `-stubdb file.json` supplies the same
Material_Master lookup fixture as the existing BOM CLI. For example:

```sh
go run ./cmd/bomrun -workflow pipelines/custom.yaml \
  -cfg '{"db_enabled":true,"sap_enabled":false}' -stubdb material-master.json
```

A stub maps normalized MPN keys to records such as
`{"ABC123":{"MaterialNo":"100001","ZBlock":""}}`.
As with the original CLI, SQL lookups use `-stubdb`; the HTTP service injects
the actual SQL Server client. Configured SAP requests can run from either.
BOM recipes use their own input/output paths, so do not combine `-workflow`
with `-bom`, `-prio` or `-out`. Generic table workflows retain their existing
flag restrictions and do not accept BOM `-cfg`/`-stubdb` options.

## Use a recipe through HTTP

Place the recipe beside your deployed configuration, for example:

```text
/opt/miservice/
  miservice
  config.json
  pipelines/
    custom.yaml
```

Add these keys to your existing `config.json` (retain your other settings):

```json
{
  "bom_workflow": "pipelines/custom.yaml",
  "bom_pipelines": {
    "custom": "pipelines/custom.yaml"
  }
}
```

`bom_workflow` replaces the recipe used by `/api/bom/upload` and its existing
`/api/bom/download` route. Omit it to use the embedded default. `bom_pipelines`
adds named upload/download pairs; omit `bom_workflow` if you only want an
additional pipeline. Recipe paths resolve relative to the config directory.
Pipeline IDs match `^[a-z][a-z0-9-]{0,39}$`: a lowercase initial letter, then
lowercase letters, digits or hyphens, at most 40 characters.

Restart the service after editing config or recipe files; recipes are loaded
and validated at startup, with invalid recipes preventing startup. For PM2:

```sh
pm2 restart miservice
curl -F 'files=@testdata/bom_basic.xlsx' \
  -F 'files=@testdata/prio_basic.xlsx' -F 'bam=113280' \
  http://localhost:3081/api/pipelines/custom/upload
curl -o custom.xlsx http://localhost:3081/api/pipelines/custom/download
```

The upload contract remains two multipart `files` fields and the original
`bam` employee-number validation. A filename containing `prio` or `shortage`
identifies the Priority file; otherwise the first file is treated as the BOM.
HTTP supplies uploaded input paths and managed output paths: YAML input/output
paths are ignored for execution, but must still satisfy schema validation.
There is no need to copy the CLI sample input files to the server.

A custom upload returns its own `/api/pipelines/custom/download` URL. Each
pipeline tracks its latest successful output in memory, independently of the
original endpoint; restart clears that pointer. Custom output filenames start
with `pipeline_custom_`, and retention ownership is separate for each pipeline
and employee. Each named pipeline has its own `outputs/pipelines/<name>/`
directory, manifest and cleanup scheduler, so even identically named uploads
cannot overwrite another pipeline's workbook. The original endpoint keeps its
existing filename format and output directory.

## Edit orchestration or add a new primitive

Each `process` entry contains `stage` and optionally `enabled: false`.
Omitting `enabled` enables the stage. `RecipeStageRegistry` in
[`recipe.go`](../internal/modules/bom/recipe.go) registers the original
`DefaultStages` from [`stages.go`](../internal/modules/bom/stages.go), so recipes
execute the same Go functions as the original pipeline. Do not mix generic
operations such as `filter`, `join` or `group` into a BOM recipe.

Validation rejects unknown fields, unknown or duplicate stages (even when
disabled), duplicate keys, YAML aliases/merge keys and multiple documents.
`workflow` must be nonblank, `engine` must be `bom`, `input` must contain exactly
nonblank `bom` and `priority` paths, and `output.file` must be an XLSX path.
Enabled `read-inputs` must be the first entry and enabled `write-workbook` the
last. Enabled stages must satisfy these dependencies earlier in the list:

| Stage | Required enabled predecessors |
|---|---|
| `pcb-scope` | `group-rows` |
| `pcb-reformat` | `pcb-scope` |
| `cft-001-vendor-policy` | `pcb-reformat` |
| `material-master` | `cft-001-vendor-policy` |
| `sap-enrich` | `material-master` |
| `cft-002-priority` | `group-rows`, `pcb-scope`, `cft-001-vendor-policy`; also `material-master` and `sap-enrich` when those are enabled |

Stages other than read/write may be omitted or disabled when no enabled stage
depends on them. For example, disabling both `material-master` and `sap-enrich`
is valid while retaining priority selection; disabling only `material-master`
while leaving SAP enabled is invalid. The validator constrains dependencies;
it does not establish workbook equivalence for a changed recipe.

Existing stage selection and permitted ordering changes require only YAML and
a restart. A genuinely new primitive requires Go: implement a function working
on `Job`/rows, register a named `Stage` in `RecipeStageRegistry`, and update
`Recipe.Validate` dependencies in [`recipe.go`](../internal/modules/bom/recipe.go)
to encode its prerequisites and any effects on later stages. Leave the original
`DefaultStages` unchanged when adding a custom-only primitive. Add it to
`default.yaml` only if it belongs in the default behavior. Update row/layout/
writer code if the primitive adds workbook data. Test the primitive and recipe
validation, then run the BOM golden regression and `go test ./...`; use the
original default recipe as the equivalence baseline. See
[EXTENDING.md](EXTENDING.md) for implementation conventions.
