# Mai Studio: implementation plan

Status: proposed implementation; this document does not claim these features exist.

## 1. Goal and scope

Build a local desktop developer tool using **Tauri v2, React, TypeScript, and
React Flow (`@xyflow/react`)**. Developers open a workspace, visually author
workflows, validate them, run them against Excel inputs, inspect intermediate
results, and export a Linux deployment as **`mai.zip`**.

Use **Mai Studio** for the desktop tool and **MaiService** for its deployment UI.
Keep the existing Go entry points `cmd/migo` and `cmd/miservice`; initially ship
the service executable as `miservice` to preserve existing operations.

The desktop app runs on Windows, macOS, and Linux. Release Linux service builds
for amd64 and arm64. Verify supported desktop OS versions and native Tauri v2
prerequisites during the first milestone. Linux is the deployment target, not
the only desktop host.

MVP includes local workspaces, full-fidelity loading of existing YAML, visual
editing of common operations, source editing for advanced operations, Excel
inspection, offline fixture runs, and secure deployment packaging. Live SQL/REST
runs require explicit activation of a named connection profile. Remote server
provisioning, uploading releases, collaborative editing, and arbitrary code
plugins are later work.

## 2. Repository facts and constraints

- `internal/workflow` is the production parser, compiler, expression evaluator,
  execution engine, and workbook writer. Reuse it for desktop runs.
- `cmd/migo` runs workflows on local files; `cmd/miservice` serves HTTP endpoints.
- YAML steps execute in order against a current table and named saved datasets.
  `if` and `split` contain nested ordered steps; arbitrary graph scheduling is
  not currently supported.
- Built-in workflows are embedded from `workflows/`; files in
  `<config directory>/workflows/` override or extend them.
- Config lookup supports `-config`, `MISERVICE_CONFIG`, and config files beside
  the process/binary. Upload and output storage lives beside the selected config.
- Existing Go tests, golden workbooks, fake SAP fixtures, and SQL stubs provide
  the baseline for compatibility. `make check` is the Go quality gate.
- Existing HTTP health checks are liveness checks, not proof of dependency health.
- The current latest-output download behavior is process-wide per workflow.
  Multiple PM2 instances or multiple tenants need additional design.
- README deployment examples refer to PM2 files, but those files must be created
  and tested as part of this work; documentation alone is not an implementation.

## 3. Architecture and module boundaries

```text
React workspace UI
  ├── React Flow editor + inspector + YAML editor
  ├── virtualized workbook viewer + result comparison
  └── validation, execution, and deployment panels
          │ typed Tauri commands and events
Tauri v2 / Rust host
  ├── workspace authorization and atomic persistence
  ├── runner supervision, cancellation, and bounded event transport
  ├── local credential-store integration
  └── validated build profiles, staging, profile-driven signing/encryption, ZIP packaging
          │ versioned framed JSON over stdin/stdout
Go desktop runner sidecar
  ├── internal/workflow: parse, validate, execute, trace
  ├── internal/sheet: input workbook inspection
  └── workbook artifact inspection and output metadata

Linux deployment: existing cmd/miservice + configuration + workflows + PM2
```

Suggested layout:

```text
apps/studio/src/
  app/                  application shell and navigation
  features/workspace/   documents, file tree, dirty state, recovery
  features/workflow/    graph adapter, inspector, source editor
  features/workbook/    grid, sheet tabs, cell inspector, comparison
  features/runs/        execution state, diagnostics, history
  features/deploy/      profile editor and packaging progress
  shared/               accessible primitives and generated protocol types
apps/studio/src-tauri/src/
  workspace/ runner/ credentials/ deployment/ commands/
cmd/mai-runner/          versioned desktop protocol entry point
internal/studio/        runner orchestration and workbook inspection
internal/workflow/      production semantics and optional trace hooks
schemas/                protocol and editor-document schemas
deploy/                 reviewed PM2 and installation templates
```

Use one owner for each concern. React components do not launch shell commands,
read arbitrary paths, or implement workflow semantics. Rust does not duplicate
the Go evaluator. Keep deployment settings separate from runtime config and
editor layout. Generate TypeScript protocol types from a versioned schema and
test compatibility with Go/Rust codecs.

