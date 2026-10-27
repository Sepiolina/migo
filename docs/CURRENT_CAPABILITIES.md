# Current capabilities

This describes the implementation including the Linux package builder milestone
as of **27 October 2026**. The [implementation plan](MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md)
describes the broader roadmap; features in that plan are not all available yet.

## What you can do today

Use **Mai Studio** to open a local workspace, create or edit a YAML workflow,
configure common steps through forms, validate it, run it against Excel fixtures,
inspect the generated workbook, and build a Linux deployment ZIP. Use
**MaiService** to serve workflows over HTTP, including configured SQL Server and REST/SAP enrichment. Use **migo** to
execute a workflow from the command line without starting a server.

| Component | Purpose | Current execution mode |
| --- | --- | --- |
| Mai Studio | Desktop workflow development with React Flow and Excel inspection | Local, offline fixture runs; external SQL/HTTP calls blocked |
| MaiService | Go HTTP microservice for processing uploaded workbooks | Local or server deployment; external integrations depend on configuration and connectivity |
| migo | Go command-line workflow runner | Local execution, optional runtime configuration and SQL stub |

## Mai Studio desktop app

### Workspace and workflow files

- Choose a local project folder through the native desktop picker.
- Discover YAML workflows under `workflows/`, or open another workspace-relative
  YAML path, such as `examples/parts-summary.yaml`.
- Create a workflow by editing the example and saving to an unused `.yaml` or
  `.yml` path. The parent directory must already exist.
- Save changes with file revision checks. If another program changed the file,
  Studio rejects the overwrite and keeps the editor contents for recovery or Save As.
- Undo and redo source/step edits within the current session.
- Receive unsaved-work prompts when switching documents, changing workspaces, or
  closing the native window. Unapplied settings block conflicting actions.

Workflow saves use temporary files and atomic replacement. File access is scoped
to the selected workspace, with path and symlink checks. These protections do not
replace operating-system permissions or backups. There is no autosave, crash
recovery store, collaboration, or persisted input-binding profile yet.

### Visual editor

The canvas displays ordered execution from left to right using larger,
operation-specific cards with titles, icons, colours, and readable summaries.
Branches have separate paths and connect back to the next ordered step.

- Start at readable 100% scale; use **Overview** to see the full graph or
  **Focus selected** to inspect a particular node.
- Search the **Add node** library, append a supported operation, select it, and
  configure its settings.
- Move top-level steps earlier or later, or delete them. Moving a compound step
  also moves its contained branches.
- Show **Project & inputs** only when needed; it does not occupy the canvas by default.
- Use **YAML source** for complete workflow definitions and complex operations.

The graph is an editor for the existing ordered YAML engine. Execution order is
not determined by dragging cards or drawing arbitrary wires. Nested steps are
visible, but nested authoring still uses source editing.

### Guided node settings

| Node | Available controls |
| --- | --- |
| Read data | Input/saved-table reference; structured read settings where supported, including explicit sheet name or index |
| Filter rows | Column, comparison, text/number type, and value; expression mode for complex conditions |
| Calculate columns | Column/expression rows |
| Select columns | Output-name/source-column rows |
| Rename columns | Current/new-name rows or a supported name expression |
| Sort rows | Columns, descending order, numeric comparison |
| Group rows | Grouping columns and supported aggregation column lists |
| Join tables | Saved table, matching columns, join mode, normalization, and columns to add |
| Save table | Saved dataset name |

**Apply changes** records one undoable edit; **Discard changes** abandons the
pending settings. Guided edits preserve settings outside the exposed controls.
Shapes that cannot be represented safely—such as certain ordered numeric-looking
mapping keys or comma-containing column names—use **Advanced YAML** instead.

Other engine operations, including branching, SQL, REST, deduplication, and window
calculations, can appear as named cards when loaded from YAML. They are not all
available as guided forms or library entries. Calculated expressions still require
expression syntax; this is not a complete Scratch-style block language.

### Validation and execution

- Validate with the production Go parser/compiler, including workflow structure,
  expression compilation, and declared references.
- Bind named workflow inputs to Excel files inside the workspace.
- Execute using the same workflow engine as the service.
- Run offline: both unconditional and conditional external SQL/HTTP operations
  remain unavailable in the desktop runner. Turning a workflow flag on does not
  enable live desktop connections.
- Generate `.xlsx` output under `.studio/runs/<run-id>/output.xlsx` and show its
  path and SHA-256 hash.
- Mark output stale when the workflow source, workspace, or bindings change.

Validation cannot prove that every expression will succeed for every input row.
Execution is needed to catch data-dependent problems. Changes to workbook file
contents outside Studio are not currently monitored for stale-result detection.
There is no Cancel button or per-step execution trace yet; native requests have
a bounded timeout and supervised child processes.

### Excel inspection

The **Excel data** panel is closed by default. Open it to inspect data, expand it
for a larger view, dock it again, or close it without changing the workflow.

- Shortcuts to bound inputs and the generated output workbook.
- Workbook path entry, sheet selection, and previous/next paging.
- Read-only cell values, row numbers, and a cell inspector for complete text.
- Preview of the actual generated output artifact, rather than a second workbook
  independently rendered by the frontend.
- Input support for `.xlsx` and Excel 97–2003 BIFF8 `.xls`; output is `.xlsx`.

The current preview is **value-oriented**, not pixel-identical to Excel. It does
not reproduce full workbook styling, charts, merged-cell layouts, macros, or
formula recalculation. Workbook editing and intermediate-step tables are not
available. Inspect the generated file in Excel when final formatting matters.

