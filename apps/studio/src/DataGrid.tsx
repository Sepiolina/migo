import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import type { ColumnProfile } from "./api";
import { columnRef } from "./pipeline";

export type ColumnAction = { step: Record<string, unknown>; label: string };

const kindTag: Record<string, string> = { number: "#", text: "Aa", bool: "◐", empty: "∅", mixed: "?" };
const arrowMoves: Record<string, Cell> = {
  ArrowUp: { row: -1, column: 0 },
  ArrowDown: { row: 1, column: 0 },
  ArrowLeft: { row: 0, column: -1 },
  ArrowRight: { row: 0, column: 1 },
};
const worksheetGutterWidth = 58;

type Cell = { row: number; column: number };
type Selection = { active: Cell; anchor: Cell };
type WorksheetGrid = {
  columnOffset: number;
  totalColumns: number;
  widths: number[];
  rowHeights?: number[];
  cellStyle: (row: number, column: number) => CSSProperties | undefined;
  focusCell?: Cell & { request: number };
  onActivate: (cell: Cell) => void;
  onNavigate: (cell: Cell) => void;
};

function selectionBounds({ active, anchor }: Selection) {
  return {
    top: Math.min(active.row, anchor.row),
    bottom: Math.max(active.row, anchor.row),
    left: Math.min(active.column, anchor.column),
    right: Math.max(active.column, anchor.column),
  };
}

