import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { StepTrace } from "./api";
import { buildDiagram, diagramSections, type WorkflowDiagram } from "./diagramModel";
import { readPipeline } from "./pipeline";

function expandedDiagram(source: string, trace?: StepTrace[]) {
  const pipeline = readPipeline(source);
  return buildDiagram(source, pipeline, new Set(diagramSections(pipeline, source).map(({ id }) => id)), trace);
}

function node(diagram: WorkflowDiagram, id: string) {
  return diagram.nodes.find((candidate) => candidate.id === id)!;
}

function connects(diagram: WorkflowDiagram, kind: "order" | "read" | "write", from: string, to: string) {
  return diagram.edges.some((edge) => edge.kind === kind && edge.from === from && edge.to === to);
}

describe("workflow diagram", () => {
  it("groups all 43 BOM operations under the eight section dividers", () => {
    const source = readFileSync(new URL("../../../workflows/bom.yaml", import.meta.url), "utf8");
    const pipeline = readPipeline(source);
    const sections = diagramSections(pipeline, source);
    const section = sections.find((candidate) => candidate.steps.some((step) => step.id === "steps.14"))!;
    expect(sections).toHaveLength(8);
    expect(section.label).toBe("groups");
    expect(sections.flatMap(({ steps }) => steps).filter(({ depth }) => depth === 0)).toHaveLength(43);
    const collapsed = buildDiagram(source, pipeline);
    const expanded = buildDiagram(source, pipeline, new Set([section.id]));
    expect(node(collapsed, section.id)).toMatchObject({ expanded: false, detail: "Steps 13–17" });
    expect(collapsed.nodes.some(({ kind }) => kind === "step")).toBe(false);
    expect(node(expanded, "steps.14")).toMatchObject({
      detail: "Step 15", selection: { kind: "step", id: "steps.14" },
    });
    expect(connects(expanded, "order", "steps.14", "steps.15")).toBe(true);
    expect(connects(collapsed, "read", "input.0", "section.0")).toBe(true);
    expect(connects(collapsed, "read", "table.steps.4", section.id)).toBe(true);
    expect(buildDiagram(source, readPipeline(source))).toEqual(collapsed);
  });

  it("preserves branch structure and the alternative versions of conditionally overwritten tables", () => {
    const diagram = expandedDiagram(`
input:
  records: {}
steps:
  - from: {input: records, sheet: [Sheet1, 1]}
  - save: parts
  - if: 'an expression containing parts and never_a_reference'
    then:
      - save: parts
      - split:
          where: 'true'
          then:
            - save: matching
          else:
            - append: [matching]
    else:
      - join: {with: parts, on: [Key]}
  - compare: {with: parts, by: [Key]}
  - append: [matching, future]
  - save: future
output:
  sheets:
    Results: parts
`);
    expect(connects(diagram, "order", "steps.2", "branch.steps.2.0")).toBe(true);
    expect(connects(diagram, "order", "steps.2", "branch.steps.2.1")).toBe(true);
    expect(connects(diagram, "order", "merge.steps.2.then.1", "merge.steps.2")).toBe(true);
    expect(connects(diagram, "order", "steps.2.else.0", "merge.steps.2")).toBe(true);
    expect(connects(diagram, "order", "merge.steps.2", "steps.3")).toBe(true);
    expect(connects(diagram, "read", "table.steps.1", "steps.2.else.0")).toBe(true);
    expect(connects(diagram, "read", "table.steps.2.then.0", "steps.2.else.0")).toBe(false);
    expect(connects(diagram, "read", "table.steps.1", "steps.3")).toBe(true);
    expect(connects(diagram, "read", "table.steps.2.then.0", "steps.3")).toBe(true);
    expect(connects(diagram, "write", "steps.2.then.1.split.then.0", "table.steps.2.then.1.split.then.0")).toBe(true);
    expect(connects(diagram, "read", "table.steps.2.then.1.split.then.0", "steps.2.then.1.split.else.0")).toBe(true);
    expect(connects(diagram, "read", "table.steps.2.then.1.split.then.0", "steps.4")).toBe(true);
    expect(node(diagram, "steps.4").unresolved).toEqual(["Conditional table:", "matching", "Unresolved: future"]);
    expect(connects(diagram, "read", "table.steps.5", "steps.4")).toBe(false);
    expect(connects(diagram, "read", "table.steps.1", "output.0")).toBe(true);
    expect(connects(diagram, "read", "table.steps.2.then.0", "output.0")).toBe(true);
    expect(JSON.stringify(diagram)).not.toContain("never_a_reference");
  });

  it("resolves remote saves and inherited output datasets through explicit YAML fields", () => {
    const diagram = expandedDiagram(`
steps:
  - sql: {save: remote_sql, query: "SELECT 'private_sql_value'"}
  - rest: {save: remote_rest, url: "http://localhost/hidden", body: "private_rest_body"}
  - from: remote_sql
output:
  sheets:
    - {name: Main, from: remote_rest, columns: [{header: Key}]}
    - {name: Inherited, like: Main}
    - {name: Override, like: Main, from: remote_sql}
    - {name: Invalid, like: Later}
    - {name: Later, from: remote_rest}
`);
    expect(connects(diagram, "read", "table.steps.0", "steps.2")).toBe(true);
    expect(connects(diagram, "read", "table.steps.1", "output.0")).toBe(true);
    expect(connects(diagram, "read", "table.steps.1", "output.1")).toBe(true);
    expect(connects(diagram, "read", "table.steps.0", "output.2")).toBe(true);
    expect(node(diagram, "output.3").unresolved?.join(" ")).toContain("Unresolved sheet template: Later");
    expect(JSON.stringify(diagram)).not.toMatch(/private_sql_value|localhost|private_rest_body/);
  });

  it("keeps structural labels and counts while excluding paths, free text, and stale or nested trace data", () => {
    const source = `
name: 'Test <&> "workflow"'
input:
  data: {label: '/private/path/my sheet.xlsx', match: "sample binding"}
steps:
  # ---- /private/team/secret note ----
  - from: data
  # Do not show silent_note_sample
  - log: 'literal sample: 11035'
  - if: 'row text with private data'
    then:
      - set: {column_name: 'hidden formula'}
  - from: '../private/other samples.xlsx'
  - filter: 'x == "sensitive_value"'
  - fail: "customer message"
output:
  sheets:
    'a long display label <&> which can wrap over multiple lines in the diagram': missing
`;
    const inputs = [
      { step: 0, name: "from" },
      { step: 1, name: "different_current_operation" },
      { step: 2, name: "if" },
      { step: 3, name: "from", skipped: true },
      { step: 4, name: "filter", error: "full trace error" },
    ];
    const trace = inputs.map((entry) => ({
      line: 0, total: 2, rows: [["trace values"]], log: ["trace log"], ms: 10, columns: [], ...entry,
    }));
    const diagram = expandedDiagram(source, trace);
    expect(diagram.title).toBe('Test <&> "workflow"');
    expect(node(diagram, "section.0").label).toBe("Steps");
    expect(node(diagram, "steps.0").rowCount).toBe(2);
    expect(node(diagram, "steps.2").rowCount).toBe(2);
    expect(node(diagram, "steps.2.then.0").rowCount).toBeUndefined();
    expect(diagram.nodes.filter(({ rowCount }) => rowCount !== undefined)).toHaveLength(2);
    expect(node(diagram, "output.0").lines.length).toBeGreaterThan(1);
    expect(JSON.stringify(diagram)).not.toMatch(/private\/|samples\.xlsx|literal sample|11035|silent_note_sample|sensitive_value|hidden formula|trace values|trace log|full trace error|customer message|sample binding/);
    expect(diagram.nodes.every(({ x, y, width, height }) => x >= 0 && y >= 0 && x + width <= diagram.width && y + height <= diagram.height)).toBe(true);
  });

  it("provides finite bounds for an empty workflow", () => {
    const source = "name: Blank\nsteps: []\n";
    expect(buildDiagram(source, readPipeline(source))).toMatchObject({
      title: "Blank", width: expect.any(Number), height: expect.any(Number), nodes: [], edges: [],
    });
  });
});
