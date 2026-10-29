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
export function isDesktop() {
  return "__TAURI_INTERNALS__" in window;
}
export async function request<T>(
  command: string,
  params: Record<string, unknown>,
): Promise<T> {
  if (!isDesktop())
    throw new Error(
      "Open the Tauri desktop app to access local workspaces. Browser mode previews the editor only.",
    );
  const reply = await invoke<{
    ok: boolean;
    result?: T;
    error?: { message: string };
  }>("runner_request", {
    request: { version: 1, id: crypto.randomUUID(), command, params },
  });
  if (!reply.ok)
    throw new Error(reply.error?.message ?? "Runner request failed.");
  return reply.result as T;
}
