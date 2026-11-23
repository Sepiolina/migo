// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { request } from "./api";
import { WorkflowTests } from "./WorkflowTests";

vi.mock("./api", async (importOriginal) => ({
  ...await importOriginal<typeof import("./api")>(),
  request: vi.fn(),
}));

const runner = vi.mocked(request);
const workspace = "/project";
const workflowPath = "workflows/orders.yaml";
const params = { workspace, workflowPath };
const revision = "1".repeat(64);
const nextRevision = "2".repeat(64);
const suite = {
  name: "orders",
  path: "fixtures/orders.test.yaml",
  cases: [{ name: "writes totals" }, { name: "rejects invalid orders", expectedError: "invalid quantity" }],
};
const discovery = { workflowPath, revision, suites: [suite] };
const report = {
  ...discovery,
  suites: [{
    ...suite,
    cases: [
      {
        ...suite.cases[0], outcome: "failed", durationMs: 0,
        error: 'sheet "Totals" cell B2: got "21", expected ""',
        mismatch: { kind: "cell", sheet: "Totals", cell: "B2", expected: "", actual: "21" },
      },
      { ...suite.cases[1], outcome: "passed", durationMs: 13, actualError: "row 2: invalid quantity" },
    ],
  }],
};

const runAll = () => screen.findByRole("button", { name: "Run all tests" });
const field = (label: string) => screen.getByText(label, { selector: "dt" }).nextElementSibling?.textContent;

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

it("lists saved workflow cases and shows structured workbook and expected-error results", async () => {
  runner.mockResolvedValueOnce(discovery).mockResolvedValueOnce(report);
  const user = userEvent.setup();
  const save = vi.fn();
  render(<WorkflowTests {...params} dirty={false} onSave={save} onBack={vi.fn()} />);
  const orders = within(await screen.findByRole("region", { name: "orders" }));
  expect(orders.getByText("writes totals")).toBeTruthy();
  expect(orders.getByText("rejects invalid orders")).toBeTruthy();
  await user.click(await runAll());

  expect(runner).toHaveBeenNthCalledWith(1, "discoverTests", params);
  expect(runner).toHaveBeenNthCalledWith(2, "runTests", params, expect.any(AbortSignal));
  expect(save).not.toHaveBeenCalled();
  expect(await orders.findByText("Passed")).toBeTruthy();
  expect(orders.getByText("Failed")).toBeTruthy();
  expect(orders.getByText("0 ms")).toBeTruthy();
  expect(orders.getByText("13 ms")).toBeTruthy();
  expect(field("Tested revision")).toBe(revision);
  expect(field("Sheet")).toBe("Totals");
  expect(field("Cell")).toBe("B2");
  expect(field("Expected value")).toBe('""');
  expect(field("Actual value")).toBe('"21"');
  expect(field("Expected error fragment")).toBe("invalid quantity");
  expect(field("Actual error")).toBe("row 2: invalid quantity");
});

it("keeps dirty edits out of test-saved runs and sends the saved revision after a successful save", async () => {
  runner.mockResolvedValueOnce(discovery).mockResolvedValueOnce(report)
    .mockResolvedValueOnce({ ...report, revision: nextRevision });
  const user = userEvent.setup();
  let finishSave: (revision: string) => void = () => undefined;
  const save = vi.fn(() => new Promise<string>((resolve) => { finishSave = resolve; }));
  render(<WorkflowTests {...params} dirty onSave={save} onBack={vi.fn()} />);
  const single = await screen.findByRole("button", { name: "Run suite orders" });

  await user.click(single);
  expect(runner).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: "Test saved version" }));
  expect(runner).toHaveBeenNthCalledWith(2, "runTests", { ...params, path: suite.path }, expect.any(AbortSignal));
  expect(save).not.toHaveBeenCalled();

  await user.click(await runAll());
  await user.click(screen.getByRole("button", { name: "Save and run tests" }));
  expect(await screen.findByText("Saving workflow…")).toBeTruthy();
  expect(runner).toHaveBeenCalledTimes(2);
  finishSave(nextRevision);
  expect(await screen.findByText(nextRevision)).toBeTruthy();
  expect(runner).toHaveBeenNthCalledWith(3, "runTests", { ...params, revision: nextRevision }, expect.any(AbortSignal));
});

