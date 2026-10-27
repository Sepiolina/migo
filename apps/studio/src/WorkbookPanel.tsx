import { useEffect, useState } from "react";
import type { Inspection, RunResult } from "./api";
export function WorkbookPanel({
  inspection,
  artifact,
  ready,
  path,
  loadedPath,
  onPath,
  onInspect,
  onClose,
  fullScreen,
  onExpand,
  stale,
  inputs,
}: {
  inspection?: Inspection;
  artifact?: RunResult;
  ready: boolean;
  path: string;
  loadedPath: string;
  onPath: (path: string) => void;
  onInspect: (path: string, sheet?: string, offset?: number) => void;
  onClose: () => void;
  fullScreen: boolean;
  onExpand: () => void;
  stale: boolean;
  inputs: Record<string, string>;
}) {
  const [selectedCell, setSelectedCell] = useState<string>();
  useEffect(() => {
    setSelectedCell(undefined);
  }, [loadedPath, inspection?.sheet, inspection?.offset]);
  return (
    <section className="workbook" aria-label="Excel data panel">
      <div className="workbookbar">
        <div>
          <strong>Excel data</strong>
          <span className="subtle">Read-only workbook inspector</span>
        </div>
        <div className="toolbar">
          <button onClick={onExpand}>
            {fullScreen ? "Dock panel" : "Expand data"}
          </button>
          <button aria-label="Close Excel data" onClick={onClose}>
            Close ×
          </button>
        </div>
      </div>
      <div className="data-tabs">
        {Object.entries(inputs)
          .filter(([, p]) => p)
          .map(([name, p]) => (
            <button disabled={!ready} key={name} onClick={() => onInspect(p)}>
              Input · {name}
            </button>
          ))}
        <button
          disabled={!ready || !artifact}
          onClick={() => artifact && onInspect(artifact.path)}
        >
          Output workbook{stale && artifact ? " · stale" : ""}
        </button>
      </div>
      <div className="inspectbar">
        <input
          aria-label="Workbook path"
          value={path}
          onChange={(event) => onPath(event.target.value)}
          placeholder="Workspace-relative Excel path"
        />
        <button
          disabled={!ready || !path.trim()}
          onClick={() => onInspect(path)}
        >
          Inspect workbook
        </button>
        {inspection && (
          <select
            aria-label="Sheet"
            value={inspection.sheet}
            disabled={!ready}
            onChange={(event) => onInspect(loadedPath, event.target.value)}
          >
            {inspection.sheets.map((sheet) => (
              <option key={sheet}>{sheet}</option>
            ))}
          </select>
        )}
      </div>
      {inspection ? (
        <>
          <div className="data-context">
            {loadedPath} · {inspection.totalRows} rows{" "}
            <span>Click a cell to read its complete value.</span>
          </div>
          <div className="grid">
            <table>
              <thead>
                <tr>
                  <th>#</th>
                  {Array.from(
                    {
                      length: Math.max(
                        0,
                        ...inspection.rows.map((row) => row.length),
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
                      <td key={j}>
                        <button
                          className="cell-value"
                          title={cell}
                          onClick={() => setSelectedCell(cell)}
                        >
                          {cell || "\u00a0"}
                        </button>
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {selectedCell !== undefined && (
            <div className="cell-inspector">
              <strong>Cell value</strong>
              <output>{selectedCell}</output>
              <button onClick={() => setSelectedCell(undefined)}>Clear</button>
            </div>
          )}
          <div className="pager">
            <span>
              {inspection.sheet} · rows{" "}
              {inspection.rows.length ? inspection.offset + 1 : 0}–
              {inspection.offset + inspection.rows.length} of{" "}
              {inspection.totalRows}
            </span>
            <button
              disabled={!ready || inspection.offset === 0}
              onClick={() =>
                onInspect(
                  loadedPath,
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
                onInspect(loadedPath, inspection.sheet, inspection.offset + 100)
              }
            >
              Next
            </button>
          </div>
        </>
      ) : (
        <div className="empty">
          <strong>Choose an input workbook or run your workflow</strong>
          <p>
            Use the tabs above to inspect input and output in the same place.
          </p>
        </div>
      )}
      {artifact && (
        <div className="artifact">
          Generated: {artifact.path}
          <span title={artifact.sha256}>
            SHA-256 {artifact.sha256.slice(0, 16)}…
          </span>
        </div>
      )}
    </section>
  );
}
