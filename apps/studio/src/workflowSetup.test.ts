import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { parseDocument } from "yaml";
import {
  readWorkbookSettings,
  validateWorkbookSettings,
  writeWorkbookSettings,
} from "./workflowSetup";

const source = `# Workflow comment
name: Report
input:
  parts: {label: Parts, match: "(?i)parts"} # input comment
steps:
  - save: result
output:
  sheets:
    - name: Parts
      from: result
      freeze: true
      columns: [{header: Part, width: 12}]
`;
describe("workbook settings", () => {
  it("preserves comments, match patterns, steps and detailed sheet options through edits", () => {
    const settings = readWorkbookSettings(source)!;
    settings.inputs[0].id = "items";
    settings.inputs[0].label = "Items";
    settings.outputs[0].name = "Results";
    settings.outputs[0].table = "totals";
    const updated = writeWorkbookSettings(source, settings);
    expect(updated).toContain("# Workflow comment");
    expect(updated).toContain("# input comment");
    const value = parseDocument(updated).toJS();
    expect(value.input.items).toEqual({ label: "Items", match: "(?i)parts" });
    expect(value.steps).toEqual([{ save: "result" }]);
    expect(value.output.sheets[0]).toEqual({
      name: "Results",
      from: "totals",
      freeze: true,
      columns: [{ header: "Part", width: 12 }],
    });
  });
  it("round trips the repository's detailed workflow and supports sheet mappings", () => {
    const example = readFileSync("../../examples/parts-summary.yaml", "utf8");
    const settings = readWorkbookSettings(example)!;
    settings.name = "New report";
    const before = parseDocument(example).toJS();
    const after = parseDocument(
      writeWorkbookSettings(example, settings),
    ).toJS();
    expect(after.output).toEqual(before.output);
    expect(after.steps).toEqual(before.steps);
    const simple = `name: Test\ninput: {parts: {label: Parts}}\nsteps: []\noutput: {sheets: {Parts: result}}\n`;
    const simpleSettings = readWorkbookSettings(simple)!;
    simpleSettings.outputs[0].name = "Summary";
    expect(
      parseDocument(writeWorkbookSettings(simple, simpleSettings)).toJS().output
        .sheets,
    ).toEqual({ Summary: "result" });
  });
  it("rejects invalid names and unsafe structures", () => {
    const settings = readWorkbookSettings(source)!;
    settings.inputs.push({ key: "new", id: "parts", label: "Another" });
    settings.outputs.push({ key: "new", name: "parts", table: "" });
    expect(validateWorkbookSettings(settings)).toEqual(
      expect.arrayContaining([
        "Input identifiers must be unique.",
        "Output sheet names must be unique, ignoring case.",
        "Enter a saved table name for each output sheet.",
      ]),
    );
    expect(() => writeWorkbookSettings(source, settings)).toThrow();
    expect(
      readWorkbookSettings(
        source.replace(
          'parts: {label: Parts, match: "(?i)parts"}',
          "parts: &shared {label: Parts}\n  other: *shared",
        ),
      ),
    ).toBeNull();
    expect(readWorkbookSettings(source + "\n---\nname: Other\n")).toBeNull();
  });
});
