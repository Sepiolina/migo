// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { parse } from "yaml";
import App from "./App";
import type { StepTrace } from "./api";

vi.mock("@tauri-apps/api/window", () => ({ getCurrentWindow: () => ({ onCloseRequested: async () => () => undefined }) }));
vi.mock("@tauri-apps/api/core", () => ({ invoke: async () => { throw new Error("no host"); } }));

const col = (name: string, kind: "text" | "number" = "text") => ({ name, kind, filled: 2, distinct: 2 });
/** A runner stand-in: the trace after each step of the parts template. */
const handlers: Record<string, (payload: unknown) => void> = {};
const emit = (event: string, payload: unknown) => act(async () => handlers[event]?.(payload));
function bridge() {
  const calls: { command: string; args?: Record<string, unknown> }[] = [];
  const trace = (step: number): StepTrace[] =>
    [0, 1, 2, 3].map((i) => ({
      step: i,
      name: ["from", "filter", "group", "save"][i],
      line: i + 5,
      columns: i < 2 ? [col("Part Number"), col("Description"), col("Quantity", "number")] : [col("Part Number"), col("Quantity", "number")],
      total: i === 0 ? 4 : i === 1 ? 3 : 2,
      rows: i === step ? [["00123", i < 2 ? "Resistor" : "5", "2"], ["00456", "Capacitor", "1"]] : undefined,
      ms: 1,
    }));
  (window as unknown as { __maiBridge: unknown }).__maiBridge = {
    invoke: async (command: string, args?: Record<string, unknown>) => {
      calls.push({ command, args });
      if (command === "choose_workspace") return { path: "/work" };
      if (command === "list_workflows") return { paths: [] };
      if (command === "choose_sample_files") return { files: [{ name: "parts.xlsx", path: "samples/parts.xlsx", copied: true }] };
      if (command === "runner_request") {
        const req = (args as { request: { command: string; params: { step: number } } }).request;
        if (req.command === "trace") return { ok: true, result: { steps: trace(req.params.step), format: "xlsx" } };
        if (req.command === "validate") return { ok: true, result: { name: "x", inputs: [], sheets: [] } };
      }
      throw new Error(`unexpected ${command}`);
    },
    listen: (event: string, handler: (payload: unknown) => void) => {
      handlers[event] = handler;
      return () => delete handlers[event];
    },
  };
  return calls;
}
const yaml = () => (screen.getByLabelText("Workflow YAML") as HTMLTextAreaElement).value;

