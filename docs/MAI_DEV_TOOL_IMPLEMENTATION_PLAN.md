# Mai Studio + MaiService: automation workbench implementation plan

Planning baseline: **29 October 2026**, repository `Sepiolina/migo`, inspected
commit **1291d7f207d8f1a0f644601eabba9240ed9c9fbf**. This replaces the original
foundation plan. It is a design and engineering proposal, not a release note.
No application implementation is included in this revision.

## 1. Executive recommendation and product-design thesis

**Build one local automation workbench around a readable, guided Blockly
workflow.** A user should be able to choose an Excel file, assemble meaningful
steps, run the workflow, and explain the result without opening YAML.

Retain Tauri v2, React, Blockly, the production Go engine, and the existing Linux
package builder. Use a contextual inspector to make block arguments understandable.
Provide an accessible step outline over the same document and commands. Make
React Flow a secondary **Overview** for dependencies and, later, execution status.
Put **Advanced YAML** behind a deliberate command. Do not maintain three equally
prominent authoring products or replace the engine with a graph scheduler.

The organizing unit is **a workflow with inputs, steps, results, and a recorded
run**, not a collection of editors. Selection links those areas. Progressive
disclosure separates daily authoring from source editing and release packaging.
Enterprise quality means predictable changes, accessible controls, recoverable
work, bounded resource use, and honest execution/security status.

Deliver a complete local journey first. Then add durable recovery, structured
diagnostics, cancellable execution, bounded intermediate inspection, and richer
authoring. Remote deployment, accounts, collaboration, live Studio connections,
and automatic server upgrades are independent initiatives.

Assumptions: single local user; Windows is the primary authoring platform, Linux
is also tested; workspaces may be under version control; real workbooks are private;
service operators have an existing trusted server administration channel. macOS
release support needs its own validation. All targets below are **proposed targets
to validate**, not measured performance or usability results.

## 2. Current-state assessment

### Evidence and confidence

“Confirmed” below means implementation was inspected at the baseline, with related
test sources where indicated. It does not mean a fresh desktop, server, or live
integration test was performed for this planning revision. The older plan is
historical intent; [current capabilities](CURRENT_CAPABILITIES.md) is descriptive
documentation, not independent verification.

| Area | Confirmed implementation and evidence | Boundary or missing behavior |
| --- | --- | --- |
| Desktop and views | Tauri v2 + React; Blocks is default; graph and source views in [App.tsx](../apps/studio/src/App.tsx), [BlockEditor.tsx](../apps/studio/src/BlockEditor.tsx), [model.ts](../apps/studio/src/model.ts) | Graph edges project execution order; they are not arbitrary program wires. Graph selection supports guided top-level settings today. |
| Visual steps | [blocklyBlocks.ts](../apps/studio/src/blocklyBlocks.ts), [blocklyWorkflow.ts](../apps/studio/src/blocklyWorkflow.ts): read, filter, calculate, rename, select, sort, group, save, simple join, dedupe; basic value/logic blocks | Several shapes are deliberately narrow, such as one calculated/renamed column. Nested/unsupported steps and expressions are opaque. Column lists entered as comma-separated text are not schema-aware selectors. |
| Workbook settings/templates | [WorkflowSetup.tsx](../apps/studio/src/WorkflowSetup.tsx), [workbookSettings.ts](../apps/studio/src/workbookSettings.ts), [workflowTemplates.ts](../apps/studio/src/workflowTemplates.ts) | Input labels/IDs and output bindings have forms; two starter templates exist. Renaming identifiers does not automatically refactor all references. No guided first-run journey. |
| Source/history | [useDocument.ts](../apps/studio/src/useDocument.ts), [authoring.ts](../apps/studio/src/authoring.ts), Blockly codec | Source string owns session history; YAML nodes preserve untouched constructs/options. Graph IDs derive from indexes/branch paths, not durable identities. No universal lossless visual conversion. |
| Saving | [workspace-core/src/lib.rs](../apps/studio/src-tauri/workspace-core/src/lib.rs), App draft guards | Scoped paths, revision conflicts, atomic file replacement, unsaved/pending-draft protection. No autosave, persistent recovery journal, versioned project manifest, or persisted input profile. |
| Engine | [workflow.go](../internal/workflow/workflow.go), [table.go](../internal/workflow/table.go), [expr.go](../internal/workflow/expr.go), [remote.go](../internal/workflow/remote.go) | Ordered steps, nested `if`/`split`, additional table operations, SQL/REST. Not a general loop/subflow runtime. Strict parsing rejects unknown document fields. |
| Local execution | [runner.go](../internal/studio/runner.go), [api.ts](../apps/studio/src/api.ts) | Production parser/compiler and engine; explicit SQL/HTTP blockers. Validation alone does not certify offline compatibility or row-dependent correctness. Protocol v1 has terminal responses, not step events. |
| Results | Runner and [WorkbookPanel.tsx](../apps/studio/src/WorkbookPanel.tsx) | Reads actual generated XLSX and returns its SHA-256. Preview is read-only strings, 100 rows/page in UI. No intermediate datasets, full styles, or formula recalculation. |
| Resource handling | Runner, native [lib.rs](../apps/studio/src-tauri/src/lib.rs), [runner_process.rs](../apps/studio/src-tauri/src/runner_process.rs) | Workbook size/expansion and protocol limits; supervised children/timeouts. Paging still loads worksheet rows before slicing. No user Cancel command, progress stream, or retained run-history UI. |
| Packaging/security | [PackageBuilder.tsx](../apps/studio/src/PackageBuilder.tsx), [internal/deploy](../internal/deploy), native host | Linux amd64/arm64 `mai.zip`, Ed25519 signing, age config encryption, native OS key store, inventory, verifier, fresh-target installer, PM2 config. Public profiles in localStorage; no key lifecycle UI or build Cancel. |
| Service | [internal/app/server.go](../internal/app/server.go), [workflow reference](WORKFLOWS.md) | Configured SQL/REST/SAP paths; health is liveness. Latest outputs belong to one process. PM2 restart supervision does not establish high availability. |

### Documented behavior that still needs environment-specific verification

- Windows installer usability/signing, interactive OS key-store unlock/recovery,
  and macOS distribution cannot be established from source or headless CI alone.
- Generated PM2/install instructions exist; the user's Linux server, DB/SAP
  credentials, networking, recovery procedures, and live workload were not tested
  during this review. Cross-architecture ELF checks are not runtime validation.
- Existing browser, Go integration, native supervision, and Windows CI tests are
  valuable regression assets. Their existence is not evidence of WCAG conformance,
  large-data performance, or successful novice task completion.

### Corrections to the original plan

The original plan predates the delivered Blockly work and package builder. It
proposes features that remain absent: durable workspace metadata, tracing, user
cancellation, style-aware previews, key rotation, release symlink activation,
in-place upgrades, and rollback. Its example release/shared directory layout is
not the current fresh-target installer contract. Its proposed Minisign choice is
superseded by implemented Ed25519 signing. This plan preserves those implemented
contracts and treats the remaining ideas as scoped future work.

## 3. Target personas and critical user journeys

| Persona | Primary need | Product priority / success target |
| --- | --- | --- |
| Analyst / business author | Turn a workbook into a reliable result without learning syntax | At least 8 of 10 representative first-time users finish the worked example within 10 minutes, without YAML or facilitator intervention. |
| Technical author | Understand, change, and reuse complex logic quickly | Keyboard insertion/configuration; source access with preserved context; reusable parameters and fragments after explicit contracts exist. |
| Operator / administrator | Know what was run/built, with which inputs and protections | Inspect run/package identity and validation evidence; recover from errors; package without making security choices repeatedly. |

