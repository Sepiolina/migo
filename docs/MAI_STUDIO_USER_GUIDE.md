# Mai Studio user guide

Mai Studio edits YAML workflows and runs them locally with the production Go
engine. This milestone supports offline Excel fixtures. SQL and external REST
connections are disabled.

## Start and open a workspace

From the repository root:

```sh
cd apps/studio
npm ci
npm run desktop:dev
```

Install the native prerequisites listed in the
[development guide](MAI_STUDIO_DEVELOPMENT.md) first. `npm run dev` opens an editor
preview in a browser; folder access, saving, validation, workbook inspection, and
execution require the Tauri desktop app.

Choose **Open workspace** and select a local project folder. All workflow and
workbook paths are relative to that folder. For the bundled example, select the
repository root. Use synthetic inputs while developing; keep private workbooks,
credentials, and generated runs out of source control.

## Open, create, and save workflows

The **Open a workflow…** list discovers `.yaml` and `.yml` files recursively inside
the workspace's `workflows/` directory. It skips symlinks. The initial document is
a built-in parts-summary example, ready to edit in **YAML source**.

To create a workflow, edit that document or replace its YAML, enter a new
workspace-relative **Save path**, and choose **Save workflow**. For example, use
`workflows/summary.yaml`; create the `workflows` directory outside Studio first if
it does not exist. Parent directories must already exist. Saving to a different,
unused path creates a copy. An existing destination cannot be overwritten this
way. `Ctrl/Cmd+S` saves to the current Save path.

A dot and **Unsaved** mark changes since the last successful save or load.
Switching workflows or workspaces asks whether to discard those edits; choose
**Keep editing** to save them first. Save before closing the app. There is no
autosave or recovery store, and input bindings are session settings rather than
part of the saved YAML.

A save checks whether the file changed on disk since it was opened or last saved.
A conflict leaves your editor contents intact and reports an error in the status
bar. Preserve those contents in a new, unused Save path, then reopen the original
to compare the external changes. Studio has no merge dialog or force-overwrite
control. Discovery is refreshed when opening a workspace; reopen the workspace if
files added externally do not appear.

## Edit and inspect steps

Use **Workflow canvas** to see ordered execution and branches. Select a top-level
step to inspect its operation and edit its argument as a YAML scalar, mapping, or
list. **Apply argument** records the edit as one undoable change. Change the
operation itself in **YAML source**. Use the source editor for nested steps and
whenever the inspector does not offer argument editing, including shorthand or
multi-operation step mappings.

**Add step** appends a `filter: 'true'` step. Select a top-level step and use
**Delete step**, **Move up**, or **Move down** to change execution order. Moving a
compound step moves its branches with it. Canvas nodes cannot be dragged into a
new execution order or connected manually. YAML remains the executable source;
source errors should be corrected in the source editor.

**Undo** and **Redo** cover source edits and applied step changes. Loading a
document resets this history. Outside text fields, use `Ctrl/Cmd+Z` to undo and
`Ctrl/Cmd+Shift+Z` or `Ctrl/Cmd+Y` to redo. Other shortcuts outside text fields are
`Alt+Insert` to add, `Delete`/`Backspace` to delete the selected top-level step,
and `Alt+Up`/`Alt+Down` to move it. Text fields retain their normal text shortcuts.

## Validate, bind inputs, and run

1. Choose **Validate** to check the current editor YAML with the Go parser. Errors
   appear in the status bar. Validation populates the declared input fields.
2. Under **Input files**, bind each input name to an existing workbook inside the
   workspace. For the initial parts-summary example, bind `parts` to
   `examples/parts.xlsx`. Validate again after changing input declarations.
3. Choose **Run workflow**. The runner executes the current editor contents, so
   saving is a separate action. External SQL/REST operations cannot run offline.
4. Inspect the generated workbook in the read-only Excel workspace. Use the sheet
   selector and **Previous**/**Next** to page through rows. You can also enter an
   input workbook path and choose **Inspect workbook**.

Outputs are written under `.studio/runs/` in the workspace. The output path and
SHA-256 identify the generated XLSX artifact. The preview reads actual workbook
cell values; styling, formula metadata, and full Excel visual fidelity are not
shown. Open the artifact in a spreadsheet application for those details.

Editing YAML or changing input bindings marks the displayed output stale. Run
again to refresh it. Changes to workbook files made outside Studio are not
tracked automatically; rerun after changing them.

## Workspace limits and troubleshooting

Workflow files must be UTF-8, at most 1 MiB, and end in `.yaml` or `.yml`. Absolute
paths, `..` traversal, and symlinks in workflow paths are rejected. Workbook paths
must resolve to files inside the selected workspace. A selected workspace grants
local file access to that folder; choose a folder you trust.

If native commands are disabled, start `npm run desktop:dev` rather than the
browser preview and open a workspace. If an input cannot be read, check that its
path is relative to the workspace and the file exists there. If the sidecar
cannot start, verify Go is on PATH or set `MAI_GO` to its executable. Missing
GTK/WebKit libraries or a missing desktop session prevent the Linux native host
from starting; see the development guide for dependencies.

Intermediate execution traces, cancellation controls, live connections, and the
Linux service package builder are planned. The disabled **Build Linux package**
control does not generate a deployment package.
