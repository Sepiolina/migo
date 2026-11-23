import { useEffect, useState } from "react";

export type Problem = {
  level: "error" | "warning" | "info";
  text: string;
  step?: string;
  code?: string;
  retry?: () => void;
  retryLabel?: string;
};
export type LogLine = { time: string; text: string; warn?: boolean };
type Tab = "problems" | "log" | "yaml";

/** Problems, run log and the full YAML, docked below the table like an IDE's output panel. */
export function Console({
  problems,
  log,
  source,
  onSourceDraft,
  onSource,
  onGoTo,
  open,
  onOpen,
}: {
  problems: Problem[];
  log: LogLine[];
  source: string;
  onSourceDraft?: (source: string) => void;
  onSource: (source: string) => void;
  onGoTo: (step: string) => void;
  open: Tab | null;
  onOpen: (tab: Tab | null) => void;
}) {
  const [localDraft, setLocalDraft] = useState(source);
  useEffect(() => {
    if (!onSourceDraft) setLocalDraft(source);
  }, [source, onSourceDraft]);
  const draft = onSourceDraft ? source : localDraft;
  const actionable = problems.filter((p) => p.level !== "info");
  const hasErrors = actionable.some((p) => p.level === "error");
  const tab = (t: Tab, label: React.ReactNode) => (
    <button className="tb" role="tab" aria-selected={open === t} aria-pressed={open === t} onClick={() => onOpen(open === t ? null : t)}>
      {label}
    </button>
  );
  return (
    <section className="console" aria-label="Console">
      <div className="ctabs" role="tablist">
        {tab("problems", <>Problems {actionable.length > 0 && <span className={`count ${hasErrors ? "bad" : ""}`}>{actionable.length}</span>}</>)}
        {tab("log", "Run log")}
        {tab("yaml", "YAML")}
        <span className="spacer" />
        {open && <button className="tb" aria-label="Close console" onClick={() => onOpen(null)}>⌄</button>}
      </div>
      <div className={`cbody ${open === "problems" ? "" : "closed"}`}>
        {actionable.length ? (
          actionable.map((p, i) => {
            const className = `problem ${p.level === "error" ? "bad" : ""}`;
            const content = (
              <>
                <b aria-label={p.level}>{p.level === "error" ? "✕" : "⚠"}</b>
                <span>{p.text}</span>
              </>
            );
            const step = p.step;
            return (
              <div className="problem-row" key={i}>
                {step ? (
                  <button className={className} title={p.code} onClick={() => onGoTo(step)}>{content}</button>
                ) : (
                  <div className={className} title={p.code}>{content}</div>
                )}
                {p.retry && <button className="tb problem-retry" aria-label={p.retryLabel} onClick={p.retry}>Retry</button>}
              </div>
            );
          })
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
          onChange={(e) => (onSourceDraft ?? setLocalDraft)(e.target.value)}
          onBlur={() => (onSourceDraft || draft !== source) && onSource(draft)}
          onKeyDown={(e) => {
            if (e.key.toLowerCase() === "s" && (e.metaKey || e.ctrlKey) && (onSourceDraft || draft !== source)) onSource(draft);
          }}
        />
      </div>
    </section>
  );
}
