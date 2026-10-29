# Mai Studio development

Mai Studio is being implemented in stages. The current milestone connects a Tauri v2
React desktop workspace with Blockly and React Flow to the production Go workflow engine. The full
roadmap is in [the implementation plan](MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md).

## Current scope

The runner validates workflow source, inspects Excel input workbooks, and runs
workflows offline to generate inspectable XLSX artifacts. The default Blocks view
uses Blockly 12.4.1 for a typed, snap-together step chain and visual expressions.
Workbook setup edits names, inputs, and output sheets while preserving advanced
settings. Incomplete or disconnected blocks and pending workbook setup drafts
guard saving, running, switching views, and closing. Unsupported steps and
expressions remain opaque and preserved. Workflow canvas provides the existing
React Flow graph, node library, and guided settings; YAML source supports advanced
and nested authoring. The shell also provides workspace discovery, revision-checked
saves, undo/redo, input bindings, and result inspection.

The native package builder cross-builds Linux service ZIPs, with secure signing
and configuration encryption or explicit trusted-local mode. Intermediate traces,
live desktop connections, and cancellation controls remain subsequent milestones. The production service and its HTTP
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

The visual editor separates block definitions and copy/paste state in
`src/blocklyBlocks.ts`, the restricted expression grammar in
`src/blocklyExpressions.ts`, and YAML round-trip editing in
`src/blocklyWorkflow.ts`. `BlockEditor.tsx` owns the canvas lifecycle;
`workbookSettings.ts` preserves workbook metadata when the setup form changes.

From the repository root, `make check` runs Go formatting, vet, compilation, race
tests, and the existing example workflows. From `apps/studio`, run `npm ci`,
`npm run build`, and `npm test` for frontend checks. Run
`node scripts/smoke-blockly.mjs` from `apps/studio` to generate a visual workflow
and execute it and both starter templates against the Go engine and Excel fixture.
Run `node scripts/smoke-blockly-ui.mjs` to check real block editing, reopening,
undo, incomplete edits, and the compact layout in a system-installed Chrome or
Chromium browser. Set `MAI_CHROME` to its executable if it is not detected.
Run `node scripts/smoke-runner.mjs` from `apps/studio` for the serialized runner
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
workspace access and supervises the sidecar. Go owns validation, expression
semantics, workbook reading, and execution. Blockly and workbook setup translate
edits to workflow source; they do not evaluate expressions or use a Blockly
JavaScript generator. Unsupported content must round-trip intact. The graph
projects ordered source steps and branches; its ports are not editable wires.
`predev` and `prebuild` copy Blockly media locally for offline use.
Protocol responses use version 1 and preserve request IDs; unsupported commands,
paths, and versions must fail explicitly. Offline means all external SQL/HTTP
operations are unavailable, including unconditional steps.

## Start the desktop app

For Windows installation, PowerShell commands, and starting the HTTP microservice,
see the [README getting-started instructions](../README.md).

```sh
cd apps/studio
npm ci
npm run desktop:dev
```

`desktop:dev` builds the Go runner for the Rust host target before starting Tauri.
If Go is not on PATH, set `MAI_GO` to the Go executable. `npm run desktop:build`
builds the desktop distribution. The in-app **Build Linux package** command
builds the separate Linux service deployment ZIP.

Open **Project & inputs** and choose the repository folder as the workspace. The
initial document contains the parts-summary example; you can also paste `examples/parts-summary.yaml` into the
source editor. Validate, bind `parts` to `examples/parts.xlsx`, then run. Open
**Excel data** to inspect the generated artifact, select input/output workbooks,
or page through another sheet. Artifacts are stored under `.studio/runs/`. The
preview displays cell values only; styles, formula metadata, and full Excel visual fidelity remain
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

`desktop:build` packages the Studio application and runner. For Linux service ZIP
creation, security setup, verification before extraction, and installation, follow
the [package builder user guide](MAI_STUDIO_USER_GUIDE.md#build-a-linux-deployment-package).

## Linux package builder implementation

`PackageBuilder.tsx` edits public deployment profiles; the native bridge strictly
whitelists fields, authorizes saved inputs inside the workspace, and reads the
Ed25519 signing identity from the OS credential store. Private signing material
is passed only through the native-to-runner request, not the frontend or saved
profile. Secure mode signs the checksum inventory and encrypts config with the
server's age public recipient. Trusted-local mode is explicitly unsigned and
includes plaintext configuration. OS credential-store availability in an
interactive desktop session requires platform testing; headless tests do not
prove that keys persist or can be unlocked on a user's machine.

`internal/deploy` requires the full trusted source checkout and Go on PATH. It
builds with `GOOS=linux`, selected `GOARCH`, `CGO_ENABLED=0`, `-mod=readonly`, and
`GOTOOLCHAIN=local`. Missing modules download through the fixed Go proxy and
checksum database. It checks ELF architecture and archive contents; a local
health smoke test runs only on a matching Linux host with the recognized server
listener implementation. Otherwise the result explicitly reports runtime untested.
That smoke test disables database/SAP and is not a production configuration test.

Artifacts go to `.studio/packages/package-*/mai.zip`. Do not commit packages or
real configuration. Profiles use workspace-scoped localStorage for public
settings; there is no private-key export/recovery/rotation UI. Native OS storage
is separate from browser preferences.

From the repository root, run `go test ./internal/deploy` for builder tests and
`python3 -m unittest discover -s scripts -p 'test_verify_mai_package.py'` for the
independent verifier tests (Python 3 and OpenSSL 3 required). Native and frontend
checks above also cover the bridge and form. Windows native CI compilation/tests
complement frontend checks; they do not establish installer operation or actual
credential-store behavior in an interactive session.

The independently obtained `scripts/verify-mai-package.py` verifies signature,
archive inventory, paths, and checksums before extraction. Never execute ZIP
scripts to establish initial trust. `--allow-unsigned` is an explicit integrity-only
path for trusted-local packages. Installation stages a new target, decrypts with
server-local `AGE_IDENTITY`, protects config with mode `0600`, and refuses existing
targets. PM2 must run as a dedicated service user. In-place upgrades, automatic
rollback, remote deployment, and signing-key lifecycle UI remain future work.

### Guided workbench document model

`workflowDocument.ts` owns source/CST snapshots, revisions, bounded history and
source transactions. `useDocument` keeps the shared React interface; visual
editors apply the same source transforms, with a grouped transaction producing
one undo entry. Invalid source is retained. This is an in-memory model, not a
crash-recovery store or durable step-identity implementation.

`WorkbenchOutline.tsx` provides a keyboard-accessible top-level step presentation;
`StepInspector` edits the same source. `columnSuggestions.ts` makes conservative
name suggestions from inspected input headers and supported preceding steps.
Unknown transforms invalidate suggestions; Go remains authoritative.

Run `node scripts/smoke-blockly.mjs` with Go on PATH to check all templates through
the real engine and inspect their XLSX values. Regenerate the synthetic Orders
workbook with `python3 scripts/generate-orders-fixture.py` from the repository root.

### Document and preview error contracts

Document commands use the latest committed in-memory snapshot even when React
batches several calls before rendering. A transaction still commits atomically
and throws transform errors synchronously; failed transforms do not change history.

A successful runner result always carries `path` and `sha256`. `preview` is optional;
`previewWarning` explains inspection or embedded-response size limits. Artifact
hashing must succeed before reporting execution success. Treat preview failures
separately in clients, preserving the output path and clearing an earlier preview.
Standalone `inspectWorkbook` requests retain their existing error behavior.
