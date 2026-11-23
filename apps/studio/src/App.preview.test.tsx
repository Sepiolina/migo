// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it } from "vitest";
import App from "./App";
import type { Trace } from "./api";
import { partsSummaryExample } from "./workflowTemplates";

type Reply = { ok: true; result: unknown } | { ok: false; error: { code: string; message: string } };
type RunnerCall = {
  command: string;
  params: Record<string, unknown>;
  resolve: (reply: Reply) => void;
};

const workspace = "/test-workspace";
const firstPath = "workflows/first.yaml";
const secondPath = "workflows/second.yaml";
const sample = "samples/parts.xlsx";
const replacement = "samples/replacement.xlsx";
const firstSource = partsSummaryExample.replace("name: Parts summary", "name: First workflow");
const secondSource = partsSummaryExample.replace("name: Parts summary", "name: Second workflow");
const host = window as Window & { __maiBridge?: { invoke: (command: string, args?: Record<string, unknown>) => Promise<unknown> } };

beforeEach(() => localStorage.clear());
afterEach(() => {
  cleanup();
  delete host.__maiBridge;
});

function controlledBridge() {
  const calls: RunnerCall[] = [];
  const revisions = new Map<string, string>();
  const documents: Record<string, string> = { [firstPath]: firstSource, [secondPath]: secondSource };
  for (const path of Object.keys(documents)) {
    localStorage.setItem(`mai.bindings:${workspace}:${path}`, JSON.stringify({ parts: sample }));
  }
  host.__maiBridge = {
    async invoke(command, args) {
      if (command === "choose_workspace") return { path: workspace };
      if (command === "list_workflows") return { paths: Object.keys(documents) };
      if (command === "read_workflow") {
        const path = String(args?.path);
        return { path, source: documents[path], revision: `rev:${path}` };
      }
      if (command === "file_revisions") {
        return Object.fromEntries((args?.paths as string[]).map((path) => [path, revisions.get(path) ?? `rev:${path}`]));
      }
      if (command === "choose_sample_file") return { path: replacement, copied: false };
      if (command === "runner_request") {
        const request = args?.request as Pick<RunnerCall, "command" | "params">;
        return new Promise<Reply>((resolve) => calls.push({ ...request, resolve }));
      }
      throw new Error(`Unexpected host command ${command}`);
    },
  };
  return {
    calls,
    revisions,
    async waitForCall(command: string, matches: (call: RunnerCall) => boolean = () => true) {
      return waitFor(() => {
        const call = calls.find((candidate) => candidate.command === command && matches(candidate));
        expect(call, `Missing matching ${command} request; received ${calls.map((candidate) => candidate.command).join(", ")}`).toBeDefined();
        return call!;
      }, { timeout: 3000, onTimeout: (error) => error });
    },
  };
}

async function openFirstWorkflow() {
  render(<App />);
  fireEvent.click(screen.getByRole("button", { name: "Open folder…" }));
  fireEvent.click(await screen.findByRole("button", { name: /First workflow/ }));
}

const workflow = () => within(screen.getByRole("complementary", { name: "Workflow" }));
const table = () => within(screen.getByRole("main", { name: "Table" }));
const toolbar = () => within(screen.getByRole("banner"));
const problems = () => within(screen.getByRole("region", { name: "Console" }));

async function reply(call: RunnerCall, result: Reply) {
  await act(async () => call.resolve(result));
}

const success = (result: unknown): Reply => ({ ok: true, result });
const failure = (message: string): Reply => ({ ok: false, error: { code: "test_failure", message } });

function trace(value: string): Trace {
  return {
    format: "xlsx",
    steps: [{
      step: 0,
      name: "from",
      line: 5,
      columns: [{ name: "Part Number", kind: "text", filled: 1, distinct: 1 }],
      total: 1,
      rows: [[value]],
      ms: 1,
    }],
  };
}

