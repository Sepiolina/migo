# Mai Studio UX and implementation roadmap

**Start with one analyst task.** Open the BOM workflow, select step 15, jump to `EffPartNumber`, and see which blank values were filled. Then open the generated workbook and check the formatting a recipient will see. These tasks give “feels like enterprise software” an observable meaning: the analyst can find the relevant data, understand its state, and verify an output independently.

This proposal is based on Migo revision `6294d2b`, the Studio code, and a Chromium session using the repository's desktop bridge and real Go runner. In the captured BOM editor, step 15 of 43 showed six rows and 23 columns. The inspector named the effective columns while the grid opened on the original ones. The same screen showed **Valid** and **Problems 1** for an informational note about offline services. The walkthrough covered the browser-hosted editor. Native keyboard handling and visual agreement with Excel need their own checks.

## Delivery order

| Release | Work to deliver | Demonstration that unlocks the next release |
| --- | --- | --- |
| 1. Navigate and trust the preview | Spreadsheet navigation, current-result status, first pass on layout and keyboard access | An analyst jumps to a named column, copies a range, and can tell whether the displayed result belongs to the current edit. |
| 2. Understand a change | Before/after for fill and filter, guided sample binding, initial performance improvements where measured | Starting in a new workspace, an analyst binds two files and identifies a changed cell and a removed row. |
| 3. Verify the output | Styled workbook viewport followed by run history | An analyst verifies a styled cell in the actual output and can reopen the same run after restarting Studio. |
| 4. Explain and maintain the workflow | Interactive diagram and SVG export, then workflow test execution in Studio | A maintainer exports a readable overview and demonstrates a passing and a failing fixture. |

Draft recovery can ship independently after the Release 1 document-state work. Accessibility and performance checks start in Release 1 and apply to every later view. These are proposed release boundaries. The workbook renderer and large-file measurements are the two decisions to resolve before estimating a delivery date.

## Release 1. Navigate and trust the preview

### 1. Spreadsheet navigation

**Current behavior.** The grid already has sticky column headings, a sticky row-number gutter, and menus that turn column actions into workflow edits. Its scroll region can receive focus. It lacks the active-cell and range-selection behavior needed to move, orient, and copy like a spreadsheet.

1. In `DataGrid.tsx`, represent the active row and column separately from the selection anchor. Use a single grid tab stop. Clicking a cell selects it, arrow keys move it, Shift+Arrow extends the selection, and Tab can leave the read-only grid. Scroll the newly active cell into view on both axes. Keep menu and text-field keystrokes local to those controls.
2. Add a compact address field and column letters alongside meaningful field names. Define step-table addresses as preview coordinates starting at A1 for the first data cell. Workbook mode uses the physical worksheet addresses, including headers and blank rows. For a request outside a sampled step preview, show the available range. For a paged workbook, load the requested range.
3. Route the inspector's **Go to column** action to the same selection handler. Retain existing controls that insert column names into expressions. Offer column profiling as a view preference and keep headers usable with profiling collapsed.
4. Copy the selected rectangle as tab-separated values, preserving empty cells, multiline text and unicode. Keep selection visible on clipboard failure and allow a retry. Restore focus to the invoking header or cell after closing a column menu.

**Acceptance.** From step 15, reach `EffPartNumber` without horizontal searching. Select and copy three values using the keyboard. Test a table wider than Z, the final row, an empty table, and changing steps when the previous selection exceeds the new dimensions. Cover clipboard contents and an actual scroll in Chromium, then repeat keyboard navigation in the packaged desktop app.

### 2. Preview correctness and diagnostics

**Current behavior.** `App.tsx` debounces trace and validation. Its trace effect discards late completions, but some consumers still read the previous trace while a new one is pending. Validation does not guard an in-flight completion after a newer edit. `Console.tsx` counts informational entries alongside warnings and errors.

