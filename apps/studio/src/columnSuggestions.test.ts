import { describe, expect, it } from "vitest";
import { suggestColumns } from "./columnSuggestions";
import type { Inspection } from "./api";
const preview: Inspection = {
  sheets: ["Orders"],
  sheet: "Orders",
  offset: 0,
  rows: [["Customer", "Quantity", "Unit price"]],
  totalRows: 5,
  truncated: false,
};
const source = `name: Totals
input: {orders: {label: Orders}}
steps:
  - from: {input: orders, sheet: Orders}
  - filter: num(Quantity) > 0
  - set: {Line total: num(Quantity) * num(col("Unit price"))}
  - group: {by: [Customer], sum: [Line total]}
  - save: totals
output: {sheets: {Totals: totals}}
`;
const suggestions = (
  text = source,
  selected = "step.4",
  inspection = preview,
  path = "orders.xlsx",
) =>
  suggestColumns(text, selected, inspection, path, { orders: "orders.xlsx" });
describe("advisory column suggestions", () => {
  it("tracks read, filter, calculation and grouped output names", () => {
    expect(suggestions(source, "step.2")).toEqual([
      "Customer",
      "Quantity",
      "Unit price",
    ]);
    expect(suggestions(source, "step.3")).toEqual([
      "Customer",
      "Quantity",
      "Unit price",
      "Line total",
    ]);
    expect(suggestions()).toEqual(["Customer", "Line total"]);
  });
  it("does not mistake output previews or another page/sheet for source headers", () => {
    expect(suggestions(source, "step.2", preview, "output.xlsx")).toEqual([]);
    expect(suggestions(source, "step.2", { ...preview, offset: 100 })).toEqual(
      [],
    );
    expect(
      suggestions(source, "step.2", { ...preview, sheet: "Other" }),
    ).toEqual([]);
  });
  it("drops guesses for unknown operations and advanced headers", () => {
    expect(
      suggestions(
        source.replace("  - filter: num(Quantity) > 0", "  - window: {}"),
      ),
    ).toEqual([]);
    expect(
      suggestions(
        source.replace("sheet: Orders", "sheet: Orders, columns: {ID: A}"),
      ),
    ).toEqual([]);
    expect(suggestions("steps: [")).toEqual([]);
    expect(suggestions(source, "step.1.then.0")).toEqual([]);
  });
  it("matches group output ordering and rejects invalid group schemas", () => {
    expect(
      suggestions(
        source.replace(
          "sum: [Line total]",
          "sum: [Line total], first: [Quantity]",
        ),
      ),
    ).toEqual(["Customer", "Quantity", "Line total"]);
    expect(
      suggestions(source.replace("sum: [Line total]", "sum: Missing")),
    ).toEqual([]);
    expect(
      suggestions(source.replace("sum: [Line total]", "sum: [Missing]")),
    ).toEqual([]);
    expect(
      suggestions(source, "step.1", {
        ...preview,
        rows: [["Customer", "  ", "Quantity"]],
      }),
    ).toEqual(["Customer", "Quantity"]);
  });
});