Use a bundled synthetic workbook and the existing starter templates; do not ask
for real company data during onboarding. First launch offers **Open workspace**
and **Try an example**. The latter creates a user-chosen local sample folder and
shows three persistent checklist items: Choose input → Build steps → Run and
inspect. It is dismissible and can be reopened through Help. No account or tour
overlay is required.

### Complete journey and state behavior

All steps use the workbench below unless a native file picker is necessary.
Loading indicators preserve the current document and focus. Success appears next
to the affected item; blocking errors remain visible until resolved.

| Step / user goal | Primary action; immediate information | Progressive disclosure and next step | Loading, empty, failure, success, recovery |
| --- | --- | --- | --- |
| Create/open | Workspace rail: **Open workspace** or **New workflow**; folder, workflow name, save status | Recent local folders, template descriptions, advanced paths; next **Choose input** | Loading file list; empty folder offers templates; invalid/unsupported source opens with diagnostics; success selects workflow; access failure offers retry/change folder without clearing edits. |
| Configure inputs | Inputs inspector: **Choose file**, sheet dropdown, detected header/sample, required binding status | Header row, trim, blank rows, fixed mapping, copying a fixture explicitly; next **Add step** | Loading sheet names; unbound state explains required file; missing/oversize/unsupported workbook names the constraint; success shows binding; rebind retains old result as stale. |
| Author | Main Blocks: **Add step** after selection; operation name, sentence summary, required arguments | Searchable catalog, contracts/examples, advanced options; next configure first highlighted field | Lazy loading retains document; empty chain offers Read Excel; incomplete block is a visible draft; valid edit updates summary; undo/discard recovers without dropping opaque content. |
| Inspect sample | Data drawer: **Inspect input**; workbook/sheet, page range, source identity | Cell details, column information, later selected-step sample; next return to selected step | Loading rows; valid zero rows distinguished from no selection; parse failure offers another sheet/file; success labels page/sample scope; failed request retains prior data with an explicit stale label. |
| Validate | Toolbar: **Validate**; syntax/structure, bindings, offline eligibility as separate results | Issues navigate to setting; technical detail reveals engine message; next **Run locally** | “Validating…”; no issues says what was checked, not “safe”; invalid source stays editable; success records validated revision; edits mark validation outdated. |
| Run | **Run locally** uses a concise inline summary of current draft revision, bound files/sheets and local mode | Expand exact input identity/output location; later parameters; next **View results** | Preflight pending; missing input links to binding; running shows real stages/elapsed time; failure links to issue; success only after artifact finalization; retry is manual and creates a new run. |
| Inspect results | Drawer automatically opens on first success; exact output, sheet, row count, time, hash/details | Expand data, cell detail, Reveal file, later intermediate data; next **Save workflow** or refine | Loading actual XLSX; zero rows is successful empty output; preview failure must not imply execution failed; recover by reopening artifact; edited document marks result stale. |
| Resolve errors | Issues drawer and contextual inspector: **Go to setting** | Plain-language reason, field/step, suggested fix, sanitized details; next **Validate again** or **Run again** | Pending recheck; empty issues panel explains scope; malformed/unknown error has copyable sanitized code; resolved issue clears only for checked revision; failed retry keeps previous successful run. |
| Save/reuse | **Save** with visible Saved / Unsaved / Draft incomplete / Conflict state | Save As; later named template/fragment; next continue or package | Saving status; unnamed document requests destination; conflict offers compare/reload/Save As; success pins revision; recovery copy remains available until explicitly discarded. |
| Package | Separate Package workspace mode: **Build Linux package**; target, config, selected overrides, security, prerequisites | Source identity, destinations, memory/PM2, verification detail; next Reveal ZIP and server instructions | Checking/building state; first-use setup; actionable key/tool/config errors; success shows actual verification level; retry after fix, retain previous package, no silent protection downgrade. |

### Worked example: Excel to grouped totals

Use a synthetic `Orders.xlsx`, sheet `Orders`, with columns `Customer`, `Quantity`,
and `Unit price`. Rows: Acme/2/10, Acme/3/10, Beta/0/7, Beta/1/5.

1. Create **Sales totals** from a copy template. Bind input `orders` to the file
   and select `Orders`. Inspect the four input rows.
2. Add **Filter rows**. Pick Quantity → as number → greater than → 0. Summary:
   “Keep rows where Quantity is greater than 0.”
3. Add **Calculate column** named `Line total`. Build Number(Quantity) ×
   Number(Unit price). Inspector explains that input cells may be text and that
   conversion follows Go expression rules; it does not promise inferred numeric
   types from formatting alone.
4. Add **Group totals**: group by Customer, sum Line total. Explain that grouping
   replaces detail rows; show the resulting column contract.
5. Add **Save table** named `totals`; Outputs maps sheet `Totals` to that table.
   Use “Save table” versus “Save workflow” consistently.
6. Validate, then Run locally. Inspect the actual XLSX: Acme/50 and Beta/5, with
   no accidental loss of identifier text. Save the workflow and reveal output.
7. Deliberately remove the file binding; show the binding issue and recover by
   choosing it again. Later tracing should show 4 → 3 → 3 → 2 rows, but the first
   slice must not invent those counts before observer support exists.

The first slice uses existing execution with explicit busy/final status. This
example requires no new scripting runtime, remote connection, or parameter grammar.

## 4. Recommended information architecture and text wireframes

```text
┌ Workspace / Sales totals   Unsaved      Validate   Run locally   ⋯ ┐
├───────────────┬───────────────────────────────────┬───────────────┤
│ Workflows     │ Build  [Overview]                 │ Filter rows   │
│  Sales totals │                                   │ Column        │
│  Stock report │  Read Orders                      │ [Quantity  v] │
│               │       ↓                           │ As [Number v] │
│ Inputs        │  Filter: Quantity > 0  ← selected │ Rule [ >   v] │
│  Orders.xlsx  │       ↓                           │ Value [0    ] │
│               │  Calculate Line total             │               │
│ Results       │       ↓                           │ What changes  │
│  Latest run   │  Group by Customer                │ Keeps rows…   │
│               │       ↓                           │ Advanced ▸    │
│ Package       │  Save table: totals     + Add step│               │
├───────────────┴───────────────────────────────────┴───────────────┤
│ Data (2 rows) | Issues (0) | Runs        [Expand] [Close]         │
│ Output · Totals · run 14 · current       Customer | Line total    │
│                                        Acme     | 50            │
│                                        Beta     | 5             │
└ Local mode · Saved 14:32 / Unsaved changes · operation status ─────┘
```

- **Persistent:** compact workflow toolbar, local-mode indicator, save state, and
  a collapsed/expanded navigation rail. Keep Validate/Run visible; packaging is
  a rail destination, not a competing primary button during authoring.
- **Contextual:** inspector for selected step, input, output, or issue. No empty
  full-width form when nothing is selected: show a concise next action instead.
  Selection is shared between Blocks, outline, Overview, issues, and source.
- **Collapsible/resizable:** project rail, inspector, and bottom Data/Issues/Runs
  drawer. Data starts closed except during onboarding or a completed first run.
  Remember pane sizes per machine, not in executable YAML. Keyboard separators
  resize in 16 px increments and expose their current value.
- **Separate workspace mode:** Package has enough space for a persistent review
  summary and progress. Returning restores the workflow selection and viewport.
  Settings/Help are secondary; do not create pages for individual steps.
- **Small windows:** at 980 × 680, collapse the rail and show either inspector or
  expanded data beside the authoring area, not all panes at minimum width. At
  narrower effective widths/200% zoom use a single panel with explicit Back.
  No horizontal scrolling of the entire application; grids/canvases may pan.

**Operation discovery:** a searchable Add step popover beside the insertion point,
also available through Ctrl/Cmd+K. Search “remove duplicates”, “sum”, “combine
tables”, not only `dedupe`, `group`, `join`. Results show title, one-sentence
effect, required input, output shape, and local availability. Do not hide advanced
operations silently: label “Advanced source” or “Service only”. Insert a configured
starting block and move focus to its first required field. Support Escape to
close and restore focus.

