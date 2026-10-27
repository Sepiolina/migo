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
  await user.click(screen.getByRole("button", { name: "Project & inputs" }));
  await user.click(screen.getByRole("button", { name: /Open workspace/ }));
  await waitFor(() => expect(screen.getByText("/project")).toBeTruthy());
  await user.click(screen.getByRole("button", { name: "Select step.0" }));
  await user.click(screen.getByRole("button", { name: "Advanced YAML" }));
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

    await user.click(screen.getByRole("button", { name: "Apply changes" }));
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(
      (screen.getByLabelText("Step argument") as HTMLTextAreaElement).value,
    ).toBe("invalid: [");
    await user.click(screen.getByRole("button", { name: "Discard changes" }));
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
    await user.click(screen.getByRole("button", { name: "Apply changes" }));
    expect(
      screen.queryByRole("button", { name: "Discard changes" }),
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

describe("optional Excel workspace", () => {
  it("starts collapsed and preserves edited workflow source through open, expand, dock, and close", async () => {
    const user = userEvent.setup();
    render(<App />);
    expect(
      screen.queryByRole("complementary", { name: "Project and inputs" }),
    ).toBeNull();
    expect(
      screen.queryByRole("region", { name: "Excel data panel" }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "YAML source" }));
    const source = screen.getByLabelText(
      "Workflow YAML source",
    ) as HTMLTextAreaElement;
    const edited = `# retain my workflow comment\n${source.value}`;
    fireEvent.change(source, { target: { value: edited } });
    await user.click(screen.getByRole("button", { name: "Excel data" }));
    expect(
      screen.getByRole("region", { name: "Excel data panel" }),
    ).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Expand data" }));
    expect(screen.getByRole("button", { name: "Dock panel" })).toBeTruthy();
    expect(
      (screen.getByLabelText("Workflow YAML source") as HTMLTextAreaElement)
        .value,
    ).toBe(edited);
    await user.click(screen.getByRole("button", { name: "Dock panel" }));
    await user.click(screen.getByRole("button", { name: "Close Excel data" }));
    expect(
      screen.queryByRole("region", { name: "Excel data panel" }),
    ).toBeNull();
    expect(
      (screen.getByLabelText("Workflow YAML source") as HTMLTextAreaElement)
        .value,
    ).toBe(edited);
    expect(native.invoke).not.toHaveBeenCalled();
  });
});

it("opens the separate package dialog and blocks native window close during a build", async () => {
  const baseInvoke = native.invoke.getMockImplementation()!;
  let resolveBuild: (result: unknown) => void = () => {};
  native.invoke.mockImplementation(
    (command: string, args?: { source?: string }) => {
      if (command === "get_signing_identity")
        return Promise.resolve({ available: false });
      if (command === "build_linux_package")
        return new Promise((resolve) => {
          resolveBuild = resolve;
        });
      return baseInvoke(command, args);
    },
  );
  const user = userEvent.setup();
  render(<App />);
  expect(
    (
      screen.getByRole("button", {
        name: "Build Linux package",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  await user.click(screen.getByRole("button", { name: "Project & inputs" }));
  await user.click(screen.getByRole("button", { name: /Open workspace/ }));
  await user.click(screen.getByRole("button", { name: "Build Linux package" }));
  await screen.findByText("No signing identity");
  expect(
    screen.getByRole("dialog", { name: "Build Linux package" }),
  ).toBeTruthy();
  await user.click(screen.getByRole("radio", { name: /Trusted local/ }));
  await user.click(screen.getByRole("button", { name: "Build ZIP package" }));
  const preventDefault = vi.fn();
  act(() => native.closeRequested!({ preventDefault }));
  expect(preventDefault).toHaveBeenCalledOnce();
  expect(native.destroy).not.toHaveBeenCalled();
  expect(screen.queryByRole("alertdialog")).toBeNull();
  await act(async () =>
    resolveBuild({
      path: "dist/test.zip",
      sha256: "abc",
      arch: "amd64",
      mode: "trusted",
      files: [],
      validation: { runtimeTested: false },
    }),
  );
  await screen.findByText("Package created");
  await user.click(
    screen.getByRole("button", { name: "Close package builder" }),
  );
  await user.click(screen.getByRole("button", { name: "YAML source" }));
  fireEvent.change(screen.getByLabelText("Workflow YAML source"), {
    target: { value: "# unsaved" },
  });
  expect(
    (
      screen.getByRole("button", {
        name: "Build Linux package",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});
