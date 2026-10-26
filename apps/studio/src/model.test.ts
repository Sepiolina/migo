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
  });
  it("represents nested branches with distinct identities", () => {
    const graph = projectWorkflow(
      "steps:\n - if: true\n   then:\n    - save: yes\n   else:\n    - save: no\n - save: end",
    );
    expect(graph.nodes.map((n) => n.id)).toContain("step.0.then.0");
    expect(graph.edges.find((e) => e.target === "step.1")?.source).toBe(
      "step.0",
    );
  });
  it("shows parse errors without inventing nodes", () => {
    const graph = projectWorkflow("steps: [");
    expect(graph.error).toBeTruthy();
    expect(graph.nodes).toEqual([]);
  });
});
