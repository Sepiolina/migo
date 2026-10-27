import { describe, expect, it } from "vitest";
import { parse } from "yaml";
import { readGuided, updateGuided } from "./guidedArgument";
import { operations } from "./operationCatalog";

describe("guided step settings", () => {
  it("provides editable defaults for every catalog operation", () => {
    for (const operation of operations)
      expect(
        readGuided(operation.operation, operation.defaultArgument),
        operation.operation,
      ).toBeDefined();
  });
  it("keeps settings outside the guided controls when editing a field", () => {
    const original =
      "with: parts\non: [Part]\nhow: left\ncustom: {nested: [one, two]}\n";
    const updated = updateGuided(original, "join", "on", ["SKU"]);
    expect(parse(updated)).toEqual({
      with: "parts",
      on: ["SKU"],
      how: "left",
      custom: { nested: ["one", "two"] },
    });
  });
  it("leaves unsupported values for the YAML editor", () => {
    expect(readGuided("set", "Amount: null")).toBeUndefined();
    expect(readGuided("join", "with: parts\nhow: outer")).toBeUndefined();
    expect(readGuided("sort", "by: Part")).toBeUndefined();
    expect(readGuided("sql", "query: SELECT 1")).toBeUndefined();
  });
  it("can clear an optional enum without inserting an invalid value", () => {
    expect(
      parse(updateGuided("with: parts\nhow: inner", "join", "how", undefined)),
    ).toEqual({ with: "parts" });
  });
});

it("uses the source editor for ordered maps with integer-like keys", () => {
  expect(readGuided("select", '"10": A\n"2": B')).toBeUndefined();
  expect(
    readGuided("from", 'input: x\ncolumns: {"10": A, "2": B}'),
  ).toBeUndefined();
  expect(
    readGuided("join", 'with: x\ntake: {"10": A, "2": B}'),
  ).toBeUndefined();
  expect(readGuided("select", '"010": A\n"2x": B')).toBeDefined();
});
it("does not split existing column names containing commas or whitespace", () => {
  expect(readGuided("sort", 'by: ["Part, Number", Qty]')).toBeUndefined();
  expect(readGuided("sort", 'by: [" Part", Qty]')).toBeUndefined();
  expect(readGuided("sort", 'by: ["", Qty]')).toBeUndefined();
});
it("preserves the type of a numeric sheet index and a numeric-looking sheet name", () => {
  const number = "input: parts\nsheet: 1";
  const name = 'input: parts\nsheet: "1"';
  expect(readGuided("from", number)).toBeDefined();
  expect(parse(updateGuided(number, "from", "trim", true)).sheet).toBe(1);
  expect(parse(updateGuided(name, "from", "trim", true)).sheet).toBe("1");
});
