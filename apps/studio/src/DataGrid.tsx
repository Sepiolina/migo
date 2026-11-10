import { useEffect, useRef, useState, type ReactNode } from "react";
import type { ColumnProfile } from "./api";
import { columnRef } from "./pipeline";

export type ColumnAction = { step: Record<string, unknown>; label: string };

const kindTag: Record<string, string> = { number: "#", text: "Aa", bool: "◐", empty: "∅", mixed: "?" };

function Profile({ c, total }: { c: ColumnProfile; total: number }) {
  const pct = total ? c.filled / total : 1;
  const max = Math.max(1, ...(c.bins ?? []));
  return (
    <span className="ch2">
      <span className={`fill ${pct < 1 ? "low" : ""}`} title={`${Math.round(pct * 100)}% filled`}>
        <i style={{ width: `${pct * 100}%` }} />
      </span>
      {c.bins ? (
        <span className="spark" aria-hidden="true" title={`${c.min} to ${c.max}`}>
          {c.bins.map((b, i) => <i key={i} style={{ height: 2 + (b / max) * 14 }} />)}
        </span>
      ) : (
        <span>{c.distinct >= 1000 ? "1000+" : c.distinct} unique</span>
      )}
      {total - c.filled > 0 && <span style={{ color: "var(--warn)" }}>{total - c.filled} blank</span>}
    </span>
  );
}

/** Actions on a column; each one becomes a step after the selected one. */
function ColumnMenu({ column, kind, columns, anchor, onAction, onClose }: { column: string; kind: string; columns: string[]; anchor: DOMRect; onAction: (a: ColumnAction) => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [ask, setAsk] = useState<null | "rename" | "equals">(null);
  const [value, setValue] = useState("");
  const c = columnRef(column);
  const num = kind === "number";
  const other = columns.find((x) => x !== column) ?? column;
  const items: ([string, string, ColumnAction | "rename" | "equals"] | null)[] = [
    ["⇅", num ? "Sort smallest first" : "Sort A → Z", { step: { sort: num ? { by: [column], numeric: true } : { by: [column] } }, label: `Sort by ${column}` }],
    ["⇅", num ? "Sort largest first" : "Sort Z → A", { step: { sort: num ? { by: [column], desc: true, numeric: true } : { by: [column], desc: true } }, label: `Sort by ${column}, descending` }],
    null,
    ["◇", "Keep rows that have a value", { step: { filter: `!blank(${c})` }, label: `Keep rows with ${column}` }],
    ["◇", "Keep rows where it equals…", "equals"],
    ["≠", "Remove duplicate values", { step: { dedupe: [column] }, label: `Remove duplicate ${column}` }],
    null,
    ["↓", "Fill blanks from the row above", { step: { fill: [column] }, label: `Fill blanks in ${column}` }],
    ["#", "Convert to number", { step: { set: { [column]: `toNumber(${c}, 0)` } }, label: `${column} as number` }],
    ["Aa", "Trim spaces", { step: { set: { [column]: `trim(${c})` } }, label: `Trim ${column}` }],
    null,
    ["⊞", `Group by ${column}`, { step: { group: { by: [column], count: [other] } }, label: `Group by ${column}` }],
    ["✎", "Rename…", "rename"],
    ["−", "Remove column", { step: { drop: [column] }, label: `Remove ${column}` }],
  ];
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>(".mi, input")?.focus();
    const away = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && onClose();
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [ask, onClose]);
  const left = Math.max(8, Math.min(anchor.left, window.innerWidth - 268));
  const top = Math.min(anchor.bottom + 4, window.innerHeight - 440);
  const submit = () => {
    const v = value.trim();
    if (!v) return;
    if (ask === "rename") onAction({ step: { rename: { [column]: v } }, label: `Rename ${column}` });
    else onAction({ step: { filter: `${c} == ${num && Number.isFinite(Number(v)) ? v : JSON.stringify(v)}` }, label: `Keep ${column} = ${v}` });
  };
  return (
    <div
      ref={ref}
      className="pop"
      role="menu"
      aria-label={`Actions on ${column}`}
      style={{ left, top: Math.max(8, top) }}
      onKeyDown={(e) => {
        if (e.key === "Escape") return onClose();
        const all = [...(ref.current?.querySelectorAll<HTMLElement>(".mi") ?? [])];
        const i = all.indexOf(document.activeElement as HTMLElement);
        if (e.key === "ArrowDown" && all.length) { e.preventDefault(); all[(i + 1) % all.length].focus(); }
        if (e.key === "ArrowUp" && all.length) { e.preventDefault(); all[(i - 1 + all.length) % all.length].focus(); }
      }}
    >
      <div className="h">{column} · {kind === "number" ? "number" : kind === "empty" ? "empty" : "text"}</div>
      {ask ? (
        <form className="quick" onSubmit={(e) => { e.preventDefault(); submit(); }}>
          <input aria-label={ask === "rename" ? "New name" : "Value"} value={value} placeholder={ask === "rename" ? column : "Value"} onChange={(e) => setValue(e.target.value)} />
          <button className="primary">{ask === "rename" ? "Rename" : "Keep"}</button>
        </form>
      ) : (
        items.map((it, i) =>
          it ? (
            <button
              key={i}
              className="mi"
              role="menuitem"
              onClick={() => {
                if (it[2] === "rename" || it[2] === "equals") {
                  setValue(it[2] === "rename" ? column : "");
                  setAsk(it[2]);
                } else onAction(it[2]);
              }}
            >
              <span className="gi" aria-hidden="true">{it[0]}</span>
              {it[1]}
              <span />
            </button>
          ) : (
            <div className="sep" key={i} />
          ),
        )
      )}
      <div className="sep" />
      <div className="hint">Each choice adds a step after the selected one.</div>
    </div>
  );
}