it("keeps the current validation error when an older validation succeeds later", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  const initialTrace = trace("Earlier sample value");
  initialTrace.steps.push({
    step: 1, name: "filter", line: 6, columns: [], total: 0, ms: 1, error: "The previous filter failed",
  });
  await reply(await bridge.waitForCall("trace"), success(initialTrace));
  expect(problems().getByText(/The previous filter failed/)).toBeTruthy();
  const older = await bridge.waitForCall("validate", ({ params }) => params.yaml === firstSource);

  fireEvent.click(screen.getByRole("tab", { name: "YAML" }));
  const yaml = screen.getByLabelText("Workflow YAML");
  const edited = firstSource.replace("First workflow", "Edited workflow");
  fireEvent.change(yaml, { target: { value: edited } });
  fireEvent.blur(yaml);
  fireEvent.click(screen.getByRole("tab", { name: /Problems/ }));
  expect(table().getByText("Earlier sample value")).toBeTruthy();
  expect(table().getByText("Updating…")).toBeTruthy();
  expect(problems().queryByText(/The previous filter failed/)).toBeNull();
  const current = await bridge.waitForCall("validate", ({ params }) => params.yaml === edited);
  await reply(current, failure("The edited workflow failed validation"));
  expect(problems().getByText("The edited workflow failed validation")).toBeTruthy();

  await reply(older, success({ name: "First workflow", inputs: [], sheets: [] }));
  expect(problems().getByText("The edited workflow failed validation")).toBeTruthy();
  expect(toolbar().getByRole("status").textContent).toContain("Has errors");
});

it("previews the replacement binding and ignores a late trace from the old sample", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  const older = await bridge.waitForCall("trace");
  expect(older.params.inputs).toEqual({ parts: sample });

  fireEvent.click(workflow().getByRole("button", { name: /parts samples\/parts.xlsx/ }));
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Browse…" })));
  fireEvent.click(workflow().getByTitle(/Read parts/));
  const current = await bridge.waitForCall("trace", ({ params }) => (
    params.step === 0 && (params.inputs as Record<string, string>).parts === replacement
  ));
  await reply(current, success(trace("Replacement part")));
  expect(table().getByText("Replacement part")).toBeTruthy();

  await reply(older, failure("The old sample could not be read"));
  expect(table().getByText("Replacement part")).toBeTruthy();
  expect(screen.queryByText("The old sample could not be read")).toBeNull();
});

it("retries a failed current preview with the same inputs and clears the diagnostic", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  const first = await bridge.waitForCall("trace");
  await reply(first, failure("The current sample could not be read"));
  expect(problems().getByText("The current sample could not be read")).toBeTruthy();

  fireEvent.click(table().getByRole("button", { name: "Retry preview" }));
  const retry = await bridge.waitForCall("trace", (call) => call !== first);
  expect(retry.params).toEqual(first.params);
  await reply(retry, success(trace("Recovered preview")));
  expect(table().getByText("Recovered preview")).toBeTruthy();
  expect(problems().queryByText("The current sample could not be read")).toBeNull();
});

it("refreshes a source preview when the sample changes at the same path", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  await bridge.waitForCall("trace");
  fireEvent.click(workflow().getByRole("button", { name: /parts samples\/parts.xlsx/ }));
  const older = await bridge.waitForCall("inspectWorkbook");
  expect(older.params.path).toBe(sample);

  bridge.revisions.set(sample, "new-content");
  fireEvent.focus(window);
  const current = await bridge.waitForCall("inspectWorkbook", (call) => call !== older);
  expect(current.params).toEqual(older.params);

  const sheet = { sheets: ["Sheet1"], sheet: "Sheet1", offset: 0, totalRows: 1, truncated: false };
  await reply(current, success({ ...sheet, rows: [["Current file version"]] }));
  await reply(older, success({ ...sheet, rows: [["Earlier file version"]] }));
  expect(table().getByText("Current file version")).toBeTruthy();
  expect(table().queryByText("Earlier file version")).toBeNull();
});

