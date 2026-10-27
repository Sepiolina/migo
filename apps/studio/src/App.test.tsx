// @vitest-environment jsdom
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Node } from "@xyflow/react";
import App from "./App";

const native = vi.hoisted(() => ({
  invoke: vi.fn(),
  destroy: vi.fn(async () => {}),
  closeRequested: undefined as
    undefined | ((event: { preventDefault: () => void }) => void),
}));
vi.mock("@tauri-apps/api/core", () => ({ invoke: native.invoke }));
vi.mock("@tauri-apps/api/window", () => ({
  getCurrentWindow: () => ({
    destroy: native.destroy,
    onCloseRequested: async (handler: typeof native.closeRequested) => {
      native.closeRequested = handler;
      return () => {};
    },
  }),
}));
// The graph library's layout requires a real browser. Preserve its selection
// callback contract while exercising the actual editor and document lifecycle.
vi.mock("@xyflow/react", () => ({
  ReactFlow: ({
    nodes,
    onNodeClick,
  }: {
    nodes: Node[];
    onNodeClick: (event: unknown, node: Node) => void;
  }) => (
    <div>
      {nodes.map((node) => (
        <button key={node.id} onClick={(event) => onNodeClick(event, node)}>
          Select {node.id}
        </button>
      ))}
    </div>
  ),
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
}));

beforeEach(() => {
  Object.defineProperty(window, "__TAURI_INTERNALS__", {
    configurable: true,
    value: {},
  });
  native.closeRequested = undefined;
  native.destroy.mockClear();
  native.invoke.mockReset();
  native.invoke.mockImplementation(
    async (command: string, args?: { source?: string }) => {
      if (command === "choose_workspace") return { path: "/project" };
      if (command === "list_workflows")
        return { paths: ["workflows/other.yaml"] };
      if (command === "save_workflow")
        return {
          path: "workflows/summary.yaml",
          source: args?.source,
          revision: "saved-revision",
        };
      throw new Error(`Unexpected native command ${command}`);
    },
  );
});
afterEach(cleanup);

async function openInspector() {
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("button", { name: /Open workspace/ }));
  await waitFor(() => expect(screen.getByText("/project")).toBeTruthy());
  await user.click(screen.getByRole("button", { name: "Select step.0" }));
  return user;
}

describe("unapplied argument protection", () => {
  it("blocks stale saves/runs and navigation, retains invalid drafts when load/workspace/close are canceled", async () => {
    const user = await openInspector();
    fireEvent.change(screen.getByLabelText("Step argument"), {
      target: { value: "invalid: [" },
    });
    expect(
      (
        screen.getByRole("button", {
          name: /Save workflow/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      (
        screen.getByRole("button", {
          name: /Run workflow/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      (screen.getByRole("button", { name: "YAML source" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    await user.click(screen.getByRole("button", { name: "Select step.1" }));
    expect(
      (screen.getByLabelText("Step argument") as HTMLTextAreaElement).value,
    ).toBe("invalid: [");
    await user.keyboard("{Control>}s{/Control}");
    expect(
      native.invoke.mock.calls.some(([command]) => command === "save_workflow"),
    ).toBe(false);

    await user.selectOptions(
      screen.getByLabelText("Open workflow"),
      "workflows/other.yaml",
    );
    expect(screen.getByRole("alertdialog")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(
      native.invoke.mock.calls.some(([command]) => command === "read_workflow"),
    ).toBe(false);

    await user.click(screen.getByRole("button", { name: /Change workspace/ }));
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(
      native.invoke.mock.calls.filter(
        ([command]) => command === "choose_workspace",
      ),
    ).toHaveLength(1);

    const preventDefault = vi.fn();
    act(() => native.closeRequested!({ preventDefault }));
    expect(preventDefault).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(native.destroy).not.toHaveBeenCalled();
    expect(
      (screen.getByLabelText("Step argument") as HTMLTextAreaElement).value,
    ).toBe("invalid: [");

    await user.click(screen.getByRole("button", { name: "Apply argument" }));
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(
      (screen.getByLabelText("Step argument") as HTMLTextAreaElement).value,
    ).toBe("invalid: [");
    await user.click(
      screen.getByRole("button", { name: "Discard argument draft" }),
    );
    expect(screen.queryByRole("alert")).toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: /Save workflow/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
  });

  it("saves the applied argument and only closes after explicit discard confirmation", async () => {
    const user = await openInspector();
    fireEvent.change(screen.getByLabelText("Step argument"), {
      target: { value: '"changed[Sheet]"' },
    });
    await user.click(screen.getByRole("button", { name: "Apply argument" }));
    expect(
      screen.queryByRole("button", { name: "Discard argument draft" }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: /Save workflow/ }));
    await waitFor(() =>
      expect(native.invoke).toHaveBeenCalledWith(
        "save_workflow",
        expect.objectContaining({
          source: expect.stringContaining('from: "changed[Sheet]"'),
          expectedRevision: null,
        }),
      ),
    );
    fireEvent.change(screen.getByLabelText("Step argument"), {
      target: { value: "another[Sheet]" },
    });
    const preventDefault = vi.fn();
    act(() => native.closeRequested!({ preventDefault }));
    expect(native.destroy).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "Discard changes and continue" }),
    );
    expect(preventDefault).toHaveBeenCalledOnce();
    expect(native.destroy).toHaveBeenCalledOnce();
  });
});