**Interaction rules:** click selects; Enter focuses the inspector; Escape returns
to the originating item. Context menu actions also appear in a visible `⋯` button.
Move up/down, duplicate, delete, and Add before/after work without dragging.
Deletion returns focus to the next step and can be undone. Ctrl/Cmd+S saves;
Ctrl/Cmd+Z and Shift+Ctrl/Cmd+Z route through document history outside ordinary
text-input editing; Ctrl/Cmd+Enter runs from the workbench, with a documented
alternative button. Never steal text-selection shortcuts or system shortcuts.

**History is not saving:** Undo/Redo changes the working document; Save writes the
workflow after a revision check; automatic recovery checkpoints protect a draft
without overwriting that file. Do not initially add autosave of workflow files.
Show “Recovery copy updated” separately from “Saved”. A conflict offers comparison,
Reload from disk after preserving the draft, or Save As; no blind overwrite button.

## 5. Visual authoring, graph, and YAML strategy

### One authoring model, multiple focused representations

1. **Build:** Blockly remains central for ordered steps and composable expressions.
   Put readable summaries in blocks; put dense lists and advanced arguments in
   the inspector. Both operate on the same commands, not independently mutable
   copies. A keyboard/screen-reader step outline uses those same commands and
   forms; it is an alternative presentation, not a fourth language.
2. **Overview:** React Flow shows order, nested branches, and named dataset
   dependencies with distinct labels/line styles. It supports selection,
   navigation, and later run overlays. Editing routes to the shared inspector.
   No graph-only mutation model, free wiring, or alternate scheduler.
3. **Advanced YAML:** available through a toolbar menu/command and on preserved
   steps. Use it for bulk edits, reviewable diffs, advanced engine constructs, and
   exact syntax. Common operations must be fully achievable without it.

Retain Blockly unless Phase 1 keyboard/usability testing demonstrates a fundamental
barrier. The outline and model adapters keep replacement possible, but replacing
Blockly is not a prerequisite or funded work in this plan.

### Canonical representation and synchronization

- **Persisted executable authority:** the existing ordered YAML document, including
  mapping order where evaluation depends on it. Go remains the semantics authority.
- **In-memory authority:** a `WorkflowDocument` containing source/CST, derived typed
  step/configuration descriptors, stable editor IDs, source spans, revision, and
  diagnostics. It is one transaction owner. The typed representation is a
  projection, not an independently serialized competing source of truth.
- **Edits:** commands such as InsertStep, MoveStep, PatchArgument, RenameBinding,
  and ApplySourceDraft patch the CST and commit one revision after checks. Blockly,
  forms, and graph receive projections. Group one gesture/form apply into one undo
  entry; avoid one entry for every programmatic block event.
- **Stable IDs:** assign UUIDs on creation; persist them in versioned `.studio`
  metadata keyed to workflow path plus source revision/hash. Duplicate creates new
  IDs; reorder retains IDs. For external/source edits, reconcile using unchanged
  CST ranges, parent branch, and structural fingerprints. Ambiguous identical steps
  receive new IDs; never map an old failure to an arbitrary step. Old run source
  maps remain attached to their original revision.
- **Diagnostics/selection:** map Go nested step path and source span to editor ID
  for the exact revision. Unsupported or stale mappings open the recorded source
  location, with an explicit “Earlier revision” label. Preserve selection, cursor,
  and viewport when switching views; source edits that delete a step select its
  surviving parent/neighbor.
- **Source drafts:** invalid YAML remains editable and recoverable. The last valid
  visual document is labeled outdated and cannot run as though it were the draft.
  Apply valid source as one document transaction; Discard restores it explicitly.
  The source editor has local text undo until Apply, then document undo can undo
  that application. Visible labels and shortcut routing distinguish the scopes.
- **Preservation:** reuse unchanged CST nodes/raw source slices where possible;
  patch only affected fields. Byte-identical no-op saves are the target. Comments
  attached to a moved step move with it; document exact exceptions in fixtures.
  Never promise arbitrary formatting preservation after a changed subtree.

### Unsupported construct policy

A preserved step shows its operation, a readable reason such as “Nested branch —
edit in Advanced YAML”, and actions View source / Copy / Move / Delete. Keep the
complete original subtree and expose only changes known to be safe. A preserved
expression stays a read-only expression chip; opening advanced editing retains its
text. Do not translate unfamiliar syntax into a guessed expression tree.

Valid engine-supported preserved steps may run if allowed in local mode. Malformed
YAML, aliases, duplicate keys, and constructs rejected by Go remain invalid;
preservation does not imply validity. Incomplete/detached blocks are recoverable
drafts, not executable steps. Before changing representation, offer Finish editing
or Discard draft; neither silently commits invalid content. Any explicit conversion
that changes unsupported content requires a previewable diff and an undoable action.

## 6. Capability matrix and scripting improvements

Legend: **E/UI** = engine and visual UI, possibly limited; **E** = engine supports
it but the full visual form is missing; **Change** = backend/engine contract needed;
**Restricted** = intentionally unavailable to Studio execution; **Defer** = separate
initiative. “Proposed” entries are not current product claims.

| Capability | Baseline classification | Recommended action |
| --- | --- | --- |
| Read/filter/set/select/rename/sort/group/save/simple join/dedupe | E/UI | Improve labels/contracts; replace comma lists with editable column tokens; extend forms without discarding hidden options. |
| Read header/mapping options; richer aggregations/joins; fill/append/stack/compare/window/pick | E, with some guided graph forms | Expose in prioritized inspector forms after schema-aware controls; full coverage is incremental. |
| Nested `if` and row `split` | E; graph can depict branches, Blockly preserves opaque shapes | Add nested branch containers after identity, diagnostics, and sample contracts. Explain whole-table conditional versus per-row partition; preserve Go recombination semantics. |
| Log/fail | E | Add safe author-facing assertion/failure forms; never label arbitrary errors recoverable. Logs need redaction before display. |
| General loops, try/catch/finally, user-defined functions | Change / Defer | No new runtime in this program. Existing REST `each`/batching is not a general loop. Existing remote `on_error` is not general exception handling. |
| SQL/REST params, batching, retries, `on_error` | E / Restricted | Allow source inspection and service compatibility information; keep Studio blockers. Validate offline eligibility before running, including nested paths. |
| Typed run parameters/defaults/constraints | Change | Add opt-in versioned engine parameter contract; SQL query params are not workflow input parameters. UI validates types/ranges/enums and Go checks them again. No source-string substitution. |
| Runtime configuration in expressions | E / limited in Studio | Engine expressions can use `config(key)`; Studio currently supplies no user runtime config. Keep that distinction visible; a saved input profile does not supply service configuration. |
| Expression suggestions and diagnostics | Partial E/UI; helpers need contract | Add schema-aware picker, searchable functions, examples, static compile feedback. Use Go function metadata/validation; never duplicate evaluator semantics in JS. |
| Schema-aware columns | Change to inspection/analysis contracts | Infer from bound data and operation effects, showing known/inferred/unknown. Sample inference is advisory, not proof of all rows. Allow deliberate custom names. |
| Configurable templates | E/UI for two fixed templates; proposed expansion | Add template input/output requirements and non-secret configuration; typed customization follows parameter support. |
| Reusable fragments/subflows | Change; no engine subflow grammar | First implement copy-on-insert fragments expanded into ordinary steps with ID/reference remapping. Record origin/version; no automatic propagation. True callable subflows deferred. |
| Intermediate samples, tracing, history | Change | Add bounded observer and storage contracts. No re-execution merely because a step was selected. |
| XLS/XLSX inspection, XLSX output | E/UI | Preserve exact artifact values; add column metadata and honest fidelity notices before considering style preview. |
| Full Excel editing/recalculation/macros | Defer | Not an Excel replacement; no formula or macro execution in preview. |
| Linux packaging/signing/encryption | E/UI | Improve review, prerequisites, status, key lifecycle and cancellation; preserve installer/verification compatibility. |
| Remote deploy, in-place upgrade/rollback, collaboration/accounts | Defer | Separate value/risk proposals; no dependency of the local workbench. |
| Python, JavaScript, arbitrary shell, plugin marketplace | Defer | Not recommended: no demonstrated need justifies a new execution/security/maintenance boundary here. |

