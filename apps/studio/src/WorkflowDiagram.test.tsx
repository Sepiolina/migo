// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import type { Trace } from "./api";
import { saveDiagramSvg } from "./diagramExport";
import type { Selection } from "./Navigator";
import { readPipeline } from "./pipeline";
import { WorkflowDiagram } from "./WorkflowDiagram";

vi.mock("./diagramExport", async (importOriginal) => ({
  ...await importOriginal<typeof import("./diagramExport")>(),
  saveDiagramSvg: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

const source = `name: Diagram test
steps:
  # First section
  - fill: { columns: [Name], value: Unknown }
  # Second section
  - filter: Name != "Unknown"
`;
const pipeline = readPipeline(source);
const first: Selection = { kind: "step", id: "steps.0" };
const second: Selection = { kind: "step", id: "steps.1" };

it("starts collapsed, traverses sections using the keyboard, and only changes the shared selection on activation", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  const { rerender } = render(<WorkflowDiagram source={source} pipeline={pipeline} selection={first} onSelect={onSelect} />);
  const section = screen.getByRole("button", { name: /First section/ });
  expect(section.getAttribute("aria-expanded")).toBe("false");
  expect(screen.queryByRole("button", { name: /Fill.*Step 1/ })).toBeNull();
  section.focus();
  await user.keyboard("{ArrowDown}{Enter}{ArrowDown}");
  const step = screen.getByRole("button", { name: /Filter.*Step 2/ });
  expect(document.activeElement).toBe(step);
  expect(onSelect).not.toHaveBeenCalled();
  await user.keyboard(" ");
  expect(onSelect).toHaveBeenCalledWith(second);
  rerender(<WorkflowDiagram source={source} pipeline={pipeline} selection={second} onSelect={onSelect} />);
  expect(step.getAttribute("aria-pressed")).toBe("true");
});

it("reveals a changed navigator selection on re-entry and exports the expanded structure without preview counts", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  const trace: Trace = { format: "csv", steps: [{ step: 1, name: "filter", line: 6, columns: [], total: 1825, ms: 1 }] };
  const { rerender } = render(<WorkflowDiagram source={source} pipeline={pipeline} selection={first} onSelect={onSelect} active={false} />);
  rerender(<WorkflowDiagram source={source} pipeline={pipeline} selection={second} onSelect={onSelect} active={false} trace={trace} />);
  expect(screen.getByRole("button", { name: /Second section/ }).getAttribute("aria-expanded")).toBe("false");
  rerender(<WorkflowDiagram source={source} pipeline={pipeline} selection={second} onSelect={onSelect} trace={trace} />);
  const count = (1825).toLocaleString();
  expect(screen.getByRole("button", { name: `Filter rows. Step 2. ${count} rows` }).getAttribute("aria-pressed")).toBe("true");
  vi.mocked(saveDiagramSvg).mockResolvedValue(true);
  await user.click(screen.getByRole("button", { name: "Export SVG" }));
  expect(screen.getByRole("status").textContent).toBe("SVG exported.");
  const svg = vi.mocked(saveDiagramSvg).mock.calls[0]?.[0];
  expect(svg).toContain("Filter rows");
  expect(svg).not.toContain(`${count} rows`);
  expect(onSelect).not.toHaveBeenCalled();
});
