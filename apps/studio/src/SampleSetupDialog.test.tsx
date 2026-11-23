// @vitest-environment jsdom
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { SampleSetupDialog } from "./SampleSetupDialog";

type SetupProps = ComponentProps<typeof SampleSetupDialog>;
type Bridge = { invoke(command: string, args?: Record<string, unknown>): Promise<unknown> };
type PickedFile = { path: string; copied: boolean } | null;
const host = window as Window & { __maiBridge?: Bridge };
const workspace = "/sample-workspace";

afterEach(() => {
  cleanup();
  delete host.__maiBridge;
});

function installBridge(files: Record<string, string[] | Error | null>, picks: PickedFile[] = []) {
  const chooseFile = vi.fn(() => picks.shift() ?? null);
  const inspectFile = vi.fn((path: string) => {
    const file = files[path];
    if (file instanceof Error) return { ok: false, error: { message: file.message } };
    if (!file) throw new Error(`Unexpected inspection for ${path}`);
    return {
      ok: true,
      result: { sheets: file, sheet: file[0], rows: [["Part"]], offset: 0, totalRows: 1, truncated: false },
    };
  });
  host.__maiBridge = {
    async invoke(command, args) {
      if (command === "choose_sample_file") return chooseFile();
      if (command === "file_revisions") {
        return Object.fromEntries((args?.paths as string[]).map((path) => [path, files[path] ? `rev:${path}` : null]));
      }
      if (command === "runner_request") {
        const runner = args?.request as { command: string; params: { workspace: string; path: string } };
        expect(runner.command).toBe("inspectWorkbook");
        expect(runner.params.workspace).toBe(workspace);
        return inspectFile(runner.params.path);
      }
      throw new Error(`Unexpected host command ${command}`);
    },
  };
  return { chooseFile, inspectFile };
}

function renderSetup(inputs: SetupProps["inputs"], bindings: SetupProps["bindings"]) {
  const onApply = vi.fn();
  const onClose = vi.fn();
  render(<SampleSetupDialog workspace={workspace} inputs={inputs} bindings={bindings} onApply={onApply} onClose={onClose} />);
  return { onApply, onClose };
}

const inputRow = (name: string) => within(screen.getByRole("region", { name }));
const samplePath = (name: string) => inputRow(name).getByRole("textbox", { name: `Sample path for ${name}` }) as HTMLInputElement;

it("distinguishes unbound, missing, unreadable and missing-sheet inputs", async () => {
  const bridge = installBridge({
    "samples/missing.xlsx": null,
    "samples/corrupt.xlsx": new Error("Invalid workbook"),
    "samples/summary.xlsx": ["Summary"],
    "samples/priority.xlsx": ["Overview", "priority backup"],
    "samples/items.csv": ["Sheet1"],
  });
  renderSetup([
    { id: "unbound", label: "Unbound source", sheetRequirements: [] },
    { id: "missing", label: "Missing source", sheetRequirements: [] },
    { id: "corrupt", label: "Corrupt source", sheetRequirements: [] },
    { id: "wrong", label: "Wrong sheet source", sheetRequirements: [["Required Data", "Backup Data"]] },
    { id: "priority", label: "Priority source", sheetRequirements: [["Priority", "Priority backup"]] },
    { id: "csv", label: "CSV source", sheetRequirements: [[1]] },
  ], {
    missing: "samples/missing.xlsx",
    corrupt: "samples/corrupt.xlsx",
    wrong: "samples/summary.xlsx",
    priority: "samples/priority.xlsx",
    csv: "samples/items.csv",
  });

  expect(await screen.findByText(/No sample file selected/i)).toBeTruthy();
  expect(await screen.findByText(/Sample file is missing/i)).toBeTruthy();
  expect(await screen.findByText(/Could not read the workbook/i)).toBeTruthy();
  expect(await screen.findByText(/Missing required sheet.*Required Data.*Backup Data/i)).toBeTruthy();
  expect(await inputRow("Priority source").findByText("Overview, priority backup")).toBeTruthy();
  expect(inputRow("Priority source").getByText("Ready.")).toBeTruthy();
  expect(await inputRow("CSV source").findByText("Sheet1")).toBeTruthy();
  expect(inputRow("CSV source").getByText("Ready.")).toBeTruthy();
  expect(screen.getByText(/Next input.*Unbound source/i)).toBeTruthy();
  expect((screen.getByRole("button", { name: "Finish" }) as HTMLButtonElement).disabled).toBe(true);
  expect(bridge.inspectFile).not.toHaveBeenCalledWith("samples/missing.xlsx");
});

it("keeps a saved path after a cancelled picker and repairs just that input", async () => {
  const user = userEvent.setup();
  const oldPath = "samples/moved.xlsx";
  const newPath = "samples/repaired.xlsx";
  const otherPath = "samples/priority.csv";
  const bridge = installBridge({
    [oldPath]: null,
    [newPath]: ["Notes", "Parts"],
    [otherPath]: ["Sheet1"],
  }, [null, { path: newPath, copied: true }]);
  const bindings = { bom: oldPath, priority: otherPath };
  const { onApply } = renderSetup([
    { id: "bom", label: "BOM", sheetRequirements: [["Parts"]] },
    { id: "priority", label: "Priority", sheetRequirements: [] },
  ], bindings);

  const locate = () => inputRow("BOM").getByRole("button", { name: /Locate file/ });
  await screen.findByText(/Sample file is missing/i);
  expect(await inputRow("Priority").findByText("Sheet1")).toBeTruthy();
  await user.click(locate());
  await waitFor(() => expect(locate().getAttribute("aria-disabled")).not.toBe("true"));
  expect(samplePath("BOM").value).toBe(oldPath);
  expect(samplePath("Priority").value).toBe(otherPath);
  expect(onApply).not.toHaveBeenCalled();

  await user.click(locate());
  expect(await inputRow("BOM").findByText("Notes, Parts")).toBeTruthy();
  expect(samplePath("BOM").value).toBe(newPath);
  expect(samplePath("Priority").value).toBe(otherPath);
  expect(onApply).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Finish" }));
  expect(bridge.chooseFile).toHaveBeenCalledTimes(2);
  expect(onApply).toHaveBeenCalledExactlyOnceWith({ bom: newPath, priority: otherPath });
  expect(bindings).toEqual({ bom: oldPath, priority: otherPath });
});
