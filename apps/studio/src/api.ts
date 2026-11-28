import { invoke } from "@tauri-apps/api/core";

export type Inspection = {
  sheets: string[];
  sheet: string;
  rows: string[][];
  offset: number;
  totalRows: number;
  truncated: boolean;
};
export type WorkbookBorder = { style: number; color?: string };
export type WorkbookCellStyle = {
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  fontColor?: string;
  fillColor?: string;
  fontSize?: number;
  fontFamily?: string;
  horizontal?: string;
  vertical?: string;
  wrap?: boolean;
  borders?: Partial<Record<"left" | "right" | "top" | "bottom", WorkbookBorder>>;
};
export type WorkbookPage = {
  sheets: string[];
  sheet: string;
  offset: number;
  columnOffset: number;
  totalRows: number;
  totalColumns: number;
  rows: string[][];
  styleIds: number[][];
  styles: Record<number, WorkbookCellStyle>;
  widths: number[];
  rowHeights?: number[];
  generalAlignment?: string[];
  unsupported: string[];
};
export type Validation = {
  name: string;
  inputs: { name: string; label: string }[];
  sheets: string[];
};
export type RunResult = {
  path: string;
  sha256: string;
  preview?: Inspection;
  previewWarning?: string;
};
export type RunManifest = {
  version: 1;
  completedAt: string;
  workflowPath: string;
  workflowName: string;
  yamlSha256: string;
  inputs?: { name: string; path: string; sha256?: string | null; changed?: boolean }[];
  outputPath: string;
  outputSha256: string;
  uncertain?: boolean;
};
export type RunHistoryEntry = {
  id: string;
  outputPath: string;
  available: boolean;
  completedAt: string;
  manifest?: RunManifest;
};
export type RunHistoryPage = { runs: RunHistoryEntry[]; nextOffset?: number | null };
export type ColumnProfile = {
  name: string;
  kind: "number" | "text" | "bool" | "empty" | "mixed";
  filled: number;
  distinct: number;
  min?: number;
  max?: number;
  bins?: number[];
};
export type StepComparison = {
  input: number;
  before: string[][] | null;
  changedRows: number;
  changedCells: number;
  changed?: (number[] | null)[];
  removed?: string[][];
  removedPositions?: number[];
  keptPositions?: number[];
};

export type StepTrace = {
  comparison?: StepComparison;
  step: number;
  name: string;
  line: number;
  skipped?: boolean;
  columns: ColumnProfile[];
  total: number;
  rows?: string[][];
  log?: string[];
  error?: string;
  ms: number;
};
/** `mocks`: sql and rest steps were answered from the workflow's mocks file. */
export type Trace = { steps: StepTrace[]; format: string; mocks?: boolean };

export class RunnerError extends Error {
  readonly code?: string;

  constructor(message: string, code?: string) {
    super(message);
    this.name = "RunnerError";
    this.code = code;
  }
}

/**
 * A stand-in for the native host, for browser development and UI tests:
 * window.__maiBridge = { invoke(command, args) } answers the same commands
 * the Tauri host does (runner_request, list_workflows, read_workflow...).
 */
type Bridge = {
  invoke: (command: string, args?: Record<string, unknown>) => Promise<unknown>;
  listen?: (event: string, handler: (payload: unknown) => void) => () => void;
  /** The browser host (src/web): an in-memory workspace instead of a folder. */
  web?: boolean;
};
const bridge = () => (window as unknown as { __maiBridge?: Bridge }).__maiBridge;

export function isDesktop() {
  return "__TAURI_INTERNALS__" in window || !!bridge();
}
/**
 * Studio runs in a browser with its own engine (vite --mode web): there is
 * no folder to open, files are imported or dropped, and results downloaded.
 */
export function isWebTool() {
  return !!bridge()?.web;
}
/** Call a native host command. */
export function native<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  const b = bridge();
  if (b) return b.invoke(command, args) as Promise<T>;
  return invoke<T>(command, args);
}
type RunnerReply<T> = { ok: boolean; result?: T; error?: { message: string; code?: string } };

function cancelled() {
  return new DOMException("Test run cancelled.", "AbortError");
}

async function runCancellableTests<T>(payload: Record<string, unknown>, id: string, signal: AbortSignal) {
  if (signal.aborted) throw cancelled();
  // Reserve the ID first so cancel can arrive before the run command without being lost.
  await native("runner_prepare_tests", { id });
  const cancel = () => native("runner_cancel_tests", { id }).catch(() => undefined);
  if (signal.aborted) {
    void cancel();
    throw cancelled();
  }
  let rejectInterruption!: (error: DOMException) => void;
  const interruption = new Promise<never>((_, reject) => { rejectInterruption = reject; });
  const abort = () => {
    void cancel();
    rejectInterruption(cancelled());
  };
  try {
    signal.addEventListener("abort", abort, { once: true });
    return await Promise.race([
      interruption,
      native<RunnerReply<T>>("runner_request", { request: payload, cancellable: true }),
    ]);
  } finally {
    signal.removeEventListener("abort", abort);
  }
}

/** Subscribe to an event the native host emits; resolves to an unsubscribe function. */
export async function listenNative<T>(event: string, handler: (payload: T) => void): Promise<() => void> {
  const b = bridge();
  if (b) return b.listen ? b.listen(event, (p) => handler(p as T)) : () => undefined;
  if (!isDesktop()) return () => undefined;
  const { listen } = await import("@tauri-apps/api/event");
  return listen<T>(event, (e) => handler(e.payload));
}

/** Send a request to the Go runner through the native host. */
export async function request<T>(command: string, params: Record<string, unknown>, signal?: AbortSignal): Promise<T> {
  if (!isDesktop())
    throw new Error("Open the Mai Studio desktop app to read files and run workflows.");
  const id = crypto.randomUUID();
  const payload = { version: 1, id, command, params };
  const reply = command === "runTests" && signal
    ? await runCancellableTests<T>(payload, id, signal)
    : await native<RunnerReply<T>>("runner_request", { request: payload });
  if (!reply.ok) throw new RunnerError(reply.error?.message ?? "Runner request failed.", reply.error?.code);
  return reply.result as T;
}