it("keeps tests idle if a requested save fails", async () => {
  runner.mockResolvedValueOnce(discovery);
  const user = userEvent.setup();
  render(<WorkflowTests {...params} dirty onSave={vi.fn().mockResolvedValue(null)} onBack={vi.fn()} />);
  await screen.findByRole("region", { name: "orders" });
  await user.click(await runAll());
  await user.click(screen.getByRole("button", { name: "Save and run tests" }));
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toContain("Tests did not run");
  expect(runner).toHaveBeenCalledTimes(1);
});

it("cancels an active run and ignores a late result after cancellation", async () => {
  let finishRun: (result: typeof report) => void = () => undefined;
  runner.mockResolvedValueOnce(discovery).mockImplementationOnce(
    () => new Promise((resolve) => { finishRun = resolve as typeof finishRun; }),
  );
  const user = userEvent.setup();
  render(<WorkflowTests {...params} dirty={false} onSave={vi.fn()} onBack={vi.fn()} />);
  await screen.findByRole("region", { name: "orders" });
  await user.click(await runAll());
  expect((await runAll() as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("button", { name: "Cancel tests" }));
  const [, , signal] = runner.mock.calls[1];
  expect(signal?.aborted).toBe(true);
  finishRun(report);
  expect(await screen.findByText("Workflow tests cancelled.")).toBeTruthy();
  expect(screen.queryByText("Tested revision")).toBeNull();
});

it("allows retry after a discovery error and displays an empty suite list", async () => {
  runner.mockRejectedValueOnce(new Error("Workspace unavailable")).mockResolvedValueOnce({ ...discovery, suites: [] });
  const user = userEvent.setup();
  render(<WorkflowTests {...params} dirty={false} onSave={vi.fn()} onBack={vi.fn()} />);
  expect((await screen.findByRole("alert")).textContent).toContain("Workspace unavailable");
  await user.click(screen.getByRole("button", { name: "Refresh suites" }));
  expect(await screen.findByText("No test suites found for this workflow.")).toBeTruthy();
  expect((await runAll() as HTMLButtonElement).disabled).toBe(true);
});

it("shows discovery issues while leaving valid suites available and renders the first differing CSV line", async () => {
  const diagnostics = [{ path: "broken.test.yaml", message: "Cannot parse suite YAML" }];
  runner.mockResolvedValueOnce({ ...discovery, diagnostics }).mockResolvedValueOnce({
    ...report,
    suites: [{
      ...suite,
      cases: [{
        ...suite.cases[0], outcome: "failed", durationMs: 2,
        mismatch: { kind: "line", line: 4, expected: "order,1", actual: "order,2" },
      }],
    }],
  });
  const user = userEvent.setup();
  render(<WorkflowTests {...params} dirty={false} onSave={vi.fn()} onBack={vi.fn()} />);
  const issues = within(await screen.findByRole("region", { name: "Suite discovery issues" }));
  expect(issues.getByText("broken.test.yaml")).toBeTruthy();
  expect(issues.getByText("Cannot parse suite YAML")).toBeTruthy();
  await user.click(await runAll());
  expect(await screen.findByText("Failed")).toBeTruthy();
  expect(field("First differing line")).toBe("4");
  expect(field("Expected line")).toBe('"order,1"');
  expect(field("Actual line")).toBe('"order,2"');
});

it("aborts an in-flight test if the open workflow changes", async () => {
  let finishRun: (result: typeof report) => void = () => undefined;
  const otherPath = "workflows/customers.yaml";
  runner.mockResolvedValueOnce(discovery).mockImplementationOnce(
    () => new Promise((resolve) => { finishRun = resolve as typeof finishRun; }),
  ).mockResolvedValueOnce({ workflowPath: otherPath, revision: nextRevision, suites: [] });
  const user = userEvent.setup();
  const callbacks = { onSave: vi.fn(), onBack: vi.fn() };
  const { rerender } = render(<WorkflowTests {...params} {...callbacks} dirty={false} />);
  await screen.findByRole("region", { name: "orders" });
  await user.click(await runAll());

  rerender(<WorkflowTests workspace={workspace} workflowPath={otherPath} {...callbacks} dirty={false} />);
  expect(runner.mock.calls[1][2]?.aborted).toBe(true);
  finishRun(report);
  expect(await screen.findByText("No test suites found for this workflow.")).toBeTruthy();
  expect(screen.queryByText("Tested revision")).toBeNull();
});
