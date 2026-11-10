import { useState } from "react";
import type { Inspection, StepTrace } from "./api";
import { Glyph, type Selection } from "./Navigator";
import {
  deleteAt,
  duplicateAt,
  expandSheets,
  itemForm,
  moveAt,
  removeInput,
  renameInput,
  setNote,
  setShortSheet,
  setValue,
  stepForm,
  yamlOf,
  type Pipeline,
} from "./pipeline";
import { Draft, StepForm } from "./StepForm";

type Change = (source: string, label: string) => void;
interface Props {
  source: string;
  pipeline: Pipeline;
  selection: Selection;
  onChange: Change;
  onError: (message: string) => void;
  onSelect: (s: Selection) => void;
  onClose: () => void;
  trace?: StepTrace[];
  columns: string[];
  bindings: Record<string, string>;
  onBind: (id: string, path: string) => void;
  inspection?: Inspection;
  route: string;
  workflowPath: string;
  canPackage: boolean;
  onPackage: () => void;
  onAddRead: (input: string) => void;
}

function Head({ op, title, sub, onClose }: { op: string; title: string; sub: string; onClose: () => void }) {
  return (
    <>
      <div className="ph">
        Inspector
        <button className="tb" aria-label="Close inspector" title="Close" onClick={onClose}>✕</button>
      </div>
      <div className="ihead">
        <Glyph op={op} />
        <div>
          <b>{title}</b>
          <span>{sub}</span>
        </div>
      </div>
    </>
  );
}

function Effect({ trace, top }: { trace?: StepTrace[]; top: number }) {
  const t = trace?.[top];
  if (!trace) return <div className="hint">Add sample files to the sources to see what this step does.</div>;
  if (!t) return <div className="hint">An earlier step failed, so this one did not run.</div>;
  if (t.error) return <div className="hint err-text">{t.error}</div>;
  if (t.skipped) return <div className="hint">Left out of the preview. The saved workflow still runs it.</div>;
  const before = top > 0 ? trace[top - 1] : undefined;
  const added = t.columns.filter((c) => !before?.columns.some((b) => b.name === c.name)).map((c) => c.name);
  const removed = before?.columns.filter((b) => !t.columns.some((c) => c.name === b.name)).map((c) => c.name) ?? [];
  return (
    <div style={{ display: "grid", gap: 4 }}>
      <div className="hint">
        {before ? `${before.total.toLocaleString()} rows in → ` : ""}
        {t.total.toLocaleString()} rows out · {t.ms < 1 ? "<1" : Math.round(t.ms)} ms
      </div>
      {added.length > 0 && <div className="hint">Adds {added.join(", ")}</div>}
      {removed.length > 0 && <div className="hint">Removes {removed.join(", ")}</div>}
      {t.log?.map((l, i) => <div key={i} className="hint mono">☰ {l}</div>)}
    </div>
  );
}

function curlFor(pipeline: Pipeline, route: string, bindings: Record<string, string>, owner: string, required: boolean) {
  const files = pipeline.inputs.map((i) => `  -F "files=@${(bindings[i.id] || `${i.id}.xlsx`).split("/").pop()}"`);
  const out = `result${pipeline.format === "csv" ? ".csv" : ".xlsx"}`;
  return [`curl -X POST http://SERVER:PORT/api/${route}/run \\`, ...files.map((f) => f + " \\"), ...(required || owner !== "bam" ? [`  -F "${owner}=1234" \\`] : []), `  -o ${out}`].join("\n");
}

