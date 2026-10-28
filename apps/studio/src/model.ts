import { parseCondition } from "./SimpleCondition";
import { parseDocument } from "yaml";
import type { Node, Edge } from "@xyflow/react";
import { getOperation } from "./operationCatalog";

export const example = `name: Parts summary
input:
  parts: {label: Parts list}
steps:
  - from: {input: parts, sheet: Parts, trim: true}
  - filter: num(Quantity) > 0
  - group: {by: [Part Number], sum: [Quantity]}
  - save: summary
output:
  sheets: {Parts: summary}
`;

export type WorkflowNodeData = {
  operation: string;
  title: string;
  summary: string;
  category: string;
  icon: string;
  label: string;
  stepIndex: number;
  branch: boolean;
};
export type WorkflowGraphNode = Node<WorkflowNodeData, "workflow">;
export type Graph = {
  nodes: WorkflowGraphNode[];
  edges: Edge[];
  error?: string;
};
const COLUMN = 390;
const ROW = 250;
const record = (value: unknown): Record<string, unknown> =>
  value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
const words = (value: unknown): string =>
  Array.isArray(value)
    ? value.map(String).join(", ")
    : value == null
      ? ""
      : String(value);

function summarize(operation: string, argument: unknown): string {
  const arg = record(argument);
  const text = typeof argument === "string" ? argument : "";
  switch (operation) {
    case "from":
      return `Read ${text || words(arg.input) || "an input"}${arg.sheet ? ` · sheet ${words(arg.sheet)}` : ""}`;
    case "save":
      return `Keep the current table as ${text || "a named dataset"}`;
    case "filter": {
      const condition = parseCondition(text);
      if (condition) {
        const comparison: Record<string, string> = { "==": "equals", "!=": "does not equal", ">": "is greater than", ">=": "is at least", "<": "is less than", "<=": "is at most" };
        return `${condition.column} ${comparison[condition.operator]} ${condition.value}`;
      }
      return text === "true" ? "Keep every row" : "Keep rows matching a custom condition";
    }
    case "if":
      return `Choose a path when ${words(argument) || "the condition matches"}`;
    case "split":
      return `Route matching rows where ${words(arg.where) || "the condition matches"}`;
    case "group":
      return `Group ${arg.by ? `by ${words(arg.by)}` : "all rows"}${arg.sum ? ` · sum ${words(arg.sum)}` : ""}`;
    case "sort":
      return `Sort by ${words(arg.by) || "selected columns"}${arg.desc ? " · descending" : " · ascending"}`;
    case "join":
      return `Join ${words(arg.with) || "a dataset"}${arg.on ? ` on ${words(arg.on)}` : ""}`;
    case "compare":
      return `Compare with ${words(arg.with) || "a baseline"}${arg.by ? ` by ${words(arg.by)}` : ""}`;
    case "set":
      return `Calculate ${Object.keys(arg).join(", ") || "columns"}`;
    case "select":
      return `Keep columns ${Object.keys(arg).join(", ") || "in the selected order"}`;
    case "rename":
      return text
        ? `Rename columns using ${text}`
        : `Rename ${Object.keys(arg).join(", ") || "columns"}`;
    case "fill":
      return `Fill blanks in ${words(argument) || "selected columns"}`;
    case "dedupe":
      return `Keep one row per ${words(arg.by || argument) || "key"}`;
    case "append":
      return `Append rows from ${words(argument) || "datasets"}`;
    case "stack":
      return `Stack ${words(arg.columns) || "columns"} into ${words(arg.into) || "one column"}`;
    case "window":
      return `Calculate columns within ${arg.by ? `groups of ${words(arg.by)}` : "each group"}`;
    case "pick":
      return `Choose one row per ${words(arg.by) || "group"}`;
    case "sql":
      return `Query ${words(arg.connection) || "the database"}${arg.save ? ` · save as ${words(arg.save)}` : ""}`;
    case "rest":
      return `Request ${words(arg.method) || "GET"} ${words(arg.url) || "an API endpoint"}`;
    case "log":
      return text || words(arg.message) || "Write a run message";
    case "fail":
      return words(arg.message) || text || "Stop when the condition matches";
    default:
      return text || getOperation(operation).description;
  }
}

