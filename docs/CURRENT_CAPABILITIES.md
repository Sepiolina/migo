# Current capabilities

This describes the implementation including the Linux package builder milestone
as of **29 October 2026**. The [implementation plan](MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md)
describes the broader roadmap; features in that plan are not all available yet.

## What you can do today

Use **Mai Studio** to open a local workspace, create or edit a workflow with visual blocks,
configure workbook inputs and outputs, validate it, run it against Excel fixtures,
inspect the generated workbook, and build a Linux deployment ZIP. Use
**MaiService** to serve workflows over HTTP, including configured SQL Server and REST/SAP enrichment. Use **migo** to
execute a workflow from the command line without starting a server.

| Component | Purpose | Current execution mode |
| --- | --- | --- |
| Mai Studio | Desktop block and graph workflow editing with Excel inspection | Local, offline fixture runs; external SQL/HTTP calls blocked |
| MaiService | Go HTTP microservice for processing uploaded workbooks | Local or server deployment; external integrations depend on configuration and connectivity |
| migo | Go command-line workflow runner | Local execution, optional runtime configuration and SQL stub |

## Mai Studio desktop app

### Workspace and workflow files

- Choose a local project folder; Studio lists the workflows in `workflows/` with
  the route each is served at.
- Start from templates: Clean a sheet, Summarize by group, Look up and enrich
  (Excel + CSV), Customer order totals, Excel to CSV, Blank.
- Save with file revision checks; a conflict keeps your edits.
- Undo and redo every edit, with the selection restored.
- Unsaved-work prompts when switching workflows or closing the window.

There is no autosave or collaboration. Sample-file bindings are remembered per
workflow on the computer, not in the YAML.

### Editing

- A steps navigator lists sources, every step (nested `if`/`split` branches
  indented, YAML comments as section notes) and outputs, with each step's effect.
- The table shows the data after the selected step, computed by the Go engine on
  the sample files, with a profile on every column.
- Column actions (sort, filter, dedupe, fill, convert, trim, group, rename,
  remove) add steps.
- The inspector edits every engine step, nested pick rules, output columns and
  styles, and the service settings; formulas are text with column chips.
- The command menu (⌘K) adds steps and reaches every command.
- The console shows problems from the parser and the preview, the run log and
  the editable YAML.
- Light and dark appearance; a compact layout and a phone layout.

Every step of `workflows/bom.yaml` is editable. Edits change only the lines they
touch and keep comments and CRLF line endings.

### Validation and execution

- Validation with the production parser after each edit.
- A step-by-step preview (offline trace) after each edit, with rows for the
  selected step and profiles for all.
- **Run** writes `.studio/runs/<run>/output.xlsx` or `output.csv` and shows it.
- Runs are offline: SQL and REST steps are skipped.

Preview limits: 25 MiB per file, 64 MiB expanded for XLSX, 200 rows per table
(profiles cover every row). There is no cancel button; native requests have a
bounded timeout.

### Endpoint and distribution

The Endpoint page sets the route, description, requester-id field, upload limit
and output format, and documents the endpoints with a `curl` example. **Build
Linux package…** builds the signed Linux service package.

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

The storage registries belong to one process. The
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
