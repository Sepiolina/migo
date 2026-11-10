import { invoke } from "@tauri-apps/api/core";

export type Inspection = {
  sheets: string[];
  sheet: string;
  rows: string[][];
  offset: number;
  totalRows: number;
  truncated: boolean;
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
export type ColumnProfile = {
  name: string;
  kind: "number" | "text" | "bool" | "empty" | "mixed";
  filled: number;
  distinct: number;
  min?: number;
  max?: number;
  bins?: number[];
};
export type StepTrace = {
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
export type Trace = { steps: StepTrace[]; format: string };

/**
 * A stand-in for the native host, for browser development and UI tests:
 * window.__maiBridge = { invoke(command, args) } answers the same commands
 * the Tauri host does (runner_request, list_workflows, read_workflow...).
 */
type Bridge = { invoke: (command: string, args?: Record<string, unknown>) => Promise<unknown> };
const bridge = () => (window as unknown as { __maiBridge?: Bridge }).__maiBridge;

export function isDesktop() {
  return "__TAURI_INTERNALS__" in window || !!bridge();
}
/** Call a native host command. */
export function native<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  const b = bridge();
  if (b) return b.invoke(command, args) as Promise<T>;
  return invoke<T>(command, args);
}
/** Send a request to the Go runner through the native host. */
export async function request<T>(command: string, params: Record<string, unknown>): Promise<T> {
  if (!isDesktop())
    throw new Error("Open the Mai Studio desktop app to read files and run workflows. The browser preview edits YAML only.");
  const reply = await native<{ ok: boolean; result?: T; error?: { message: string } }>("runner_request", {
    request: { version: 1, id: crypto.randomUUID(), command, params },
  });
  if (!reply.ok) throw new Error(reply.error?.message ?? "Runner request failed.");
  return reply.result as T;
}
