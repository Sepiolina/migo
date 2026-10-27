import React, { useEffect, useMemo, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { ReactFlow, Background, Controls, MiniMap } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import "./style.css";
import { example, projectWorkflow } from "./model";
import { changeSteps, topLevelCount } from "./authoring";
import { useDocument } from "./useDocument";
import { StepInspector } from "./StepInspector";
import {
  isDesktop,
  request,
  type Inspection,
  type Validation,
  type RunResult,
} from "./api";
export default function App() {
  const document = useDocument(example);
  const { source, setSource } = document;
  const [selected, setSelected] = useState<string>();
  const [argumentDraft, setArgumentDraft] = useState<string>();
  const hasDraft = argumentDraft !== undefined;
  const [workflowPaths, setWorkflowPaths] = useState<string[]>([]);
  const [workflowPath, setWorkflowPath] = useState("");
  const [openPath, setOpenPath] = useState("examples/parts-summary.yaml");
  const [savePath, setSavePath] = useState("workflows/summary.yaml");
  const [revision, setRevision] = useState<string | null>(null);
  const [pendingDiscard, setPendingDiscard] = useState<() => void>();
  const [workspace, setWorkspace] = useState(""),
    [tab, setTab] = useState("graph");
  const [validation, setValidation] = useState<Validation>(),
    [bindings, setBindings] = useState<Record<string, string>>({
      parts: "examples/parts.xlsx",
    });
  const [inspection, setInspection] = useState<Inspection>(),
    [artifact, setArtifact] = useState<RunResult>();
  const [inspectPath, setInspectPath] = useState("examples/parts.xlsx"),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(false);
  const [message, setMessage] = useState(
      "Open a workspace to validate and run with the Go engine.",
    ),
    [runIdentity, setRunIdentity] = useState(""),
    [validatedSource, setValidatedSource] = useState("");
  const unsavedRef = useRef(false);
  unsavedRef.current = document.dirty || hasDraft;
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (unsavedRef.current) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", beforeUnload);
    let disposed = false;
    let unlisten: (() => void) | undefined;
    if (isDesktop()) {
      const currentWindow = getCurrentWindow();
      void currentWindow
        .onCloseRequested((event) => {
          if (!unsavedRef.current) return;
          event.preventDefault();
          setPendingDiscard(() => () => {
            void currentWindow.destroy().catch((error) => {
              setError(true);
              setMessage(String(error));
            });
          });
        })
        .then((stop) => {
          if (disposed) stop();
          else unlisten = stop;
        })
        .catch((error) => {
          setError(true);
          setMessage(String(error));
        });
    }
    return () => {
      disposed = true;
      unlisten?.();
      window.removeEventListener("beforeunload", beforeUnload);
    };
  }, []);
  const currentIdentity = JSON.stringify({ source, workspace, bindings });
  const graph = useMemo(() => projectWorkflow(source), [source]);
  async function task(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError(false);
    try {
      await action();
    } catch (e) {
      setError(true);
      setMessage(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy("");
    }
  }
  async function choose() {
    await task("Opening workspace", async () => {
      const selected = await invoke<{ path: string } | null>(
        "choose_workspace",
      );
      if (!selected) {
        setMessage("Workspace selection canceled.");
        return;
      }
      setWorkspace(selected.path);
      document.load(example);
      setArgumentDraft(undefined);
      setWorkflowPath("");
      setRevision(null);
      setWorkflowPaths([]);
      setValidation(undefined);
      setValidatedSource("");
      setSelected(undefined);
      const listing = await invoke<{ paths: string[] }>("list_workflows");
      setWorkflowPaths(listing.paths);
      setArtifact(undefined);
      setInspection(undefined);
      setMessage(
        "Workspace authorized. File paths are relative to this folder.",
      );
    });
  }
  function discardGuard(action: () => void) {
    if (document.dirty || hasDraft) setPendingDiscard(() => action);
    else action();
  }
  async function loadWorkflow(path: string) {
    if (!path) return;
    await task("Loading workflow", async () => {
      const loaded = await invoke<{
        path: string;
        source: string;
        revision: string;
      }>("read_workflow", { path });
      document.load(loaded.source);
      setArgumentDraft(undefined);
      setWorkflowPath(loaded.path);
      setSavePath(loaded.path);
      setRevision(loaded.revision);
      setSelected(undefined);
      setValidation(undefined);
      setValidatedSource("");
      setMessage(`Loaded ${loaded.path}.`);
    });
  }
  async function saveWorkflow() {
    if (hasDraft) {
      setMessage("Apply or discard the argument draft before saving.");
      return;
    }
    await task("Saving workflow", async () => {
      const saved = await invoke<{
        path: string;
        source: string;
        revision: string;
      }>("save_workflow", {
        path: savePath,
        source,
        expectedRevision: savePath === workflowPath ? revision : null,
      });
      document.markSaved(saved.source);
      setWorkflowPath(saved.path);
      setSavePath(saved.path);
      setRevision(saved.revision);
      setWorkflowPaths((paths) =>
        Array.from(new Set([...paths, saved.path])).sort(),
      );
      setMessage(`Saved ${saved.path}.`);
    });
  }
  const count = topLevelCount(source);
  const selectedIndex =
    selected && /^step\.\d+$/.test(selected)
      ? Number(selected.split(".")[1])
      : undefined;
  function mutateSteps(action: "add" | "delete" | "up" | "down") {
    if (hasDraft) return;
    try {
      setSource(changeSteps(source, action, selectedIndex));
      if (action === "add") setSelected(`step.${count}`);
      else if (action === "delete") setSelected(undefined);
      else if (selectedIndex !== undefined)
        setSelected(`step.${selectedIndex + (action === "up" ? -1 : 1)}`);
    } catch (e) {
      setError(true);
      setMessage(String(e));
    }
  }
  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      const typing =
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName) ||
        target.isContentEditable;
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        if (isDesktop() && workspace && !busy && savePath) void saveWorkflow();
      } else if (
        !typing &&
        !busy &&
        !hasDraft &&
        (event.ctrlKey || event.metaKey) &&
        ["z", "y"].includes(event.key.toLowerCase())
      ) {
        event.preventDefault();
        if (event.key.toLowerCase() === "y" || event.shiftKey) document.redo();
        else document.undo();
      } else if (
        !typing &&
        !busy &&
        !hasDraft &&
        event.altKey &&
        event.key === "Insert"
      ) {
        event.preventDefault();
        mutateSteps("add");
      } else if (!typing && !busy && !hasDraft && selectedIndex !== undefined) {
        if (event.key === "Delete" || event.key === "Backspace") {
          event.preventDefault();
          mutateSteps("delete");
        } else if (
          event.altKey &&
          event.key === "ArrowUp" &&
          selectedIndex > 0
        ) {
          event.preventDefault();
          mutateSteps("up");
        } else if (
          event.altKey &&
          event.key === "ArrowDown" &&
          selectedIndex < count - 1
        ) {
          event.preventDefault();
          mutateSteps("down");
        }
      }
    };
    window.addEventListener("keydown", keyboard);
    return () => window.removeEventListener("keydown", keyboard);
  });
  async function validate() {
    if (hasDraft) return;
    await task("Validating", async () => {
      const result = await request<Validation>("validate", {
        workspace,
        yaml: source,
      });
      setValidation(result);
      setValidatedSource(source);
      setMessage(
        `Validated ${result.name}: ${result.inputs.length} inputs, ${result.sheets.length} output sheets.`,
      );
    });
  }
  async function run() {
    if (hasDraft) return;
    await task("Running workflow", async () => {
      const result = await request<RunResult>("run", {
        workspace,
        yaml: source,
        inputs: bindings,
      });
      setArtifact(result);
      setInspection(result.preview);
      setRunIdentity(currentIdentity);
      setInspectPath(result.path);
      setMessage("Run completed. Preview reads the generated Excel artifact.");
    });
  }
  async function inspect(sheet?: string, offset = 0) {
    await task("Reading workbook", async () => {
      setInspection(
        await request<Inspection>("inspectWorkbook", {
          workspace,
          path: inspectPath,
          sheet,
          offset,
          limit: 100,
        }),
      );
      setMessage(
        "Workbook loaded. Cells are read-only; original inputs are preserved.",
      );
    });
  }
  const ready = isDesktop() && !!workspace && !busy;
  return (
    <div className="app">
      <header>
        <div className="brand">
          <b className="mark">M</b>
          <strong>
            mai<span>studio</span>
          </strong>
          <small>WORKFLOW LAB</small>
        </div>
        <div className="toolbar">
          <span className="mode">● Offline fixtures</span>
          <button disabled={!ready || hasDraft} onClick={validate}>
            ✓ Validate
          </button>
          <button
            className="primary"
            disabled={!ready || !!graph.error || hasDraft}
            onClick={run}
          >
            ▶ Run workflow
          </button>
        </div>
      </header>
      {!isDesktop() && (
        <div className="notice">
          Editor preview · Start the Tauri desktop app to open folders, inspect
          Excel, validate, and run workflows.
        </div>
      )}
      <main>
        <aside>
          <div className="eyebrow">WORKSPACE</div>
          <button
            className="folder"
            disabled={!isDesktop() || !!busy}
            onClick={() => discardGuard(() => void choose())}
          >
            ▣ {workspace ? "Change workspace" : "Open workspace"}
          </button>
          <p className="path">{workspace || "Choose a local project folder"}</p>
          <hr />
          <div className="eyebrow">WORKFLOWS</div>
          <select
            aria-label="Open workflow"
            disabled={!ready}
            value={workflowPath}
            onChange={(event) => {
              const path = event.target.value;
              discardGuard(() => void loadWorkflow(path));
            }}
          >
            <option value="">Open a workflow…</option>
            {workflowPaths.map((path) => (
              <option key={path} value={path}>
                {path}
              </option>
            ))}
          </select>
          <p className="hint">
            Browse workflows/ or open a workspace-relative YAML path.
          </p>
          <label>
            Open path
            <input
              aria-label="Workflow open path"
              disabled={!!busy}
              value={openPath}
              onChange={(event) => setOpenPath(event.target.value)}
            />
          </label>
          <button
            disabled={!ready || !openPath.trim()}
            onClick={() => discardGuard(() => void loadWorkflow(openPath))}
          >
            Open workflow path
          </button>
          <label>
            Save path
            <input
              aria-label="Workflow save path"
              disabled={!!busy}
              value={savePath}
              onChange={(event) => setSavePath(event.target.value)}
            />
          </label>
          <button
            disabled={!ready || !savePath.trim() || hasDraft}
            onClick={saveWorkflow}
          >
            Save workflow{document.dirty || hasDraft ? " •" : ""}
          </button>
          <hr />
          <div className="eyebrow">INPUT FILES</div>
          <p className="hint">
            Paths relative to workspace. Validate after changing declared
            inputs.
          </p>
          {(validation?.inputs ?? [{ name: "parts", label: "Parts list" }]).map(
            (input) => (
              <label key={input.name}>
                {input.label}
                <input
                  value={bindings[input.name] ?? ""}
                  placeholder="inputs/workbook.xlsx"
                  onChange={(e) => {
                    setBindings({ ...bindings, [input.name]: e.target.value });
                  }}
                />
                <span className="hint">{input.name}</span>
              </label>
            ),
          )}
          <hr />
          <div className="eyebrow">EXECUTION</div>
          <p>
            Local Go engine <small>OFFLINE</small>
          </p>
          <p className="hint">
            SQL and external REST connections are disabled in this development
            slice.
          </p>
          <div className="bottom">
            <div className="eyebrow">LINUX DEPLOYMENT</div>
            <p className="hint">
              Secure packaging and PM2 profiles are planned for the next
              milestones.
            </p>
            <button disabled>Build Linux package · Planned</button>
          </div>
        </aside>
        <section className="workspace">
          <div className="documentbar">
            <strong>
              ◇ {workflowPath || validation?.name || "Untitled workflow"}
              {document.dirty || hasDraft ? " • Unsaved" : ""}
            </strong>
            <span className="state">
              {validatedSource === source
                ? "✓ Validated"
                : "● Edited · validate to check"}
            </span>
          </div>
          <nav>
            <button
              className={tab === "graph" ? "active" : ""}
              onClick={() => setTab("graph")}
            >
              Workflow canvas
            </button>
            <button
              className={tab === "source" ? "active" : ""}
              disabled={hasDraft}
              onClick={() => setTab("source")}
            >
              YAML source
            </button>
            <span>Ordered execution · {graph.nodes.length} steps</span>
          </nav>
          {hasDraft && (
            <div className="notice" role="status">
              Unapplied argument draft. Apply or discard it in the inspector
              before saving, running, changing steps, switching views, or
              undoing.
            </div>
          )}
          <div className="authoring-toolbar">
            <button
              disabled={!document.canUndo || !!busy || hasDraft}
              onClick={document.undo}
              title="Ctrl/Cmd+Z outside text fields"
            >
              Undo
            </button>
            <button
              disabled={!document.canRedo || !!busy || hasDraft}
              onClick={document.redo}
              title="Ctrl/Cmd+Shift+Z outside text fields"
            >
              Redo
            </button>
            <button
              disabled={!!graph.error || !!busy || hasDraft}
              onClick={() => mutateSteps("add")}
              title="Alt+Insert"
            >
              Add step
            </button>
            <button
              disabled={
                selectedIndex === undefined ||
                selectedIndex === 0 ||
                !!busy ||
                hasDraft
              }
              onClick={() => mutateSteps("up")}
              title="Alt+↑"
            >
              Move up
            </button>
            <button
              disabled={
                selectedIndex === undefined ||
                selectedIndex >= count - 1 ||
                !!busy ||
                hasDraft
              }
              onClick={() => mutateSteps("down")}
              title="Alt+↓"
            >
              Move down
            </button>
            <button
              disabled={selectedIndex === undefined || !!busy || hasDraft}
              onClick={() => mutateSteps("delete")}
              title="Delete"
            >
              Delete step
            </button>
          </div>
          <div className="editor">
            {tab === "graph" ? (
              <div className="canvas-layout">
                <div className="canvas">
                  <ReactFlow
                    nodes={graph.nodes.map((node) => ({
                      ...node,
                      selected: node.id === selected,
                    }))}
                    onNodeClick={(_, node) => {
                      if (!hasDraft) setSelected(node.id);
                    }}
                    onPaneClick={() => {
                      if (!hasDraft) setSelected(undefined);
                    }}
                    deleteKeyCode={null}
                    edges={graph.edges}
                    fitView
                    nodesConnectable={false}
                    nodesDraggable={false}
                    edgesReconnectable={false}
                    minZoom={0.2}
                  >
                    <Background gap={22} color="#d9e0e8" />
                    <Controls />
                    <MiniMap />
                  </ReactFlow>
                  <div className="canvas-note">
                    Select a step to edit · Ordered execution
                  </div>
                  {graph.error && (
                    <div role="alert" className="graph-error">
                      {graph.error}
                      <button
                        disabled={hasDraft}
                        onClick={() => setTab("source")}
                      >
                        Fix in YAML
                      </button>
                    </div>
                  )}
                </div>
                <StepInspector
                  draft={argumentDraft}
                  onDraftChange={setArgumentDraft}
                  disabled={!!busy}
                  source={source}
                  selected={selected}
                  onChange={setSource}
                  onSource={() => setTab("source")}
                />
              </div>
            ) : (
              <div className="source-editor">
                <div className="source-help">
                  Executable source · Validation uses the production Go parser.
                </div>
                <textarea
                  aria-label="Workflow YAML source"
                  spellCheck={false}
                  disabled={!!busy}
                  value={source}
                  onChange={(e) => setSource(e.target.value)}
                />
              </div>
            )}
          </div>
          <section className="workbook">
            <div className="workbookbar">
              <strong>
                Excel workspace{" "}
                <span className="subtle">Actual workbook cells</span>
              </strong>
              {artifact && (
                <span className="state">
                  {runIdentity === currentIdentity
                    ? "✓ Output generated"
                    : "● Stale output — run again"}
                </span>
              )}
            </div>
            <div className="inspectbar">
              <input
                aria-label="Workbook path"
                value={inspectPath}
                onChange={(e) => setInspectPath(e.target.value)}
                placeholder="Path relative to workspace"
              />
              <button disabled={!ready} onClick={() => inspect()}>
                Inspect workbook
              </button>
              {inspection && (
                <select
                  aria-label="Sheet"
                  value={inspection.sheet}
                  onChange={(e) => inspect(e.target.value)}
                >
                  {inspection.sheets.map((s) => (
                    <option key={s}>{s}</option>
                  ))}
                </select>
              )}
            </div>
            {inspection ? (
              <>
                <div className="grid">
                  <table>
                    <thead>
                      <tr>
                        <th>#</th>
                        {Array.from(
                          {
                            length: Math.max(
                              0,
                              ...inspection.rows.map((r) => r.length),
                            ),
                          },
                          (_, i) => (
                            <th key={i}>Column {i + 1}</th>
                          ),
                        )}
                      </tr>
                    </thead>
                    <tbody>
                      {inspection.rows.map((row, i) => (
                        <tr key={i}>
                          <th>{inspection.offset + i + 1}</th>
                          {row.map((cell, j) => (
                            <td key={j}>{cell}</td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div className="pager">
                  <span>
                    {inspection.sheet} · {inspection.totalRows} rows ·{" "}
                    {inspection.truncated ? "Paged view" : "All available rows"}
                  </span>
                  <button
                    disabled={!ready || inspection.offset === 0}
                    onClick={() =>
                      inspect(
                        inspection.sheet,
                        Math.max(0, inspection.offset - 100),
                      )
                    }
                  >
                    Previous
                  </button>
                  <button
                    disabled={
                      !ready ||
                      inspection.offset + inspection.rows.length >=
                        inspection.totalRows
                    }
                    onClick={() =>
                      inspect(inspection.sheet, inspection.offset + 100)
                    }
                  >
                    Next
                  </button>
                </div>
              </>
            ) : (
              <div className="empty">
                <span>▦</span>
                <strong>Your data, alongside your logic</strong>
                <p>
                  Inspect an input workbook or run the workflow to view its
                  generated output.
                </p>
              </div>
            )}
            {artifact && (
              <div className="artifact">
                Output: {artifact.path}
                <span title={artifact.sha256}>
                  SHA-256 {artifact.sha256.slice(0, 16)}…
                </span>
              </div>
            )}
          </section>
        </section>
      </main>
      {pendingDiscard && (
        <div className="dialog-backdrop">
          <section
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="discard-title"
            className="discard-dialog"
          >
            <h2 id="discard-title">Discard unsaved workflow changes?</h2>
            <p>
              Save your workflow and apply any argument draft first to keep
              these edits.
            </p>
            <button autoFocus onClick={() => setPendingDiscard(undefined)}>
              Keep editing
            </button>
            <button
              onClick={() => {
                const action = pendingDiscard;
                setPendingDiscard(undefined);
                action();
              }}
            >
              Discard changes and continue
            </button>
          </section>
        </div>
      )}
      <footer role="status" aria-live="polite" className={error ? "error" : ""}>
        <span>{busy ? `${busy}…` : message}</span>
        <span>Mai Studio · 0.1</span>
      </footer>
    </div>
  );
}
