import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { request, RunnerError, type ColumnProfile, type WorkbookBorder, type WorkbookCellStyle, type WorkbookPage } from "./api";
import { DataGrid } from "./DataGrid";

const lastRow = 1_048_576;
const lastColumn = 16_384;
const pageRows = 100;
const pageColumns = 32;
const maxDigitWidth = 7; // Calibri 11 at 96 DPI.
const defaultPageSize = { limit: pageRows, columnLimit: pageColumns };

type Address = { row: number; column: number };
type Viewport = { sheet?: string; offset: number; columnOffset: number; target?: Address };
type PageSize = { limit: number; columnLimit: number };
type Inspection = { revision: string; viewport: Viewport; retry: number; page?: WorkbookPage; size?: PageSize; error?: string };
type Props = {
  workspace: string;
  path: string;
  revision: string | null | undefined;
  initialSheet?: string;
  completedAt: number;
  stale: boolean;
  onRefresh: () => void;
  canRefresh: boolean;
  onSheetChange?: (sheet: string) => void;
  revisionError?: string;
  onRetryRevision?: () => void;
};

function columnLetters(column: number) {
  let result = "";
  for (let value = column + 1; value > 0; value = Math.floor((value - 1) / 26)) {
    result = String.fromCharCode(65 + (value - 1) % 26) + result;
  }
  return result;
}

function columnPixels(width: number) {
  // OOXML widths already include Excel's padding.
  return width > 0 ? Math.floor((width + Math.floor(128 / maxDigitWidth) / 256) * maxDigitWidth) : 24;
}

function parseAddress(input: string): Address | null {
  const match = /^\$?([A-Z]{1,3})\$?([1-9][0-9]{0,6})$/i.exec(input.trim());
  if (!match) return null;
  const column = [...match[1].toUpperCase()].reduce((value, letter) => value * 26 + letter.charCodeAt(0) - 64, 0);
  const row = Number(match[2]);
  return row <= lastRow && column <= lastColumn ? { row: row - 1, column: column - 1 } : null;
}

const borderStyles: Record<number, string> = {
  1: "1px solid", 2: "2px solid", 3: "1px dashed", 4: "1px dotted", 5: "3px solid", 6: "3px double",
  7: "1px dotted", 8: "2px dashed", 9: "1px dashed", 10: "2px dashed", 11: "1px dashed", 12: "2px dashed", 13: "2px dashed",
};

function cssBorder(border?: WorkbookBorder) {
  const line = border && borderStyles[border.style];
  return line && `${line} ${border.color || "#000000"}`;
}

function cellStyle(style: WorkbookCellStyle = {}, general?: string): CSSProperties {
  let horizontal = style.horizontal;
  if (!horizontal || horizontal === "general") {
    horizontal = general === "r" ? "right" : general === "c" ? "center" : "left";
  }
  const vertical = style.vertical;
  return {
    "--workbook-fill": style.fillColor,
    color: style.fontColor,
    fontFamily: style.fontFamily ? `${JSON.stringify(style.fontFamily)}, sans-serif` : undefined,
    fontSize: style.fontSize ? `${style.fontSize}pt` : undefined,
    fontWeight: style.bold ? "bold" : undefined,
    fontStyle: style.italic ? "italic" : undefined,
    textDecoration: style.underline ? "underline" : undefined,
    textAlign: horizontal === "centerContinuous" ? "center"
      : horizontal === "left" || horizontal === "center" || horizontal === "right" || horizontal === "justify" ? horizontal : undefined,
    verticalAlign: vertical === "center" ? "middle" : vertical === "top" || vertical === "bottom" ? vertical : undefined,
    whiteSpace: style.wrap ? "pre-wrap" : undefined,
    overflowWrap: style.wrap ? "anywhere" : undefined,
    borderLeft: cssBorder(style.borders?.left),
    borderRight: cssBorder(style.borders?.right),
    borderTop: cssBorder(style.borders?.top),
    borderBottom: cssBorder(style.borders?.bottom),
  } as CSSProperties;
}