it("leaves the new document's selected step in view when an earlier run finishes", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  await reply(await bridge.waitForCall("trace"), success(trace("First document part")));
  fireEvent.click(toolbar().getByRole("button", { name: /^▶ Run/ }));
  const run = await bridge.waitForCall("run");
  expect(run.params.yaml).toBe(firstSource);

  fireEvent.click(toolbar().getByRole("button", { name: "Workflows" }));
  fireEvent.click(await screen.findByRole("button", { name: /Second workflow/ }));
  const current = await bridge.waitForCall("trace", ({ params }) => params.yaml === secondSource);
  await reply(current, success(trace("Second document part")));
  expect(table().getByText("Second document part")).toBeTruthy();

  await reply(run, success({ path: "outputs/first.xlsx", sha256: "run-checksum" }));
  expect(toolbar().getByText("Second workflow")).toBeTruthy();
  expect(table().getByText("Second document part")).toBeTruthy();
  expect(screen.queryByText(/Run finished/)).toBeNull();
  expect(bridge.calls.some(({ command }) => command === "inspectWorkbook")).toBe(false);
});

it("restores Home sample setup bindings for the selected workspace when workflow paths match", async () => {
  const path = "workflows/parts.yaml";
  const firstFolder = "/workspace-one";
  const secondFolder = "/workspace-two";
  const firstSample = "samples/first.xlsx";
  const secondSample = "samples/second.xlsx";
  const secondKey = `mai.bindings:${secondFolder}:${path}`;
  localStorage.setItem(secondKey, JSON.stringify({ parts: secondSample }));
  const folders = [firstFolder, secondFolder, firstFolder];
  let activeFolder: string | undefined;
  const inspected: { workspace: string; path: string }[] = [];
  host.__maiBridge = {
    async invoke(command, args) {
      if (command === "choose_workspace") {
        activeFolder = folders.shift();
        return { path: activeFolder };
      }
      if (command === "list_workflows") return { paths: [path] };
      if (command === "read_workflow") return { path, source: partsSummaryExample, revision: "workflow-revision" };
      if (command === "choose_sample_file") return { path: firstSample, copied: true };
      if (command === "file_revisions") {
        const allowed = activeFolder === firstFolder ? firstSample : secondSample;
        return Object.fromEntries((args?.paths as string[]).map((file) => [file, file === allowed ? "sample-revision" : null]));
      }
      if (command === "runner_request") {
        const request = args?.request as { command: string; params: { workspace: string; path: string } };
        if (request.command === "validate") return success({ name: "Parts summary", inputs: [], sheets: [] });
        if (request.command === "trace") return success(trace("Part row"));
        if (request.command === "inspectWorkbook") {
          inspected.push({ workspace: request.params.workspace, path: request.params.path });
          return success({ sheets: ["Parts"], sheet: "Parts", rows: [["Part"]], offset: 0, totalRows: 1, truncated: false });
        }
      }
      throw new Error(`Unexpected host command ${command}`);
    },
  };

  const openSetup = async () => {
    fireEvent.click(await screen.findByRole("button", { name: `Set up samples for ${path}` }));
    return within(await screen.findByRole("dialog", { name: "Set up samples" }));
  };
  const chooseFolder = async (folder: string) => {
    fireEvent.click(toolbar().getByRole("button", { name: "Workflows" }));
    fireEvent.click(screen.getByRole("button", { name: "Change folder…" }));
    await screen.findByText(folder);
  };
  const pathInput = (dialog: ReturnType<typeof within>) => dialog.getByRole("textbox", { name: "Sample path for Parts list" }) as HTMLInputElement;

  render(<App />);
  fireEvent.click(screen.getByRole("button", { name: "Open folder…" }));
  const first = await openSetup();
  expect(pathInput(first).value).toBe("");
  fireEvent.click(first.getByRole("button", { name: "Browse…" }));
  await first.findByText("All samples are ready.");
  fireEvent.click(first.getByRole("button", { name: "Finish" }));
  expect(localStorage.getItem(`mai.bindings:${firstFolder}:${path}`)).toBe(JSON.stringify({ parts: firstSample }));

  await chooseFolder(secondFolder);
  const second = await openSetup();
  expect(pathInput(second).value).toBe(secondSample);
  await second.findByText("All samples are ready.");
  fireEvent.click(second.getByRole("button", { name: "Cancel" }));
  expect(localStorage.getItem(secondKey)).toBe(JSON.stringify({ parts: secondSample }));

  await chooseFolder(firstFolder);
  const firstAgain = await openSetup();
  expect(pathInput(firstAgain).value).toBe(firstSample);
  await firstAgain.findByText("All samples are ready.");
  expect(inspected).toEqual([
    { workspace: firstFolder, path: firstSample },
    { workspace: secondFolder, path: secondSample },
    { workspace: firstFolder, path: firstSample },
  ]);
});