The Go sidecar is built for the desktop host using Tauri's target-specific
sidecar naming. It is distinct from the Linux service compiled for deployment.
Normal workflow editing/running uses the bundled sidecar; compiling a service
requires an approved Go toolchain and an identified source checkout.

## 4. Workspace and persistence

A workspace is a user-selected directory with a versioned project manifest:

```text
mai.workspace.json            schema version, workflow paths, input bindings
workflows/*.yaml              canonical executable workflows
.studio/layout/*.json         node positions, stable IDs, viewport, source mapping
.studio/profiles/*.json        non-secret local run/deployment settings
.studio/runs/<run-id>/         manifests, bounded traces, generated workbooks
inputs/                       optional copied fixtures; originals stay untouched
config/                       user-selected configuration directory
```

Input bindings may reference user-approved external files using scoped handles.
Do not silently copy real workbooks into version control. Provide ignore rules
for credentials, run artifacts, real inputs, and recovery files. Save workflow
YAML and metadata atomically; keep recoverable revisions and detect external
file changes before overwriting them. Unsupported future document versions open
read-only with a clear migration message.

Each run records the source revision, exact YAML/config/input hashes, engine
version, execution mode, timestamps, outcome, and output hashes. Sensitive config
values are excluded from manifests; hash a redacted identity or use keyed hashes
where low-entropy secret values would otherwise be guessable. A result becomes
visibly stale whenever its source, bindings, or config changes.

## 5. Visual workflow authoring

### Preserve executable semantics

Use YAML as the canonical executable document and a structured ordered AST as
the editor model. React Flow renders that AST. Do not treat arbitrary edge
connections as a valid program or infer order from screen coordinates.

- Render ordered control edges separately from named dataset dependencies.
- Input and output nodes represent bindings and sheet specifications.
- `if` and `split` are nested subflows with named branches and explicit exits.
- `save`, `join`, `append`, and `compare` expose named dataset ports.
- Disallow cycles, ambiguous execution order, invalid branch exits, and references
  to datasets unavailable on a path. Backend validation remains authoritative.
- Keep stable node IDs in sidecar metadata and map them to YAML source spans.
- Preserve mapping order: column assignment order can affect execution.

Initially provide forms for `from`, `filter`, `set`, `select`, `rename`, `save`,
`join`, `sort`, `group`, `if`, `split`, and output formatting. Existing advanced
steps remain editable through a source-backed node with diagnostics. Never drop
unknown content or silently rewrite unsupported operations. Release requires
loading the current BOM workflow without semantic loss.

Source edits are parsed before synchronizing the graph. Invalid YAML stays in
the editor with diagnostics while the last valid graph is marked stale. Preserve
comments where possible with a concrete syntax tree; warn before any explicit
normalization that removes comments. Test graph → YAML → graph and YAML → graph
→ YAML for semantic equivalence, including nested branches and ordered maps.

### UX requirements

Use a resizable three-pane layout: workspace/navigation on the left, canvas or
workbook in the center, and selected-node properties on the right. A collapsible
bottom panel contains diagnostics, execution history, and sanitized logs.

Primary actions remain visible: **Validate**, **Run**, **Cancel**, and
**Build Linux Package**. Show the selected profile and whether it uses live
services beside Run. Disable actions with a specific reason rather than hiding
them. Include a command palette, search, keyboard node insertion, undo/redo,
copy/paste, auto-layout, fit-to-selection, and drag/drop with a keyboard equivalent.

Validation errors select the node, property, and YAML line. Each fixable error
has an actionable explanation. Separate compile validation from fixture execution
and production connection tests. Loading, empty, running, canceled, failed,
partial, stale, and successful states must each have designed UI behavior.

Use accessible labels, visible keyboard focus, high-contrast themes, screen-reader
status announcements, and icons/text in addition to color. Provide a linear step
list alternative to the canvas. Target WCAG 2.2 AA where applicable to a desktop
webview and test keyboard-only operation on each supported host OS.

## 6. Excel workspace: what you see is what is exported

Use a virtualized grid behind an adapter so its rendering library can be changed.
Evaluate licensing, accessibility, frozen panes, large-sheet performance, and
style support before selecting a grid. Do not assume React Flow provides a
spreadsheet component.