type Step = {
  id: string;
  operation: string;
  argument: unknown;
  index: number;
  branches: { name: "then" | "else"; steps: Step[] }[];
};
function readSteps(steps: unknown[], prefix: string): Step[] {
  return steps.map((value, index) => {
    const object = record(value);
    const operation =
      typeof value === "string"
        ? value
        : Object.keys(object)[0] || "Invalid step";
    const argument = object[operation];
    const id = `${prefix}${index}`;
    const branchObject = operation === "split" ? record(argument) : object;
    const branches: Step["branches"] = [];
    if (operation === "if" || operation === "split") {
      for (const name of ["then", "else"] as const) {
        branches.push({
          name,
          steps: readSteps(
            Array.isArray(branchObject[name])
              ? (branchObject[name] as unknown[])
              : [],
            `${id}.${name}.`,
          ),
        });
      }
    }
    return { id, operation, argument, index, branches };
  });
}
function dimensions(steps: Step[]): { width: number; height: number } {
  let width = 0;
  let height = 1;
  for (const step of steps) {
    const populated = step.branches.filter((branch) => branch.steps.length);
    const sizes = populated.map((branch) => dimensions(branch.steps));
    width +=
      1 + (sizes.length ? Math.max(...sizes.map((size) => size.width)) : 0);
    height = Math.max(
      height,
      sizes.reduce((sum, size) => sum + size.height, 0),
    );
  }
  return { width, height };
}

// This is an ordered projection, never an alternate workflow scheduler.
export function projectWorkflow(source: string): Graph {
  const doc = parseDocument(source);
  if (doc.errors.length)
    return { nodes: [], edges: [], error: doc.errors[0].message };
  let value: { steps?: unknown[] } | null;
  try {
    value = doc.toJS({ maxAliasCount: 0 });
  } catch (error) {
    return { nodes: [], edges: [], error: String(error) };
  }
  if (!value || !Array.isArray(value.steps))
    return {
      nodes: [],
      edges: [],
      error: "Add an ordered steps list to see the workflow.",
    };
  const nodes: WorkflowGraphNode[] = [];
  const edges: Edge[] = [];
  type Exit = { id: string; handle?: string; label?: string };
  const connect = (exit: Exit, target: string) =>
    edges.push({
      id: `${exit.id}${exit.handle ? `.${exit.handle}` : ""}-${target}`,
      source: exit.id,
      sourceHandle: exit.handle || "output",
      target,
      targetHandle: "input",
      label: exit.label,
      type: "smoothstep",
    });
  function layout(steps: Step[], column: number, row: number): Exit[] {
    let exits: Exit[] = [];
    for (const step of steps) {
      const definition = getOperation(step.operation);
      const summary = summarize(step.operation, step.argument);
      nodes.push({
        id: step.id,
        type: "workflow",
        position: { x: column * COLUMN, y: row * ROW },
        data: {
          operation: step.operation,
          title: definition.title,
          category: definition.category,
          icon: definition.icon,
          summary,
          label: `${step.index + 1}. ${definition.title}\n${summary}`,
          stepIndex: step.index,
          branch: step.branches.length > 0,
        },
      });
      exits.forEach((exit) => connect(exit, step.id));
      exits = [{ id: step.id }];
      column++;
      if (step.branches.length) {
        exits = [];
        let branchRow = row;
        let branchWidth = 0;
        for (const branch of step.branches) {
          if (!branch.steps.length) {
            exits.push({
              id: step.id,
              handle: branch.name,
              label: branch.name,
            });
            continue;
          }
          connect(
            { id: step.id, handle: branch.name, label: branch.name },
            branch.steps[0].id,
          );
          exits.push(...layout(branch.steps, column, branchRow));
          const size = dimensions(branch.steps);
          branchRow += size.height;
          branchWidth = Math.max(branchWidth, size.width);
        }
        column += branchWidth;
      }
    }
    return exits;
  }
  layout(readSteps(value.steps, "step."), 0, 0);
  return { nodes, edges };
}