it("reads distant physical worksheet rows and returns to the first row on request or file replacement", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  await bridge.waitForCall("trace");
  fireEvent.click(workflow().getByRole("button", { name: /parts samples\/parts.xlsx/ }));
  const initial = await bridge.waitForCall("inspectWorkbook");
  const sheet = { sheets: ["Sheet1"], sheet: "Sheet1", totalRows: 850, truncated: true };
  expect(initial.params.offset).toBe(0);
  await reply(initial, success({ ...sheet, offset: 0, rows: [["Source header"]] }));

  const jump = (row: number) => {
    fireEvent.change(table().getByRole("spinbutton", { name: "Worksheet row" }), { target: { value: row } });
    fireEvent.click(table().getByRole("button", { name: "Go to row" }));
  };
  jump(501);
  const distant = await bridge.waitForCall("inspectWorkbook", ({ params }) => params.offset === 500);
  await reply(distant, success({ ...sheet, offset: 500, rows: [["Distant worksheet value"]] }));
  expect(table().getByRole("rowheader", { name: "501" })).toBeTruthy();
  fireEvent.click(table().getByText("Distant worksheet value"));
  expect(table().getByRole("gridcell", { name: "Row 501, column A, Distant worksheet value" })).toBeTruthy();

  fireEvent.click(table().getByRole("button", { name: "First row" }));
  const firstAgain = await bridge.waitForCall("inspectWorkbook", (call) => call !== initial && call.params.offset === 0);
  await reply(firstAgain, success({ ...sheet, offset: 0, rows: [["Source header"]] }));
  expect(table().getByRole("rowheader", { name: "1" })).toBeTruthy();

  jump(601);
  const beforeSelectionChange = await bridge.waitForCall("inspectWorkbook", ({ params }) => params.offset === 600);
  await reply(beforeSelectionChange, success({ ...sheet, offset: 600, rows: [["Before selection change"]] }));
  fireEvent.click(workflow().getByTitle(/Read parts/));
  fireEvent.click(workflow().getByRole("button", { name: /parts samples\/parts.xlsx/ }));
  const reopened = await bridge.waitForCall("inspectWorkbook", (call) => (
    call !== initial && call !== firstAgain && call.params.offset === 0
  ));
  await reply(reopened, success({ ...sheet, offset: 0, rows: [["Source header"]] }));
  expect(table().getByRole("rowheader", { name: "1" })).toBeTruthy();

  jump(700);
  const oldFile = await bridge.waitForCall("inspectWorkbook", ({ params }) => params.offset === 699);
  bridge.revisions.set(sample, "replacement-revision");
  fireEvent.focus(window);
  const replacementFile = await bridge.waitForCall("inspectWorkbook", (call) => (
    call !== initial && call !== firstAgain && call !== reopened && call.params.offset === 0
  ));
  await reply(replacementFile, success({ ...sheet, totalRows: 1, truncated: false, offset: 0, rows: [["Replacement header"]] }));
  await reply(oldFile, success({ ...sheet, offset: 699, rows: [["Obsolete worksheet value"]] }));
  expect(table().getByRole("rowheader", { name: "1" })).toBeTruthy();
  expect(table().getByText("Replacement header")).toBeTruthy();
  expect(table().queryByText("Obsolete worksheet value")).toBeNull();
});

function partsTrace(partNumber: string, focusedStep: number): Trace {
  const columns: Trace["steps"][number]["columns"] = [
    { name: "Part Number", kind: "text", filled: 2, distinct: 2 },
    { name: "Quantity", kind: "number", filled: 2, distinct: 2 },
  ];
  const steps: Trace["steps"] = [{
    step: 0, name: "from", line: 5, columns, total: 2, ms: 1,
    rows: focusedStep === 0 ? [[partNumber, "3"], ["Out of stock", "0"]] : undefined,
  }];
  if (focusedStep === 1) {
    steps.push({
      step: 1, name: "filter", line: 6, columns: columns.map((column) => ({ ...column, filled: 1, distinct: 1 })),
      total: 1, rows: [[partNumber, "3"]], ms: 1,
    });
  }
  return { format: "xlsx", steps };
}

