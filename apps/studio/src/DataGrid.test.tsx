// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { DataGrid, profileRows } from "./DataGrid";

afterEach(cleanup);

function preview(names: string[], rows: string[][]) {
  return <DataGrid columns={profileRows(names, rows)} rows={rows} total={rows.length} header="Preview" />;
}

it("copies a rectangle extended from its bottom-right corner as escaped TSV", async () => {
  const user = userEvent.setup();
  render(preview(["Part", "Note", "Result"], [
    ["α-001", 'line\t1\n"line 2"', "unselected"],
    ["α-002"],
  ]));

  const finalRow = screen.getByText("α-002").closest("tr")!;
  await user.click(within(finalRow).getAllByRole("gridcell")[1]);
  await user.keyboard("{Shift>}{ArrowLeft}{ArrowUp}{/Shift}");
  await user.keyboard("{Control>}c{/Control}");

  expect(await navigator.clipboard.readText()).toBe('α-001\t"line\t1\n""line 2"""\nα-002\t');
});

it("keeps navigation in the preview when the table shrinks or becomes empty", async () => {
  const user = userEvent.setup();
  const names = Array.from({ length: 27 }, (_, index) => `Field ${index + 1}`);
  const rows = [names.map((name) => `${name} first`), names.map((name) => `${name} last`)];
  const view = render(preview(names, rows));
  const grid = screen.getByRole("grid", { name: "Preview cells" });

  await user.click(screen.getByText("Field 26 last"));
  await user.keyboard("{ArrowRight}{ArrowRight}{ArrowDown}{Control>}c{/Control}");
  expect(grid.getAttribute("aria-activedescendant")).toBe(screen.getByText("Field 27 last").id);
  expect(await navigator.clipboard.readText()).toBe("Field 27 last");

  view.rerender(preview(["Part"], [["remaining"]]));
  await user.keyboard("{ArrowRight}{ArrowDown}{Control>}c{/Control}");
  expect(grid.getAttribute("aria-activedescendant")).toBe(screen.getByText("remaining").id);
  expect(await navigator.clipboard.readText()).toBe("remaining");

  view.rerender(preview(["Part"], []));
  await user.keyboard("{ArrowRight}{ArrowDown}{Control>}c{/Control}");
  expect(grid.hasAttribute("aria-activedescendant")).toBe(false);
  expect(await navigator.clipboard.readText()).toBe("remaining");
});

it("keeps grid shortcuts out of text inputs and restores focus after closing a column menu", async () => {
  const user = userEvent.setup();
  const rows = [["initial"], ["second"]];
  render(
    <>
      <DataGrid
        columns={profileRows(["Part"], rows)}
        rows={rows}
        total={rows.length}
        header={<input aria-label="Workflow title" defaultValue="Draft" />}
        onAction={vi.fn()}
      />
      <button>Open inspector</button>
    </>,
  );

  const title = screen.getByRole("textbox", { name: "Workflow title" });
  const grid = screen.getByRole("grid", { name: "Preview cells" });
  await user.tab();
  expect(document.activeElement).toBe(title);
  await user.tab();
  expect(document.activeElement).toBe(grid);
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Open inspector" }));

  await user.click(title);
  expect(fireEvent.keyDown(title, { key: "ArrowDown" })).toBe(true);
  expect(document.activeElement).toBe(title);

  const column = screen.getByRole("button", { name: /Part/ });
  await user.click(column);
  await user.keyboard("{ArrowDown}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Sort Z → A" }));
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(column);

  await user.click(column);
  await user.click(screen.getByRole("menuitem", { name: "Rename…" }));
  const rename = screen.getByRole("textbox", { name: "New name" });
  expect(document.activeElement).toBe(rename);
  expect(fireEvent.keyDown(rename, { key: "ArrowLeft" })).toBe(true);
  expect(document.activeElement).toBe(rename);
  await user.keyboard("{Escape}");
  expect(document.activeElement).toBe(column);

  await user.click(screen.getByText("initial"));
  await user.keyboard("{Shift>}{F10}{/Shift}");
  expect(screen.getByRole("menu", { name: "Actions on Part" })).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(document.activeElement).toBe(grid);
  expect(grid.getAttribute("aria-activedescendant")).toBe(screen.getByText("initial").id);
});

it("names the active cell and announces a requested column jump once", async () => {
  const user = userEvent.setup();
  const rows = [["P-001", ""], ["P-002", "Resistor"]];
  const columns = profileRows(["Part", "Description"], rows);
  const renderGrid = (request: number) => (
    <DataGrid columns={columns} rows={rows} total={rows.length} header="Preview" focusColumn={{ name: "Description", request }} />
  );
  const view = render(renderGrid(1));
  const grid = screen.getByRole("grid");
  const blank = screen.getByRole("gridcell", { name: "Row 1, column Description, blank" });
  expect(grid.getAttribute("aria-activedescendant")).toBe(blank.id);
  const announcement = screen.getByRole("status").firstChild;
  expect(announcement?.textContent).toBe("Jumped to column Description, row 1.");

  await user.keyboard("{ArrowDown}");
  expect(screen.getByRole("gridcell", { name: "Row 2, column Description, Resistor" })).toBeTruthy();
  view.rerender(renderGrid(1));
  expect(screen.getByRole("status").firstChild).toBe(announcement);

  view.rerender(renderGrid(2));
  expect(screen.getByRole("status").firstChild).not.toBe(announcement);
  expect(screen.getByRole("status").textContent).toBe("Jumped to column Description, row 2.");
});