export function Inspector(p: Props) {
  const { source, pipeline, selection, onChange, onError, onClose } = p;
  const [showAll, setShowAll] = useState(false);
  const [copied, setCopied] = useState(false);
  const steps = pipeline.steps;

  if (selection.kind === "endpoint") {
    const svc = pipeline.service as Record<string, unknown>;
    const owner = String(svc.owner_field ?? "bam");
    const required = svc.owner_required !== false;
    const curl = curlFor(pipeline, p.route, p.bindings, owner, required);
    const eps: [string, string, string][] = [
      ["POST", "run", `Upload ${pipeline.inputs.map((i) => i.label).join(" + ") || "the files"}; the ${pipeline.format.toUpperCase()} file comes back in the response.`],
      ["POST", "upload", "Same form; keeps the result and answers with JSON and a download link."],
      ["GET", "download", "The latest result of upload."],
      ["GET", "", "This workflow's description: inputs, sheets, owner field, limits."],
    ];
    return (
      <aside className="pane insp" aria-label="Inspector">
        <Head op="rest" title="API endpoint" sub="How MaiService serves this workflow" onClose={onClose} />
        <div className="scroll">
          <div className="sec">
            <div className="lbl">Workflow</div>
            <div className="prop">
              <span><label htmlFor="wf-name">Name</label></span>
              <Draft id="wf-name" value={pipeline.name} onCommit={(v) => v.trim() && onChange(setValue(source, ["name"], v.trim()), "Rename workflow")} />
            </div>
            <label className="prop" htmlFor="wf-format">
              <span>Returns</span>
              <select id="wf-format" className="ctl" value={pipeline.format} onChange={(e) => onChange(setValue(source, ["output", "format"], e.target.value === "csv" ? "csv" : undefined), "Change output format")}>
                <option value="xlsx">Excel workbook (.xlsx)</option>
                <option value="csv">CSV file, first sheet only (.csv)</option>
              </select>
            </label>
          </div>
          <div className="sec">
            <div className="lbl">
              Service settings
              <button className="tb" aria-pressed={showAll} onClick={() => setShowAll(!showAll)}>{showAll ? "Fewer" : "Show all"}</button>
            </div>
            <StepForm source={source} argPath={["service"]} form={itemForm("mai_service")} onChange={onChange} onError={onError} columns={[]} showAll={showAll} />
            {!p.workflowPath && <div className="hint">Until it is saved, the route is taken from the workflow name.</div>}
          </div>
          <div className="sec">
            <div className="lbl">Endpoints</div>
            {eps.map(([verb, path, help]) => (
              <div className="endpoint" key={path}>
                <span className={`verb ${verb === "GET" ? "get" : ""}`}>{verb}</span>
                <code>/api/{p.route}{path ? `/${path}` : ""}</code>
                <small>{help}</small>
              </div>
            ))}
            <div className="endpoint">
              <span className="verb get">GET</span>
              <code>/api/workflows</code>
              <small>Every workflow on the server, for clients that discover them.</small>
            </div>
          </div>
          <div className="sec">
            <div className="lbl">Request</div>
            <div className="hint">
              Multipart form: one <code>files</code> part per source
              {pipeline.inputs.some((i) => i.match) ? " (matched by file name where a pattern is set, otherwise in order)" : ", in this order"}
              {required ? <>, plus <code>{owner}</code>, the requester's id.</> : <>. <code>{owner}</code> is optional.</>}
            </div>
            <div className="curl">
              <pre className="yaml">{curl}</pre>
              <button
                className="tb"
                onClick={() => {
                  navigator.clipboard?.writeText(curl).then(() => setCopied(true), () => setCopied(false));
                  setTimeout(() => setCopied(false), 1500);
                }}
              >
                {copied ? "Copied" : "Copy"}
              </button>
            </div>
          </div>
          <div className="sec">
            <div className="lbl">Distribute</div>
            <div className="hint">Build a Linux service package with the saved workflows and this server's settings, signed for verification before install.</div>
            <div className="actions-row">
              <button className="btn" disabled={!p.canPackage} onClick={p.onPackage}>Build Linux package…</button>
            </div>
            {!p.canPackage && <div className="hint">Open a workspace and save the workflow first.</div>}
          </div>
        </div>
      </aside>
    );
  }

  if (selection.kind === "source") {
    const input = pipeline.inputs.find((i) => i.id === selection.id);
    if (!input) return null;
    const readers = steps.filter((s) => s.op === "from" && s.summary.startsWith(`Read ${input.id}`));
    return (
      <aside className="pane insp" aria-label="Inspector">
        <Head op="from" title={`Source: ${input.id}`} sub="A file the workflow reads" onClose={onClose} />
        <div className="scroll">
          <div className="sec">
            <div className="lbl">Contract</div>
            <div className="prop">
              <span><label htmlFor="in-id">Name</label></span>
              <Draft id="in-id" value={input.id} onCommit={(v) => { try { onChange(renameInput(source, input.id, v.trim()), "Rename source"); p.onSelect({ kind: "source", id: v.trim() }); } catch (e) { onError((e as Error).message); } }} />
            </div>
            <div className="prop">
              <span><label htmlFor="in-label">Label</label></span>
              <Draft id="in-label" value={input.label} onCommit={(v) => onChange(setValue(source, ["input", input.id, "label"], v || undefined), "Edit label")} />
            </div>
            <div className="prop">
              <span><label htmlFor="in-match">File name</label></span>
              <div style={{ display: "grid", gap: 3 }}>
                <Draft id="in-match" value={input.match} placeholder="Any (by upload order)" code onCommit={(v) => onChange(setValue(source, ["input", input.id, "match"], v || undefined), "Edit file match")} />
                <span className="hint">A pattern such as <code>(?i)orders</code> lets clients upload files in any order.</span>
              </div>
            </div>
          </div>
          <div className="sec">
            <div className="lbl">Sample file</div>
            <div className="prop">
              <span><label htmlFor="in-file">Path</label></span>
              <div style={{ display: "grid", gap: 3 }}>
                <Draft id="in-file" value={p.bindings[input.id] ?? ""} placeholder="examples/orders.xlsx" onCommit={(v) => p.onBind(input.id, v.trim())} />
                <span className="hint">An .xlsx, .xls or .csv file inside the workspace. Used for previews and local runs only.</span>
              </div>
            </div>
            {p.inspection && (
              <div className="hint">
                {p.inspection.sheets.length} sheet{p.inspection.sheets.length === 1 ? "" : "s"}: {p.inspection.sheets.join(", ")} · {p.inspection.totalRows.toLocaleString()} rows in {p.inspection.sheet}
              </div>
            )}
          </div>
          <div className="sec">
            <div className="lbl">Used by</div>
            {readers.length ? readers.map((s) => (
              <button key={s.id} className="linkish" onClick={() => p.onSelect({ kind: "step", id: s.id })}>Step {s.top + 1}: {s.summary}</button>
            )) : (
              <>
                <div className="hint">No step reads this source yet.</div>
                <div className="actions-row"><button className="btn" onClick={() => p.onAddRead(input.id)}>Add a Read step</button></div>
              </>
            )}
            <div className="actions-row">
              <button className="btn danger" disabled={pipeline.inputs.length < 2} onClick={() => { onChange(removeInput(source, input.id), "Remove source"); p.onSelect({ kind: "endpoint" }); }}>Remove source</button>
            </div>
          </div>
        </div>
      </aside>
    );
  }

  if (selection.kind === "output") {
    const o = pipeline.outputs[selection.index];
    if (!o) return null;
    const short = pipeline.sheetsShort;
    return (
      <aside className="pane insp" aria-label="Inspector">
        <Head op="save" title={`Output: ${o.name}`} sub={pipeline.format === "csv" ? "Written as the CSV file" : "A sheet of the output workbook"} onClose={onClose} />
        <div className="scroll">
          <div className="sec">
            <div className="lbl">
              Sheet
              {!short && <button className="tb" aria-pressed={showAll} onClick={() => setShowAll(!showAll)}>{showAll ? "Fewer" : "Show all"}</button>}
            </div>
            {short ? (
              <>
                <div className="prop">
                  <span><label htmlFor="out-name">Name</label></span>
                  <Draft id="out-name" value={o.name} onCommit={(v) => v.trim() && onChange(setShortSheet(source, o.index, v.trim(), o.from), "Rename sheet")} />
                </div>
                <label className="prop" htmlFor="out-from">
                  <span>From table</span>
                  <select id="out-from" className="ctl" value={o.from} onChange={(e) => onChange(setShortSheet(source, o.index, o.name, e.target.value), "Change sheet table")}>
                    {[...new Set([o.from, ...pipeline.datasets])].map((d) => <option key={d}>{d}</option>)}
                  </select>
                </label>
                <div className="hint">Every column is written as it is.</div>
                <div className="actions-row"><button className="btn" onClick={() => onChange(expandSheets(source), "Format output columns")}>Choose and format columns…</button></div>
              </>
            ) : (
              <StepForm source={source} argPath={["output", "sheets", o.index]} form={itemForm("mai_sheet")} onChange={onChange} onError={onError} columns={p.columns} showAll={showAll} />
            )}
          </div>
          <div className="sec">
            <div className="lbl">Saved tables</div>
            <div className="hint">{pipeline.datasets.join(", ") || "None yet. Add a Save step."}</div>
          </div>
        </div>
      </aside>
    );
  }

  const index = steps.findIndex((s) => s.id === selection.id);
  const step = steps[index];
  if (!step) return null;
  const siblings = steps.filter((s) => s.listPath.join(".") === step.listPath.join("."));
  return (
    <aside className="pane insp" aria-label="Inspector">
      <Head op={step.op} title={step.spec?.title ?? step.op} sub={step.depth ? `In step ${step.top + 1}, ${step.branch === "else" ? "otherwise" : "then"} branch` : `Step ${step.top + 1} of ${siblings.length}`} onClose={onClose} />
      <div className="scroll">
        <div className="sec">
          <div className="lbl">
            Settings
            {step.spec?.fields.some((f) => f.advanced) && <button className="tb" aria-pressed={showAll} onClick={() => setShowAll(!showAll)}>{showAll ? "Fewer" : "Show all"}</button>}
          </div>
          {step.spec ? (
            <StepForm key={step.id} source={source} argPath={step.argPath} form={stepForm(step.spec)} onChange={onChange} onError={onError} columns={p.columns} showAll={showAll} />
          ) : (
            <div className="hint">Studio has no form for “{step.op}”. Edit it in YAML below the table.</div>
          )}
          {step.spec?.help && <div className="hint">{step.spec.help}</div>}
          {step.spec?.service && <div className="hint">Runs on the server only. Local previews skip external calls.</div>}
        </div>
        {step.depth === 0 && (
          <div className="sec">
            <div className="lbl">Effect</div>
            <Effect trace={p.trace} top={step.top} />
          </div>
        )}
        <div className="sec">
          <div className="lbl">Step</div>
          <div className="prop">
            <span><label htmlFor="step-note">Note</label></span>
            <Draft id="step-note" value={step.note ?? ""} placeholder="Why this step exists" multiline onCommit={(v) => onChange(setNote(source, step.path, v), "Edit note")} />
          </div>
          <div className="actions-row">
            <button className="btn" disabled={step.index === 0} onClick={() => { onChange(moveAt(source, step.path, -1), "Move step up"); p.onSelect({ kind: "step", id: [...step.listPath, step.index - 1].join(".") }); }}>Move up</button>
            <button className="btn" disabled={step.index === siblings.length - 1} onClick={() => { onChange(moveAt(source, step.path, 1), "Move step down"); p.onSelect({ kind: "step", id: [...step.listPath, step.index + 1].join(".") }); }}>Move down</button>
            <button className="btn" onClick={() => { onChange(duplicateAt(source, step.path), "Duplicate step"); p.onSelect({ kind: "step", id: [...step.listPath, step.index + 1].join(".") }); }}>Duplicate</button>
            <button className="btn danger" onClick={() => { onChange(deleteAt(source, step.path), "Delete step"); const next = siblings[step.index + 1] ?? siblings[step.index - 1]; p.onSelect(next ? { kind: "step", id: [...step.listPath, Math.min(step.index, siblings.length - 2)].join(".") } : { kind: "endpoint" }); }}>Delete</button>
          </div>
        </div>
        <div className="sec">
          <div className="lbl">YAML</div>
          <pre className="yaml">{yamlOf(source, step.path)}</pre>
        </div>
      </div>
    </aside>
  );
}
