# Mai Studio development

Mai Studio is a Tauri v2 and React desktop app around the production Go
workflow engine. It edits workflow YAML through generated forms and direct
actions on a data table, previews every step on sample files, and builds Linux
service packages. The earlier [implementation plan](MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md)
describes the Blockly-based design this replaced.

## Current scope

- Home: open a folder, list its `workflows/` with their routes, start from templates.
- Editor: steps navigator, the table after the selected step with column
  profiles and column actions, a registry-driven inspector, the endpoint
  (service) settings with the API contract, a console with problems, run log and
  the YAML, a command menu, undo with selection restore.
- Runner: `validate`, `trace` (each top-level step's column profile, log and
  error, with rows for one step; steps can be skipped), `run` (writes
  `.studio/runs/run-*/output.xlsx|csv`), `inspectWorkbook`, `buildPackage`.
  Runs are offline: SQL and REST are unavailable.

Run data is written inside the selected workspace under `.studio/runs/`. Never
commit real workbook inputs, connection credentials, or generated runs.

## Prerequisites

- Go 1.22 or newer; a C compiler for race tests.
- Node.js 22 LTS or newer and npm; use the committed package lock.
- Rust stable and the host-specific Tauri v2 prerequisites. Linux requires
  WebKitGTK 4.1 development libraries, GTK 3, librsvg, a C toolchain, and a desktop
  session. Follow the official Tauri v2 prerequisites for macOS and Windows.

The browser development server supports frontend work, but native workspace and
runner operations require the Tauri application. A browser-only build must never
pretend to have successfully executed a workflow.

## Editor architecture

| File | Role |
| --- | --- |
| `stepRegistry.ts` | One `StepSpec` per engine step and one `ItemSpec` per nested item (set entries, header titles, pick rules, output sheets, columns, styles, the service block). Forms, the add-step menu and summaries come from it. |
| `pipeline.ts` | Reads a workflow into sources, a step tree and outputs; `readForm`/`writeField` edit any field through a spec; inserts, moves, duplicates and deletes steps; keeps short forms (`save: x`, `from: a[Sheet]`) and CRLF. |
| `yamlSplice.ts` | After an edit, reuses the original text of every unchanged mapping, so a one-field change is a one-line diff. |
| `App.tsx` | State: document and undo (with selection restore), selection, sample-file bindings (localStorage per workspace and workflow), live `trace`/`validate`/`inspectWorkbook` requests, run, save, shortcuts, commands. |
| `Navigator.tsx`, `DataGrid.tsx`, `Inspector.tsx`, `StepForm.tsx`, `Console.tsx`, `CommandMenu.tsx`, `Home.tsx` | The panes. `DataGrid` turns column-menu choices into steps; `StepForm` renders any registry form, including nested item lists. |
| `api.ts` | The native bridge. `window.__maiBridge` can stand in for the Tauri host (see `scripts/studio-bridge.mjs`). |
| `studio.css` | Design tokens (light and dark) and the layout, including the compact and phone layouts. |

Go owns validation, expression semantics, workbook reading and execution; the
frontend never evaluates expressions. A step without a registry spec still shows
its YAML and is saved unchanged.

### Add a step to Studio

1. Add a `StepSpec` to `steps` in `src/stepRegistry.ts`:

   ```ts
   {
     op: "upper", type: "mai_upper", title: "Upper-case columns",
     heading: "Upper-case", category: "Columns", icon: "A",
     description: "Upper-case text in columns.", help: "Column titles separated by commas.",
     form: "map", list: "", fields: [list("", "", { name: "COLUMNS" })],
     example: "[Column]",
   },
   ```
   `form` says where fields live (`map`, `entries` for a whole-map argument,
   `inline` for if/then/else); `scalar`/`list` name the field a short form fills.
   Field kinds: `text`, `template`, `number`, `bool`, `enum`, `list`, `expr`,
   `entries`, `items`, `steps`, `yaml`. Mark rarely used ones `advanced`; the
   inspector hides them until used or **Show all**.
3. Optionally give it a sentence in `summarize` (`pipeline.ts`) and a column
   action in `DataGrid.tsx`.
2. For nested structures add an `ItemSpec` and point an `entries` or `items`
   field at it.
4. `npm test` checks every registry example round-trips through its form
   (`pipeline.test.ts`).

## Checks

From the repository root, `make check` runs Go formatting, vet, compilation, race
tests, and the example workflows. From `apps/studio`:

- `npm test`: the pipeline model (BOM, short forms, nested items, CRLF, every
  registry example), the app (template editing, command menu, endpoint, preview
  and column actions with a mocked runner), document history, package builder.
- `npm run build`, then `node scripts/smoke-studio.mjs`: drives the built app in
  a system Chrome or Chromium against the real Go runner, through the bridge in
  `scripts/studio-bridge.mjs` on a temporary copy of the examples. It previews
  the BOM step by step, adds and undoes a column action, checks a one-line YAML
  edit, the endpoint page, runs and saves a template, and the phone layout. Set
  `MAI_CHROME` if Chrome is not found.
- `node scripts/smoke-runner.mjs`: the serialized runner contract.
- `cargo test --locked --manifest-path src-tauri/workspace-core/Cargo.toml`, and
  with native prerequisites `cargo test --locked` and `cargo fmt --check` in
  `src-tauri`.

The BOM fixtures under `testdata/` and the files in `examples/` (including the
CSV `regions.csv`) give engine coverage without company services.

## Design boundaries

React renders the document and native results. Tauri authorizes workspace access
and supervises the sidecar. Go owns validation, expression semantics, workbook
reading and execution. Unsupported content must round-trip intact. Protocol
responses use version 1 and preserve request IDs; unsupported commands, paths and
versions fail explicitly. Offline means all external SQL/HTTP operations are
unavailable.

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

Choose **Open folder…** and the repository folder, then a workflow or a template.
Templates bind their sources to files in `examples/`. Artifacts are stored under
`.studio/runs/`. Previews show cell values; styles and formulas are in the
generated file itself.

## Runner protocol

Build the standalone runner with `go build -o /tmp/mai-runner ./cmd/mai-runner`.
It accepts one JSON request per line and emits one JSON response per line; normal
logs must not be written to stdout. For example:

```json
{"version":1,"id":"inspect-1","command":"inspectWorkbook","params":{"workspace":"/absolute/path/to/migo","path":"examples/parts.xlsx","limit":50}}
```

Successful responses contain `version`, matching `id`, `ok: true`, and `result`.
Failures contain `ok: false` and an `error` with `code` and `message`. `validate`,
`trace` and `run` take `params.yaml`; `trace` and `run` also take named relative
paths in `params.inputs`, and `trace` takes `step` (the step whose rows to
return), `skip` and `limit`. `inspectWorkbook` accepts `sheet`, `offset`, and `limit`.
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

### Document model

`workflowDocument.ts` owns source/CST snapshots, revisions, bounded history and
source transactions; `useDocument` is its React interface. Every edit is one
transaction and one undo entry; invalid source is retained. This is an in-memory
model, not a crash-recovery store.

### Document and preview error contracts

Document commands use the latest committed in-memory snapshot even when React
batches several calls before rendering. A transaction still commits atomically
and throws transform errors synchronously; failed transforms do not change history.

A successful runner result always carries `path` and `sha256`. `preview` is optional;
`previewWarning` explains inspection or embedded-response size limits. Artifact
hashing must succeed before reporting execution success. Treat preview failures
separately in clients, preserving the output path and clearing an earlier preview.
Standalone `inspectWorkbook` requests retain their existing error behavior.
