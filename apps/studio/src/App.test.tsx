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
      if (command === "runner_request") {
        const req = (args as { request: { command: string; params: { step: number } } }).request;
        if (req.command === "trace") return { ok: true, result: { steps: trace(req.params.step), format: "xlsx" } };
        if (req.command === "validate") return { ok: true, result: { name: "x", inputs: [], sheets: [] } };
      }
      throw new Error(`unexpected ${command}`);
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
});