1. Give each result an identity appropriate to the operation. Validation follows the workspace and YAML. A trace follows the workflow, sample revisions, skipped steps, and focused step. Bind inspections to the file revision, sheet, and requested window. Keep a generation counter as well as an identity so a delayed response from an earlier visit cannot supersede a newer request.
2. Supply the navigator, status badge, inspector, grid, and console with the same current-result selector. Label a retained grid **Updating** and suppress stale error badges. If the latest request fails, show its error and a retry action. Apply the same identity check before a completed run changes the user's selected view.
3. Make **Problems** count actionable errors and warnings. Move the ordinary offline SQL/REST notice into preview details. Preserve actual failures and navigation to the failing step. Carry the runner's structured error code through `api.ts` alongside display text.

**Acceptance.** Resolve two validation requests in reverse order, change a binding while tracing, and switch documents while a run finishes. Only the current document receives navigation and diagnostics. A failed current request enables retry. An informational-only preview has a zero problem count.

### 3. Layout, density, and accessibility

1. Add keyboard-adjustable splitters for the workflow navigator and inspector. Set minimum widths from usable content, persist preferred widths locally, and clamp them when the window shrinks. Retain the existing compact layout. Keep the full selected step title visible above the table.
2. Group inspector controls by task. Put source binding first for input nodes and affected fields first for transformation steps. Move step deletion into a clearly labeled action group. Use existing theme tokens for selected, focused, updating, and invalid states. Pair color with text or shape.
3. Make shortcut labels follow the platform modifier used by the existing handlers. Restore focus after closing search, an inspector, or a dialog. Give the active cell an accessible row, column, and value. Announce a completed range jump once, without announcing every cell as the user scrolls.

**Acceptance.** At 200% zoom and in the existing compact layout, open a workflow, select a step, close a panel, and return to the same cell using only the keyboard. Check light and dark themes and one supported desktop screen reader. Extend the existing Studio smoke journey with focus and horizontal-overflow assertions.

## Release 2. Understand a change

### 4. Before/after for fill and filter

1. Extend the Go trace response with an optional, bounded comparison for the focused operation. For fill, retain the targeted columns and a sample of their pre-step values before the operation mutates the table. For filter, record included and rejected input row positions during the existing predicate evaluation. This preserves identity when two rows have identical values.
2. Return totals separately from sampled examples. For fill, report changed cell count and changed row count. For filter, report input, retained, and removed row counts. Keep collection within the existing runner frame limit and label truncated samples. Older requests continue to receive the existing trace.
3. Offer **Before**, **After**, and **Affected columns** on supported steps. Use a text-accessible change indicator. Preserve the active column when switching views. For grouped, skipped, or nested operations without their own trace, show the available parent or step summary.

**Acceptance.** Verify the selected BOM fill with a repeated blank and a value that stays blank. Filter a fixture with duplicate rows and an all-removed case. Check the counts against the engine and confirm comparison data never changes the generated output. Account for the frame size with a fixture containing unusually long text.

### 5. Sample setup and repair

