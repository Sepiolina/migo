import type { StepTrace } from "./api";
import type { Pipeline } from "./pipeline";
import { categoryColours, stepByOp } from "./stepRegistry";

export type Selection =
  | { kind: "step"; id: string }
  | { kind: "source"; id: string }
  | { kind: "output"; index: number }
  | { kind: "endpoint" };

export const glyphFor = (op: string) => {
  const spec = stepByOp.get(op);
  return { icon: spec?.icon ?? "•", colour: spec ? categoryColours[spec.category] : "#8a8a95" };
};
export function Glyph({ op }: { op: string }) {
  const g = glyphFor(op);
  return <span className="glyph" style={{ background: g.colour }} aria-hidden="true">{g.icon}</span>;
}
const Eye = ({ off }: { off: boolean }) => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
    <path d="M1.5 8s2.4-4.5 6.5-4.5S14.5 8 14.5 8 12.1 12.5 8 12.5 1.5 8 1.5 8z" />
    {off ? <path d="M2 14 14 2" /> : <circle cx="8" cy="8" r="2" />}
  </svg>
);

export function Navigator({
  pipeline,
  selection,
  onSelect,
  trace,
  skipped,
  onToggleSkip,
  onAdd,
  onAddInput,
  onAddOutput,
  route,
  bindings,
  hidden,
}: {
  pipeline: Pipeline;
  selection: Selection;
  onSelect: (s: Selection) => void;
  trace?: StepTrace[];
  skipped: Set<number>;
  onToggleSkip: (top: number) => void;
  onAdd: () => void;
  onAddInput: () => void;
  onAddOutput: () => void;
  route: string;
  bindings: Record<string, string>;
  hidden?: boolean;
}) {
  const sel = (s: Selection) => JSON.stringify(s) === JSON.stringify(selection);
  const delta = (top: number) => {
    const t = trace?.[top];
    if (!t) return null;
    if (t.error) return <span className="delta minus">error</span>;
    if (t.skipped) return null;
    const before = top > 0 ? trace![top - 1] : undefined;
    const rows = before ? t.total - before.total : 0;
    const cols = before ? t.columns.length - before.columns.length : 0;
    if (t.log?.length) return <span className="delta warn" title={t.log.join("\n")}>☰ {t.log.length}</span>;
    if (rows) return <span className={`delta ${rows < 0 ? "minus" : "plus"}`}>{rows > 0 ? "+" : "−"}{Math.abs(rows).toLocaleString()}</span>;
    if (cols > 0) return <span className="delta plus">+{cols} col</span>;
    if (cols < 0) return <span className="delta minus">−{-cols} col</span>;
    return null;
  };
  let lastNote: string | undefined;
  return (
    <aside className="pane nav" aria-label="Workflow" hidden={hidden}>
      <div className="ph">Workflow</div>
      <div className="scroll">
        <div className="group">
          <div className="glabel">Endpoint</div>
          <button className={`row ${sel({ kind: "endpoint" }) ? "sel" : ""}`} onClick={() => onSelect({ kind: "endpoint" })}>
            <span />
            <span className="fmt">API</span>
            <span className="name mono">/api/{route}</span>
            <span />
          </button>
        </div>
        <div className="group">
          <div className="glabel">
            Sources
            <button className="tb" aria-label="Add source" title="Add source" onClick={onAddInput}>+</button>
          </div>
          {pipeline.inputs.map((i) => {
            const file = bindings[i.id];
            const ext = file?.match(/\.(xlsx|xls|csv|tsv|txt)$/i)?.[1] ?? "file";
            return (
              <button key={i.id} className={`row ${sel({ kind: "source", id: i.id }) ? "sel" : ""}`} onClick={() => onSelect({ kind: "source", id: i.id })}>
                <span />
                <span className="fmt">{ext}</span>
                <span className="name">{i.id}<small>{file || "no sample file"}</small></span>
                <span />
              </button>
            );
          })}
        </div>
        <div className="group">
          <div className="glabel">
            Steps
            <button className="tb" aria-label="Add step" title="Add step (⌘⇧A)" onClick={onAdd}>+</button>
          </div>
          {pipeline.steps.map((s) => {
            const off = skipped.has(s.top);
            const top = s.depth === 0;
            const err = top && trace?.[s.top]?.error;
            const note = top && s.note && s.note !== lastNote ? s.note : undefined;
            if (note) lastNote = note;
            return (
              <div key={s.id}>
                {note && <div className="section-note" title={note}>{note.split("\n")[0]}</div>}
                <div
                  role="button"
                  tabIndex={0}
                  className={`row ${sel({ kind: "step", id: s.id }) ? "sel" : ""} ${off ? "off" : ""} ${err ? "err" : ""}`}
                  style={{ paddingLeft: 2 + s.depth * 14 }}
                  title={s.summary}
                  onClick={() => onSelect({ kind: "step", id: s.id })}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onSelect({ kind: "step", id: s.id });
                    }
                  }}
                >
                  <span className="num">{top ? s.top + 1 : s.index === 0 && s.branch ? <span className="branch">{s.branch}</span> : ""}</span>
                  <Glyph op={s.op} />
                  <span className="name">{s.summary}</span>
                  <span className="side">
                    {top && delta(s.top)}
                    {top && (
                      <button
                        className="eye"
                        aria-label={off ? "Include this step in the preview" : "Preview without this step"}
                        title={off ? "Include in preview" : "Preview without this step (not saved)"}
                        onClick={(e) => {
                          e.stopPropagation();
                          onToggleSkip(s.top);
                        }}
                      >
                        <Eye off={off} />
                      </button>
                    )}
                  </span>
                </div>
              </div>
            );
          })}
          <button className="insert" onClick={onAdd}>
            + Add step <span className="kbd" style={{ marginLeft: "auto" }}>⌘⇧A</span>
          </button>
        </div>
        <div className="group">
          <div className="glabel">
            Outputs <span className="fmt" style={{ marginLeft: 6 }}>{pipeline.format}</span>
            <button className="tb" aria-label="Add output sheet" title="Add output sheet" onClick={onAddOutput} style={{ marginLeft: "auto" }}>+</button>
          </div>
          {pipeline.outputs.map((o) => (
            <button key={o.index} className={`row ${sel({ kind: "output", index: o.index }) ? "sel" : ""}`} onClick={() => onSelect({ kind: "output", index: o.index })}>
              <span />
              <span className="fmt">{pipeline.format === "csv" ? "csv" : "sheet"}</span>
              <span className="name">{o.name}<small>from {o.from}</small></span>
              <span />
            </button>
          ))}
        </div>
      </div>
    </aside>
  );
}
