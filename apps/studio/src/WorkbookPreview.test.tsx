// @vitest-environment jsdom
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { request, RunnerError, type WorkbookPage } from "./api";
import { WorkbookPreview } from "./WorkbookPreview";

vi.mock("./api", async (importOriginal) => ({ ...await importOriginal<typeof import("./api")>(), request: vi.fn() }));
const inspect = vi.mocked(request);

const artifact = {
  workspace: "/workspace",
  path: "outputs/BOM.xlsx",
  revision: "revision-one",
  completedAt: 1_700_000_000_000,
  stale: false,
  canRefresh: true,
  onRefresh: vi.fn(),
};

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

function workbook(page: Partial<WorkbookPage> = {}): WorkbookPage {
  return {
    sheets: ["BOM", "Debug"], sheet: "BOM", offset: 0, columnOffset: 0, totalRows: 2_000, totalColumns: 50,
    rows: [["Part", "Qty"], ["A-001", "2"]],
    styleIds: [[1, 1], [0, 0]],
    styles: { 1: { bold: true, fillColor: "#203050", fontColor: "#FFFFFF", horizontal: "center" } },
    widths: [20, 8],
    unsupported: ["Charts"],
    ...page,
  };
}

it("shows the actual sheet and styles, switches sheets, and jumps to physical addresses", async () => {
  const user = userEvent.setup();
  inspect.mockImplementation(async (_, params) => {
    if (params.sheet === "Debug" && params.offset === 1999 && params.columnOffset === 49) {
      return workbook({ sheet: "Debug", offset: 1999, columnOffset: 49, rows: [["Far cell"]], widths: [20] });
    }
    return workbook({ sheet: String(params.sheet ?? "BOM") });
  });
  const onSheetChange = vi.fn();
  render(<WorkbookPreview {...artifact} onSheetChange={onSheetChange} />);

  const grid = await screen.findByRole("grid", { name: "Worksheet cells" });
  const cell = within(grid).getByText("Part");
  expect(cell.style.fontWeight).toBe("bold");
  expect(cell.style.getPropertyValue("--workbook-fill")).toBe("#203050");
  expect(within(grid).getByRole("columnheader", { name: "A" })).toBeTruthy();
  expect(within(grid).getByRole("rowheader", { name: "1" })).toBeTruthy();
  const limitations = screen.getByText("Preview limitations");
  await user.click(limitations);
  expect(limitations.closest("details")?.open).toBe(true);
  expect(screen.getByText("Charts")).toBeTruthy();

  await user.click(screen.getByRole("tab", { name: "Debug" }));
  expect(onSheetChange).toHaveBeenCalledWith("Debug");
  await screen.findByRole("grid");

  const address = screen.getByRole("textbox", { name: "Cell address" });
  await user.type(address, "AX2000{Enter}");
  const target = await screen.findByRole("gridcell", { name: "Row 2000, column AX, Far cell" });
  const moved = screen.getByRole("grid");
  expect(moved.getAttribute("aria-activedescendant")).toBe(target.id);
  expect(document.activeElement).toBe(moved);
  expect(inspect).toHaveBeenLastCalledWith("inspectWorkbookPage", {
    workspace: "/workspace", path: "outputs/BOM.xlsx", sheet: "Debug", offset: 1999,
    columnOffset: 49, limit: 100, columnLimit: 32,
  });
});

it("navigates across windows and copies a range in the destination window", async () => {
  const user = userEvent.setup();
  inspect.mockImplementation(async (_, params) => {
    if (params.offset === 2) return workbook({ offset: 2, rows: [["A-002", "3"], ["A-003", "4"]] });
    return workbook();
  });
  render(<WorkbookPreview {...artifact} />);
  await user.click(await screen.findByText("A-001"));
  expect((screen.getByRole("textbox", { name: "Cell address" }) as HTMLInputElement).value).toBe("A2");
  await user.keyboard("{ArrowDown}");
  await screen.findByRole("gridcell", { name: "Row 3, column A, A-002" });
  expect((screen.getByRole("textbox", { name: "Cell address" }) as HTMLInputElement).value).toBe("A3");
  await user.keyboard("{Shift>}{ArrowDown}{ArrowRight}{/Shift}{Control>}c{/Control}");
  expect(await navigator.clipboard.readText()).toBe("A-002\t3\nA-003\t4");
  expect((screen.getByRole("textbox", { name: "Cell address" }) as HTMLInputElement).value).toBe("B4");
  await user.keyboard("{PageUp}{PageUp}");
  await screen.findByRole("gridcell", { name: "Row 2, column B, 2" });
});

it("applies saved row heights, column widths, and General alignment", async () => {
  inspect.mockResolvedValue(workbook({
    rows: [["00123", "123", "TRUE", "Explicit"]],
    widths: [4, 24, 8, 10],
    rowHeights: [28],
    styleIds: [[0, 0, 0, 1]],
    styles: { 0: { horizontal: "general" }, 1: { horizontal: "right" } },
    generalAlignment: [".rcc"],
  }));
  render(<WorkbookPreview {...artifact} />);

  const grid = await screen.findByRole("grid", { name: "Worksheet cells" });
  const [text, numeric, logical, explicit] = within(grid).getAllByRole("gridcell");
  expect(text.closest("tr")?.style.getPropertyValue("--worksheet-row-height")).toBe("28pt");
  expect([...grid.querySelectorAll("col")].slice(1, 3).map((col) => col.style.width)).toEqual(["28px", "168px"]);
  expect([text, numeric, logical, explicit].map((cell) => cell.style.textAlign)).toEqual(["left", "right", "center", "right"]);
});

