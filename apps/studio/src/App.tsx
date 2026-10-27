import React, { useEffect, useMemo, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { getCurrentWindow } from "@tauri-apps/api/window";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  type ReactFlowInstance,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import "./style.css";
import { example, projectWorkflow, type WorkflowGraphNode } from "./model";
import { changeSteps, topLevelCount } from "./authoring";
import { useDocument } from "./useDocument";
import { workflowNodeTypes } from "./WorkflowNode";
import { operations } from "./operationCatalog";
import { addOperation } from "./nodeAuthoring";
import { WorkbookPanel } from "./WorkbookPanel";
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
  const [showProject, setShowProject] = useState(false);
  const [showData, setShowData] = useState(false);
  const [fullData, setFullData] = useState(false);
  const [showLibrary, setShowLibrary] = useState(false);
  const [nodeSearch, setNodeSearch] = useState("");
  const flow = useRef<ReactFlowInstance<WorkflowGraphNode> | null>(null);
  const [loadedInspectPath, setLoadedInspectPath] = useState("");
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
        setShowLibrary(true);
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
      setLoadedInspectPath(result.path);
      setMessage(
        "Run completed. Open Excel data to inspect the generated workbook.",
      );
    });
  }
  async function inspect(sheet?: string, offset = 0, path = inspectPath) {
    await task("Reading workbook", async () => {
      setInspection(
        await request<Inspection>("inspectWorkbook", {
          workspace,
          path,
          sheet,
          offset,
          limit: 100,
        }),
      );
      setLoadedInspectPath(path);
      setInspectPath(path);
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
          <button
            aria-expanded={showProject}
            onClick={() => setShowProject(!showProject)}
          >
            Project & inputs
          </button>
          <button
            aria-expanded={showData}
            onClick={() => setShowData(!showData)}
          >
            Excel data{artifact ? " · Output ready" : ""}
          </button>
          <span className="mode">● Offline</span>
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
        {showProject && (
          <aside aria-label="Project and inputs">
            <div className="eyebrow">WORKSPACE</div>
            <button
              className="folder"
              disabled={!isDesktop() || !!busy}
              onClick={() => discardGuard(() => void choose())}
            >
              ▣ {workspace ? "Change workspace" : "Open workspace"}
            </button>
            <p className="path">
              {workspace || "Choose a local project folder"}
            </p>
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
            {(
              validation?.inputs ?? [{ name: "parts", label: "Parts list" }]
            ).map((input) => (
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
            ))}
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
        )}
        <section
          className={`workspace ${showData ? "data-open" : ""} ${showData && fullData ? "data-full" : ""}`}
        >
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
            <span>Left → right · {count} steps</span>
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
              onClick={() => setShowLibrary(!showLibrary)}
              aria-expanded={showLibrary}
              title="Choose a node from the library"
            >
              + Add node
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
          {showLibrary && (
            <section className="node-library" aria-label="Node library">
              <div className="library-heading">
                <div>
                  <strong>Add a node</strong>
                  <p>
                    Choose what happens next. Settings open after insertion.
                  </p>
                </div>
                <button onClick={() => setShowLibrary(false)}>
                  Close library
                </button>
              </div>
              <input
                aria-label="Search nodes"
                autoFocus
                placeholder="Search nodes: read, filter, group…"
                value={nodeSearch}
                onChange={(event) => setNodeSearch(event.target.value)}
              />
              <div className="library-items">
                {operations
                  .filter((op) =>
                    `${op.title} ${op.operation} ${op.description}`
                      .toLowerCase()
                      .includes(nodeSearch.toLowerCase()),
                  )
                  .map((op) => (
                    <button
                      className={`library-item category-${op.category}`}
                      key={op.operation}
                      disabled={hasDraft || !!busy}
                      onClick={() => {
                        try {
                          setSource(
                            addOperation(
                              source,
                              op.operation,
                              op.defaultArgument,
                            ),
                          );
                          setSelected(`step.${count}`);
                          setTab("graph");
                          setShowLibrary(false);
                        } catch (error) {
                          setError(true);
                          setMessage(String(error));
                        }
                      }}
                    >
                      <span className="library-icon">{op.icon}</span>
                      <span>
                        <strong>{op.title}</strong>
                        <small>{op.description}</small>
                      </span>
                    </button>
                  ))}
              </div>
            </section>
          )}
          <div className="editor">
            {tab === "graph" ? (
              <div className="canvas-layout">
                <div className="canvas">
                  <ReactFlow<WorkflowGraphNode>
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
                    nodeTypes={workflowNodeTypes}
                    onInit={(instance) => {
                      flow.current = instance;
                    }}
                    defaultViewport={{ x: 48, y: 80, zoom: 1 }}
                    fitViewOptions={{ minZoom: 0.1, maxZoom: 1, padding: 0.15 }}
                    nodesConnectable={false}
                    nodesDraggable={false}
                    edgesReconnectable={false}
                    minZoom={0.1}
                    maxZoom={1.75}
                  >
                    <Background gap={22} color="#d9e0e8" />
                    <Controls showFitView={false} showInteractive={false} />
                    <MiniMap pannable zoomable />
                  </ReactFlow>
                  <div
                    className="canvas-navigation"
                    aria-label="Canvas navigation"
                  >
                    <button
                      onClick={() =>
                        void flow.current?.setViewport(
                          { x: 48, y: 80, zoom: 1 },
                          { duration: 200 },
                        )
                      }
                    >
                      Readable 100%
                    </button>
                    <button
                      onClick={() =>
                        void flow.current?.fitView({
                          padding: 0.15,
                          duration: 200,
                        })
                      }
                    >
                      Overview
                    </button>
                    <button
                      disabled={!selected}
                      onClick={() =>
                        void flow.current?.fitView({
                          nodes: [{ id: selected! }],
                          minZoom: 1,
                          maxZoom: 1,
                          duration: 200,
                        })
                      }
                    >
                      Focus selected
                    </button>
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
                {selected && (
                  <StepInspector
                    draft={argumentDraft}
                    onDraftChange={setArgumentDraft}
                    disabled={!!busy}
                    source={source}
                    selected={selected}
                    onChange={setSource}
                    onSource={() => setTab("source")}
                  />
                )}
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
          {showData && (
            <WorkbookPanel
              inspection={inspection}
              artifact={artifact}
              ready={ready}
              path={inspectPath}
              loadedPath={loadedInspectPath}
              onPath={setInspectPath}
              onInspect={(path, sheet, offset) =>
                void inspect(sheet, offset, path)
              }
              onClose={() => setShowData(false)}
              fullScreen={fullData}
              onExpand={() => setFullData(!fullData)}
              stale={runIdentity !== currentIdentity}
              inputs={bindings}
            />
          )}
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
