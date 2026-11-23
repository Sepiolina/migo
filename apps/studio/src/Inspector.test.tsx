// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Inspector } from "./Inspector";
import type { Selection } from "./Navigator";
import { readPipeline } from "./pipeline";
import { partsSummaryExample } from "./workflowTemplates";

afterEach(cleanup);

function renderInspector(selection: Selection, source = partsSummaryExample) {
  const onBrowse = vi.fn();
  const onGoToColumn = vi.fn();
  const onChange = vi.fn();
  render(
    <Inspector source={source} pipeline={readPipeline(source)} selection={selection}
      onChange={onChange} onError={vi.fn()} onSelect={vi.fn()} onClose={vi.fn()}
      columns={[]} jumpColumns={["Part Number"]} onGoToColumn={onGoToColumn}
      bindings={{ parts: "samples/parts.xlsx" }} onBind={vi.fn()} onBrowse={onBrowse}
      route="parts-summary" workflowPath="" canPackage={false} onPackage={vi.fn()} onAddRead={vi.fn()} />,
  );
  return { onBrowse, onGoToColumn, onChange };
}

it("presents the sample controls before source settings", () => {
  const { onBrowse } = renderInspector({ kind: "source", id: "parts" });
  const headings = screen.getAllByRole("heading");
  expect(headings.slice(0, 3).map((heading) => heading.textContent)).toEqual(["Sample file", "Used by", "Source settings"]);
  const sample = within(screen.getByRole("region", { name: "Sample file" }));
  expect((sample.getByRole("textbox", { name: "Path" }) as HTMLInputElement).value).toBe("samples/parts.xlsx");
  fireEvent.click(sample.getByRole("button", { name: "Browse…" }));
  expect(onBrowse).toHaveBeenCalledWith("parts");
  expect(within(screen.getByRole("region", { name: "Source settings" })).getByRole("textbox", { name: "Name" })).toBeTruthy();
});

it("keeps the column jump and separates delete from the step navigation actions", () => {
  const source = partsSummaryExample.replace("filter: num(Quantity) > 0", "fill: [Part Number]");
  const { onGoToColumn, onChange } = renderInspector({ kind: "step", id: "steps.1" }, source);
  const affected = within(screen.getByRole("region", { name: "Affected columns" }));
  fireEvent.click(affected.getByRole("button", { name: "Go to column Part Number" }));
  expect(onGoToColumn).toHaveBeenCalledWith("Part Number");
  expect(onChange).not.toHaveBeenCalled();

  const actions = within(screen.getByRole("region", { name: "Step actions" }));
  expect(actions.getByRole("button", { name: "Duplicate" })).toBeTruthy();
  expect(actions.queryByRole("button", { name: "Delete step" })).toBeNull();
  const removal = within(screen.getByRole("region", { name: "Remove step" }));
  fireEvent.click(removal.getByRole("button", { name: "Delete step" }));
  const [[updated, label]] = onChange.mock.calls;
  expect(label).toBe("Delete step");
  expect(readPipeline(updated).steps.map((step) => step.op)).toEqual(["from", "group", "save"]);
});