**Operation contracts:** each catalog entry declares execution scope (table/row),
required input/schema, available saved tables, produced columns/table identity,
cardinality behavior, effect classification, and engine availability. For example,
Filter preserves columns but can reduce rows; Calculate preserves rows and adds or
replaces a column; Group replaces schema and reduces to grouping keys/aggregates;
Join can multiply rows. Show those consequences before the run. Unknown schemas
remain unknown; do not block every valid advanced workflow on incomplete inference.

**Parameter recommendation:** propose a small namespaced `params` expression binding
with string/number/boolean/enum values, defaults and required/range validation.
Phase 5 must first define collision rules, numeric/coercion semantics, and invocation
compatibility. Legacy YAML remains unchanged. Adopt an explicit optional dialect
version only after Go parser, CLI/service invocation, and packaging capability
checks support it. Old engines must reject new syntax clearly; never strip it.
Connection secrets remain native/service configuration references, not ordinary
parameters, YAML literals, or template defaults.

**Execution boundary:** authoring a service workflow does not authorize executing
its integrations in Studio. Show “Service step — unavailable in local runs”. New
offline preflight conservatively rejects SQL/HTTP steps even in conditional paths;
offer a deliberately separate fixture workflow, not silent omission. Existing
runtime transport blockers remain defense in depth. Local builds may fetch Go
dependencies; distinguish that tooling network access from offline workflow runs.
Packaging requires a trusted Go source checkout and can execute its compiled
service during eligible same-host smoke validation. Show source identity and
require deliberate selection of that trusted checkout. Workspace path checks and
offline workflow transport blockers are not a sandbox for hostile source code.

## 7. Design system, accessibility, and interaction specification

### Tokens and primitives

Use CSS custom properties shared by React components and Blockly theme generation.
Start with an accessible light theme; dark mode waits until every component,
Blockly field/widget, graph edge, grid, focus ring, and status state passes the same
contrast matrix. Do not maintain two partly styled themes.

| Token family | Starting specification (validate in webview) |
| --- | --- |
| Typography | System sans; body/form 16 px / 1.5, supporting labels 14 px minimum, section title 20 px, page title 24 px; source/cell detail monospace 14–16 px. Block text 17 px at default zoom. |
| Space | 4/8/12/16/24/32 px scale. Related fields 8–12 px; groups 24 px. Avoid dense unstructured borders. |
| Surfaces/text | Background `#F6F7FB`, surface `#FFFFFF`, text `#172033`, muted text `#475569`; functional border `#64748B`, decorative separator `#D8DEE9`. |
| Actions/status | Action/focus `#1D4ED8`; success `#166534`, warning `#92400E`, danger `#B91C1C` on light surfaces. Pair with icon and word. Category colors distinguish operation families, not validity. |
| Shape/elevation | 6 px control / 10 px panel radii. Flat main panes; subtle elevation only for popovers/dialogs. No shadow-only selection indicator. |
| Interaction | Distinct hover, pressed, selected, disabled, busy, invalid states. 2 px focus outline plus 2 px offset, never obscured by sticky areas. Disabled action has nearby reason. |
| Density | Comfortable controls 40–44 px, rows 40 px; compact controls/rows 32 px with the same readable text. Minimum 24 × 24 px targets or adequate spacing; primary touch targets 44 px. |

Target **WCAG 2.2 AA**, including contrast (4.5:1 ordinary text, 3:1 large text and
functional UI), focus visibility/not obscured, target size, and alternatives to
dragging. This is a release target, not a current conformance claim.

Reuse **React Aria Components** for dialogs, popovers, menus, tabs, tooltips, and
form behavior behind thin local wrappers; validate its license/bundle impact and
Tauri focus behavior in Phase 0. It supplies semantics and keyboard patterns,
not visual design or automatic compliance. Keep existing React Flow/Blockly
adapters; avoid importing an entire admin-dashboard design language. Own custom
domain controls (column picker, expression builder, workflow inspector) because
generic forms do not express their semantics. Migrate primitives incrementally.

### Component behavior

- Forms: persistent labels, required state and short examples, field-level errors
  after blur/Apply, summary for multiple errors, focus first failed field. A
  configuration group has one Apply/Cancel transaction when several fields must
  change together. Simple valid selections can commit immediately and remain undoable.
- Tables: read-only semantic table first, sticky headers, page range/total, keyboard
  cell access and copy, full-value inspector. Label sampled/truncated/unknown counts.
  Add virtualization only if measurements require it and preserve accessible row
  indices; explicit paging remains an alternative.
- Inspectors: selected step name, effect summary, common settings, collapsed
  Advanced. Retain meaningful focus after an update; never reset a form on an
  unrelated render. Long labels wrap; tooltip is supplementary, not sole access.
- Tabs/dialogs: arrow-key tabs; focus trap/restore in dialogs; Escape closes only
  dismissible UI. Use dialogs for destructive discard, key rotation/backup, and
  genuine modal choices, not everyday step editing.
- Notifications: inline persistent error/action states; polite status announcements;
  short success toasts are supplementary. No toast is the only place an error exists.
- Status: “Running”, “Stopping”, “Stopped”, “Failed”, “Succeeded”, “Result outdated”,
  “Preview unavailable”; distinguish them through text/icon as well as color.
- Terminology: Workflow, Step, Input file, Saved table, Output workbook, Run locally,
  Build Linux package. Avoid “node argument” in analyst flows. Example error:
  “Choose an input file for Orders before running” with **Choose file**.

**Data fidelity:** show the actual output workbook, not a frontend reconstruction.
The first redesign promises readable values/sheet/order, not style equivalence.
Later cell metadata can distinguish engine-consumed text, workbook display value,
formula text and cached value. Label cached formulas as potentially outdated.
Formatting previews must enumerate supported features; charts/merged layouts/macros
and Excel recalculation remain outside scope. Large cell contents are text, never HTML.

### Credentials and signing-key UX

Normal authoring never requires signing setup. In Package, offer **Use existing
identity** or **Create signing identity**, show a public fingerprint and copy/export
public key, then accept a named server's public age recipient. Native code owns
private material. Feedback distinguishes Ready, Unlock needed, Key unavailable,
and Server setup untested. Local syntax checks cannot certify server decryption.

Phase 6 adds multiple identity references and explicit rotation: create new key,
export its public trust material, provide operator instructions to trust it, then
switch future builds. Keep old public verification identities and explain that
old encrypted packages still require the matching server private identity.
Changing a profile never re-encrypts an existing ZIP.

For backup, offer an **explicit encrypted private-key backup** only through a
reviewed native flow using a vetted encrypted-container implementation. Select a
destination and confirm the public fingerprint; keep passphrase/private bytes out
of React by using a native secure prompt/helper. If that flow cannot be implemented
and tested reliably, ship public-key export and recovery guidance first and mark
private backup unavailable. No plaintext export or custom cryptography. Import
validates identity and requires an intentional replace/new-identity decision.

