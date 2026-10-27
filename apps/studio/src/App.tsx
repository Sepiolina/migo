import React, { useMemo, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { ReactFlow, Background, Controls, MiniMap } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import "./style.css";
import { example, projectWorkflow } from "./model";
import {
  isDesktop,
  request,
  type Inspection,
  type Validation,
  type RunResult,
} from "./api";
export default function App() {
  const [source, setSource] = useState(example),
    [workspace, setWorkspace] = useState(""),
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
      setArtifact(undefined);
      setInspection(undefined);
      setMessage(
        "Workspace authorized. File paths are relative to this folder.",
      );
    });
  }
  async function validate() {
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
          <button disabled={!ready} onClick={validate}>
            ✓ Validate
          </button>
          <button
            className="primary"
            disabled={!ready || !!graph.error}
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
            onClick={choose}
          >
            ▣ {workspace ? "Change workspace" : "Open workspace"}
          </button>
          <p className="path">{workspace || "Choose a local project folder"}</p>
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
            <strong>◇ {validation?.name ?? "Parts summary"}</strong>
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
              onClick={() => setTab("source")}
            >
              YAML source
            </button>
            <span>Ordered execution · {graph.nodes.length} steps</span>
          </nav>
          <div className="editor">
            {tab === "graph" ? (
              <>
                <ReactFlow
                  nodes={graph.nodes}
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
                  Edit YAML to change execution · Branches preserve engine order
                </div>
                {graph.error && (
                  <div role="alert" className="graph-error">
                    {graph.error}
                    <button onClick={() => setTab("source")}>
                      Fix in YAML
                    </button>
                  </div>
                )}
              </>
            ) : (
              <div className="source-editor">
                <div className="source-help">
                  Executable source · Validation uses the production Go parser.
                </div>
                <textarea
                  aria-label="Workflow YAML source"
                  spellCheck={false}
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
      <footer role="status" aria-live="polite" className={error ? "error" : ""}>
        <span>{busy ? `${busy}…` : message}</span>
        <span>Mai Studio · 0.1</span>
      </footer>
    </div>
  );
}
