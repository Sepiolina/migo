// @vitest-environment jsdom
import { createHash, webcrypto } from "node:crypto";
import { TextEncoder } from "node:util";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { native, request, type Inspection, type RunHistoryEntry } from "./api";
import { RunsPanel } from "./RunsPanel";

vi.mock("./api", async (importOriginal) => ({
  ...await importOriginal<typeof import("./api")>(),
  native: vi.fn(),
  request: vi.fn(),
}));

const runner = vi.mocked(request);
const revisions = vi.mocked(native);
const source = "name: Monthly summary\n";
const sha256 = (content: string) => createHash("sha256").update(content).digest("hex");
const outputHash = sha256("output");
const completedAt = "2026-11-22T10:22:30.120Z";

function recordedRun(id: string, filename = "output.xlsx"): RunHistoryEntry {
  const outputPath = `.studio/runs/${id}/${filename}`;
  return {
    id, outputPath, completedAt, available: true,
    manifest: {
      version: 1, completedAt, outputPath,
      workflowPath: "workflows/monthly.yaml", workflowName: "Monthly summary",
      yamlSha256: sha256(source), outputSha256: outputHash,
    },
  };
}

const workspace = "/workspace";
const matches = (label: string) => screen.getByText(label, { selector: "dt", exact: true }).nextElementSibling?.textContent;
const inputRow = (name: string) => within(screen.getByText(name, { selector: "b", exact: true }).closest("li")!);
const openOutput = () => screen.findByRole("button", { name: "Open output" });

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.resetAllMocks();
});

it("compares current YAML and input bytes independently of paths and shows unknown fingerprints", async () => {
  vi.stubGlobal("crypto", webcrypto);
  vi.stubGlobal("TextEncoder", TextEncoder);
  const run = recordedRun("run-12345678");
  const hash = sha256("recorded input");
  run.manifest!.uncertain = true;
  run.manifest!.inputs = [
    { name: "changed", path: "inputs/same.csv", sha256: hash, changed: true },
    { name: "relocated", path: "inputs/original.csv", sha256: hash },
    { name: "unknown", path: "inputs/missing.csv", sha256: hash },
    { name: "removed", path: "inputs/removed.csv", sha256: hash },
  ];
  runner.mockResolvedValue({ runs: [run] });
  revisions.mockResolvedValue({ [run.outputPath]: outputHash });
  const current = {
    workflowPath: "workflows/monthly.yaml", source,
    inputs: [
      { name: "changed", path: "inputs/same.csv", sha256: sha256("new input") },
      { name: "relocated", path: "inputs/copy.csv", sha256: hash.toUpperCase() },
      { name: "unknown", path: "inputs/missing.csv", sha256: null },
      { name: "added", path: "inputs/added.csv", sha256: hash },
    ],
  };
  const onBack = vi.fn();
  const onRunCurrent = vi.fn();
  const onRefreshCurrent = vi.fn();
  const props = { workspace, onBack, onRunCurrent, onRefreshCurrent, canRunCurrent: true };
  const user = userEvent.setup();
  const { rerender } = render(<RunsPanel {...props} current={current} />);

  expect(document.activeElement).toBe(screen.getByRole("heading", { name: "Runs" }));
  await waitFor(() => expect(matches("Current YAML")).toBe("Matches"));
  expect(inputRow("changed").getByText("Current binding").nextElementSibling?.textContent).toBe("Matches");
  expect(inputRow("changed").getByText("Current bytes").nextElementSibling?.textContent).toBe("Changed");
  expect(inputRow("relocated").getByText("Current binding").nextElementSibling?.textContent).toContain("Changed");
  expect(inputRow("relocated").getByText("Current bytes").nextElementSibling?.textContent).toBe("Matches");
  expect(inputRow("unknown").getByText("Current bytes").nextElementSibling?.textContent).toBe("Unknown");
  expect(inputRow("removed").getByText("Removed")).toBeTruthy();
  expect(inputRow("added").getByText(/Added since this run/)).toBeTruthy();
  expect(screen.getByText(/Input provenance is uncertain/)).toBeTruthy();
  expect(inputRow("changed").getByText(/pre-run file/)).toBeTruthy();

  await user.click(screen.getByText("Recorded SHA-256 fingerprints"));
  expect(screen.getByText(run.manifest!.yamlSha256)).toBeTruthy();
  rerender(<RunsPanel {...props} current={{ ...current, source: source + "# draft\n" }} />);
  await waitFor(() => expect(matches("Current YAML")).toBe("Changed"));
  await user.click(screen.getByRole("button", { name: "Refresh runs" }));
  expect(onRefreshCurrent).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Run current workflow" }));
  expect(onRunCurrent).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: /Back to workflow/ }));
  expect(onBack).toHaveBeenCalledOnce();
});

