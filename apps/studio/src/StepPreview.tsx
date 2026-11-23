import { useState, type ComponentProps } from "react";
import { DataGrid } from "./DataGrid";
import type { StepComparison, StepTrace } from "./api";

type Props = Pick<ComponentProps<typeof DataGrid>, "added" | "header" | "onAction" | "focusColumn"> & {
  title: string;
  trace: StepTrace;
  comparison?: StepComparison;
  fillColumns?: string[];
};

type View = "After" | "Before" | "Removed rows";

export function StepPreview({ title, trace, comparison, fillColumns, header, onAction, ...grid }: Props) {
  const [view, setView] = useState<View>("After");
  const [affected, setAffected] = useState(false);
  const mode = comparison ? view : "After";
  const fill = trace.name === "fill";
  const input = comparison?.input ?? trace.total;
  const removed = input - trace.total;
  const views: View[] = fill ? ["Before", "After"] : ["Before", "After", "Removed rows"];
  const rows = (mode === "Before" ? comparison?.before : mode === "Removed rows" ? comparison?.removed : trace.rows) ?? [];
  const total = mode === "Before" ? input : mode === "Removed rows" ? removed : trace.total;
  const rowNumbers = mode === "Removed rows" ? comparison?.removedPositions :
    mode === "After" ? comparison?.keptPositions : undefined;
  const indices = comparison && fill && affected
    ? trace.columns.flatMap((column, index) => fillColumns?.includes(column.name) ? [index] : [])
    : undefined;
  const visibleHeader = mode !== "After" || indices
    ? <><b>{title}</b><span className="meta">{mode} · {total.toLocaleString()} rows · {indices?.length ?? trace.columns.length} columns</span></>
    : header;
  const controls = comparison && (
    <div className="step-comparison" role="group" aria-label="Step comparison">
      {views.map((v) => <button key={v} className="tb" aria-pressed={mode === v} onClick={() => setView(v)}>{v}</button>)}
      {fill && <button className="tb" aria-pressed={affected} onClick={() => setAffected(!affected)}>Affected columns</button>}
      <span>{fill ? `${comparison.changedCells} cells changed in ${comparison.changedRows} rows` :
        `${input} input · ${trace.total} retained · ${removed} removed`}</span>
      {fill && comparison.changedCells > 0 && <span className="hint">Dashed underline marks a changed cell</span>}
      {fill && mode === "After" && (comparison.before?.length ?? 0) < rows.length &&
        <span>Change markers cover the first {comparison.before?.length ?? 0} rows.</span>}
    </div>
  );
  return <DataGrid {...grid}
    columnView={indices ? "affected" : "all"} columns={indices ? indices.map((j) => trace.columns[j]) : trace.columns}
    rows={indices ? rows.map((row) => indices.map((j) => row[j] ?? "")) : rows}
    total={total} rowNumbers={rowNumbers}
    changedCells={comparison && fill ? (i, j) => comparison.changed?.[i]?.includes(indices?.[j] ?? j) ?? false : undefined}
    hideProfiles={mode !== "After"} onAction={mode === "After" ? onAction : undefined}
    header={<>{visibleHeader}{controls}</>} />;
}