**Implemented in Studio.** The Chromium smoke test uses the Go runner to inspect a CSV and a two-sheet XLSX, bind both BOM inputs, and repair a moved priority workbook while keeping step 15 selected. A separate editor test opens the same workflow path in two workspaces and checks which sample each one restores. The smoke test simulates the native file picker. [User guide](MAI_STUDIO_USER_GUIDE.md#set-up-or-repair-sample-files)

**Problem.** The BOM needs two files before the first meaningful preview. Studio already supports per-source Browse, path entry, template samples, and saved bindings. The improvement is making readiness and recovery visible in one place.

1. In Home and the editor's empty state, expose **Set up samples** with one row per declared input. Reuse the current picker, copy-into-workspace behavior, and workspace/workflow binding key. Display the selected relative path, the available sheet names if inspection succeeds, and the next unresolved input.
2. Distinguish an unbound input, a missing saved file, an unreadable workbook, and a missing required sheet when the workflow declares a literal sheet. Keep dynamic sheet names for the runner to resolve. Offer **Locate file** in the affected row. Preserve valid bindings while repairing another.
3. After the last input becomes ready, return to the previously selected step and request one preview. A cancelled picker keeps the prior binding. In browser-only mode, keep template editing available and explain that reading local data requires desktop Studio.

**Acceptance.** Bind the BOM and priority files, reopen the workflow, move one file, and repair just that input. Verify inspection of a CSV and a multi-sheet XLSX. Check that choosing a different workspace with the same workflow filename restores only that workspace's bindings.

### 6. Preview speed and large-sheet navigation

**Implemented in Studio.** Focused step revisits reuse a bounded session cache. Source previews can jump to a physical worksheet row, and XLSX inspection retains a bounded page. The performance report covers both fixtures, the timing methodology, and the release budgets. [Benchmark and results](MAI_STUDIO_PREVIEW_PERFORMANCE.md)

**Evidence.** Selecting a different step changes the trace key. The Go trace loop executes the top-level steps and profiles the current table at each one. Workbook inspection accepts offsets, but reads the sheet rows before slicing the requested page. The 200-row UI preview limits display size while larger inputs can still affect CPU and memory.

1. Measure cold open, switching to an unseen step, revisiting a step, and jumping to a distant worksheet row. Use the small BOM and a larger synthetic fixture generated with `scripts/generate-orders-fixture.py`. Record machine, fixture dimensions, p50/p95 latency, runner invocations, and peak memory. Measure runner time separately from grid rendering.
2. If repeat traces dominate, cache focused traces by the identity in item 2 with a byte bound and least-recently-used eviction. Start in the existing Studio session. Invalidate on workspace switch, YAML edit, skip change, or input-content change at the same path. Coalesce identical in-flight requests. Measure fingerprint costs on a warm visit before choosing a digest strategy.
3. If workbook reads dominate, add a bounded row/column window reader for XLSX and reuse the workbook metadata for nearby pages. Preserve actual populated cells beyond a stale dimension tag. If DOM work dominates, virtualize the grid while preserving logical row numbers, clipboard ranges, and focus as elements recycle.

**Acceptance.** A warm revisit makes no new runner call on a cache hit. A replaced sample produces fresh values. Rerun the same benchmark and report before/after latency and memory. Set numerical release budgets from that baseline and test them on the same class of machine. Ship only the optimization supported by the measurement.

## Release 3. Verify the output

### 7. Styled workbook preview

**Implemented in Studio.** Successful XLSX runs open the saved workbook with sheet tabs, an A1 address field, and the output's formatting. The reference check covers both sheets of the sample BOM. The fidelity report records the independent comparison, the LibreOffice Calc reference, and the remaining Microsoft Excel visual check. [Workbook fidelity report](MAI_STUDIO_WORKBOOK_FIDELITY.md)

**Fidelity decision.** Step tables contain intermediate data. The workbook preview opens the file written by the run. In `internal/workflow/excel.go`, the writer evaluates BOM row rules, merges styles, and assigns cell style IDs. Reading those saved styles gives Studio the same blue, red, or yellow cells that the writer emitted. Reimplementing the BOM rules in React would create a second source of truth.

1. Generate a reference XLSX from a deterministic fixture. Inventory sheet order, addresses, displayed numbers, widths, header fonts/fills, alignment, and examples of differently styled data cells. Confirm which BOM rows actually exercise each style. Create a small targeted fixture for any missing branch. Record a desktop Excel reference at a fixed zoom and font for manual visual comparison.
2. Prototype the current grid rendering a bounded rectangle from an additive runner response. Include sheet metadata, physical coordinates, formatted text, and a deduplicated style dictionary. Compare it with an embedded workbook renderer using the same file. Evaluate actual style coverage, keyboard behavior, payload size, offline availability, redistribution terms, and bundle impact. Prefer extending the grid if it satisfies the fixture.
3. Introduce **Workbook preview** from a successful run. Add sheet tabs, an address field, and the navigation from Release 1. Show the run time and output filename. When inputs or YAML change, show **Previous run** and **Run to refresh**. CSV opens in the data viewport. If output generation succeeds and preview fails, retain the output and display the preview error separately.
4. Page requests through the native workspace-scoped bridge. Keep replies within the existing 1 MiB runner frame and observe the current workbook size limit. Display unsupported formatting in preview details so a user knows what needs checking in Excel.

**Acceptance.** Compare displayed values, column widths, header font and fill, blue priority cells, and red flagged cells against the saved workbook. Test a blank styled cell, stale dimensions, multiple sheets, a failed preview, and a same-path sample replacement. Compare formatting properties automatically and review the fixed reference visually. Pixel agreement can vary with installed fonts and zoom.

### 8. Run history and output provenance

**Implemented in Studio.** The workspace Runs panel reopens saved outputs and compares a selected run with the open workflow. New runs publish a versioned manifest alongside the artifact. [User guide](MAI_STUDIO_USER_GUIDE.md#reopen-an-earlier-run) · [Runner protocol](MAI_STUDIO_DEVELOPMENT.md#run-manifests-and-listing)

**Problem.** Local runs already create unique directories and return an output path and SHA-256. The editor holds the latest result in memory, and reopening another workflow resets it. Analysts need to answer “Which file did I actually check?”

1. Write a versioned run manifest atomically alongside each successful output. Record completion time, workspace-relative workflow and output paths, YAML digest, input names and fingerprints, and the existing output digest. Keep workbook contents out of the manifest. Capture input identities at execution time and mark provenance uncertain if an input changes during the run.
2. Add a workspace-scoped run-list operation. Read manifests lazily and show older output directories as **Earlier run · metadata unavailable**. A malformed manifest affects only its own entry. Publish completed runs once. Handle missing outputs with a visible unavailable state.
3. Add a **Runs** panel showing workflow, time, and output. Opening a historical result loads its workbook. Use the stored identities to explain why it differs from the current document or bindings. Offer **Run current workflow** as a separate action.

**Acceptance.** Two runs with identical YAML and different samples remain distinguishable after restart. Reopening an old result leaves the current editor draft and selection intact. Test legacy output, a missing file, interrupted manifest publication, and overlapping runs.

## Release 4. Explain and maintain the workflow

### 9. Interactive diagram and SVG export

**Implemented in Studio.** Choose Diagram in the editor to explore the workflow and export the current expansion as SVG. [User guide](MAI_STUDIO_USER_GUIDE.md#explore-and-export-a-workflow-diagram)

1. Build a read-only projection from the existing pipeline model. Generate nodes for inputs, operations, and outputs. Show execution order explicitly. Add labeled edges for statically resolvable named-table reads/writes. Represent nested conditional steps as branches within their parent. Label unresolved references and avoid inferring dependencies from free text.
2. Start with a collapsed overview grouped by YAML section notes. Support expand/collapse, fit to view, zoom, keyboard traversal, and selection synchronized with the navigator and inspector. Returning to Table restores the selected step. Attach runtime counts only where the engine has a matching trace.
3. Export the current expansion state as standalone SVG with a title, legend, inline styles, escaped labels, and a view box covering all visible nodes. Include workflow structure by default. Keep sample values and local paths out of the export. Derive positions deterministically so repeated exports remain comparable.

**Acceptance.** Open the 43-step BOM without samples, expand the section containing step 15, select it, and switch back to Table. Export both overview and expanded diagrams and open each offline. Test blank workflows, nested branches, long labels, and labels containing XML characters. Expanding or exporting leaves YAML and undo history untouched.

### 10. Workflow regression tests from Studio

**Implemented in Studio.** Open a saved workflow and choose Tests to run its suites. [User guide](MAI_STUDIO_USER_GUIDE.md#run-workflow-regression-tests) · [Runner protocol](MAI_STUDIO_DEVELOPMENT.md#workflow-regression-suites)

**Opportunity.** Migo already supports `*.test.yaml` and `migo test examples`. The repository includes both an expected workbook and an expected-error case. A Studio test panel can reuse the same test engine and fixtures used in CI.

1. Add workspace-scoped discovery and read-only execution commands for saved workflows. Reuse `internal/wftest` discovery, loading, offline execution, and comparison. Resolve suite paths relative to the test file and verify that workflow, inputs, stubs, and expected files fall inside the selected workspace. Apply cancellation and a runtime bound through the runner.
2. Present suite and case names, duration, and outcome. On spreadsheet mismatch, return structured sheet, cell, expected value, and actual value; retain the CLI's current human-readable error. For CSV show the first differing line. For an expected-error case display the expected message fragment and actual outcome.
3. Make the tested revision explicit. If the open document is dirty, offer **Save and run tests** or **Test saved version**. Keep ordinary test execution read-only with respect to golden files. The existing CLI update command remains available for deliberate fixture changes.

**Acceptance.** Run the orders tutorial's passing and expected-error cases from desktop Studio and the CLI. Introduce a discrepancy in a temporary copy, confirm both identify the same mismatch, and verify the expected workbook's hash stays identical. Check a missing fixture and cancellation. Workbook appearance is tested separately under item 7 because the current golden comparison checks cell values.

## Independent improvement. Recover drafts and handle save conflicts

Studio already has undo, dirty-state guards, and revision-checked saves. A local recovery draft covers a crash or interrupted session. A conflict screen makes the existing revision check actionable when another editor has saved changes.

1. After edits settle, write a versioned local draft containing workflow identity, base revision, source, and timestamp. Include untitled templates using a draft ID. Keep recovery separate from production YAML and workspace sample data. Avoid persisting stale drafts from late writes by sequencing writes per document. Define an expiry policy and expose **Discard recovery copy**.
2. On reopen, offer **Restore draft**, **Open saved version**, and a comparison. A matching base revision can restore directly into dirty editor state. If the file changed externally, display saved and draft versions with **Save copy** and **Reload disk version**. Preserve existing revision checks on every overwrite. Treat tab/instance identity separately so concurrent editors retain their own drafts.
3. Clear the matching draft after a confirmed save or deliberate discard. If new edits were made while the save was pending, retain the newer recovery copy. Handle unavailable draft storage with a visible status while keeping manual save available.

**Acceptance.** Simulate an abrupt close after the draft write, a disk edit in a second instance, and a save that completes while the user continues typing. Recover the correct text in each case. Verify explicit discard stays discarded and opening an unrelated workspace reveals none of the prior workspace's drafts.

## Verification and rollout

| Layer | Checks for each relevant increment |
| --- | --- |
| Go workflow engine and runner | Extend trace, inspection, and run tests with bounded fixtures. Check older request shapes, oversized replies, missing files, and cancellation. Use the repository's `make check` before release. |
| React editor | Run `npm test` in `apps/studio`. Add focused regressions for request ordering, range selection, dirty drafts, and shared diagram/table selection. Keep YAML round-trip and selection-aware undo checks passing. |
| Native host | Extend command authorization, revision checks, and path-scoping tests for new operations. Run the workspace-core and Tauri Rust suites on supported hosts. Exercise native dialogs, clipboard, and platform shortcuts in the packaged app. |
| End to end | Build Studio and extend `scripts/smoke-studio.mjs` for BOM navigation, workbook inspection, and diagram export against the real runner. Extend `scripts/smoke-runner.mjs` when the protocol changes. Keep browser-only template editing in the test matrix. |
| Usability | Before Release 1, observe an analyst binding both samples and finding a changed part number. Before Releases 3 and 4, observe verification of a styled output and explanation of a workflow section. Repeat with the same task wording after delivery. Record time, unassisted completion, and the step where help was needed. |

**First implementation ticket.** Add keyboard cell selection to the current grid and a jump action for `EffPartNumber`. Demonstrate it using the existing BOM smoke fixture with the inspector open. This is small enough to review independently and gives the team a working interaction against which to judge later layout and rendering decisions.

## Repository references

- Current architecture, development commands, and desktop boundaries. [Studio development guide](MAI_STUDIO_DEVELOPMENT.md)
- Existing features and end-user terminology. [Studio user guide](MAI_STUDIO_USER_GUIDE.md)
- Grid rendering, preview coordination, and console severity. [DataGrid](../apps/studio/src/DataGrid.tsx) · [App](../apps/studio/src/App.tsx) · [Console](../apps/studio/src/Console.tsx)
- Document history and revision-checked filesystem saves. [Document hook](../apps/studio/src/useDocument.ts) · [Workspace core](../apps/studio/src-tauri/workspace-core/src/lib.rs)
- Trace sampling, workbook creation, and runner limits. [Trace](../internal/workflow/trace.go) · [XLSX writer](../internal/workflow/excel.go) · [Studio runner](../internal/studio/runner.go)
- Regression testing and end-to-end fixtures. [Workflow test engine](../internal/wftest/wftest.go) · [Orders suite](../examples/tests/orders-totals.test.yaml) · [Studio smoke test](../apps/studio/scripts/smoke-studio.mjs)