Provide Input, Step Result, and Final Output tabs, sheet selection, row/column
headers, frozen headers, search, filtering, selection/copy, and a cell inspector.
Display row counts and indicate sampling/truncation explicitly. Intermediate
results are datasets; they are not styled Excel sheets until output rendering.

The **final preview must read the actual generated `.xlsx` artifact**. Download
or reveal that exact artifact instead of generating a second workbook in React.
A metadata endpoint returns paged cells and supported styles from the file.
Offer side-by-side input/output and previous-run comparison with added, removed,
and changed rows, using configured keys where available; row position alone is
not a reliable identity after sort/group/join.

Define fidelity explicitly:

- Cell values, types, order, sheet names, and supported number formats must match
  the downloaded artifact. Preserve leading zeros and distinguish blank from zero.
- Preview the writer's fonts, fills, borders, alignment, widths, heights, and
  freeze panes. Verify supported styles against exported workbooks.
- Input inspection shows both workbook display values and values consumed by the
  engine, since current input readers normalize cells to text.
- Formula cells show formula text and cached values separately. The current engine
  is not an Excel recalculation engine; never claim fresh formula results.
- Pixel-identical rendering across Excel, LibreOffice, fonts, and operating systems
  is not guaranteed. Unsupported charts, macros, merged cells, or formats require
  a visible fidelity notice and an Open in Excel action, never silent omission.
- Input editing, if enabled, writes a derived fixture with an explicit Save As;
  inspection never mutates the original. Macro execution is unsupported.

Acceptance uses fixture workbooks with text/numeric IDs, dates, blank cells,
Unicode, multiple sheets, styles, and `.xls` input. Check generated `.xlsx` cells
and supported styles using the Go reader plus representative manual Excel and
LibreOffice checks. The preview must show the same artifact hash as export.

## 7. Runner, validation, and tracing

Add `cmd/mai-runner` with a versioned request/response/event protocol. Commands:
`capabilities`, `validate`, `inspectWorkbook`, `run`, `cancel`, `getRows`, and
`getArtifact`. Requests carry opaque workspace/run/file handles, request IDs,
protocol version, and bounded parameters; the host resolves authorized paths.

Events include run/step started, completed, failed, canceled, diagnostics,
progress, and artifact ready. Use length-bounded framed messages, backpressure,
and stderr for process diagnostics so logs cannot corrupt stdout protocol data.
Reject unsupported versions with an upgrade action.

Extend the Go engine with optional observer hooks and structured diagnostics:
node/source identity, phase, severity, error code, and source span. Preserve the
HTTP API's existing error text. Observe nested steps as well as top-level steps.
Capture immutable, bounded table snapshots before/after a selected step; do not
retain live table pointers or serialize every row at every step.

Validate syntax, expressions, references, branch paths, inputs, output mappings,
and deployment config separately. Row-dependent expression failures still need
execution; static validation must not promise that every input will succeed.

Run modes:

1. Offline: connections disabled or replaced with explicit fixtures; network
   egress must be blocked even if a workflow includes unconditional REST/SQL.
2. Live: selected connection profile, explicit user activation, bounded timeouts
   and retries, visible external destinations. Never auto-run side-effecting steps
   while typing or repeatedly retry them without an idempotency policy.

Cancellation propagates through Go context to file processing and network calls;
add checkpoints to long loops. Rust escalates to terminating the supervised child
if it exceeds a bounded grace period. Clear partial artifacts and distinguish
cancellation from failure. Each run uses an isolated directory. Preview caches are
bounded and invalidated by source/input/config/engine identity; live results are
not reused without an explicit cache policy.

## 8. Build Linux Package menu

The menu is a guided panel with sections for Source, Runtime Config, Deployment,
Security, and Review & Build. Persist non-secret settings as a versioned deployment profile.

| Setting | Behavior |
| --- | --- |
| Source | Select verified repository root and revision; display dirty-file warning and record actual source identity |
| Target | Linux amd64 or arm64; CGO disabled by default |
| Config folder | Read selected `config.json`, selected workflow overrides, and explicitly approved assets |
| Runtime | Port, bind address, DB/SAP enablement, external dependency requirements |
| Deployment | Installation root, service user, PM2 name, Node/PM2 version requirements |
| Availability | Memory ceiling, Go memory limit, restart/backoff policy, shutdown timeout, retention/disk budget |
| Configuration packaging | Import the selected real `config.json`; Secure Release encrypts it automatically for the saved server recipient |
| Security preset | Secure Release (recommended): signed package and encrypted config; Trusted Local: explicit signed or plaintext choices saved in the profile |
| Output | Destination for `mai.zip`, release version, saved signing identity and server recipient |

