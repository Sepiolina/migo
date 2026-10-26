# Mai Studio development

Mai Studio is being implemented in stages. This first slice connects a Tauri v2
React/React Flow desktop workspace to the production Go workflow engine. The full
roadmap is in [the implementation plan](MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md).

## Current scope

The initial runner validates YAML, inspects Excel input workbooks, and runs
workflows offline to generate inspectable XLSX artifacts. The desktop shell
provides workspace selection, source-backed graph visualization, input bindings,
and result inspection. Advanced visual authoring, intermediate traces, live
connections, cancellation controls, and the secure Linux release wizard remain
subsequent milestones. The production service and its HTTP contract are unchanged.

Run data is written inside the selected workspace under `.studio/runs/`. Never
commit real workbook inputs, connection credentials, or generated runs. Workspaces
and input bindings should point to synthetic fixtures while developing the app.

## Prerequisites

- Go 1.22 or newer; a C compiler for race tests.
- Node.js 22 LTS or newer and npm; use the committed package lock.
- Rust stable and the host-specific Tauri v2 prerequisites. Linux requires
  WebKitGTK 4.1 development libraries, GTK 3, librsvg, a C toolchain, and a desktop
  session. Follow the official Tauri v2 prerequisites for macOS and Windows.

The browser development server supports frontend work, but native workspace and
runner operations require the Tauri application. A browser-only build must never
pretend to have successfully executed a workflow.

## Checks

From the repository root, `make check` runs Go formatting, vet, compilation, race
tests, and the existing example workflows. From `apps/studio`, run `npm ci`,
`npm run build`, and `npm test` for frontend checks. Native Rust compilation also
requires the system libraries above; a successful web build does not validate the
native desktop host.

Use `examples/parts-summary.yaml` with `examples/parts.xlsx` for the smallest
end-to-end fixture. The BOM fixtures under `testdata/` provide broader engine
coverage without access to company services.

## Design boundaries

React renders workflow documents and displays native results. Tauri authorizes
workspace access and supervises the sidecar. Go owns validation, workbook reading,
and execution. Do not reimplement expression evaluation in TypeScript or Rust.
Protocol responses use version 1 and preserve request IDs; unsupported commands,
paths, and versions must fail explicitly. Offline means all external SQL/HTTP
operations are unavailable, including unconditional steps.

## Start the desktop app

```sh
cd apps/studio
npm ci
npm run desktop:dev
```

`desktop:dev` builds the Go runner for the Rust host target before starting Tauri.
If Go is not on PATH, set `MAI_GO` to the Go executable. `npm run desktop:build`
builds the desktop distribution, not the planned Linux service deployment ZIP.

Choose the repository folder as the workspace. Paste the contents of
`examples/parts-summary.yaml` into the source editor, bind `parts` to
`examples/parts.xlsx`, validate, then run. Inspect the generated artifact under
`.studio/runs/` or select another sheet in its preview. The first preview displays
cell values only; styles, formula metadata, and full Excel visual fidelity remain
roadmap work. Generated workbooks themselves come from the production writer.

## Runner protocol

Build the standalone runner with `go build -o /tmp/mai-runner ./cmd/mai-runner`.
It accepts one JSON request per line and emits one JSON response per line; normal
logs must not be written to stdout. For example:

```json
{"version":1,"id":"inspect-1","command":"inspectWorkbook","params":{"workspace":"/absolute/path/to/migo","path":"examples/parts.xlsx","limit":50}}
```

Successful responses contain `version`, matching `id`, `ok: true`, and `result`.
Failures contain `ok: false` and an `error` with `code` and `message`. `validate`
and `run` take `params.yaml`; `run` also takes named relative paths in
`params.inputs`. `inspectWorkbook` accepts `sheet`, `offset`, and `limit`.
The native bridge replaces the workspace path with the folder approved by the
native picker. The standalone runner is a local developer utility, not a remotely
accessible authorization service.
