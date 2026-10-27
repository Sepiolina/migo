// @vitest-environment jsdom
import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import {
  conditionExpression,
  parseCondition,
  SimpleCondition,
} from "./SimpleCondition";

it("generates safe expressions for names and values with quotes", () => {
  const condition = {
    column: 'Part "No"',
    operator: "==",
    value: 'A "quoted" value',
    type: "text" as const,
  };
  expect(conditionExpression(condition)).toBe(
    'str(col("Part \\"No\\"")) == "A \\"quoted\\" value"',
  );
  expect(parseCondition(conditionExpression(condition))).toEqual(condition);
});
it("recognizes numeric comparisons from existing workflows", () => {
  expect(parseCondition("num(Quantity) > 0")).toEqual({
    column: "Quantity",
    operator: ">",
    value: "0",
    type: "number",
  });
  expect(parseCondition('num(col("Part Number")) <= -1.5')).toEqual({
    column: "Part Number",
    operator: "<=",
    value: "-1.5",
    type: "number",
  });
  expect(parseCondition("num(Quantity) > 0 && Active")).toBeUndefined();
});
it("preserves complex expressions until the builder is edited and validates the column", async () => {
  const user = userEvent.setup();
  const changes: string[] = [];
  const validity: boolean[] = [];
  function Harness() {
    const [expression, setExpression] = useState("num(Qty) > 0 && Active");
    return (
      <SimpleCondition
        expression={expression}
        disabled={false}
        onChange={(next) => {
          changes.push(next);
          setExpression(next);
        }}
        onValidityChange={(value) => validity.push(value)}
      />
    );
  }
  render(<Harness />);
  await user.click(screen.getByRole("button", { name: "Build condition" }));
  expect(changes).toEqual([]);
  await user.type(screen.getByLabelText("Value"), "x");
  expect(validity.at(-1)).toBe(false);
  expect(
    screen.getByText("Enter a column name to complete the condition."),
  ).toBeTruthy();
  await user.type(screen.getByLabelText("Column"), "Status");
  expect(validity.at(-1)).toBe(true);
  expect(changes.at(-1)).toBe('str(col("Status")) == "x"');
});