Explain recovery limits before identity creation: loss of an unbacked signing key
requires a new signing identity and operator trust update; public keys cannot
restore it. Lost server age identities cannot decrypt previous config archives.
Destructive key deletion/rotation has a review showing affected profiles; ordinary
builds reuse approved selections without repeated confirmations. A key-store or
decryption failure never switches Secure to Trusted local automatically.
Never place secrets in previews, telemetry, logs, project metadata, or support
bundles. Keep server setup/verification instructions explicit and locally copyable.

## 8. Technical architecture, compatibility, and migration approach

### Incremental module boundaries

Extract responsibilities from current components rather than reorganizing every
file at once. The following paths are **proposed**, not existing directories:

```text
React app/shell
  features/document     CST, commands, history, stable IDs, revision maps
  features/authoring    Blockly adapter, outline, inspector, Overview, source
  features/data         input contracts, paged artifact/cell inspection
  features/runs         run state, issues, history, bounded sample views
  features/package      profiles, build review/status, public key lifecycle
  shared/ui             tokens and accessible primitives
  shared/protocol       generated, runtime-validated transport types
          │ typed Tauri commands/events
Rust host               authorized workspace I/O, recovery, job supervision,
                        artifact publication, credential operations
          │ versioned bounded sidecar transport
Go runner               authoritative compile/execute, diagnostics, observer
Go workflow engine      existing semantics; optional observer/context extensions
```

Use a reducer/command service with focused React subscriptions for document state,
building on `useDocument`; do not introduce another global state library in the
first slice. Keep UI preferences, dirty document, incomplete drafts, jobs, and
persisted run metadata separate. Blockly workspace objects never become the
canonical application state. Generate transport types from a checked-in schema
and validate envelopes at Rust/Go boundaries; TypeScript types alone are insufficient.

### Workspace storage and versioning

Legacy folders open without conversion. Introduce metadata lazily, with explicit
versioning and default ignore rules:

```text
workflows/*.yaml                    existing executable documents
.studio/project.json               proposed versioned bindings/public preferences
.studio/documents/<id>.json         proposed editor IDs/source-hash associations
.studio/recovery/<document-id>/     proposed private draft checkpoints
.studio/runs/<run-id>/              existing output root; proposed run manifests
.studio/packages/<package-id>/      existing package output root
```

Store relative input paths when within the workspace; retain workspace path
authorization. Do not expand access to external files as a hidden convenience.
Offer explicit copying into a fixture folder when desired, with privacy guidance.
Never copy workbooks or credentials into Git automatically. Public profile migration
from localStorage is a one-time import with schema validation, preserving the old
record until the new one reads successfully; key references contain no private data.

YAML and sidecar metadata cannot be atomically replaced as a pair on all targets.
Write metadata with a source hash and generation; commit YAML with existing revision
checks, then metadata. A mismatch after crash means ignore/rebuild identity metadata,
not corrupt or overwrite YAML. Recovery records use atomic generation manifests
and can contain source plus incomplete block/form drafts, not just valid YAML.
Checkpoints target 2 seconds after inactivity, with bounded retention and private
permissions. Sensitive files pasted into source remain private data: recovery is
excluded from Git and exports, never advertised as encrypted storage.

Unknown future metadata versions open source read-only or offer a copy opened in
legacy mode; never mutate unfamiliar metadata. Legacy workflows do not gain a
`schemaVersion` field until the Go parser accepts the new dialect. Schema upgrades
are explicit, with backup and minimum-engine metadata; no downgrade that discards
parameters or branching. Ship old/new fixture tests together.

### Execution, diagnostics, and cancellation contracts

Current v1 commands are `capabilities`, `validate`, `inspectWorkbook`, `run`, and
`buildPackage`. They return one terminal result; there is no cancel/event channel.
Introduce negotiated v2 alongside v1, maintaining existing CLI/service behavior:

| Proposed contract | Required semantics |
| --- | --- |
| `analyzeWorkflow` | Structured diagnostic code/severity/phase, source span, nested step path, field path, revision; static operation/effect/schema metadata and offline eligibility. Go is authoritative. |
| `startRun` / `startBuild` | Immutable job ID and request revision; Rust registers ownership before acknowledging. Capture actual source, non-secret settings and input snapshot identities. |
| Job events | Sequence number, job/revision, phase, step path, elapsed time, available row counts, terminal outcome; bounded queue and backpressure. Logs cannot block cancellation or corrupt protocol stdout. |
| `cancelJob` | Separate native control path; must not queue behind the synchronous run request. Idempotent cancellation token, cooperative context propagation, grace deadline, then process-tree termination. |
| `getRun` / `getRows` | Read recorded immutable manifest/bounded snapshots through scoped handles; never execute a workflow in response to data selection. |
| Artifact ready | Publish only finalized/hash-checked outputs; preview is a separate operation and may fail independently. |

Initially allow one foreground mutating job per workspace and explain conflicts.
Enforce ownership across app instances using a native workspace/job lock with
process start identity and a unique generation token, not a PID alone. A second
instance cannot cancel or clean the first instance's live job. Startup recovery
must acquire ownership and fence stale generations before deleting staging files;
test lock contention, PID reuse, crashes, and recovery by a second instance.
Inspections may run within bounded concurrency. Never show fake percentages: use
stage/elapsed time until real work totals exist. Observer hooks report nested step
start/end/error and row counts only where available, with no change to engine results.
Go errors are structured at creation, not parsed from localized prose in React.
Preserve established HTTP error behavior through adapters.

**Run intent and reproducibility:** before dispatch show current saved/unsaved
revision, input names/sheets, parameters, offline mode, output destination. Run an
immutable snapshot of the current valid draft, not a later file read. Copy or
otherwise snapshot approved input bytes privately and hash those exact bytes;
re-stat/hash detects mutation while capturing. Quota failure blocks with a clear
choice rather than running against an uncertain snapshot. Record source snapshot,
input hashes, engine/build version, resolved non-secret parameters, mode, timestamps,
outcome, output hashes, and redaction policy. Secret references are recorded as
identifiers only; no unhashed secrets or publicly guessable secret hashes.

History initially retains metadata and final artifacts, with input retention
explicitly controlled by disk budget/privacy settings. If original snapshots have
expired, label rerun “Uses current files”, not reproducible. Reproducibility means
identifiable inputs/configuration, not a promise of bit-identical XLSX ZIP bytes.
Input content changes invalidate current-result status when detected, including
on focus/preflight; filesystem watching alone is not proof of identical bytes.

**Intermediate inspection:** explicit capture policy (selected steps, at most
200 rows/100 columns per sample, 10 MiB total per run initially) with truncation
metadata. Clone bounded snapshots, never retain live mutable table pointers.
Show them as “Recorded sample”, not full output. A bounded sample does not reduce
the engine's full-table memory use. Running on a sample may change grouping/join
results; if later offered, call it “Run with sample inputs”, never “dry run”.

**Cancellation:** UI moves Running → Stopping immediately after native acceptance;
show Stopped only after process exit and artifact cleanup/quarantine are confirmed.
Distinguish cooperative stop, forced termination, and cleanup failure. Start with
a two-second cooperative grace period, then measure forced cleanup separately.
Add context
checkpoints to expensive table/read/write loops; preserve existing Unix process
groups and Windows Job Objects. Handle completion/cancel races with one terminal
state and a fenced artifact commit. Retrying is explicit. Cancellation is not
rollback of external side effects; local SQL/HTTP remain blocked, and service-side
retry/idempotency design is separate.

**Failure and output recovery:** write in per-run private staging and publish the
final workbook atomically. Current runner can delete a generated file when preview
inspection fails; separate “execution succeeded / preview unavailable” so a valid
artifact survives. Keep prior successful outputs. On startup, reconcile unfinished
jobs and clean only owned staging directories with no live owner. Hard process kills
may bypass Go defers; Rust/startup recovery owns residual cleanup. Never publish
partial XLSX/ZIP as successful. Report retained/quarantined files and disk-full or
permission failures with a retry/reveal action.