Desktop workbook inspection limits files to 25 MiB, including generated output
previews. It checks XLSX ZIP expansion against a 64 MiB limit and bounds protocol
frames and preview pages.
The UI requests 100 rows per page. Paging limits the response size; it is not a
guarantee that workbook parsing itself uses constant memory.

## Workflow engine and migo CLI

The Go engine supports more operations than the desktop form library currently
exposes:

- Reading sheets by name/index, header detection, fixed-column mapping, trimming,
  blank-row skipping, and row-number columns.
- Filtering, calculated columns, selecting/renaming, filling, deduplication,
  sorting, grouping, window calculations, and ordered row-picking rules.
- Joining, appending, stacking, and comparing saved tables.
- Conditional branches, row splitting, logging, and explicit workflow failures.
- Parameterized SQL queries and REST requests with batching and configured retries.
- Multiple output sheets, column definitions, computed output cells, and
  supported cell styles, including row-dependent styles resolved while writing.
  These are not a general Excel formula engine.

Run any workflow through `migo`, either writing a workbook or using `-check` to
execute without keeping output. The CLI also accepts runtime JSON configuration
and a SQL stub for fixture-based testing. See the [workflow reference](WORKFLOWS.md)
for exact syntax and examples.

## MaiService microservice

- Build the server with `go build -o maiservice ./cmd/maiservice` (use
  `maiservice.exe` on Windows).
- Serve each workflow through `POST /api/<name>/upload` and
  `GET /api/<name>/download`, plus `GET /api/health` for liveness.
- Load built-in embedded workflows and config-directory workflow overrides.
- Configure SQL Server and REST/SAP enrichment for workflows that use them.
- Apply upload limits, temporary-upload cleanup, output retention, and a shared
  concurrency gate for expensive jobs.
- Shut down gracefully on supported termination signals.
- Run the Linux binary under PM2 using the configuration example in the README.

The latest-download state and storage registries belong to one process. The
current PM2 instructions therefore use a single instance. Health reports liveness,
not full dependency readiness. Multi-user/job output authorization, service-level
access controls, and high-availability deployment remain further work; deployment
should use the appropriate trusted network/gateway controls.

## Packaging and platform status

**Build Linux package** creates `.studio/packages/package-*/mai.zip` from saved
configuration, selected workflow overrides, and a complete trusted MaiService
source checkout. Go is required on the build computer; missing dependencies are
downloaded automatically through the Go module proxy and checksum database.
Choose amd64/arm64, a relative `config.json`, new `/opt` target, service name,
port, and memory settings. Selected overrides add or replace workflows; embedded
built-in routes remain active.

- **Secure** signs the inventory with Ed25519 and encrypts configuration to the
  Linux machine's age public recipient. The native host keeps the signing identity
  in the OS credential store; the UI exposes only the public key and fingerprint.
- **Trusted local** is explicitly unsigned and includes plaintext configuration.
- Profiles save public settings in workspace-scoped localStorage. There is no
  signing-key export, recovery, backup, or rotation UI.
- Results show file inventory, hash, architecture, mode, and actual validation
  status. ELF checks do not imply runtime testing; a local health check is
  conditional on a matching Linux host and recognized server implementation.
- The independent `scripts/verify-mai-package.py` requires Python 3 and OpenSSL.
  It checks the signature against an independently trusted public key and verifies
  the complete payload inventory before extraction. Do not execute ZIP scripts
  before this check. Unsigned verification requires explicit `--allow-unsigned`
  and establishes integrity only.
- The installer requires a new target, stages installation, decrypts with a
  server-local age identity, and protects plaintext config with mode `0600`.
  After privileged installation, assign ownership to the dedicated service user
  and run PM2 as that user. In-place upgrades and remote deployment are absent.

See the [package builder guide](MAI_STUDIO_USER_GUIDE.md#build-a-linux-deployment-package)
for the one-time prerequisites and full verification/install commands.

| Area | Current status |
| --- | --- |
| Windows desktop development | Source-based Tauri commands; frontend and watcher CI, plus native compilation/tests configured for this milestone |
| Windows desktop installer | Local Tauri build command documented; installer operation and signed distribution require separate validation |
| Linux desktop native host | Native compilation/tests configured with GTK/WebKit prerequisites |
| OS signing credential store | Native backends configured; headless CI does not validate interactive key storage/unlocking |
| macOS desktop release | No validated release/distribution workflow documented yet |
| Windows/Linux service binaries | Go builds; Linux amd64/arm64 package cross-builds supported |
| PM2 deployment | Generated single-instance ecosystem config and new-target installation; manual server provisioning/startup |
| One-click Linux `mai.zip` build | Implemented; secure and trusted-local modes |
| Signing and config encryption | Native OS-store Ed25519 signing and age recipient encryption; key lifecycle UI remains future work |

Frontend, Go builder, native bridge, and independent-verifier checks cover
different layers. A successful build or headless test does not prove installer
operation, OS credential-store access, target-architecture runtime behavior, or
live company integrations. Test those on the intended desktop and Linux server.

## Where to go next

- [README: installation, Windows desktop, microservice, and PM2](../README.md)
- [Studio user guide](MAI_STUDIO_USER_GUIDE.md)
- [Studio developer guide](MAI_STUDIO_DEVELOPMENT.md)
- [Workflow syntax and supported operations](WORKFLOWS.md)
- [Planned implementation](MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md)
