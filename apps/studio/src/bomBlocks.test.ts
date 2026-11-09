import { describe, expect, it } from "vitest";
import * as Blockly from "blockly/core";
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { loadWorkflow, serializeWorkflow } from "./blocklyWorkflow";
import { steps } from "./stepRegistry";

const bom = readFileSync("../../workflows/bom.yaml", "utf8");
function open(source = bom) {
  const ws = new Blockly.Workspace();
  const result = loadWorkflow(ws, source);
  return { ws, result };
}
const changedLines = (a: string, b: string) => {
  const x = a.split("\n"),
    y = b.split("\n");
  return y.filter((line, i) => line !== x[i]).length + Math.abs(x.length - y.length);
};
describe("BOM workflow as blocks", () => {
  it("opens every BOM step and sheet as editable blocks and round-trips byte for byte", () => {
    const { ws, result } = open();
    expect(result.preservedSteps).toBe(0);
    expect(ws.getAllBlocks(false).some((b) => b.type === "mai_preserved")).toBe(false);
    const used = new Set(ws.getAllBlocks(false).map((b) => b.type));
    for (const type of ["mai_if", "mai_sql", "mai_rest", "mai_window", "mai_pick", "mai_pick_rule", "mai_stack", "mai_fill", "mai_log", "mai_fail", "mai_alias", "mai_param", "mai_sheet", "mai_out_column", "mai_out_style"])
      expect(used).toContain(type);
    expect(serializeWorkflow(ws, bom)).toBe(bom);
  });
  it("changes one field with a one-line diff", () => {
    const { ws } = open();
    const dedupe = ws
      .getAllBlocks(false)
      .find((b) => b.type === "mai_dedupe" && b.getFieldValue("BY") === "Key")!;
    dedupe.setFieldValue("first", "KEEP");
    const output = serializeWorkflow(ws, bom);
    expect(changedLines(bom, output)).toBe(1);
    expect(output).toContain("- dedupe: {by: [Key], keep: first}");
  });
  it("edits a complex expression in place and keeps the rest of the step", () => {
    const { ws } = open();
    const assign = ws
      .getAllBlocks(false)
      .find((b) => b.type === "mai_assign" && b.getFieldValue("COLUMN") === "Forbidden")!;
    const code = assign.getInputTargetBlock("VALUE")!;
    expect(code.type).toBe("mai_code");
    code.setFieldValue(String(code.getFieldValue("CODE")).replace('"KDS"', '"KDS", "ACME"'), "CODE");
    const output = serializeWorkflow(ws, bom);
    const forbidden = (y: string) =>
      parse(y).steps.find((s: any) => s.set?.Forbidden).set;
    expect(forbidden(output).Forbidden).toContain('"ACME"');
    expect({ ...forbidden(output), Forbidden: "" }).toEqual({ ...forbidden(bom), Forbidden: "" });
    expect(changedLines(bom, output)).toBeLessThan(6);
  });
  it("edits nested branch steps and output styles", () => {
    const { ws } = open();
    const log = ws
      .getAllBlocks(false)
      .find((b) => b.type === "mai_log" && String(b.getFieldValue("MESSAGE")).startsWith("SAP enrichment: no"))!;
    log.setFieldValue("SAP: nothing to query.", "MESSAGE");
    const style = ws
      .getAllBlocks(false)
      .find((b) => b.type === "mai_out_style" && b.getFieldValue("FILL") === "#0070C0")!;
    style.setFieldValue("#1F4E79", "FILL");
    const output = serializeWorkflow(ws, bom);
    expect(output).toContain("- log: 'SAP: nothing to query.'");
    expect(output).toContain('fill: "#1F4E79"');
    expect(changedLines(bom, output)).toBe(2);
  });
  it("adds a step from every registry spec through its library example", () => {
    for (const spec of steps) {
      const source =
        spec.op === "if"
          ? "steps:\n  - if: 'true'\n    then: []\n"
          : `steps:\n  - ${spec.op}: ${spec.example}\n`;
      const { ws, result } = open(source);
      expect(result.preservedSteps, spec.op).toBe(0);
      expect(parse(serializeWorkflow(ws, source)), spec.op).toEqual(parse(source));
    }
  });
});
import { expressionOf, setExpression } from "./blocklyWorkflow";
describe("inspector expression editing", () => {
  it("replaces an expression from text, visual when possible, as typed otherwise", () => {
    const source = "steps:\n  - filter: 'Qty > 1'\n";
    const ws = new Blockly.Workspace();
    loadWorkflow(ws, source);
    const filter = ws.getAllBlocks(false).find((b) => b.type === "mai_filter")!;
    expect(expressionOf(filter, "VALUE")).toBe("Qty > 1");
    setExpression(filter, "VALUE", "Qty >= 2 && Price > 0");
    expect(filter.getInputTargetBlock("VALUE")!.type).toBe("mai_logic");
    expect(parse(serializeWorkflow(ws, source)).steps[0].filter).toBe("Qty >= 2 && Price > 0");
    setExpression(filter, "VALUE", "norm(Code) in ['A', 'B']");
    expect(filter.getInputTargetBlock("VALUE")!.type).toBe("mai_code");
    expect(parse(serializeWorkflow(ws, source)).steps[0].filter).toBe("norm(Code) in ['A', 'B']");
  });
});