### Fast security setup through the UI

Security is a first-class packaging feature, not a collection of commands the
developer must remember. Recommend **Secure Release** for new deployment profiles.
After one-time setup, **Compile & Create mai.zip** validates, compiles, encrypts,
signs, and verifies automatically. Normal local editing and runs do not require
release keys or unlock prompts.

The Security panel provides:

1. **Signing identity — Create / Import / Use existing.** Generate a signing key
   using a vetted implementation in the native host; protect it with the OS secure
   store or a selected hardware/managed provider. The webview receives a key ID
   and public fingerprint, never private key material. Reuse the selection across
   builds; request OS unlock only when the provider requires it.
2. **Target server — Add / Select.** Save a friendly server name and import its
   public encryption recipient through paste or file selection. Private decryption
   keys stay on the server. A guided first-time bootstrap screen generates an
   inspectable setup script for the operator to run once on that server. It creates
   the recipient identity with restricted permissions and returns only its public
   recipient. No remote execution is added to the compile button.
3. **Establish trust once.** Export the signing public key and show its fingerprint
   for installation through the existing trusted administration channel. The
   bootstrap instructions also install a trusted verifier outside the release ZIP.
   The first downloaded archive must not establish its own trust. Mark a server
   profile as configured separately from whether its setup was actually tested.
4. **Check setup.** Validate key availability and recipient syntax locally; create
   a test package and show a copyable server verification/decryption command.
   Local validation alone must not claim the server has the correct private key.
   Record a completed server test with timestamp and key fingerprints.
5. **Recovery and rotation.** Provide Export Public Key, Change Recipient, Rotate
   Signing Key, and recovery guidance. Keep encrypted old-release rollback viable
   during rotation. Private-key backup is an explicit protected operation, never
   part of a project export or mai.zip. Lost keys produce an actionable recovery
   state, not silent fallback to an unsigned/plaintext build.

Show concise states: **Ready**, **Unlock needed**, **Server setup untested**, or
**Action required**, each with an inline fix. Put algorithms and detailed trust
settings under Advanced. Profiles contain only references/public identities.
Keep persistent security choices visible in the build summary; do not repeat
confirmation dialogs for unchanged settings. Changing to Trusted Local is an
explicit profile choice, not an automatic response to a security failure.

Use established tooling, for example age for recipient encryption and Minisign
for manifest signatures, behind a native adapter. Confirm platform support and
key-store integration in Phase 0 before selecting versions. No custom crypto.

The app shows effective settings, included filenames, redacted config, expected
network destinations, and prerequisite failures before compilation. Never recurse
and bundle the entire selected folder: exclude workbooks, uploads, outputs, logs,
Git metadata, credentials, and unrelated files. Resolve symlinks and reject paths
escaping approved roots. Config validation must reject contradictory or invalid
settings, not merely accept JSON syntax.

The **Compile & Create mai.zip** button performs a cancelable pipeline:

1. Resolve and snapshot approved sources/config/workflows into private staging.
2. Verify toolchain version and trusted checksum; check dependencies against
   `go.sum`; use readonly module mode. Do not disable TLS or checksum checks.
3. Run production workflow/config validation and required Go quality gates.
4. Invoke Go directly with argument arrays and an allowlisted environment, e.g.:
   `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=readonly -trimpath
   -o <staging>/miservice ./cmd/miservice`.
5. Generate PM2 configuration and installation/verification instructions; bundle
   the selected config encrypted as config.json.age for Secure Release (or
   config.json for an explicitly selected plaintext profile), workflow overrides,
   SBOM, and release manifest.
   Keep config values redacted in UI summaries and build logs.
6. Smoke-test the target binary on a matching Linux runner (or a documented
   emulator). Windows/macOS cannot natively execute this Linux artifact. Local
   host-side engine checks are supplementary; mark untested cross-builds clearly
   and require target validation before a release is labeled deploy-ready.
