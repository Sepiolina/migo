# Mai Studio development

Mai Studio is being implemented in stages. The current milestone connects a Tauri v2
React/React Flow desktop workspace to the production Go workflow engine. The full
roadmap is in [the implementation plan](MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md).

## Current scope

The runner validates YAML, inspects Excel input workbooks, and runs workflows
offline to generate inspectable XLSX artifacts. The desktop shell provides
workspace workflow discovery, revision-checked saves, source-backed graph
visualization, top-level step editing and ordering, undo/redo, input bindings, and
result inspection. Nested authoring and unsupported step forms use the YAML
source editor.
Intermediate traces, live connections, cancellation controls, and the secure Linux
release wizard remain subsequent milestones. The production service and its HTTP
contract are unchanged. See the [user guide](MAI_STUDIO_USER_GUIDE.md) for the
editing and execution flow.

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
`npm run build`, and `npm test` for frontend checks. Run
`node scripts/smoke-runner.mjs` from `apps/studio` for the serialized runner
integration check: it builds Go, validates a synthetic fixture, runs it in a
temporary workspace, verifies the artifact hash, and compares workbook previews.
Run `cargo test --locked --manifest-path src-tauri/workspace-core/Cargo.toml`
from `apps/studio` for filesystem discovery/save/conflict checks without GTK.
With native prerequisites installed, run `cargo test --locked` and
`cargo fmt --check` from `apps/studio/src-tauri`. Native Rust compilation also
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

Choose the repository folder as the workspace. The initial document contains the
parts-summary example; you can also paste `examples/parts-summary.yaml` into the
source editor. Validate, bind `parts` to `examples/parts.xlsx`, then run. Inspect
the generated artifact under
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

## Workspace files and trust boundary

The native picker establishes the workspace root; frontend-supplied workspace
paths cannot expand it. Workflow discovery recurses through `workflows/` and
skips symlinks. Read/save accepts workspace-relative `.yaml`/`.yml` paths,
rejects symlinks and traversal, and limits UTF-8 documents to 1 MiB. Save parents
must already exist. Saves use temporary files in the destination directory and
revision checks to detect external changes; creating a document never silently
overwrites an existing file. This is local desktop access, not a multi-user
filesystem authorization service.

Workbook inputs and inspection targets must resolve inside the approved
workspace. Runs write artifacts under `.studio/runs/`; use fixtures and exclude
those outputs from commits. Source saves and offline execution are independent:
a run executes editor YAML, including unsaved edits.

## Native troubleshooting

If `desktop:dev` cannot find Go, install it on PATH or set `MAI_GO` to the Go
executable. The sidecar build also needs Rust's host target information, so
install Rust stable and Cargo before starting the desktop app.

On Ubuntu, install the Tauri prerequisites, including `libwebkit2gtk-4.1-dev`,
`libgtk-3-dev`, `librsvg2-dev`, `libayatana-appindicator3-dev`, and `patchelf`, plus
a C/C++ toolchain and `pkg-config`. A missing `gdk-3.0` or WebKit `pkg-config`
package indicates missing native development libraries. Launching the app also
requires a desktop display/session. In a headless environment, use frontend,
workspace-core, and runner checks; those do not establish that the desktop window
launches correctly.

`desktop:build` packages the Studio application and runner. The in-app Linux
service package builder, secure release wizard, and PM2 deployment profiles are
planned and cannot be exercised in this milestone.