it("reuses a successful focused trace when revisiting a step", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  const read = await bridge.waitForCall("trace", ({ params }) => params.step === 0);
  await reply(read, success(partsTrace("In stock", 0)));
  expect(table().getByText("Out of stock")).toBeTruthy();

  fireEvent.click(workflow().getByTitle("Keep rows where num(Quantity) > 0"));
  const filter = await bridge.waitForCall("trace", ({ params }) => params.step === 1);
  await reply(filter, success(partsTrace("In stock", 1)));
  expect(table().getByText("In stock")).toBeTruthy();
  expect(table().queryByText("Out of stock")).toBeNull();

  fireEvent.click(workflow().getByTitle(/Read parts/));
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 350));
  });
  expect(bridge.calls.filter(({ command }) => command === "trace").map(({ params }) => params.step)).toEqual([0, 1]);
  expect(table().getByText("Out of stock")).toBeTruthy();
});

it("refreshes a previously visited step after a sample at the same path is replaced", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  const originalRead = await bridge.waitForCall("trace", ({ params }) => params.step === 0);
  await reply(originalRead, success(partsTrace("Original part", 0)));
  expect(table().getByText("Original part")).toBeTruthy();

  fireEvent.click(workflow().getByTitle("Keep rows where num(Quantity) > 0"));
  const originalFilter = await bridge.waitForCall("trace", ({ params }) => params.step === 1);
  await reply(originalFilter, success(partsTrace("Original part", 1)));

  bridge.revisions.set(sample, "replacement-content");
  fireEvent.focus(window);
  const updatedFilter = await bridge.waitForCall("trace", (call) => call !== originalFilter && call.params.step === 1);
  await reply(updatedFilter, success(partsTrace("Replacement part", 1)));
  expect(table().getByText("Replacement part")).toBeTruthy();

  fireEvent.click(workflow().getByTitle(/Read parts/));
  const updatedRead = await bridge.waitForCall("trace", (call) => call !== originalRead && call.params.step === 0);
  expect(updatedRead.params).toEqual(originalRead.params);
  await reply(updatedRead, success(partsTrace("Replacement part", 0)));
  expect(table().getByText("Replacement part")).toBeTruthy();
  expect(table().getByText("Out of stock")).toBeTruthy();
  expect(table().queryByText("Original part")).toBeNull();
});

it("retains the saved workbook after a preview failure and marks it previous when the sample changes", async () => {
  const bridge = controlledBridge();
  await openFirstWorkflow();
  await reply(await bridge.waitForCall("trace"), success(trace("First part")));
  fireEvent.click(toolbar().getByRole("button", { name: /^▶ Run/ }));
  const firstRun = await bridge.waitForCall("run");
  expect(firstRun.params.omitPreview).toBe(true);
  const path = ".studio/runs/run-one/output.xlsx";
  await reply(firstRun, success({ path, sha256: "first" }));
  const firstPage = await bridge.waitForCall("inspectWorkbookPage");
  expect(firstPage.params.path).toBe(path);
  await reply(firstPage, failure("The saved sheet could not be displayed"));
  const workbook = within(screen.getByRole("main", { name: "Workbook preview" }));
  expect(workbook.getByText("output.xlsx")).toBeTruthy();
  expect(workbook.getByText("The saved sheet could not be displayed")).toBeTruthy();

  bridge.revisions.set(sample, "new-content");
  fireEvent.focus(window);
  expect(await workbook.findByText("Previous run")).toBeTruthy();
  fireEvent.click(workbook.getByRole("button", { name: "Run to refresh" }));
  const refresh = await bridge.waitForCall("run", (call) => call !== firstRun);
  const refreshedPath = ".studio/runs/run-two/output.xlsx";
  await reply(refresh, success({ path: refreshedPath, sha256: "second" }));
  await bridge.waitForCall("inspectWorkbookPage", ({ params }) => params.path === refreshedPath);
  expect(screen.queryByText("Previous run")).toBeNull();
  expect(screen.queryByText("The saved sheet could not be displayed")).toBeNull();
});
