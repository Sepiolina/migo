// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { readDrafts } from "./draftRecovery";

const path = "workflows/current.yaml";
const original = "name: Current workflow\nsteps: []\n";
const draft = original + "# local draft\n";
const latest = draft + "# newer edit\n";
const external = original + "# external edit\n";
const workspace = "/first";
const host = window as Window & { __maiBridge?: { invoke: (command: string, args?: Record<string, unknown>) => Promise<unknown> } };

function bridge() {
  const files = new Map([[path, { path, source: original, revision: "initial" }]]);
  const state = {
    folder: workspace,
    pending: undefined as Promise<void> | undefined,
    files,
  };
  const invoke = vi.fn(async (command: string, args?: Record<string, unknown>) => {
    if (command === "choose_workspace") return { path: state.folder };
    if (command === "list_workflows") return { paths: [...files.keys()] };
    if (command === "read_workflow") {
      if (!files.has(String(args?.path))) throw "File not found";
      return files.get(String(args?.path));
    }
    if (command === "save_workflow") {
      await state.pending;
      const requested = String(args?.path);
      const file = files.get(requested);
      if (args?.expectedRevision !== (file?.revision ?? null)) throw "Workflow conflict: file changed externally";
      const saved = { path: requested, source: String(args?.source), revision: "saved" };
      files.set(requested, saved);
      return saved;
    }
    if (command === "file_revisions") return {};
    if (command === "runner_request") return { ok: true, result: { steps: [], inputs: [], sheets: [] } };
    throw new Error(`Unexpected bridge command ${command}`);
  });
  host.__maiBridge = { invoke };
  return { state, invoke };
}

const button = (name: string | RegExp) => screen.getByRole("button", { name });
const yaml = () => screen.getByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement;

async function openFolder() {
  await act(async () => fireEvent.click(button("Open folder…")));
}

async function openCurrent() {
  fireEvent.click(button(/^Current workflow/));
  await screen.findByRole("tab", { name: "YAML" });
  fireEvent.click(screen.getByRole("tab", { name: "YAML" }));
}

async function typeDraft() {
  await openFolder();
  await openCurrent();
  yaml().focus();
  fireEvent.change(yaml(), { target: { value: draft } });
  await waitFor(() => expect(readDrafts(localStorage, workspace)[0]?.source).toBe(draft));
}

beforeEach(() => localStorage.clear());
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  delete host.__maiBridge;
});

describe("workflow recovery", () => {
  it("checkpoints focused YAML, opens the saved version without consuming it, and restores dirty text", async () => {
    bridge();
    const first = render(<App />);
    await typeDraft();
    expect(document.activeElement).toBe(yaml());
    first.unmount();

    render(<App />);
    await openFolder();
    fireEvent.click(button(/^Current workflow/));
    await screen.findByRole("dialog", { name: "Recover workflow draft" });
    await act(async () => fireEvent.click(button("Open saved version")));
    expect(readDrafts(localStorage, workspace)[0]?.source).toBe(draft);
    fireEvent.click(button("Workflows"));

    fireEvent.click(button(/^Current workflow/));
    expect((await screen.findByRole("region", { name: "Draft version YAML" })).textContent).toBe(draft);
    await act(async () => fireEvent.click(button("Restore draft")));
    fireEvent.click(screen.getByRole("tab", { name: "YAML" }));
    expect(yaml().value).toBe(draft);
    expect(screen.getByText("— Edited")).toBeTruthy();
  });

  it("compares a rejected save with the latest disk YAML and saves the newer editor text as a copy", async () => {
    const { state, invoke } = bridge();
    render(<App />);
    await typeDraft();
    state.files.set(path, { path, source: external, revision: "external" });
    fireEvent.click(button("Save"));
    const review = await screen.findByRole("dialog", { name: "Workflow save conflict" });
    expect(within(review).getByRole("region", { name: "Saved version YAML" }).textContent).toBe(external);
    expect(within(review).getByRole("region", { name: "Draft version YAML" }).textContent).toBe(draft);
    fireEvent.click(within(review).getByRole("button", { name: "Save copy" }));
    const copy = screen.getByRole("dialog", { name: "Save a workflow copy" });
    await act(async () => fireEvent.click(within(copy).getByRole("button", { name: "Save copy" })));
    expect(invoke).toHaveBeenCalledWith("save_workflow", { path, source: draft, expectedRevision: "initial" });
    expect(state.files.get(path)?.source).toBe(external);
    expect(state.files.get("workflows/current-copy.yaml")?.source).toBe(draft);
    expect(readDrafts(localStorage, workspace)).toEqual([]);
  });

  it("rebases text typed during a pending save before the debounce can run", async () => {
    const { state } = bridge();
    let finish!: () => void;
    state.pending = new Promise((resolve) => { finish = resolve; });
    const first = render(<App />);
    await typeDraft();
    fireEvent.click(button("Save"));
    fireEvent.change(yaml(), { target: { value: latest } });
    await act(async () => finish());
    expect(state.files.get(path)?.source).toBe(draft);
    expect(readDrafts(localStorage, workspace)[0]).toMatchObject({
      source: latest,
      savedSource: draft,
      baseRevision: "saved",
    });
    first.unmount();

    render(<App />);
    await openFolder();
    fireEvent.click(button(/^Current workflow/));
    expect((await screen.findByRole("region", { name: "Draft version YAML" })).textContent).toBe(latest);
    await act(async () => fireEvent.click(button("Restore draft")));
    expect(screen.getByText("— Edited")).toBeTruthy();
  });

  it("keeps recovery workspace-scoped and removes the selected draft when deliberately discarding", async () => {
    const { state } = bridge();
    const first = render(<App />);
    await typeDraft();
    first.unmount();

    state.folder = "/second";
    render(<App />);
    await openFolder();
    expect(screen.queryByRole("heading", { name: "Recovery drafts" })).toBeNull();
    state.folder = workspace;
    await act(async () => fireEvent.click(button("Change folder…")));
    fireEvent.click(button(/^Current workflow/));
    await screen.findByRole("dialog", { name: "Recover workflow draft" });
    await act(async () => fireEvent.click(button("Restore draft")));
    fireEvent.click(button("Workflows"));
    fireEvent.click(button("Discard changes and continue"));
    expect(readDrafts(localStorage, workspace)).toEqual([]);
    expect(screen.queryByRole("heading", { name: "Recovery drafts" })).toBeNull();
  });
  it("moves an unassigned template checkpoint into the first chosen folder", async () => {
    const { state } = bridge();
    render(<App />);
    fireEvent.click(button(/Summarize by group/));
    fireEvent.click(screen.getByRole("tab", { name: "YAML" }));
    const source = yaml().value + "# unassigned edit\n";
    fireEvent.change(yaml(), { target: { value: source } });
    await waitFor(() => expect(readDrafts(localStorage, "unassigned")[0]?.source).toBe(source));

    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    fireEvent.change(screen.getByRole("combobox", { name: "Search commands" }), { target: { value: "Open project folder" } });
    await act(async () => fireEvent.keyDown(screen.getByRole("combobox", { name: "Search commands" }), { key: "Enter" }));
    expect(readDrafts(localStorage, "unassigned")).toEqual([]);
    expect(readDrafts(localStorage, workspace)[0]?.source).toBe(source);

    fireEvent.click(button("Save"));
    const dialog = screen.getByRole("dialog", { name: "Save workflow" });
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Save" })));
    expect(state.files.get("workflows/parts-summary.yaml")?.source).toBe(source);
    expect(readDrafts(localStorage, workspace)).toEqual([]);
  });

});