7. Hash the final payload, including encrypted config bytes, and sign the canonical
   manifest with the saved signing identity. Verify the signature and hashes before
   atomically publishing `mai.zip`. Explicit Trusted Local profiles may omit signing;
   any failure in a selected protection blocks publication without downgrading it.
8. Show artifact path, size, target, source revision, test status, signing status, and
   checksum with Copy Path / Reveal Folder actions. Retain a sanitized build log.

Build subprocesses receive no free-form shell fragments. Limit allowed flags and
validate names and ports. Tool bootstrap and dependency network access are explicit
prerequisites. Pin tool versions and lockfiles; record dirty-source content hashes.
Deterministic archive ordering/timestamps support repeatability, but bit-for-bit
reproducibility must be verified before being advertised.

## 9. Package and configuration contract

“Combine configuration with the output binary” means a single deployable archive
containing the executable and its configuration, not secrets compiled into Go.
Existing runtime overrides make this compatible with the service today.

```text
mai.zip
  mai/
    miservice
    config/config.json.age           selected real config, encrypted for server
    # config/config.json instead for explicit plaintext profiles
    config/workflows/*.yaml           selected overrides; beside effective config
    ecosystem.config.js
    install.sh
    verify-release.sh
    manifest.json
    manifest.sig                     required for Secure Release
    sbom.cdx.json
    DEPLOY.md
```

Deployment assumption: MaiService runs on the same trusted, privately managed
server as the database and related services. Authorized developers/operators
control the package and server. The UI imports the real config.json, including
connection settings, and packages it using the saved security profile. Secure
Release signs the package and encrypts the config; the one-time UI wizard handles
key selection and server bootstrap guidance. Trusted Local permits direct config
bundling when deliberately selected. Never embed configuration into the executable,
so operators can change settings without recompiling.

Treat the resulting archive as private operational data: keep it out of source
control and public downloads, restrict local access, and transfer it through the
existing authenticated deployment channel. A shared server reduces network
exposure but does not protect against a compromised process or unauthorized local
user. Retain service-user isolation, restrictive file permissions, and redacted
logs without adding configuration ceremony to the developer workflow.

Checksums detect accidental changes, not authenticity. Secure Release therefore
signs the manifest and encrypts configuration automatically, even for the trusted
server. Use independently provisioned trust keys; never package private signing
or decryption keys. Encryption protects the packaged config, not its plaintext
runtime use on the server, which still requires filesystem permissions.
Do not export standalone hashes of secret values or publish private manifests.

On installation, config lives in `/opt/mai/shared/config.json` with mode 0600,
owned by the dedicated service user. Workflows live in `/opt/mai/shared/workflows`;
uploads and outputs live beside the config to match current code. Binaries live
in `/opt/mai/releases/<release-id>/`, selected by `/opt/mai/current`.

The independently provisioned verifier checks signatures and hashes before running
any installer from the archive. Installer validates paths, restores executable
permissions explicitly (ZIP extraction is not portable), checks architecture and
prerequisites, and never overwrites existing config without a reviewed migration.
For encrypted profiles, decrypt using the server identity into a private temporary
file, validate the result, and atomically install it with mode 0600. Decryption is
a deployment step, so PM2 restarts do not require a human unlock. Never pass secrets
in command-line arguments or logs. Record which workflow/config version was
activated; rollback must restore compatible workflows/config as well as the binary.

## 10. PM2 operation and availability

Generate `ecosystem.config.js` from a fixed tested template. Include no secrets.
Example defaults, configurable within validated bounds:

```js
module.exports = {
  apps: [{
    name: 'mai-service',
    script: '/opt/mai/current/miservice',
    cwd: '/opt/mai/shared',
    interpreter: 'none',
    args: ['-config', '/opt/mai/shared/config.json'],
    exec_mode: 'fork',
    instances: 1,
    autorestart: true,
    min_uptime: '10s',
    max_restarts: 10,
    exp_backoff_restart_delay: 1000,
    max_memory_restart: '650M',
    kill_timeout: 15000,
    time: true,
    env: { PORT: '3081', GOMEMLIMIT: '400MiB' }
  }]
};
```

Keep one instance because latest-output state and file registries are local to one
process. PM2 is a restart supervisor, not high availability or zero-downtime
failover. Do not enable `wait_ready` without implementing compatible readiness
signaling. Poll HTTP health/readiness explicitly after restart.