**Redaction:** structured safe error fields by default; scrub URLs/connection data,
paths where appropriate, and value-bearing messages before persistence/display.
Treat user `log` messages and engine row errors as potentially sensitive. Raw logs
are not a production-default escape hatch. Support bundles require a visible
content inventory and omit workbook/source contents by default.

## 9. Prioritized implementation phases and acceptance criteria

Effort is relative: **S** = roughly 1–2 engineer-weeks, **M** = 3–5, **L** = 6–9,
**XL** = 10–14. These are planning ranges, not commitments; assume familiarity with
React/Tauri/Go and include implementation/test work, with design/QA participation.
An indicative team is two engineers, a half-time designer and shared QA. Calendar
time depends on integration/review and cannot be inferred by simply dividing effort.

Dependencies: P0 → P1 → P2 → P3 → P4. P5 requires P2 and uses P3 diagnostics;
parameter/subflow work waits for its contract review. P6 uses P3 cancellation and
P2 profile persistence, and can overlap P4/P5. P7 gates release of each completed
slice; it is not permission to defer testing until the end.

### P0 — Baseline, contracts, and usability prototype (M)

- **Outcome/scope:** a tested design for the complete example, source-backed
  capability ledger, operation inventory and compatibility fixtures. Prototype
  narrow-window panes, readable blocks, inspector and keyboard outline.
- **Non-goals:** production UI rewrite, new workflow syntax, live integrations.
- **Affected components/contracts:** audit App, BlockEditor, StepInspector,
  WorkbookPanel, PackageBuilder, document reducer, runner/native protocol;
  write ADRs for CST authority, IDs, typed protocol, primitives and source drafts.
- **Prerequisites:** baseline checkout and synthetic fixture access.
- **Compatibility:** capture current examples, built-in BOM YAML, outputs and
  unsupported constructs before changing adapters. Record strict parser rules.
- **Acceptance:** every matrix row linked to evidence/owner; prototype completes
  worked journey without YAML; five exploratory participants expose priority
  obstacles; approve keyboard path and document/cancellation contracts.
- **Tests:** run existing gates to establish a fresh baseline; CST no-op/mutation
  fixtures, platform focus spike, Go observer feasibility, license review.
- **Risk/mitigation:** choosing abstractions before real tasks; use the example
  and current production YAML to bound decisions. Estimate refined at exit.

### P1 — First complete local workbench slice (L)

- **Outcome/scope:** create/open → bind → visually author → inspect input → validate
  → run → inspect output → fix → save in one shell. Add searchable insertion,
  contextual common-step forms, readable tokens, optional Data drawer and outline.
  Include templates and inline first-run guidance; preserve Package access.
  Include a narrow column picker for explicit first-row headers in the tutorial:
  use the existing inspected rows as suggestions, propagate known names through
  filter/calculate/group, and allow manual names with an unknown-schema label.
  General header inference, runtime types and advanced schema analysis wait for P4.
- **Non-goals:** tracing, persisted history, parameter grammar, fake progress or
  Cancel before backend support. Packaging implementation remains intact.
- **Affected components/contracts:** refactor App shell; reuse BlockEditor,
  WorkflowSetup, operationCatalog, StepInspector, WorkbookPanel/useDocument.
  Existing `validate`/`run`/`inspectWorkbook` and save IPC suffice for this narrow
  slice. Implement the minimal `WorkflowDocument` source/CST, command, draft and
  history owner now, with shared commands for supported steps. Durable IDs,
  external-source reconciliation and persisted recovery follow in P2; do not
  introduce a temporary second synchronization layer.
- **Prerequisites:** P0 vocabulary, design tokens and fixture contract.
- **Compatibility:** preserve existing YAML and both source/graph access through
  secondary commands; keep draft guards and external-revision saves.
- **Acceptance:** worked example yields Acme/50, Beta/5 from actual XLSX; no source
  editing needed; Data panel can be closed without losing selection; 980 × 680
  layout has no app overflow; complete core flow with keyboard only. Static
  validation and busy/final execution states are honestly labeled.
- **Tests:** supported-step serialization regression, component keyboard/focus,
  actual Go execution and XLSX/hash checks, production-CSP browser smoke, native
  Windows/Linux open/save/run. Test zero rows, missing inputs and invalid expressions.
- **Risk/mitigation:** inspector and blocks disagree; both commit through shared
  transactions and expose draft state. Do not expand every operation at once.

### P2 — Durable document identity and recoverable workspace (L)

- **Outcome/scope:** consistent selection/undo across views, persisted bindings,
  recovery checkpoints and clear save/conflict handling; metadata migrations.
- **Non-goals:** workflow autosave, collaborative merge, unrestricted code/visual
  conversion, executable dialect changes.
- **Affected components/contracts:** `useDocument` → document service; model IDs,
  Blockly CST adapters; native/workspace-core metadata and checkpoint APIs;
  native disk revision/conflict details. Introduce source draft Apply/Discard.
- **Prerequisites:** P1 command boundary and P0 source/identity ADR.
- **Compatibility:** metadata optional and hash-bound; YAML remains runnable by
  the current service. Rebuild ambiguous IDs rather than guess. Crash between
  YAML/metadata writes must leave valid YAML and recoverable draft.
- **Acceptance:** reorder preserves IDs; duplicate creates new IDs; cross-view
  undo restores content and meaningful selection; recover a detached block and
  invalid source after forced close; external edits never silently overwrite;
  unfamiliar metadata opens safely. Recovery status never claims file saved.
- **Tests:** reorder/duplicate/nested/source-change identity fixtures; crash fault
  injection at each persistence step; read-only/disk-full/path-escape/conflict
  tests; migration from missing/current/future metadata; missing key refs.
- **Risk/mitigation:** ambiguous source reconciliation; preserve old run revision
  maps, regenerate uncertain IDs, retain source recovery independently.

### P3 — Trustworthy jobs, errors, cancellation and final artifacts (XL)

- **Outcome/scope:** preflight with step-linked issues; real job stages, elapsed
  time and safe summaries; Cancel runs/builds with confirmed cleanup; recorded
  final run history and stale-input detection; preview failures preserve artifacts.
- **Non-goals:** intermediate capture, external side-effect rollback, live desktop
  connections, inferred percentages, automatic retry of a whole workflow.
- **Affected components/contracts:** api.ts/protocol schema; Rust lib/job registry,
  runner_process supervision; Go runner and workflow diagnostic/context hooks;
  Run/Issues panels and PackageBuilder job state. Retain v1 adapters.
- **Prerequisites:** P2 stable identity/revisions; reviewed job/terminal-state
  contract and private staging/retention policy.
- **Compatibility:** observer disabled by default for service/CLI; existing output
  semantics and error adapters preserved. New Studio negotiates runner capabilities
  and disables unsupported controls with a reason.
- **Acceptance:** errors navigate to exact field or clearly labeled source span;
  nested SQL/HTTP preflight blocks local run; stop acknowledgment target <250 ms,
  cooperative stop target <2 s, forced process-tree termination target ≤5 s after
  grace expiration; one terminal state; no published partial artifact. Valid output
  remains revealable when too large to preview. History identifies exact run inputs.
- **Tests:** cancellation during read/transform/write/build/stdin stall; child
  descendant cleanup on Windows/Linux; cancel/finish race; process crash; disk
  full; preview limit failure; stale input mutation; redaction canaries; protocol
  version mismatch/oversize/event backpressure. Golden engine results unchanged.
- **Risk/mitigation:** long loops ignore context and job events overwhelm UI;
  checkpoints plus existing force-kill supervision, bounded queues and throttling.

### P4 — Data-aware authoring and bounded intermediate inspection (L)

- **Outcome/scope:** schema-aware column pickers, better expression help and
  recorded selected-step samples; honest operation input/output contracts.