it("previews changed CSV bytes, skips the header when paging, and closes an unavailable output", async () => {
  const run = recordedRun("run-12345678", "output.csv");
  const currentHash = sha256("replacement");
  let currentRevision: string | null = currentHash;
  revisions.mockImplementation(async () => ({ [run.outputPath]: currentRevision }));
  runner.mockImplementation(async (method, params) => {
    if (method === "listRuns") return { runs: [run] };
    const offset = Number(params.offset);
    return {
      sheets: ["CSV"], sheet: "CSV", offset, totalRows: 102, truncated: false,
      rows: offset ? [["Last record"]] : [["Customer"], ...Array.from({ length: 100 }, (_, index) => [`Customer ${index + 1}`])],
    } satisfies Inspection;
  });

  const user = userEvent.setup();
  render(<RunsPanel workspace={workspace} onBack={() => {}} />);
  const open = await openOutput();
  expect(screen.getByText(/output file has changed/i)).toBeTruthy();
  await user.click(open);
  expect(await screen.findByRole("columnheader", { name: /Customer/ })).toBeTruthy();
  expect(screen.getByText("Customer 1")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Next rows" }));
  expect(await screen.findByText("Last record")).toBeTruthy();
  expect(runner).toHaveBeenLastCalledWith("inspectWorkbook", {
    workspace, path: run.outputPath, offset: 101, limit: 100,
  });

  currentRevision = null;
  await user.click(screen.getByRole("button", { name: "Refresh runs" }));
  expect(await screen.findByText("Output file is unavailable.")).toBeTruthy();
  expect(screen.queryByText("CSV preview")).toBeNull();
  expect(screen.queryByRole("button", { name: /^(Open|Close) output$/ })).toBeNull();
});

it("opens an earlier workbook without metadata and preserves the listed output when inspection fails", async () => {
  const run = recordedRun("run-older123");
  delete run.manifest;
  const workbookError = "Workbook could not be read";
  runner.mockResolvedValueOnce({ runs: [run] }).mockRejectedValueOnce(new Error(workbookError));
  revisions.mockResolvedValue({ [run.outputPath]: outputHash });

  const user = userEvent.setup();
  render(<RunsPanel workspace={workspace} onBack={() => {}} />);
  await user.click(await openOutput());
  expect(within(screen.getByRole("list", { name: "Recent runs" })).getByText("Earlier run · metadata unavailable")).toBeTruthy();
  expect(screen.getByText(/Run metadata is unavailable/)).toBeTruthy();
  expect(await screen.findByText(workbookError)).toBeTruthy();
  expect(screen.getByText(run.outputPath)).toBeTruthy();
  expect(runner).toHaveBeenLastCalledWith("inspectWorkbookPage", expect.objectContaining({ workspace, path: run.outputPath }));
  expect(screen.getByRole("button", { name: "Close output" })).toBeTruthy();
});

it("retains a selected older page after refreshing and distinguishes runs completed together", async () => {
  const run = recordedRun("run-12345678");
  const older = recordedRun("run-98765432");
  runner.mockImplementation(async (_, { offset }) => offset ? { runs: [older] } : { runs: [run], nextOffset: 1 });
  revisions.mockResolvedValue({ [run.outputPath]: outputHash, [older.outputPath]: outputHash });
  const user = userEvent.setup();
  render(<RunsPanel workspace={workspace} onBack={() => {}} />);
  await user.click(await screen.findByRole("button", { name: "Load more runs" }));
  const olderButton = await screen.findByRole("button", { name: /Monthly summary.*98765432/ });
  await user.click(olderButton);
  expect(olderButton.getAttribute("aria-current")).toBe("true");
  await user.click(screen.getByRole("button", { name: "Refresh runs" }));
  await waitFor(() => expect(runner).toHaveBeenCalledTimes(4));
  expect(screen.getByRole("button", { name: /Monthly summary.*98765432/ }).getAttribute("aria-current")).toBe("true");
  expect(screen.getByText(older.outputPath)).toBeTruthy();
});
