import { describe, expect, it } from "vitest";
import * as Blockly from "blockly/core";
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import {
  loadWorkflow,
  registerWorkflowBlocks,
  serializeWorkflow,
} from "./blocklyWorkflow";

const partsSource = readFileSync("../../examples/parts-summary.yaml", "utf8");
const bomSource = readFileSync("../../workflows/bom.yaml", "utf8");

function opened(source: string) {
  const ws = new Blockly.Workspace();
  loadWorkflow(ws, source);
  return ws;
}
function step(ws: Blockly.Workspace, type: string) {
  return ws.getAllBlocks(false).find((b) => b.type === type)!;
}
describe("Blockly workflow codec", () => {
  it("edits sheet numbers without changing them into names or dropping read options", () => {
    const source = "steps:\n  - from: {input: parts, sheet: 1, trim: true}\n";
    const ws = opened(source);
    const read = step(ws, "mai_from");
    expect(read.getFieldValue("SHEET_KIND")).toBe("number");
    read.setFieldValue("2", "SHEET");
    expect(parse(serializeWorkflow(ws, source)).steps[0].from).toEqual({
      input: "parts",
      sheet: 2,
      trim: true,
    });
    read.setFieldValue("0", "SHEET");
    expect(() => serializeWorkflow(ws, source)).toThrow(/sheet number/);
  });
  it("changes a shorthand source to a numbered sheet using the engine's mapping form", () => {
    const source = "steps:\n  - from: parts # Keep this comment\n";
    const ws = opened(source);
    const read = step(ws, "mai_from");
    read.setFieldValue("number", "SHEET_KIND");
    read.setFieldValue("1", "SHEET");
    const result = serializeWorkflow(ws, source);
    expect(parse(result).steps[0].from).toEqual({ input: "parts", sheet: 1 });
    expect(result).toContain("# Keep this comment");
  });
  it("keeps significant whitespace in names when another setting changes", () => {
    const source =
      'steps:\n  - from: {input: parts, sheet: " Parts "}\n  - set: {" Part ": "1"}\n';
    const ws = opened(source);
    step(ws, "mai_from").setFieldValue("other", "SOURCE");
    step(ws, "mai_number").setFieldValue("2", "VALUE");
    expect(parse(serializeWorkflow(ws, source)).steps).toEqual([
      { from: { input: "other", sheet: " Parts " } },
      { set: { " Part ": "2" } },
    ]);
  });
  it.each([
    ["parts summary", partsSource],
    ["BOM", bomSource],
  ])("preserves the real workflow %s", (_name, source) => {
    const ws = opened(source);
    const output = serializeWorkflow(ws, source);
    expect(parse(output)).toEqual(parse(source));
    expect(output).toContain("# ");
    expect(ws.getTopBlocks(false)).toHaveLength(1);
  });
  it("opens a simple combined condition as editable comparison blocks", () => {
    const source = `steps:\n  - filter: 'PartNumber != "" && num(Quantity) > 0'\n`;
    const ws = opened(source);
    expect(
      ws.getAllBlocks(false).filter((b) => b.type === "mai_compare"),
    ).toHaveLength(2);
    expect(
      ws
        .getAllBlocks(false)
        .find(
          (b) =>
            b.type === "mai_column" && b.getFieldValue("NAME") === "Quantity",
        )
        ?.getFieldValue("MODE"),
    ).toBe("number");
    expect(parse(serializeWorkflow(ws, source))).toEqual(parse(source));
  });
  it("reopens generated conditions with arithmetic, grouping, negation and quoted columns", () => {
    const source = `steps:\n  - filter: 'num(Quantity) > 0'\n`;
    let ws = opened(source);
    step(ws, "mai_number").setFieldValue("5", "VALUE");
    let output = serializeWorkflow(ws, source);
    ws = opened(output);
    expect(step(ws, "mai_column").getFieldValue("MODE")).toBe("number");
    expect(step(ws, "mai_number").getFieldValue("VALUE")).toBe(5);
    step(ws, "mai_number").setFieldValue("6", "VALUE");
    output = serializeWorkflow(ws, output);
    expect(parse(output).steps[0].filter).toBe('(num(col("Quantity")) > 6)');

    const complex = `steps:\n  - filter: '!((num(col("Unit \\\"Price")) * 1e-5) >= (num(col("Qty")) + 2)) && (index > 1)'\n`;
    ws = opened(complex);
    expect(step(ws, "mai_not")).toBeDefined();
    expect(
      ws.getAllBlocks(false).filter((b) => b.type === "mai_arithmetic"),
    ).toHaveLength(2);
    expect(step(ws, "mai_column").getFieldValue("NAME")).toBe('Unit "Price');
    const numbers = ws
      .getAllBlocks(false)
      .filter((b) => b.type === "mai_number");
    numbers
      .find((b) => Number(b.getFieldValue("VALUE")) === 2)!
      .setFieldValue("3", "VALUE");
    output = serializeWorkflow(ws, complex);
    expect(parse(output).steps[0].filter).toContain("index > 1");
    ws = opened(output);
    expect(step(ws, "mai_not")).toBeDefined();
    expect(
      ws.getAllBlocks(false).filter((b) => b.type === "mai_arithmetic"),
    ).toHaveLength(2);
    expect(
      ws
        .getAllBlocks(false)
        .some(
          (b) =>
            b.type === "mai_custom_expression" &&
            b.getParent()?.type === "mai_compare",
        ),
    ).toBe(true);
  });
  it("preserves unsupported calls and ternaries as a whole expression", () => {
    for (const expr of ['trim(Name) == "x"', "Flag ? X : Y && Z"]) {
      const source = `steps:\n  - filter: '${expr}'\n`;
      const ws = opened(source);
      expect(step(ws, "mai_filter").getInputTargetBlock("VALUE")?.type).toBe(
        "mai_custom_expression",
      );
      expect(parse(serializeWorkflow(ws, source))).toEqual(parse(source));
    }
  });
  it("patches known fields while preserving comments, options and custom expressions", () => {
    const source = `name: Demo\nsteps:\n  # Read the workbook\n  - from: {input: parts, sheet: Old, trim: true}\n  - set: {Result: 'trim(col("Name"))'}\n  - group: {by: [Result], sum: [Qty], first: [Other]}\n  - sql: {query: SELECT 1}\noutput: {sheets: []}\n`;
    const ws = opened(source);
    step(ws, "mai_from").setFieldValue("New", "SHEET");
    step(ws, "mai_group").setFieldValue("Amount", "SUM");
    const output = serializeWorkflow(ws, source);
    expect(parse(output).steps).toEqual([
      { from: { input: "parts", sheet: "New", trim: true } },
      { set: { Result: 'trim(col("Name"))' } },
      { group: { by: ["Result"], sum: ["Amount"], first: ["Other"] } },
      { sql: { query: "SELECT 1" } },
    ]);
    expect(output).toContain("# Read the workbook");
    expect(step(ws, "mai_custom_expression").isEditable()).toBe(false);
  });
  it("serializes connected visual expressions in pipeline order", () => {
    const source = "steps: []\n";
    const ws = opened(source),
      root = step(ws, "mai_workflow");
    const filter = ws.newBlock("mai_filter"),
      compare = ws.newBlock("mai_compare"),
      col = ws.newBlock("mai_column"),
      number = ws.newBlock("mai_number"),
      save = ws.newBlock("mai_save");
    col.setFieldValue("Quantity", "NAME");
    number.setFieldValue("5", "VALUE");
    compare.setFieldValue(">=", "OP");
    save.setFieldValue("result", "NAME");
    root.getInput("STEPS")!.connection!.connect(filter.previousConnection!);
    filter.getInput("VALUE")!.connection!.connect(compare.outputConnection!);
    compare.getInput("LEFT")!.connection!.connect(col.outputConnection!);
    compare.getInput("RIGHT")!.connection!.connect(number.outputConnection!);
    filter.nextConnection!.connect(save.previousConnection!);
    expect(parse(serializeWorkflow(ws, source)).steps).toEqual([
      { filter: '(col(\"Quantity\") >= 5)' },
      { save: "result" },
    ]);
  });
  it("rejects detached, incomplete, disabled and invalid blocks", () => {
    const source = "steps: []\n",
      ws = opened(source),
      root = step(ws, "mai_workflow"),
      filter = ws.newBlock("mai_filter");
    expect(() => serializeWorkflow(ws, source)).toThrow(/detached/);
    root.getInput("STEPS")!.connection!.connect(filter.previousConnection!);
    expect(() => serializeWorkflow(ws, source)).toThrow(/socket/);
    const value = ws.newBlock("mai_boolean");
    filter.getInput("VALUE")!.connection!.connect(value.outputConnection!);
    filter.setDisabledReason(true, "test");
    expect(() => serializeWorkflow(ws, source)).toThrow(/disabled/);
  });
  it("rejects aliases, duplicate keys and custom tags", () => {
    registerWorkflowBlocks();
    for (const source of [
      "steps: &steps []\nother: *steps\n",
      "steps: []\nsteps: []\n",
      "steps: !unknown []\n",
    ])
      expect(() => opened(source)).toThrow();
  });
  it("preserves row variables when another part of a condition changes", () => {
    const source = `steps:\n  - filter: 'index > 1'\n`;
    const ws = opened(source);
    step(ws, "mai_number").setFieldValue("2", "VALUE");
    expect(parse(serializeWorkflow(ws, source)).steps[0].filter).toBe(
      "(index > 2)",
    );
  });
  it.each([
    'select: {"A, B": "A, B"}',
    'sort: {by: ["A, B"]}',
    'group: {by: ["A, B"], sum: [Qty]}',
    'join: {with: prior, on: ["A, B"]}',
    'dedupe: {by: ["A, B"]}',
  ])(
    "preserves a step whose column name cannot be represented: %s",
    (declaration) => {
      const source = `steps:\n  - ${declaration}\n`;
      const ws = opened(source);
      expect(step(ws, "mai_preserved")).toBeDefined();
      expect(parse(serializeWorkflow(ws, source))).toEqual(parse(source));
    },
  );
  it("validates grouping against both visible and preserved aggregates", () => {
    const source = "steps: []\n",
      ws = opened(source);
    const group = ws.newBlock("mai_group");
    step(ws, "mai_workflow")
      .getInput("STEPS")!
      .connection!.connect(group.previousConnection!);
    group.setFieldValue("Key", "BY");
    expect(() => serializeWorkflow(ws, source)).toThrow(/at least one/);
    group.setFieldValue("Key", "SUM");
    expect(() => serializeWorkflow(ws, source)).toThrow(/distinct/);
    group.setFieldValue("Qty", "SUM");
    expect(parse(serializeWorkflow(ws, source)).steps[0].group.sum).toEqual([
      "Qty",
    ]);

    const existing = "steps:\n  - group: {by: [Key], first: [Description]}\n";
    const loaded = opened(existing);
    step(loaded, "mai_group").setFieldValue("Other", "BY");
    expect(
      parse(serializeWorkflow(loaded, existing)).steps[0].group.first,
    ).toEqual(["Description"]);
    step(loaded, "mai_group").setFieldValue("Description", "BY");
    expect(() => serializeWorkflow(loaded, existing)).toThrow(/distinct/);
  });
  it("copies hidden step options and opaque expressions with duplicated blocks", () => {
    const source = `steps:\n  - join: {with: prior, on: [ID], right_on: [OtherID], normalize: true, take: {Result: Name}}\n  - set: {Clean: 'trim(col("Name"))'}\n  - sql: {query: SELECT 1}\n`;
    const ws = opened(source);
    const duplicate = (block: Blockly.Block) =>
      Blockly.serialization.blocks.append(
        Blockly.serialization.blocks.save(block, {
          addNextBlocks: false,
          saveIds: false,
        })!,
        ws,
      );
    const join = step(ws, "mai_join");
    const set = step(ws, "mai_set");
    const preserved = step(ws, "mai_preserved");
    const joinCopy = duplicate(join),
      setCopy = duplicate(set),
      preservedCopy = duplicate(preserved);
    preserved.nextConnection!.connect(joinCopy.previousConnection!);
    joinCopy.nextConnection!.connect(setCopy.previousConnection!);
    setCopy.nextConnection!.connect(preservedCopy.previousConnection!);
    joinCopy.setFieldValue("inner", "HOW");
    setCopy.setFieldValue("CleanCopy", "COLUMN");
    const steps = parse(serializeWorkflow(ws, source)).steps;
    expect(steps[3].join).toEqual({
      with: "prior",
      on: ["ID"],
      right_on: ["OtherID"],
      normalize: true,
      take: { Result: "Name" },
      how: "inner",
    });
    expect(steps[4].set).toEqual({ CleanCopy: 'trim(col("Name"))' });
    expect(steps[5].sql).toEqual({ query: "SELECT 1" });
  });
});