- **Non-goals:** full Excel styling/editing, recalculation, automatic preview runs,
  fully streaming execution, arbitrary data diffs.
- **Affected components/contracts:** Go operation/function metadata, optional
  observer and bounded immutable samples; typed cell/column inspection;
  expression/column inspector controls and Data drawer.
- **Prerequisites:** P3 jobs/redaction/history and P2 IDs; performance baseline.
- **Compatibility:** unknown schema/expression remains supported through preserved
  source; sample collection must not alter execution or leak into service outputs.
- **Acceptance:** filter/set/group example has accurate schema transitions; sample
  counts/truncation disclosed; selecting step never starts execution; samples obey
  per-run budget; uncommon names (commas, Unicode, spaces) selectable safely.
- **Tests:** observer-on/off identical results, sample mutation isolation, memory
  limits, stale schema after upstream edit, expression compilation parity, cached
  formula/type display fixtures and accessibility of large-data controls.
- **Risk/mitigation:** sampled inference misleads; label confidence and retain
  manual names. Paging hides memory cost; instrument actual parser/engine RSS.

### P5 — Broader low-code logic and deliberate reuse (XL; split into releases)

- **Outcome/scope:** expose engine-supported nested if/split and prioritized advanced
  table operations; then typed parameters and copy-on-insert fragments/templates.
- **Non-goals:** graph scheduling, general loops, arbitrary scripts, automatic
  fragment updates, general try/catch or callable subflow runtime.
- **Affected components/contracts:** Blockly/outline nested containers, catalog and
  typed inspector, Go parameter dialect/invocation validation, CLI/service adapters
  for opt-in parameters, package capability checks, fragment expansion/source maps.
- **Prerequisites:** P2 IDs/commands and P3 structured validation; P4 schema contracts
  for useful column help. Parameter ADR approved before changing engine grammar.
- **Compatibility:** first ship visual coverage with existing engine semantics;
  parameter dialect is explicit and gated by minimum engine capability. Existing
  workflows never migrate silently. Fragments expand to ordinary steps and names.
- **Acceptance:** branch fixtures match current engine behavior and schema merge
  rules; parameter types/defaults checked in Go and UI; old engines fail clearly;
  inserted fragments rename colliding datasets/IDs safely and survive source editing.
- **Tests:** nested branch/map ordering golden tests, branch-specific diagnostics,
  parameter coercion/injection/version tests, service/CLI/Studio parity, name
  collision/opaque fragment cases, all source/visual preservation fixtures.
- **Risk/mitigation:** scope balloons into a language redesign; deliver coverage,
  parameters, then fragments independently. Defer constructs without clear semantics.

### P6 — Predictable packaging and security lifecycle (L; backup flow separately sized)

- **Outcome/scope:** persistent Package review, recoverable prerequisite/key errors,
  public-key export, multiple identity references/rotation, recipient guidance,
  verified job status and cancellation; explicit backup/recovery UX as specified.
- **Non-goals:** remote deploy, fresh-installer replacement, in-place upgrades,
  automatic rollback, claiming cross-builds were executed on target hardware.
- **Affected components/contracts:** PackageBuilder/profile persistence, native
  credential commands, existing internal/deploy, independent verifier docs and
  public trust material; P3 job transport for builds. Native backup is gated on
  reviewed secure prompting/container support and has a separate estimate.
- **Prerequisites:** P2 profile migration and P3 jobs; native OS key-store tests and
  operator trust/rotation design. Does not block the local-workflow pilot.
- **Compatibility:** keep ZIP format, Ed25519/age and fresh `/opt` installer behavior;
  version any manifest extension. Continue single-instance PM2 and independent
  pre-extraction verification. No destructive key replacement on profile import.
- **Acceptance:** repeat secure build reuses identity/recipient without ceremony;
  missing key blocks without downgrade; old/new public keys verify corresponding
  packages; Cancel cleans staging; artifact card separates compiled, integrity
  verified, runtime tested, and server setup untested. No private key reaches UI.
- **Tests:** locked/missing/corrupt key, wrong recipient, backup/import if enabled,
  rotation trust rejection, cancelled build descendants, failed output publish,
  malicious archive/path, independent verifier, PM2 template/fresh install; actual
  amd64/arm64 runtime testing clearly recorded or marked untested.
- **Risk/mitigation:** convenience hides trust assumptions; permanent public
  fingerprint/security summary and exact server-side verification instructions.

### P7 — Pilot, performance, accessibility and safe application release (M)

- **Outcome/scope:** measured, documented rollout of completed slices, trustworthy
  fallback and desktop release notes; analyst/operator tutorials and support guide.
- **Non-goals:** service auto-update/rollback or enterprise authentication rollout.
- **Affected components/contracts:** release configuration, local feature switches,
  schema migration fixtures, help/docs, test harnesses; no new execution semantics.
- **Prerequisites:** required slice gates passed; P1 can pilot before later phases.
- **Compatibility:** release binary rollback leaves untouched legacy YAML runnable;
  back up before explicit schema migration; old app uses read-only/copy behavior
  for newer projects. Feature switches cannot downgrade a document's semantics.
- **Acceptance:** usability and performance targets below measured on named machines;
  keyboard/screen-reader core journey passes; no unresolved data-loss, silent
  rewrite, unauthorized-path or secret-exposure defects; Windows/Linux release
  smoke passes; known limitations and restore instructions published.
- **Tests:** supported platform matrix, migration/restore rehearsal, manual assistive
  technology, signed installer testing when distribution is offered, pilot workbook
  fixtures and resource stress. Do not call a source build a validated installer.
- **Risk/mitigation:** headless success masks real desktop issues; native webview
  and user tests are separate gates, not replaced by browser screenshots.

## 10. Testing, performance targets, risks, and unresolved decisions

### Regression and validation strategy

Preserve existing Go checks, frontend tests, production-CSP browser smoke, real
Blockly-to-Go/XLSX integration, Windows watcher regression, and native supervision
tests. Use the repository's documented commands in
[the development guide](MAI_STUDIO_DEVELOPMENT.md); do not substitute mock-only UI
tests for actual engine behavior.

| Layer | Required evidence |
| --- | --- |
| Serialization/document | No-op byte preservation; visual → YAML → visual semantic equivalence for supported shapes; unknown options/comments/order; invalid source recovery; opaque expressions/nested shapes; alias/duplicate rejection; reorder/duplicate/undo identity; fixture BOM unaffected. Use generated edits only where invariant testing adds value. |
| Engine/protocol | Golden workbooks, structured issue paths, optional observer neutrality, offline nested restrictions, parameter version compatibility, frame/event budgets, redaction and process cancellation. |
| Workspaces/native | File conflicts, symlinks, permissions, atomic replacement, crash between generations, checkpoints of invalid drafts, future metadata rejection, Windows locked-file behavior. |
| End to end | Worked example with actual XLSX values; missing/replaced input, empty output, row-dependent error, reopen/save, view switches, cancel during each stage, crash/recover, preview failure with valid output, build/verify/cancel. |
| Accessibility | Automated checks plus keyboard-only, Windows NVDA/WebView2 and Linux Orca/WebKit manual sessions; focus at 200% zoom, drag alternatives, reduced motion, table paging, long labels and error announcements. macOS VoiceOver when macOS becomes supported. |
| Usability | P0 exploratory five users; P1/P7 ten representative new users including analysts, plus experienced authors/operators. Measure task completion, time, wrong turns, error recovery and trust comprehension. Small samples guide iteration, not statistical certainty. |
| Security/package | No private signing/decryption keys or connection credentials sent through product IPC to the webview; no secret values in logs or exported recovery/support data. Private workbook/source data may be displayed and recovered locally but must not be exported automatically. Test scoped artifacts, cancel cleanup, independent verification, key failure/rotation, fresh Linux install and PM2 smoke. Live DB/SAP remains a separate test suite requiring fixtures/environment. |

