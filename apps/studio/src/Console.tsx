import { useEffect, useState } from "react";

export type Problem = { level: "error" | "warning" | "info"; text: string; step?: string };
export type LogLine = { time: string; text: string; warn?: boolean };
type Tab = "problems" | "log" | "yaml";

/** Problems, run log and the full YAML, docked below the table like an IDE's output panel. */
export function Console({
  problems,
  log,
  source,
  onSource,
  onGoTo,
  open,
  onOpen,
}: {
  problems: Problem[];
  log: LogLine[];
  source: string;
  onSource: (source: string) => void;
  onGoTo: (step: string) => void;
  open: Tab | null;
  onOpen: (tab: Tab | null) => void;
}) {
  const [draft, setDraft] = useState(source);
  useEffect(() => setDraft(source), [source]);
  const errors = problems.filter((p) => p.level === "error").length;
  const tab = (t: Tab, label: React.ReactNode) => (
    <button className="tb" role="tab" aria-selected={open === t} aria-pressed={open === t} onClick={() => onOpen(open === t ? null : t)}>
      {label}
    </button>
  );
  return (
    <section className="console" aria-label="Console">
      <div className="ctabs" role="tablist">
        {tab("problems", <>Problems {problems.length > 0 && <span className={`count ${errors ? "bad" : ""}`}>{problems.length}</span>}</>)}
        {tab("log", "Run log")}
        {tab("yaml", "YAML")}
        <span className="spacer" />
        {open && <button className="tb" aria-label="Close console" onClick={() => onOpen(null)}>⌄</button>}
      </div>
      <div className={`cbody ${open === "problems" ? "" : "closed"}`}>
        {problems.length ? (
          problems.map((p, i) => (
            <button key={i} className={`problem ${p.level === "error" ? "bad" : ""}`} disabled={!p.step} onClick={() => p.step && onGoTo(p.step)}>
              <b>{p.level === "error" ? "✕" : p.level === "warning" ? "⚠" : "ⓘ"}</b>
              <span>{p.text}</span>
            </button>
          ))
        ) : (
          <span className="hint">No problems.</span>
        )}
      </div>
      <div className={`cbody ${open === "log" ? "" : "closed"}`}>
        {log.length ? log.map((l, i) => (
          <div key={i} className={`cline ${l.warn ? "w" : ""}`}><span>{l.time}</span><span>{l.text}</span></div>
        )) : <span className="hint">Run or preview the workflow to see its log.</span>}
      </div>
      <div className={`cbody yaml-open ${open === "yaml" ? "" : "closed"}`}>
        <textarea
          aria-label="Workflow YAML"
          spellCheck={false}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => draft !== source && onSource(draft)}
          onKeyDown={(e) => {
            if (e.key === "s" && (e.metaKey || e.ctrlKey) && draft !== source) onSource(draft);
          }}
        />
      </div>
    </section>
  );
}