describe("Studio", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => {
    cleanup();
    delete (window as unknown as { __maiBridge?: unknown }).__maiBridge;
  });

  it("opens a template in the browser preview and edits it through the inspector", async () => {
    render(<App />);
    expect(screen.getByText(/browser preview/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Summarize by group/ }));
    const nav = screen.getByRole("complementary", { name: "Workflow" });
    expect(within(nav).getByText("Keep rows where num(Quantity) > 0")).toBeTruthy();
    fireEvent.click(within(nav).getByText("Keep rows where num(Quantity) > 0"));
    const condition = screen.getByLabelText("Condition") as HTMLTextAreaElement;
    fireEvent.change(condition, { target: { value: "num(Quantity) > 1" } });
    fireEvent.blur(condition);
    expect(parse(yaml()).steps[1].filter).toBe("num(Quantity) > 1");
    expect(yaml()).toContain("  - filter: num(Quantity) > 1\n");
    // Undo restores the source.
    fireEvent.click(screen.getAllByRole("button", { name: "Undo" })[0]);
    expect(parse(yaml()).steps[1].filter).toBe("num(Quantity) > 0");
  });

  it("adds steps from the command menu and documents the endpoint", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: /Summarize by group/ }));
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    fireEvent.change(screen.getByLabelText("Search commands"), { target: { value: "remove columns" } });
    fireEvent.keyDown(screen.getByLabelText("Search commands"), { key: "Enter" });
    expect(parse(yaml()).steps[1]).toEqual({ drop: ["Column"] });
    fireEvent.click(screen.getByText("/api/parts-summary"));
    expect(screen.getByText("/api/parts-summary/run")).toBeTruthy();
    const route = screen.getByLabelText("Route /api/");
    fireEvent.change(route, { target: { value: "parts" } });
    fireEvent.blur(route);
    expect(parse(yaml()).service).toEqual({ route: "parts" });
    expect(screen.getByText("/api/parts/run")).toBeTruthy();
  });

  it("previews each step with the runner and turns column actions into steps", async () => {
    const calls = bridge();
    render(<App />);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open folder…" })));
    fireEvent.click(screen.getByRole("button", { name: /Summarize by group/ }));
    const grid = await screen.findByRole("region", { name: "Rows" }, { timeout: 3000 });
    expect(within(grid).getByText("Resistor")).toBeTruthy();
    const trace = calls.find((c) => (c.args as { request?: { command: string } })?.request?.command === "trace")!;
    expect((trace.args as { request: { params: { inputs: unknown } } }).request.params.inputs).toEqual({ parts: "examples/parts.xlsx" });
    const header = within(grid).getAllByRole("button").find((b) => b.textContent?.includes("Quantity"))!;
    fireEvent.click(header);
    fireEvent.click(screen.getByRole("menuitem", { name: "Sort largest first" }));
    expect(parse(yaml()).steps[1]).toEqual({ sort: { by: ["Quantity"], desc: true, numeric: true } });
    // The new step is selected and previewed in its turn.
    await waitFor(() => expect(document.querySelector(".nav .row.sel")?.textContent).toContain("Sort by Quantity, largest first"));
  });

  it("binds a sample file chosen with Browse", async () => {
    const calls = bridge();
    render(<App />);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open folder…" })));
    fireEvent.click(screen.getByRole("button", { name: /Summarize by group/ }));
    const nav = screen.getByRole("complementary", { name: "Workflow" });
    const source = within(nav).getAllByRole("button").find((b) => b.querySelector(".name")?.firstChild?.textContent === "parts")!;
    fireEvent.click(source);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Browse…" })));
    expect((screen.getByLabelText("Path") as HTMLInputElement).value).toBe("samples/parts.xlsx");
    await waitFor(() => {
      const traces = calls.filter((c) => (c.args as { request?: { command: string } })?.request?.command === "trace");
      expect((traces.at(-1)!.args as { request: { params: { inputs: unknown } } }).request.params.inputs).toEqual({ parts: "samples/parts.xlsx" });
    });
  });

  const lastInputs = (calls: { args?: Record<string, unknown> }[]) => {
    const traces = calls.filter((c) => (c.args as { request?: { command: string } })?.request?.command === "trace");
    return (traces.at(-1)?.args as { request: { params: { inputs: unknown } } } | undefined)?.request.params.inputs;
  };

  it("imports sample files from the source list and from files dropped on the window", async () => {
    const calls = bridge();
    render(<App />);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open folder…" })));
    fireEvent.click(screen.getByRole("button", { name: /Summarize by group/ }));
    // The row's import button binds the picked file to that source.
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Import a sample file for parts" })));
    expect(calls.find((c) => c.command === "choose_sample_files")?.args).toEqual({ multiple: false });
    await waitFor(() => expect(lastInputs(calls)).toEqual({ parts: "samples/parts.xlsx" }));
    // A dropped file goes to the source; one that fails to import is reported.
    await emit("samples-drag", true);
    expect(screen.getByText("Drop to use as sample files")).toBeTruthy();
    await emit("samples-dropped", { files: [{ name: "new.csv", path: "samples/new.csv", copied: true }, { name: "x.txt", error: "Choose an .xlsx, .xls or .csv file" }] });
    await emit("samples-drag", false);
    expect(screen.queryByText("Drop to use as sample files")).toBeNull();
    await waitFor(() => expect(lastInputs(calls)).toEqual({ parts: "samples/new.csv" }));
    fireEvent.click(screen.getByRole("tab", { name: "Run log" }));
    expect(screen.getByText(/Could not import x\.txt/)).toBeTruthy();
    expect(screen.getByText(/parts ← samples\/new\.csv \(copied into the folder\)/)).toBeTruthy();
  });

  it("resizes the panels and remembers the sizes", async () => {
    bridge();
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: /Summarize by group/ }));
    const nav = screen.getByRole("separator", { name: "Resize the workflow panel" });
    // (Dragging is covered by scripts/smoke-studio.mjs: jsdom has no pointer coordinates.)
    fireEvent.keyDown(nav, { key: "ArrowRight" });
    fireEvent.keyDown(nav, { key: "ArrowRight" });
    expect(nav.getAttribute("aria-valuenow")).toBe("292");
    const insp = screen.getByRole("separator", { name: "Resize the inspector" });
    fireEvent.keyDown(insp, { key: "ArrowLeft" }); // the inspector grows to the left
    expect(insp.getAttribute("aria-valuenow")).toBe("316");
    fireEvent.keyDown(screen.getByRole("separator", { name: "Resize the console" }), { key: "ArrowUp" });
    const app = document.querySelector(".app") as HTMLElement;
    expect(app.style.getPropertyValue("--nav-w")).toBe("292px");
    expect(app.style.getPropertyValue("--insp-w")).toBe("316px");
    expect(app.style.getPropertyValue("--console-h")).toBe("144px");
    expect(localStorage.getItem("mai.layout.nav")).toBe("292");
    fireEvent.doubleClick(nav);
    expect(nav.getAttribute("aria-valuenow")).toBe("260");
  });
});