### Proposed performance and usability budgets

Reference hardware for initial measurements: Windows 11, Intel Core i5-1135G7
(4 cores/8 threads), 16 GB RAM, NVMe SSD, 1920 × 1080 at 100% and 150% scaling,
WebView2 version recorded. Repeat Linux checks on an equivalently provisioned
Ubuntu 24.04 machine, recording WebKit/GTK versions. Pin app/toolchain revisions;
measure cold and warm runs separately, five trials minimum for task benchmarks,
and enough interaction samples for p95. These are acceptance targets to validate.

| Scenario | Target / measurement boundary |
| --- | --- |
| First-run example | ≥80% unassisted completion within 10 min; ≥80% identify output and recover the missing-file issue within 2 min; no YAML required. |
| Routine editing | 200-step workflow: p95 visible response <100 ms for selection/insertion; sustained pan/scroll aims ≥50 fps; 1,000-step stress case remains navigable using outline/collapsed groups, not a promise of full-canvas smoothness. |
| Validation | Local 200-step, ≤100 KiB workflow without workbook scan: p95 <500 ms after warm runner startup. Report startup/inspection separately. |
| Data | 10,000 × 20 and 100,000 × 20 synthetic cells, plus 10,000 × 100 wide case; each within current 25 MiB file / 64 MiB expansion limits. Include numeric/text/Unicode/blank/multi-sheet fixtures. Record compressed and expanded bytes. |
| First data page | Small fixture <1 s warm; 100,000 × 20 <5 s cold; subsequent 100-row pages <250 ms warm, likely requiring a bounded cache/reader change. UI input remains <100 ms while parsing is off-webview. |
| Local execution | Worked example <2 s warm; filter/calculate/group 100,000 × 20 target <15 s, process-tree peak RSS <1 GiB. Baseline may fail; optimize measured bottlenecks before increasing limits. |
| Cancellation | UI acceptance <250 ms; cooperative termination <2 s where checkpoints operate; force termination ≤5 s after grace expiry; verify no child remains and no partial artifact is published. |
| Recovery/history | Checkpoint ≤2 s after inactivity, reopen latest checkpoint <2 s for 200 steps; browse 100 retained run manifests <500 ms warm. Default proposed retention: 20 runs / 1 GiB, show usage and exclusions, never delete in-use artifacts. |

Do not assume cell count implies file/expansion size. Over-limit fixtures must fail
early with an actionable message and no UI freeze. Measure webview, Rust, Go and
descendant memory separately and in total. Current full-table execution/paged
inspection is not bounded-memory streaming; if targets require redesign, scope
that engine change explicitly rather than raising file limits unnoticed.

### Rollout and rollback

Ship the new shell behind a local release flag during pilot. Maintain the legacy
presentation temporarily against the same document/engine; stop accepting new
independent behavior in it. First release changes no workflow dialect. Remove the
old presentation only after task and compatibility gates pass.

Before any metadata/dialect migration, create a private backup and validate the
new copy before activation. A failed migration leaves the original usable.
Application rollback means reinstalling the previous desktop version and opening
an unchanged legacy document or restored backup; newer dialect features cannot
be losslessly downgraded by hiding them. This is separate from the absent remote
deployment, server upgrade, and automatic service rollback features.

### Principal risks and decisions to resolve in Phase 0

| Risk / decision | Recommendation and resolution gate |
| --- | --- |
| Blockly accessibility / large canvases | Keep it with a first-class keyboard outline and shared inspector; test real assistive tech before expanding nested UI. Replacement only if the combined approach fails tasks. |
| Stable IDs across arbitrary YAML rewrites | Hash-bound metadata plus conservative reconciliation; ambiguous IDs regenerate. Never claim unrestricted bidirectional identity preservation. |
| Incorrect type/schema hints | Label inferred/unknown; authoritative Go validation; no automatic column coercion based only on Excel formatting. |
| Cancellation during uninterruptible library work | Cooperative checkpoints plus existing native process-tree termination, staged outputs and startup reconciliation. Measure each phase on both platforms. |
| Run snapshots consume disk / contain private data | Private retention budget, explicit input retention choice, no automatic Git/support export; stop on quota failure rather than weakening reproducibility. |
| Redaction cannot safely classify arbitrary messages | Structured allowlisted summaries first; sensitive raw output withheld by default; canary tests and manual review before enabling log export. |
| New parameter language complicates deployment | Separate P5 gate; versioned engine support and package checks; existing workflows unchanged. |
| Native backup prompting and crypto container | P6 spike selects maintained implementation and tests all target OSes; public export/recovery guidance ships independently if secure private backup is not ready. |
| Sample data / reference hardware differs from users | Validate size budgets with representative local fixtures during pilot; do not infer large-workbook capability from the tiny tutorial. |
| Limited team capacity | Fund P0–P3 first. P4–P6 can follow without blocking a useful complete local workbench. |

No open question blocks this plan. The remaining choices are explicit engineering
spikes with gates; they do not justify adding cloud accounts, live connections,
new scripting runtimes, or remote deployment to the redesign.

## 11. Keep / improve / replace / defer versus the original plan

| Decision | Original direction | Revised recommendation |
| --- | --- | --- |
| **Keep** | Tauri/React, Go production semantics, local files, actual-XLSX preview, scoped native I/O | Reuse implemented foundation and regression assets; incremental modules, no rewrite. |
| **Keep** | Secure one-click Linux package and PM2 | Preserve delivered Ed25519/age format, independent verifier and fresh-target installer; state runtime-test limits. |
| **Improve** | Canvas plus properties plus workbook panels | One contextual workbench, readable blocks, searchable insert, optional data drawer, keyboard outline and clear next action. |
| **Improve** | Source preservation and unsaved-work protection | CST transactions, durable IDs, source draft boundary, revision-linked issues, crash recovery and conflict comparison. |
| **Improve** | Proposed tracing/cancellation/security wizard | Explicit protocol/backend dependencies, measurable cleanup semantics, bounded samples and key lifecycle, delivered in separate slices. |
| **Replace** | React Flow as primary editor with broad parallel editing surfaces | Blockly Build as default; graph Overview for navigation/diagnostics; YAML as deliberate advanced editing. |
| **Replace** | Greenfield sequential foundation milestones | Existing-product migration: complete local slice early, then recovery/jobs/data/reuse/packaging polish. |
| **Replace** | Broad “what you see is exported” fidelity expectation | Exact generated artifact and value inspection first; enumerate limited styles/formula metadata separately. |
| **Defer** | Live Studio profiles, release symlinks/upgrades/rollback, broad production hardening bundled into UX | Separate scoped initiatives; keep local SQL/HTTP restrictions and current installer contract. Preserve service limitations visibly. |
| **Defer** | Any implication of universal visual/code conversion or enterprise cloud requirements | Opaque safe fallback; single-user offline remains complete. No accounts, marketplace or arbitrary scripting runtime. |

First implementation commitment after plan approval: **P0 and P1**, with the worked
example as the acceptance anchor. P2/P3 establish reliable recovery and execution
before richer authoring increases complexity. Approval of this document does not
claim the proposed capabilities are implemented.

### Implementation progress — first workbench slice, 29 October 2026

Delivered an initial P1 slice: Build with Blocks/Step outline, searchable operation
insertion, shared inspector and document transactions, conservative column
suggestions, a next-action strip, automatic final-output inspection, and the
Orders totals tutorial with real Go/XLSX verification. Existing workspace guards,
advanced views, offline mode and packaging remain available.

This is not completion of P0–P7: durable IDs/recovery, source Apply/Discard
semantics, full accessible expression authoring, richer schema contracts,
structured execution diagnostics/cancellation, intermediate samples and key
lifecycle work remain planned. Usability/performance targets remain unmeasured.