Server prerequisites: supported pinned Node LTS and PM2, dedicated unprivileged
user, protected directories, reverse proxy/TLS, firewall rules, disk capacity,
log rotation, backup policy, and time synchronization. Generate commands for
`pm2 start`, `pm2 save`, and operator-reviewed `pm2 startup`; do not execute remote
server commands from the compile button. Validate the template on Linux, including
paths containing spaces and invalid profile input rejection.

Installation stages a release, verifies it, activates workflows/config safely,
updates `current`, restarts PM2, and polls readiness with a bounded timeout. On
failure, restore the previous compatible release and restart. Test SIGTERM drain,
forced timeout, crash/restart loops, disk full, and rollback. Add an application
readiness endpoint that checks required configuration and writable storage;
optional unavailable integrations must not cause perpetual restart loops.

## 11. Confidentiality, integrity, and availability requirements

These are implementation and verification requirements, not a claim that a ZIP
or Tauri automatically secures the service.

| Property | Required controls | Evidence before production |
| --- | --- | --- |
| Confidentiality | Tauri v2 least-privilege capabilities; strict CSP; no remote web content; scoped filesystem access; OS secure store or selected secure key provider; Secure Release config encryption; redacted logs/traces; private archive handling and authenticated transfer; restricted runtime config permissions | Capability/path-escape tests, log scans, archive inclusion checks (only approved config secrets), transfer and permission checks |
| Integrity | Backend validates IPC and profiles; no arbitrary shell execution; Go checksum verification; pinned JS/Rust lockfiles; payload checksums; signed manifest for Secure Release; controlled workflow/config activation | Corrupt ZIP/config rejection; signature rejection when signing is enabled, command injection tests, dependency audit and provenance record |
| Availability | Size/time/concurrency limits, cancellation, bounded preview/trace storage, PM2 restart/backoff, shutdown drain, retention, disk monitoring, backups and rollback | Load/cancel/crash/disk-full tests, recovery rehearsal, restoration test |

Production service work must include configurable bind address and origin policy,
authentication/authorization at a trusted gateway or service, upload rate/size
limits, and per-user/job output authorization. The existing latest-download route
must not be exposed to multiple untrusted users as-is. Default to private network
access until those controls are implemented and tested. Reject insecure TLS
downgrades in production connection profiles unless an explicitly documented
legacy exception is approved; current example settings need review.

Treat workflows and Excel files as untrusted input. Bound decompressed workbook
sizes, sheets, rows, cells, regex/expression work, and trace output. Prevent ZIP
path traversal and symlink escapes. Preview cell contents as text, never HTML or
executable formulas. Apply outbound destination policy for REST/SQL, including
redirect validation, while allowing explicitly configured internal company hosts.

Desktop signing/update verification and dependency license/security checks are
release requirements. Disable telemetry by default; never collect workbook
contents or secrets. Production signing credentials must use OS secure storage,
hardware-backed signing, or a managed signing service with auditable access.

## 12. Implementation phases and exit criteria

| Phase | Deliverables | Exit criteria |
| --- | --- | --- |
| 0 — Contracts and spikes | Architecture decisions, protocol schema, supported OS/toolchains, grid/license evaluation, threat model, fidelity fixtures | Existing BOM round-trip approach and actual-XLSX preview proven; deployment/config contract reviewed |
| 1 — Desktop foundation | Tauri v2 + React/TS, workspace permissions, design system, command palette, atomic save/recovery, CI | Opens/saves a workspace on all supported hosts; unauthorized paths rejected; keyboard navigation works |
| 2 — Engine bridge | Host-specific Go sidecar, versioned IPC, structured diagnostics, bounded tracing/cancellation | Existing golden tests unchanged; host results equal CLI results; failed/canceled runs clean up |
| 3 — Workflow editor | React Flow AST adapter, nested branches, common inspectors, source fallback, undo/redo | BOM loads losslessly; invalid wiring blocked; round-trip and source synchronization tests pass |
| 4 — Excel workspace | Input inspection, paged grids, step snapshots, actual-output preview, diffs | Exported artifact matches preview; supported styles verified; large files remain responsive |
| 5 — Deployment builder | Validated profiles, config-folder import, cross-build, UI security wizard, saved key/recipient profiles, signed manifest and encrypted config, PM2 templates | One-click Secure Release after setup; verified mai.zip for amd64/arm64; only approved files included; target Linux smoke tests pass |
| 6 — Production hardening | Access controls, network policy, readiness, quotas, logs/retention, install/rollback | Security negative tests, crash/restart, graceful shutdown, rollback and restore rehearsals pass |
| 7 — Pilot and release | Signed desktop installers, migration notes, tutorials, support bundle, usability fixes | Developers complete create → run → inspect → package without CLI intervention except explicit server provisioning |