function selectionTsv(rows: string[][], selection: Selection) {
  const { top, bottom, left, right } = selectionBounds(selection);
  return rows.slice(top, bottom + 1).map((row) =>
    Array.from({ length: right - left + 1 }, (_, column) => {
      const value = row[left + column] ?? "";
      return /[\t\r\n"]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
    }).join("\t"),
  ).join("\n");
}

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
  controls,
  footer,
  empty,
  onAction,
  focusColumn,
  rowNumbers,
  rowOffset,
  changedCells,
  hideProfiles = false,
  columnView,
  worksheet,
}: {
  columns: ColumnProfile[];
  rows: string[][];
  total: number;
  added?: Set<string>;
  header: ReactNode;
  controls?: ReactNode;
  footer?: ReactNode;
  empty?: ReactNode;
  onAction?: (a: ColumnAction) => void;
  rowNumbers?: number[];
  rowOffset?: number;
  changedCells?: (row: number, column: number) => boolean;
  hideProfiles?: boolean;
  columnView?: string;
  focusColumn?: { name: string; request: number };
  worksheet?: WorksheetGrid;
}) {
  const [menu, setMenu] = useState<{ column: string; kind: string; rect: DOMRect } | null>(null);
  const [selection, setSelection] = useState<Selection | null>(null);
  const [copyError, setCopyError] = useState(false);
  const [jumpAnnouncement, setJumpAnnouncement] = useState<{ request: number; text: string } | null>(null);
  const grid = useRef<HTMLDivElement>(null);
  const table = useRef<HTMLTableElement>(null);
  const menuTrigger = useRef<HTMLElement | null>(null);
  const lastColumnRequest = useRef<number | undefined>(undefined);
  const lastCellRequest = useRef<number | undefined>(undefined);
  const copyAttempt = useRef(0);
  const id = useId();
  const cellId = (row: number, column: number) => `${id}-${row}-${column}`;
  const rowCount = rows.length;
  const columnCount = columns.length;
  const validSelection = selection && [selection.active, selection.anchor].every(
    ({ row, column }) => row < rowCount && column < columnCount,
  ) ? selection : null;
  const bounds = validSelection && selectionBounds(validSelection);
  const active = validSelection?.active;
  const rowNumber = (row: number) => rowNumbers?.[row] ?? (rowOffset ?? 0) + row + 1;
  const jumpColumn = focusColumn ? columns.findIndex((column) => column.name === focusColumn.name) : -1;
  const jumpRequest = focusColumn?.request;
  const jumpName = focusColumn?.name;
  const focusCell = worksheet?.focusCell;
  const sheetColumnOffset = worksheet?.columnOffset;
  const onActivate = worksheet?.onActivate;

  const selectCell = useCallback((cell: Cell, extend: boolean) => {
    copyAttempt.current++;
    setCopyError(false);
    setSelection((previous) => {
      const anchor = previous?.anchor;
      return { active: cell, anchor: extend && anchor && anchor.row < rowCount && anchor.column < columnCount ? anchor : cell };
    });
  }, [rowCount, columnCount]);

  const closeMenu = useCallback(() => {
    setMenu(null);
    menuTrigger.current?.focus({ preventScroll: true });
    menuTrigger.current = null;
  }, []);

  useLayoutEffect(() => {
    if (menu && !onAction) closeMenu();
  }, [menu, onAction, closeMenu]);

  useLayoutEffect(() => {
    if (empty != null || !rowCount || !columnCount) {
      setSelection(null);
    } else if (selection && !validSelection) {
      const cell = {
        row: Math.min(selection.active.row, rowCount - 1),
        column: Math.min(selection.active.column, columnCount - 1),
      };
      selectCell(cell, false);
    }
  }, [empty, rowCount, columnCount, selection, validSelection, selectCell]);

  const previousColumns = useRef({ columns, columnView });
  useLayoutEffect(() => {
    const previous = previousColumns.current;
    previousColumns.current = { columns, columnView };
    if (!selection || previous.columnView === columnView || !rowCount || !columnCount) return;
    const name = previous.columns[selection.active.column]?.name;
    const column = Math.max(0, columns.findIndex((c) => c.name === name));
    selectCell({ row: Math.min(selection.active.row, rowCount - 1), column }, false);
  }, [columnView, columns, selection, rowCount, columnCount, selectCell]);

  useLayoutEffect(() => {
    if (jumpRequest === undefined || jumpRequest === lastColumnRequest.current || jumpColumn < 0 || !rowCount || !table.current) return;
    const row = active?.row ?? 0;
    lastColumnRequest.current = jumpRequest;
    table.current.focus({ preventScroll: true });
    selectCell({ row, column: jumpColumn }, false);
    setJumpAnnouncement({ request: jumpRequest, text: `Jumped to column ${jumpName || jumpColumn + 1}, row ${row + 1}.` });
  }, [jumpRequest, jumpColumn, jumpName, rowCount, active?.row, selectCell]);

  useLayoutEffect(() => {
    const viewport = grid.current;
    if (!active || !viewport || !table.current) return;
    const cell = table.current.tBodies[0]?.rows[active.row]?.cells[active.column + 1];
    if (!cell) return;
    const view = viewport.getBoundingClientRect();
    const target = cell.getBoundingClientRect();
    const gutter = cell.parentElement?.firstElementChild?.getBoundingClientRect().width ?? 0;
    const headerHeight = table.current.tHead?.getBoundingClientRect().height ?? 0;
    const left = view.left + gutter;
    const right = view.left + viewport.clientWidth;
    const top = view.top + headerHeight;
    const bottom = view.top + viewport.clientHeight;
    if (target.left < left) viewport.scrollLeft += target.left - left;
    else if (target.right > right) viewport.scrollLeft += Math.min(target.right - right, target.left - left);
    if (target.top < top) viewport.scrollTop += target.top - top;
    else if (target.bottom > bottom) viewport.scrollTop += Math.min(target.bottom - bottom, target.top - top);
  }, [active, rowCount, columnCount]);

  useLayoutEffect(() => {
    if (rowOffset === undefined) return;
    if (grid.current) grid.current.scrollTop = 0;
    setSelection(null);
  }, [rowOffset]);

  useLayoutEffect(() => {
    if (sheetColumnOffset === undefined) return;
    if (grid.current) grid.current.scrollLeft = 0;
    setSelection(null);
  }, [sheetColumnOffset]);

  useLayoutEffect(() => {
    if (!focusCell || focusCell.request === lastCellRequest.current || !table.current ||
        focusCell.row < 0 || focusCell.row >= rowCount || focusCell.column < 0 || focusCell.column >= columnCount) return;
    lastCellRequest.current = focusCell.request;
    table.current.focus({ preventScroll: true });
    selectCell({ row: focusCell.row, column: focusCell.column }, false);
  }, [focusCell, rowCount, columnCount, selectCell]);

  useLayoutEffect(() => {
    if (active) onActivate?.(active);
  }, [active, onActivate]);

  const openMenu = (column: number, trigger: HTMLElement) => {
    const header = table.current?.tHead?.rows[0]?.cells[column + 1]?.querySelector("button");
    if (!header || !onAction) return;
    menuTrigger.current = trigger;
    setMenu({ column: columns[column].name, kind: columns[column].kind, rect: header.getBoundingClientRect() });
  };

  const handleGridKey = (event: KeyboardEvent<HTMLTableElement>) => {
    if (event.target !== event.currentTarget || !active || !validSelection) return;
    let move = arrowMoves[event.key];
    if (worksheet && (event.key === "PageDown" || event.key === "PageUp")) {
      const viewportHeight = grid.current?.clientHeight ?? 0;
      const headerHeight = table.current?.tHead?.offsetHeight ?? 0;
      const rowHeight = table.current?.tBodies[0]?.rows[active.row]?.offsetHeight || 22;
      const visibleRows = Math.max(1, Math.floor((viewportHeight - headerHeight) / rowHeight));
      move = { row: event.key === "PageDown" ? visibleRows : -visibleRows, column: 0 };
    }
    if (move && !event.altKey && !event.metaKey && !event.ctrlKey) {
      event.preventDefault();
      const next = { row: active.row + move.row, column: active.column + move.column };
      const bounded = {
        row: Math.max(0, Math.min(next.row, rowCount - 1)),
        column: Math.max(0, Math.min(next.column, columnCount - 1)),
      };
      if (worksheet && !event.shiftKey && (next.row !== bounded.row || next.column !== bounded.column)) {
        worksheet.onNavigate(next);
      } else {
        selectCell(bounded, event.shiftKey);
      }
    } else if (onAction && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) {
      event.preventDefault();
      openMenu(active.column, event.currentTarget);
    } else if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "c" && navigator.clipboard?.writeText) {
      event.preventDefault();
      const attempt = ++copyAttempt.current;
      setCopyError(false);
      navigator.clipboard.writeText(selectionTsv(rows, validSelection)).catch(() => {
        if (copyAttempt.current === attempt) setCopyError(true);
      });
    }
  };

  return (
    <main className={worksheet ? "stage workbook-stage" : "stage"} aria-label={worksheet ? "Workbook preview" : "Table"} tabIndex={-1}>
      <div className="sr-only" role="status" aria-atomic="true">
        {jumpAnnouncement && <span key={jumpAnnouncement.request}>{jumpAnnouncement.text}</span>}
      </div>
      <div className="sbar">
        {header}
        {copyError && validSelection && <span className="tag warn" role="status">Couldn’t copy the selection. Try copying again.</span>}
      </div>
      {controls}
      {empty ?? (
        <div ref={grid} className={worksheet ? "grid worksheet-grid" : "grid"} role="region" aria-label="Rows" tabIndex={-1}>
          <table
            ref={table}
            style={worksheet ? { width: worksheet.widths.reduce((sum, width) => sum + width, worksheetGutterWidth) } : undefined}
            role="grid"
            aria-label={worksheet ? "Worksheet cells" : "Preview cells"}
            aria-readonly="true"
            aria-multiselectable="true"
            aria-rowcount={rowOffset === undefined ? undefined : (worksheet ? Math.max(total, rowOffset + rowCount) : total) + 1}
            aria-colcount={worksheet ? Math.max(worksheet.totalColumns, worksheet.columnOffset + columnCount) + 1 : undefined}
            aria-keyshortcuts={onAction && rowCount ? "Shift+F10" : undefined}
            aria-activedescendant={active && cellId(active.row, active.column)}
            tabIndex={!rowCount && columnCount && onAction ? -1 : 0}
            onFocus={(e) => {
              if (e.target === e.currentTarget && !active && rowCount && columnCount) selectCell({ row: 0, column: 0 }, false);
            }}
            onKeyDown={handleGridKey}
            onCopy={(e) => {
              if (e.target !== e.currentTarget || !validSelection) return;
              e.preventDefault();
              try {
                e.clipboardData.setData("text/plain", selectionTsv(rows, validSelection));
                setCopyError(false);
              } catch {
                setCopyError(true);
              }
            }}
          >
            {worksheet && (
              <colgroup>
                <col style={{ width: worksheetGutterWidth }} />
                {worksheet.widths.map((width, index) => <col key={index} style={{ width }} />)}
              </colgroup>
            )}
            <thead>
              <tr aria-rowindex={rowOffset === undefined ? undefined : 1}>
                <th className="rn" scope="col">#</th>
                {columns.map((c, j) => (
                  <th key={`${j}:${c.name}`} className={added?.has(c.name) ? "new" : ""} scope="col"
                    aria-colindex={worksheet ? worksheet.columnOffset + j + 2 : undefined}>
                    {worksheet ? c.name : (
                      <button
                        className="colh"
                        aria-haspopup={onAction ? "menu" : undefined}
                        aria-expanded={menu?.column === c.name}
                        tabIndex={rowCount ? -1 : undefined}
                        disabled={!onAction}
                        onClick={(e) => openMenu(j, e.currentTarget)}
                      >
                        <span className="ch1">
                          <span className="ty">{kindTag[c.kind] ?? "Aa"}</span>
                          {c.name || "(blank)"}
                          {onAction && <span className="chev">▾</span>}
                        </span>
                        {!hideProfiles && <Profile c={c} total={total} />}
                      </button>
                    )}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i} aria-rowindex={rowOffset === undefined ? undefined : rowOffset + i + 2}
                  style={worksheet?.rowHeights?.[i] ? { "--worksheet-row-height": `${worksheet.rowHeights[i]}pt` } as CSSProperties : undefined}>
                  <th scope="row" className="rn">{rowNumber(i)}</th>
                  {columns.map((c, j) => {
                    const v = r?.[j] ?? "";
                    const selected = !!bounds && i >= bounds.top && i <= bounds.bottom && j >= bounds.left && j <= bounds.right;
                    const changed = changedCells?.(i, j);
                    const isActive = active?.row === i && active.column === j;
                    return (
                      <td
                        key={j}
                        id={cellId(i, j)}
                        role="gridcell"
                        aria-label={isActive || changed ? `Row ${rowNumber(i)}, column ${c.name || j + 1}, ${v === "" ? "blank" : v}${changed ? ", changed by fill" : ""}` : undefined}
                        data-changed={changed || undefined}
                        aria-selected={selected}
                        aria-colindex={worksheet ? worksheet.columnOffset + j + 2 : undefined}
                        data-active={isActive || undefined}
                        className={`${added?.has(c.name) ? "new " : ""}${c.kind === "number" ? "n " : ""}${v === "" ? "blank" : ""}`}
                        title={worksheet || v.length > 30 ? v : undefined}
                        style={worksheet?.cellStyle(i, j)}
                        onMouseDown={(e) => {
                          if (e.button === 0) e.preventDefault();
                        }}
                        onClick={(e) => {
                          table.current?.focus({ preventScroll: true });
                          selectCell({ row: i, column: j }, e.shiftKey && !!validSelection);
                        }}
                      >
                        {v}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
          {!worksheet && total > rows.length && <div className="more-rows">
            {rowOffset !== undefined && rowCount > 0
              ? `Showing rows ${(rowOffset + 1).toLocaleString()}–${(rowOffset + rowCount).toLocaleString()} of ${total.toLocaleString()}.`
              : <>Showing {rows.length.toLocaleString()} of {total.toLocaleString()} rows.{!hideProfiles && " Profiles above cover every row."}</>}
          </div>}
        </div>
      )}
      {footer}
      {menu && onAction && (
        <ColumnMenu
          column={menu.column}
          kind={menu.kind}
          columns={columns.map((c) => c.name)}
          anchor={menu.rect}
          onClose={closeMenu}
          onAction={(a) => {
            closeMenu();
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
