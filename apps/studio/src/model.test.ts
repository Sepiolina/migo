import { describe, it, expect } from "vitest";
import { projectWorkflow, example } from "./model";
describe("ordered workflow projection", () => {
  it("keeps execution order and source intact", () => {
    const graph = projectWorkflow(example);
    expect(graph.nodes.map((n) => n.id)).toEqual([
      "step.0",
      "step.1",
      "step.2",
      "step.3",
    ]);
    expect(graph.edges).toHaveLength(3);
    expect(example).toContain("group:");
    expect(graph.nodes.every((node) => node.type === "workflow")).toBe(true);
    expect(graph.nodes[1].position.x).toBeGreaterThan(
      graph.nodes[0].position.x,
    );
    expect(new Set(graph.nodes.map((node) => node.position.y)).size).toBe(1);
    expect(graph.nodes[2].data.summary).toBe("Group by ID · sum Quantity");
  });
  it("represents nested branches with distinct identities", () => {
    const graph = projectWorkflow(
      "steps:\n - if: true\n   then:\n    - save: yes\n   else:\n    - save: no\n - save: end",
    );
    expect(graph.nodes.map((n) => n.id)).toContain("step.0.then.0");
    expect(
      graph.edges.filter((e) => e.target === "step.1").map((e) => e.source),
    ).toEqual(["step.0.then.0", "step.0.else.0"]);
  });
  it("preserves the bypass path for a missing else branch", () => {
    const graph = projectWorkflow(
      "steps:\n - if: true\n   then:\n    - save: matched\n - save: end",
    );
    expect(
      graph.edges.find(
        (edge) => edge.source === "step.0" && edge.target === "step.1",
      ),
    ).toMatchObject({ label: "else", sourceHandle: "else" });
  });
  it("reserves lanes for nested branches without overlapping cards", () => {
    const graph = projectWorkflow(`steps:
 - split:
     where: Active
     then:
      - if: true
        then:
         - save: nested_yes
        else:
         - save: nested_no
      - save: matched
     else:
      - save: unmatched
 - save: end
`);
    expect(graph.nodes.map((node) => node.id)).toContain(
      "step.0.then.0.else.0",
    );
    for (const node of graph.nodes) {
      for (const other of graph.nodes) {
        if (node.id === other.id) continue;
        const horizontalOverlap =
          Math.abs(node.position.x - other.position.x) < 300;
        const verticalOverlap =
          Math.abs(node.position.y - other.position.y) < 196;
        expect(
          horizontalOverlap && verticalOverlap,
          `${node.id} overlaps ${other.id}`,
        ).toBe(false);
      }
    }
    expect(
      graph.edges
        .filter((edge) => edge.target === "step.1")
        .map((edge) => edge.source),
    ).toEqual(["step.0.then.1", "step.0.else.0"]);
    expect(
      graph.edges.every(
        (edge) =>
          graph.nodes.some((node) => node.id === edge.source) &&
          graph.nodes.some((node) => node.id === edge.target),
      ),
    ).toBe(true);
  });
  it("shows parse errors without inventing nodes", () => {
    const graph = projectWorkflow("steps: [");
    expect(graph.error).toBeTruthy();
    expect(graph.nodes).toEqual([]);
  });
  it("rejects recursive aliases without crashing the editor", () => {
    const graph = projectWorkflow(
      "steps: &steps\n - if: true\n   then: *steps",
    );
    expect(graph.error).toBeTruthy();
    expect(graph.nodes).toEqual([]);
  });
});
