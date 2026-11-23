// @vitest-environment jsdom
import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { StepPreview } from "./StepPreview";

it("shows removed filter rows with original positions and full counts when the sample is bounded", () => {
  const comparison = { input: 5, changedCells: 0, changedRows: 0, before: [["a"], ["b"]],
    keptPositions: [1, 4], removedPositions: [2, 3], removed: [["b"], ["b"]] };
  render(<StepPreview title="Filter" comparison={comparison} trace={{ name: "filter", total: 2, step: 0, line: 1, ms: 0,
    columns: [{name:"Part",kind:"text",filled:2,distinct:2}], rows: [["a"], ["d"]] }} header="Filter" />);
  expect(screen.getByText("5 input · 2 retained · 3 removed")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", {name: "Removed rows"}));
  const grid = within(screen.getByRole("grid"));
  expect(grid.getAllByRole("rowheader").map((el) => el.textContent)).toEqual(["2", "3"]);
  expect(grid.getAllByText("b")).toHaveLength(2);
  expect(screen.getByText(/Showing 2 of 3 rows/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", {name: "After"}));
  expect(grid.getAllByRole("rowheader").map((el) => el.textContent)).toEqual(["1", "4"]);
});
