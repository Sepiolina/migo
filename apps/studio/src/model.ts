import { parseDocument } from "yaml";
import type { Node, Edge } from "@xyflow/react";

export const example = `name: Parts summary
input:
  parts: {label: Parts list}
steps:
  - from: parts[Parts]
  - filter: num(Quantity) > 0
  - group: {by: [ID], sum: [Quantity]}
  - save: summary
output:
  sheets: {Parts: summary}
`;

export type Graph = { nodes: Node[]; edges: Edge[]; error?: string };
// This is an ordered projection, never an alternate workflow scheduler.
export function projectWorkflow(source: string): Graph {
  const doc = parseDocument(source);
  if (doc.errors.length)
    return { nodes: [], edges: [], error: doc.errors[0].message };
  const value = doc.toJS() as { steps?: unknown[] } | null;
  if (!value || !Array.isArray(value.steps))
    return {
      nodes: [],
      edges: [],
      error: "Add an ordered steps list to see the workflow.",
    };
  const nodes: Node[] = [],
    edges: Edge[] = [];
  function visit(
    steps: unknown[],
    prefix: string,
    x: number,
    y: number,
  ): number {
    let previous: string | undefined;
    steps.forEach((step, index) => {
      const id = `${prefix}${index}`;
      const object =
        step && typeof step === "object"
          ? (step as Record<string, unknown>)
          : {};
      const operation =
        typeof step === "string"
          ? step
          : (Object.keys(object)[0] ?? "Invalid step");
      const arg = object[operation];
      const detail =
        typeof arg === "string"
          ? arg
          : operation === "if"
            ? "Conditional branch"
            : JSON.stringify(arg ?? "");
      nodes.push({
        id,
        position: { x, y },
        data: {
          label: `${index + 1}. ${operation}\n${detail.length > 55 ? detail.slice(0, 55) + "…" : detail}`,
        },
        style: { width: 235, whiteSpace: "pre-wrap" },
      });
      if (previous)
        edges.push({
          id: `${previous}-${id}`,
          source: previous,
          target: id,
          type: "smoothstep",
        });
      previous = id;
      y += 112;
      if (operation === "if" || operation === "split") {
        const branches =
          operation === "split" && arg && typeof arg === "object"
            ? (arg as Record<string, unknown>)
            : object;
        for (const [branch, dx] of [
          ["then", -150],
          ["else", 150],
        ] as const) {
          if (
            Array.isArray(branches[branch]) &&
            (branches[branch] as unknown[]).length
          ) {
            const child = `${id}.${branch}.0`;
            edges.push({
              id: `${id}-${child}`,
              source: id,
              target: child,
              label: branch,
              type: "smoothstep",
            });
            y = Math.max(
              y,
              visit(
                branches[branch] as unknown[],
                `${id}.${branch}.`,
                x + dx,
                y,
              ),
            );
          }
        }
      }
    });
    return y;
  }
  visit(value.steps, "step.", 0, 0);
  return { nodes, edges };
}