export function DataGrid({
  columns,
  rows,
  total,
  added,
  header,
  empty,
  onAction,
}: {
  columns: ColumnProfile[];
  rows: string[][];
  total: number;
  added?: Set<string>;
  header: ReactNode;
  empty?: ReactNode;
  onAction?: (a: ColumnAction) => void;
}) {
  const [menu, setMenu] = useState<{ column: string; kind: string; rect: DOMRect } | null>(null);
  return (
    <main className="stage" aria-label="Table">
      <div className="sbar">{header}</div>
      {empty ?? (
        <div className="grid" role="region" aria-label="Rows" tabIndex={0}>
          <table>
            <thead>
              <tr>
                <th className="rn" scope="col">#</th>
                {columns.map((c, j) => (
                  <th key={`${j}:${c.name}`} className={added?.has(c.name) ? "new" : ""} scope="col">
                    <button
                      className="colh"
                      aria-haspopup={onAction ? "menu" : undefined}
                      aria-expanded={menu?.column === c.name}
                      disabled={!onAction}
                      onClick={(e) => setMenu({ column: c.name, kind: c.kind, rect: (e.currentTarget as HTMLElement).getBoundingClientRect() })}
                    >
                      <span className="ch1">
                        <span className="ty">{kindTag[c.kind] ?? "Aa"}</span>
                        {c.name || "(blank)"}
                        {onAction && <span className="chev">▾</span>}
                      </span>
                      <Profile c={c} total={total} />
                    </button>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i}>
                  <td className="rn">{i + 1}</td>
                  {columns.map((c, j) => {
                    const v = r?.[j] ?? "";
                    return (
                      <td key={j} className={`${added?.has(c.name) ? "new " : ""}${c.kind === "number" ? "n " : ""}${v === "" ? "blank" : ""}`} title={v.length > 30 ? v : undefined}>
                        {v}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
          {total > rows.length && <div className="more-rows">Showing {rows.length.toLocaleString()} of {total.toLocaleString()} rows. Profiles above cover every row.</div>}
        </div>
      )}
      {menu && onAction && (
        <ColumnMenu
          column={menu.column}
          kind={menu.kind}
          columns={columns.map((c) => c.name)}
          anchor={menu.rect}
          onClose={() => setMenu(null)}
          onAction={(a) => {
            setMenu(null);
            onAction(a);
          }}
        />
      )}
    </main>
  );
}

/** Profile columns of a plain preview (sources and outputs), where all cells are text. */
export function profileRows(header: string[], rows: string[][]): ColumnProfile[] {
  return header.map((name, j) => {
    const vals = rows.map((r) => r[j] ?? "").filter((v) => v.trim() !== "");
    const nums = vals.map(Number).filter((n) => Number.isFinite(n));
    const isNum = vals.length > 0 && nums.length === vals.length;
    const p: ColumnProfile = { name, kind: vals.length ? (isNum ? "number" : "text") : "empty", filled: vals.length, distinct: new Set(vals).size };
    if (isNum && nums.length > 1) {
      p.min = Math.min(...nums);
      p.max = Math.max(...nums);
      p.bins = Array(8).fill(0);
      for (const n of nums) p.bins[Math.min(7, Math.floor(((n - p.min) / (p.max - p.min || 1)) * 8))]++;
    }
    return p;
  });
}