it("keeps the completed file visible during a failed inspection and retries only the preview", async () => {
  const user = userEvent.setup();
  inspect.mockRejectedValueOnce(new Error("Workbook is busy")).mockResolvedValueOnce(workbook());
  const refresh = vi.fn();
  render(<WorkbookPreview {...artifact} stale onRefresh={refresh} />);
  await screen.findByText("Workbook is busy");
  expect(screen.getByText("BOM.xlsx")).toBeTruthy();
  expect(screen.getByText("Previous run")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByRole("grid");
  expect(refresh).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Run to refresh" }));
  expect(refresh).toHaveBeenCalledOnce();
});

const oversized = new RunnerError("Workbook page exceeds the 1 MiB response limit.", "response_too_large");

it("keeps the keyboard target in a reduced page and reuses the smaller dimensions for that sheet", async () => {
  const user = userEvent.setup();
  inspect.mockImplementation(async (_, params) => {
    const offset = Number(params.offset);
    const columnOffset = Number(params.columnOffset);
    const sheet = String(params.sheet ?? "BOM");
    if (columnOffset === 0) {
      return workbook({ sheet, rows: Array.from({ length: 100 }, () => ["Part", "Qty"]) });
    }
    if (params.limit === 100) throw oversized;
    return workbook({
      sheet, offset, columnOffset, widths: [20],
      rows: Array.from({ length: Number(params.limit) }, () => ["Part number"]),
    });
  });
  render(<WorkbookPreview {...artifact} />);
  await screen.findByRole("grid");
  await user.type(screen.getByRole("textbox", { name: "Cell address" }), "B80{Enter}");
  await user.keyboard("{ArrowRight}");
  const target = await screen.findByRole("gridcell", { name: "Row 80, column C, Part number" });
  const grid = screen.getByRole("grid");
  expect(grid.getAttribute("aria-activedescendant")).toBe(target.id);
  expect(document.activeElement).toBe(grid);
  expect(inspect.mock.calls.slice(1).map(([, { sheet, offset, columnOffset, limit, columnLimit }]) =>
    [sheet, offset, columnOffset, limit, columnLimit])).toEqual([
    ["BOM", 0, 2, 100, 32],
    ["BOM", 30, 2, 50, 16],
  ]);

  await user.keyboard("{ArrowRight}");
  await screen.findByRole("gridcell", { name: "Row 80, column D, Part number" });
  expect(inspect).toHaveBeenLastCalledWith("inspectWorkbookPage", expect.objectContaining({
    sheet: "BOM", offset: 30, columnOffset: 3, limit: 50, columnLimit: 16,
  }));
  expect(inspect).toHaveBeenCalledTimes(4);
});

it("shows the runner error when even one cell exceeds the response limit", async () => {
  inspect.mockRejectedValue(oversized);
  render(<WorkbookPreview {...artifact} />);
  expect((await screen.findByRole("alert")).textContent).toContain(oversized.message);
  expect(inspect).toHaveBeenCalledTimes(8);
  expect(inspect).toHaveBeenLastCalledWith("inspectWorkbookPage", expect.objectContaining({
    offset: 0, columnOffset: 0, limit: 1, columnLimit: 1,
  }));
});

it("stops retrying an oversized page when its revision changes during a request", async () => {
  let rejectOldPage!: (reason: Error) => void;
  inspect.mockRejectedValueOnce(oversized)
    .mockImplementationOnce(() => new Promise((_, reject) => { rejectOldPage = reject; }))
    .mockResolvedValueOnce(workbook({ rows: [["New revision", "Qty"]] }));
  const { rerender } = render(<WorkbookPreview {...artifact} />);
  await act(async () => {});
  expect(inspect).toHaveBeenCalledTimes(2);
  rerender(<WorkbookPreview {...artifact} revision="revision-two" />);
  await screen.findByText("New revision");
  await act(async () => rejectOldPage(oversized));
  expect(inspect).toHaveBeenCalledTimes(3);
  expect(inspect).toHaveBeenLastCalledWith("inspectWorkbookPage", expect.objectContaining({
    limit: 100, columnLimit: 32,
  }));
  expect(screen.getByText("New revision")).toBeTruthy();
});

it("ignores a pending reduced page after jumping to another address", async () => {
  const user = userEvent.setup();
  let resolveOldPage!: (page: WorkbookPage) => void;
  inspect.mockResolvedValueOnce(workbook())
    .mockRejectedValueOnce(oversized)
    .mockImplementationOnce(() => new Promise((resolve) => { resolveOldPage = resolve; }))
    .mockResolvedValueOnce(workbook({ offset: 199, rows: [["New address", "Qty"]] }));
  render(<WorkbookPreview {...artifact} />);
  await screen.findByRole("grid");
  const address = screen.getByRole("textbox", { name: "Cell address" });
  await user.type(address, "A100{Enter}");
  expect(inspect).toHaveBeenCalledTimes(3);
  await user.clear(address);
  await user.type(address, "A200{Enter}");
  const target = await screen.findByRole("gridcell", { name: "Row 200, column A, New address" });
  await act(async () => resolveOldPage(workbook({ offset: 99, rows: [["Old address", "Qty"]] })));
  expect(screen.getByRole("grid").getAttribute("aria-activedescendant")).toBe(target.id);
  expect(screen.queryByText("Old address")).toBeNull();
  expect(inspect).toHaveBeenCalledTimes(4);
});