export function WorkbookPreview({
  workspace, path, revision, initialSheet, completedAt, stale, onRefresh, canRefresh,
  onSheetChange, revisionError, onRetryRevision,
}: Props) {
  const [viewport, setViewport] = useState<Viewport>({ sheet: initialSheet, offset: 0, columnOffset: 0 });
  const [inspection, setInspection] = useState<Inspection>();
  const [sheetList, setSheetList] = useState<{ revision: string; names: string[]; sheet: string }>();
  const [retry, setRetry] = useState(0);
  const [address, setAddress] = useState("");
  const [invalidAddress, setInvalidAddress] = useState(false);
  const [jump, setJump] = useState<(Address & { request: number })>();
  const previousSheet = useRef(initialSheet);
  const jumpRequest = useRef(0);
  const pageSizes = useRef<{ key: string; sheets: Map<string | undefined, PageSize> }>(undefined);
  const addressId = useId();
  const addressErrorId = useId();

  function openSheet(sheet?: string) {
    setViewport({ sheet, offset: 0, columnOffset: 0 });
    setAddress("");
    setInvalidAddress(false);
    setJump(undefined);
  }

  useLayoutEffect(() => {
    if (previousSheet.current === initialSheet) return;
    previousSheet.current = initialSheet;
    if (initialSheet !== viewport.sheet) openSheet(initialSheet);
  }, [initialSheet, viewport.sheet]);

  useEffect(() => {
    if (!revision) return;
    const inspectedRevision = revision;
    let active = true;
    const key = JSON.stringify([workspace, path, inspectedRevision]);
    if (pageSizes.current?.key !== key) pageSizes.current = { key, sheets: new Map() };
    const sizes = pageSizes.current.sheets;
    const { target, ...area } = viewport;
    let sheet = area.sheet;
    let size = sizes.get(sheet) ?? defaultPageSize;
    async function load() {
      while (active) {
        try {
          const offset = target ? Math.max(area.offset, target.row - size.limit + 1) : area.offset;
          const columnOffset = target ? Math.max(area.columnOffset, target.column - size.columnLimit + 1) : area.columnOffset;
          const page = await request<WorkbookPage>("inspectWorkbookPage", { workspace, path, sheet, offset, columnOffset, ...size });
          if (!active) return;
          sizes.set(sheet, size);
          sizes.set(page.sheet, size);
          setInspection({ revision: inspectedRevision, viewport, retry, page, size });
          setSheetList({ revision: inspectedRevision, names: page.sheets, sheet: page.sheet });
          return;
        } catch (error) {
          if (!active) return;
          if (sheet && error instanceof Error && error.message.startsWith("Unknown sheet")) {
            sheet = undefined;
            size = sizes.get(sheet) ?? defaultPageSize;
            continue;
          }
          if (error instanceof RunnerError && error.code === "response_too_large" && (size.limit > 1 || size.columnLimit > 1)) {
            size = {
              limit: Math.max(1, Math.ceil(size.limit / 2)),
              columnLimit: Math.max(1, Math.ceil(size.columnLimit / 2)),
            };
            continue;
          }
          setInspection({ revision: inspectedRevision, viewport, retry, error: error instanceof Error ? error.message : String(error) });
          return;
        }
      }
    }
    void load();
    return () => { active = false; };
  }, [workspace, path, revision, viewport, retry]);

  const current = inspection?.revision === revision && inspection?.viewport === viewport && inspection?.retry === retry ? inspection : undefined;
  const page = current?.page;
  const known = sheetList?.revision === revision ? sheetList : undefined;
  const sheet = page?.sheet ?? viewport.sheet ?? known?.sheet;
  const rows = page?.rows ?? [];
  const widths = page?.widths ?? [];
  const columns = widths.map((_, index): ColumnProfile => ({
    name: columnLetters((page?.columnOffset ?? 0) + index), kind: "text", filled: 0, distinct: 0,
  }));
  const focusCell = jump && page ? {
    row: jump.row - page.offset, column: jump.column - page.columnOffset, request: jump.request,
  } : undefined;
  const ranAt = new Date(completedAt);
  const file = path.replace(/[\\/]+$/, "").split(/[\\/]/).at(-1) ?? path;

  function selectSheet(name: string) {
    if (name === sheet) return;
    openSheet(name);
    onSheetChange?.(name);
  }

  function switchTab(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const names = known?.names ?? [];
    let next: number;
    switch (event.key) {
      case "ArrowLeft": next = (index - 1 + names.length) % names.length; break;
      case "ArrowRight": next = (index + 1) % names.length; break;
      case "Home": next = 0; break;
      case "End": next = names.length - 1; break;
      default: return;
    }
    event.preventDefault();
    event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
    selectSheet(names[next]);
  }

  function goTo(target: Address, fromGrid = false) {
    setAddress(`${columnLetters(target.column)}${target.row + 1}`);
    setInvalidAddress(false);
    setJump({ ...target, request: ++jumpRequest.current });
    const rowVisible = page && target.row >= page.offset && target.row < page.offset + rows.length;
    const columnVisible = page && target.column >= page.columnOffset && target.column < page.columnOffset + columns.length;
    if (rowVisible && columnVisible) return;
    setViewport({
      sheet,
      target,
      offset: rowVisible ? page.offset : fromGrid && page && target.row < page.offset
        ? Math.max(0, target.row - (current?.size?.limit ?? pageRows) + 1) : target.row,
      columnOffset: columnVisible ? page.columnOffset : fromGrid && page && target.column < page.columnOffset
        ? Math.max(0, target.column - (current?.size?.columnLimit ?? pageColumns) + 1) : target.column,
    });
  }

  const updateAddress = useCallback(({ row, column }: Address) => {
    if (!page) return;
    setAddress(`${columnLetters(page.columnOffset + column)}${page.offset + row + 1}`);
    setInvalidAddress(false);
  }, [page]);

  function navigate({ row, column }: Address) {
    if (!page) return;
    goTo({
      row: Math.max(0, Math.min(page.offset + row, Math.max(page.totalRows, page.offset + rows.length) - 1)),
      column: Math.max(0, Math.min(page.columnOffset + column, Math.max(page.totalColumns, page.columnOffset + columns.length) - 1)),
    }, true);
  }

  const status = revisionError ? (
    <div className="empty" role="alert">
      <b>Couldn’t check the workbook file</b>
      <div className="err">{revisionError}</div>
      {onRetryRevision && <button className="tb" onClick={onRetryRevision}>Retry</button>}
    </div>
  ) : current?.error ? (
    <div className="empty" role="alert">
      <b>Couldn’t load the workbook preview</b>
      <div className="err">{current.error}</div>
      <button className="tb" onClick={() => setRetry(retry + 1)}>Retry</button>
    </div>
  ) : revision === null ? (
    <div className="empty" role="status">The output file is unavailable.</div>
  ) : <div className="empty" role="status">{revision ? "Loading worksheet…" : "Checking workbook file…"}</div>;

  return (
    <DataGrid
      header={
        <>
          <b>Workbook preview</b>
          <span className="meta workbook-filename" title={path}>{file}</span>
          <time className="meta" dateTime={ranAt.toISOString()} title={ranAt.toLocaleString()}>
            {ranAt.toLocaleString()}
          </time>
          {stale && <span className="tag warn">Previous run</span>}
          {stale && <button className="tb" onClick={onRefresh} disabled={!canRefresh}>Run to refresh</button>}
        </>
      }
      controls={known && (
        <div className="workbook-tools">
          <form onSubmit={(event) => {
            event.preventDefault();
            const target = parseAddress(address);
            if (target) goTo(target);
            else setInvalidAddress(true);
          }}>
            <label htmlFor={addressId}>Cell address</label>
            <input id={addressId} className="ctl" placeholder="A1" value={address}
              autoComplete="off" spellCheck={false} aria-invalid={invalidAddress}
              aria-describedby={invalidAddress ? addressErrorId : undefined}
              onChange={(event) => { setAddress(event.target.value); setInvalidAddress(false); }} />
            <button className="tb" type="submit">Go</button>
          </form>
          {invalidAddress && <span id={addressErrorId} role="alert">Enter a cell from A1 to XFD1048576.</span>}
          {page && <span className="workbook-range">
            {columnLetters(page.columnOffset)}{page.offset + 1}–{columnLetters(page.columnOffset + columns.length - 1)}{page.offset + rows.length}
          </span>}
          {page?.unsupported?.length ? (
            <details className="workbook-limit">
              <summary>Preview limitations</summary>
              <p>Check these features in Excel.</p>
              <ul>{page.unsupported.map((feature) => <li key={feature}>{feature}</li>)}</ul>
            </details>
          ) : null}
        </div>
      )}
      footer={known && (
        <div className="workbook-tabs" role="tablist" aria-label="Workbook sheets">
          {known.names.map((name, index) => (
            <button key={name} role="tab" aria-selected={name === sheet} tabIndex={name === sheet ? 0 : -1}
              onClick={() => selectSheet(name)} onKeyDown={(event) => switchTab(event, index)}>
              {name}
            </button>
          ))}
        </div>
      )}
      rows={rows}
      columns={columns}
      total={page?.totalRows ?? 0}
      rowOffset={page?.offset}
      empty={page ? undefined : status}
      worksheet={{
        columnOffset: page?.columnOffset ?? 0,
        totalColumns: page?.totalColumns ?? 0,
        widths: widths.map(columnPixels),
        rowHeights: page?.rowHeights,
        cellStyle: (row, column) => cellStyle(
          page?.styles?.[page.styleIds?.[row]?.[column]],
          page?.generalAlignment?.[row]?.[column],
        ),
        focusCell,
        onActivate: updateAddress,
        onNavigate: navigate,
      }}
    />
  );
}