Phases 0–2 precede editor integration. Deployment template work can proceed after
Phase 0, but release packaging depends on validation and production hardening.
Do not ship a compile button that reports success before packaging and target
verification finish. Estimate calendar effort after the platform/grid/trace
spikes; those are the main unknowns.

## 13. Testing and developer experience

Retain `make check` and add documented desktop commands for install, dev, lint,
typecheck, unit tests, integration tests, and packaging. Pin Node/package-manager,
Rust, Tauri, React Flow, and Go versions; commit lockfiles. CI builds sidecars and
desktop apps on an OS matrix, separately from Linux deployment artifacts.

- Go: parser/AST/diagnostic contracts, observer neutrality, cancellation, workbook
  fidelity, existing BOM golden fixtures, and service security regressions.
- React: graph commands, undo/redo, source synchronization, stale results, forms,
  accessibility, and loading/error states. Use component tests and browser-based
  integration tests with typed mock IPC.
- Rust: path authorization, archive inclusion, profile validation, process
  cancellation, secret redaction, signature checks, encryption/decryption failures,
  locked/missing keys, profile downgrade prevention, and atomic staging cleanup.
- Desktop integration: real sidecar and filesystem tests on supported OSes; choose
  a Tauri-compatible automation approach per platform rather than assuming every
  webview supports the same WebDriver tooling.
- Linux deployment: both architectures, archive integrity failures, PM2 startup,
  readiness, SIGTERM, permission failures, restart policy, upgrade and rollback,
  wrong-recipient rejection, initial trust bootstrap, and key rotation/recovery.

Initial measurable performance budgets, to be tuned using named reference hardware:
canvas interaction remains usable at 200 nodes; validation response under 500 ms
for ordinary workflows; a 100k-row workbook uses paging and bounded memory without
blocking UI input; cancel acknowledgment under one second with eventual bounded
termination. Record workbook dimensions and file sizes with benchmark results.

Provide a bundled tutorial workspace using synthetic fixtures and a sanitized
support bundle exporter. No tests require real company credentials. Present
failed, skipped, unsupported, and untested checks separately.

## 14. Release acceptance checklist

- [ ] Developer opens a folder, imports Excel inputs, edits the graph or YAML,
  validates, runs, and inspects input/intermediate/final output in one workspace.
- [ ] The graph preserves production execution semantics and existing BOM results.
- [ ] Final preview and exported Excel refer to the exact same generated artifact;
  unsupported visual features and cached formula values are clearly identified.
- [ ] Run cancellation, recovery, external file conflicts, and stale results work.
- [ ] Deployment panel imports approved config files, validates all conditions,
  executes the build, and produces `mai.zip` with progress and actionable failures.
- [ ] ZIP contains the Linux executable, approved config/workflows, tested PM2
  ecosystem file, verification/install documentation, manifest/signature, and SBOM
  according to the selected profile.
- [ ] After one UI setup, Secure Release automatically encrypts selected config,
  signs the manifest, and verifies the package without manual crypto commands.
- [ ] Private keys never enter the webview, project files, logs, or package; missing
  keys or failed verification never silently downgrade the selected protections.
- [ ] Server bootstrap, trusted verification, decryption, key rotation/recovery,
  and unattended PM2 restart work. Installed config is mode 0600 and replaceable.
- [ ] Trusted Local choices are explicit and saved; daily editing/running remains
  independent of release-key availability.
- [ ] Target Linux validation, PM2 operation, access controls, rollback, and backup
  restoration pass before the release is labeled production-ready.
- [ ] Keyboard accessibility, license review, signed desktop distribution, and
  Go/TypeScript/Rust quality gates pass.
