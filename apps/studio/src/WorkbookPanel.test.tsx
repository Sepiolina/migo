// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WorkbookPanel } from "./WorkbookPanel";
import type { Inspection, RunResult } from "./api";

afterEach(cleanup);
const completeCell =
  "A complete workbook cell value that must remain readable beyond the grid's clipped display.";
const inspection: Inspection = {
  sheets: ["Parts", "Summary"],
  sheet: "Parts",
  rows: [[completeCell]],
  offset: 100,
  totalRows: 201,
  truncated: true,
};
const artifact: RunResult = {
  path: "outputs/result.xlsx",
  sha256: "abc123",
  preview: inspection,
};

it("keeps paging and sheet changes on the loaded workbook while the user edits another path", async () => {
  const user = userEvent.setup();
  const onInspect = vi.fn();
  function Harness() {
    const [path, setPath] = useState("inputs/loaded.xlsx");
    return (
      <WorkbookPanel
        inspection={inspection}
        artifact={artifact}
        ready
        path={path}
        loadedPath="inputs/loaded.xlsx"
        onPath={setPath}
        onInspect={onInspect}
        onClose={() => {}}
        fullScreen={false}
        onExpand={() => {}}
        stale
        inputs={{ parts: "inputs/parts.xlsx" }}
      />
    );
  }
  render(<Harness />);
  await user.clear(screen.getByLabelText("Workbook path"));
  await user.type(screen.getByLabelText("Workbook path"), "inputs/draft.xlsx");
  await user.click(screen.getByRole("button", { name: "Next" }));
  expect(onInspect).toHaveBeenLastCalledWith(
    "inputs/loaded.xlsx",
    "Parts",
    200,
  );
  await user.click(screen.getByRole("button", { name: "Previous" }));
  expect(onInspect).toHaveBeenLastCalledWith("inputs/loaded.xlsx", "Parts", 0);
  await user.selectOptions(screen.getByLabelText("Sheet"), "Summary");
  expect(onInspect).toHaveBeenLastCalledWith("inputs/loaded.xlsx", "Summary");
  await user.click(screen.getByRole("button", { name: "Inspect workbook" }));
  expect(onInspect).toHaveBeenLastCalledWith("inputs/draft.xlsx");
  await user.click(screen.getByRole("button", { name: "Input · parts" }));
  expect(onInspect).toHaveBeenLastCalledWith("inputs/parts.xlsx");
  await user.click(
    screen.getByRole("button", { name: "Output workbook · stale" }),
  );
  expect(onInspect).toHaveBeenLastCalledWith("outputs/result.xlsx");
});

it("shows the complete selected cell and lets the user clear the cell inspector", async () => {
  const user = userEvent.setup();
  render(
    <WorkbookPanel
      inspection={inspection}
      ready
      path="parts.xlsx"
      loadedPath="parts.xlsx"
      onPath={() => {}}
      onInspect={() => {}}
      onClose={() => {}}
      fullScreen={false}
      onExpand={() => {}}
      stale={false}
      inputs={{}}
    />,
  );
  await user.click(screen.getByRole("button", { name: completeCell }));
  expect(screen.getByRole("status").textContent).toBe(completeCell);
  await user.click(screen.getByRole("button", { name: "Clear" }));
  expect(screen.queryByRole("status")).toBeNull();
});
