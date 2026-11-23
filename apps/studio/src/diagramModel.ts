import { isMap, isScalar, isSeq, parseDocument, type Document, type Node } from "yaml";
import type { StepTrace } from "./api";
import type { Selection } from "./Navigator";
import type { Path, Pipeline, StepItem } from "./pipeline";

export interface DiagramSection {
  id: string;
  label: string;
  steps: StepItem[];
}

export interface DiagramNode {
  id: string;
  kind: "input" | "section" | "step" | "output" | "table" | "branch" | "merge";
  label: string;
  lines: string[];
  detail?: string;
  unresolved?: string[];
  rowCount?: number;
  selection?: Selection;
  sectionId?: string;
  expanded?: boolean;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface DiagramEdge {
  id: string;
  kind: "order" | "read" | "write";
  from: string;
  to: string;
  label?: string;
  path: string;
  labelX: number;
  labelY: number;
}

export interface WorkflowDiagram {
  title: string;
  width: number;
  height: number;
  nodes: DiagramNode[];
  edges: DiagramEdge[];
}

const NODE_WIDTH = 310;
const MARGIN = 24;
const ROW_GAP = 40;
const BRANCH_GAP = 52;
const TABLE_GAP = 220;
const MAX_LABEL_LINE = 24;

function structuralLabel(value: string, fallback: string) {
  // Structural names and section headings are the only user-authored labels.
  // Suppress path-looking tokens even when they occur in one of those fields.
  const text = value.replace(/[\u0000-\u001f\u007f-\u009f]/g, " ").replace(/\s+/g, " ").trim();
  if (/\bfile:\/\//i.test(text)
    || /(?:^|[\s"'(<{\[])(?:[a-z]:[\\/]|[\\/]{1,2}|~[\\/]|\.{1,2}[\\/])/i.test(text)
    || /\b[\w.-]{2,}[\\/][\w.-]+/.test(text)
    || /\b[\w.-]+[\\/][\w.-]{2,}/.test(text)
    || /\b[^\s]+\.(?:xlsx?|xlsm|csv|tsv)\b/i.test(text)) return fallback;
  return text || fallback;
}

function wrap(label: string): string[] {
  let pending = Array.from(label);
  const lines: string[] = [];
  while (pending.length > MAX_LABEL_LINE && lines.length < 2) {
    const boundary = pending.slice(0, MAX_LABEL_LINE + 1).lastIndexOf(" ");
    const end = boundary >= MAX_LABEL_LINE / 2 ? boundary : MAX_LABEL_LINE;
    lines.push(pending.slice(0, end).join(""));
    pending = pending.slice(end);
    if (pending[0] === " ") pending.shift();
  }
  lines.push(pending.length > MAX_LABEL_LINE ? pending.slice(0, MAX_LABEL_LINE - 1).join("") + "…" : pending.join(""));
  return lines;
}

function sectionsFrom(pipeline: Pipeline, yaml?: Document.Parsed): DiagramSection[] {
  const dividers = new Map<string, string>();
  const steps = yaml?.get("steps", true);
  if (isSeq(steps)) {
    for (const step of pipeline.steps) {
      if (step.depth !== 0) continue;
      const comment = (steps.items[step.index] as Node | undefined)?.commentBefore
        ?? (step.index === 0 ? steps.commentBefore : undefined);
      const divider = comment?.split("\n").find((line) => /^\s*-{2,}\s*.+?\s*-{2,}\s*$/.test(line));
      if (divider) dividers.set(step.id, divider.replace(/^\s*-+\s*|\s*-+\s*$/g, ""));
    }
  }
  const sections: DiagramSection[] = [];
  let lastNote: string | undefined;
  for (const step of pipeline.steps) {
    const note = dividers.size ? dividers.get(step.id)
      : step.depth === 0 && step.note && step.note !== lastNote ? step.note : undefined;
    if (note || !sections.length) {
      sections.push({
        id: `section.${step.top}`,
        label: structuralLabel(note?.split("\n")[0] ?? "", "Steps"),
        steps: [],
      });
    }
    if (note) lastNote = note;
    sections.at(-1)!.steps.push(step);
  }
  return sections;
}

export function diagramSections(pipeline: Pipeline, source?: string): DiagramSection[] {
  return sectionsFrom(pipeline, source === undefined ? undefined : parseDocument(source));
}

type TableVersion = { writers: string[]; guaranteed: boolean };
type Tables = Map<string, TableVersion>;
type Relationship = Pick<DiagramEdge, "kind" | "from" | "to" | "label">;
type NodeContents = Pick<DiagramNode, "id" | "kind" | "label"> & Partial<Pick<DiagramNode,
  "detail" | "rowCount" | "selection" | "sectionId" | "expanded">>;

const scalar = (value: unknown) => isScalar(value) && typeof value.value === "string" ? value.value : "";
const branchPath = (step: StepItem, branch: string): Path => [
  ...(step.op === "split" ? step.argPath : step.path), branch,
];
const stepNodeId = (step: StepItem) => step.id;
const outputNodeId = (index: number) => `output.${index}`;

function mergeAlternatives(result: Tables, then: Tables, otherwise: Tables) {
  for (const name of new Set([...then.keys(), ...otherwise.keys()])) {
    const a = then.get(name);
    const b = otherwise.get(name);
    result.set(name, {
      writers: [...new Set([...(a?.writers ?? []), ...(b?.writers ?? [])])],
      guaranteed: !!a?.guaranteed && !!b?.guaranteed,
    });
  }
}

/** Project only structured arguments interpreted by the runner. */
export function buildDiagram(
  source: string,
  pipeline: Pipeline,
  expanded: ReadonlySet<string> = new Set(),
  trace?: readonly StepTrace[],
): WorkflowDiagram {
  const yaml = parseDocument(source);
  const sections = sectionsFrom(pipeline, yaml);
  const inputIds = new Map(pipeline.inputs.map((input, index) => [input.id, `input.${index}`]));
  const lists = new Map<string, StepItem[]>();
  const visibleStep = new Map<string, string>();
  const warnings = new Map<string, Set<string>>();
  const relationships: Relationship[] = [];
  const tableNodes = new Map<string, { node: NodeContents; writer: string }>();
  const traceByStep = new Map(trace?.map((step) => [step.step, step]));
  for (const section of sections) {
    for (const step of section.steps) {
      visibleStep.set(step.id, expanded.has(section.id) ? stepNodeId(step) : section.id);
      const key = step.listPath.join(".");
      const list = lists.get(key) ?? [];
      if (!lists.has(key)) lists.set(key, list);
      list.push(step);
    }
  }
  const children = (step: StepItem, branch: string) => lists.get(branchPath(step, branch).join(".")) ?? [];
  const warn = (target: string, message: string) => {
    const messages = warnings.get(target) ?? new Set();
    messages.add(message);
    warnings.set(target, messages);
  };
  const read = (visible: string, name: string, tables: Tables, sourceType: "table" | "input" | "either") => {
    const label = structuralLabel(name, "unnamed reference");
    const table = sourceType !== "input" ? tables.get(name) : undefined;
    const input = sourceType !== "table" ? inputIds.get(name) : undefined;
    if (table) {
      for (const from of table.writers) relationships.push({ kind: "read", from, to: visible, label: `Read ${label}` });
      if (!table.guaranteed) warn(visible, `Conditional table: ${label}`);
    } else if (input) {
      relationships.push({ kind: "read", from: input, to: visible, label: `Read ${label}` });
    } else {
      warn(visible, `Unresolved: ${label}`);
    }
  };
  const write = (step: StepItem, visible: string, name: string, tables: Tables) => {
    if (!name.trim() || inputIds.has(name)) {
      warn(visible, `Unresolved write: ${structuralLabel(name, "unnamed table")}`);
      return;
    }
    const key = JSON.stringify([visible, name]);
    const label = structuralLabel(name, "Named table");
    let table = tableNodes.get(key);
    if (!table) {
      table = { node: { id: `table.${step.id}`, label, kind: "table" }, writer: visible };
      tableNodes.set(key, table);
      relationships.push({ kind: "write", from: visible, to: table.node.id, label: `Write ${label}` });
    }
    tables.set(name, { writers: [table.node.id], guaranteed: true });
  };

  function inspect(steps: StepItem[], tables: Tables) {
    for (const step of steps) {
      const visible = visibleStep.get(step.id)!;
      const arg = yaml.getIn(step.argPath, true);
      switch (step.op) {
        case "from": {
          if (isMap(arg)) {
            read(visible, scalar(arg.get("input", true)), tables, "input");
          } else {
            const value = scalar(arg);
            const bracket = value.indexOf("[");
            if (bracket < 0) read(visible, value, tables, "either");
            else if (bracket > 0 && value.endsWith("]") && value.length >= bracket + 3) {
              read(visible, value.slice(0, bracket), tables, "input");
            } else {
              warn(visible, "Unresolved: invalid input reference");
            }
          }
          break;
        }
        case "save":
        case "sql":
        case "rest":
          write(step, visible, scalar(isMap(arg) ? arg.get("save", true) : arg), tables);
          break;
        case "join":
        case "compare":
          read(visible, isMap(arg) ? scalar(arg.get("with", true)) : "", tables, "table");
          break;
        case "append":
          for (const entry of isSeq(arg) && arg.items.length ? arg.items : [undefined]) {
            read(visible, scalar(entry), tables, "table");
          }
          break;
        case "if": {
          const then = new Map(tables);
          const otherwise = new Map(tables);
          inspect(children(step, "then"), then);
          inspect(children(step, "else"), otherwise);
          mergeAlternatives(tables, then, otherwise);
          break;
        }
        case "split":
          // Both branches run in this order and share the saved tables.
          inspect(children(step, "then"), tables);
          inspect(children(step, "else"), tables);
          break;
      }
    }
  }

  const tables: Tables = new Map();
  inspect(lists.get("steps") ?? [], tables);
  const priorSheets = new Map<string, { from: string; columns: boolean }>();
  for (const output of pipeline.outputs) {
    const id = outputNodeId(output.index);
    let from = output.from;
    let columns = false;
    if (!pipeline.sheetsShort) {
      const outputPath: Path = ["output", "sheets", output.index];
      const like = scalar(yaml.getIn([...outputPath, "like"], true));
      from = scalar(yaml.getIn([...outputPath, "from"], true));
      const ownColumns = yaml.getIn([...outputPath, "columns"], true);
      columns = isSeq(ownColumns) && ownColumns.items.length > 0;
      if (like) {
        const base = priorSheets.get(like);
        if (base?.columns) {
          from ||= base.from;
          columns = true;
        } else {
          warn(id, `Unresolved sheet template: ${structuralLabel(like, "unnamed sheet")}`);
        }
      }
    }
    read(id, from, tables, "table");
    priorSheets.set(output.name, { from, columns });
  }

  const nodes: DiagramNode[] = [];
  const byId = new Map<string, DiagramNode>();
  let right = MARGIN + NODE_WIDTH;
  let bottom = MARGIN;
  function place(content: NodeContents, x: number, y: number) {
    const lines = wrap(content.label);
    const messages = warnings.get(content.id);
    const unresolved = messages && [...messages].flatMap(wrap);
    const height = 37 + 17 * (lines.length - 1) + (content.detail ? 18 : 0) + 17 * (unresolved?.length ?? 0);
    const node: DiagramNode = { ...content, x, y, width: NODE_WIDTH, height, lines, unresolved };
    nodes.push(node);
    byId.set(node.id, node);
    right = Math.max(right, x + node.width);
    bottom = Math.max(bottom, y + height);
    return node;
  }
  const floor = (node: DiagramNode) => node.y + node.height;
  const order = (from: DiagramNode | undefined, to: DiagramNode) => {
    if (from) relationships.push({ kind: "order", from: from.id, to: to.id });
  };
  function listWidth(steps: StepItem[]): number {
    let width = NODE_WIDTH;
    for (const step of steps) {
      if (step.op === "if" || step.op === "split") {
        width = Math.max(width, listWidth(children(step, "then")) + BRANCH_GAP + listWidth(children(step, "else")));
      }
    }
    return width;
  }
  function placeSteps(steps: StepItem[], x: number, y: number, previous: DiagramNode, sectionId: string) {
    for (const step of steps) {
      const match = step.depth === 0 ? traceByStep.get(step.top) : undefined;
      const rowCount = match?.name === step.op && !match.skipped && !match.error && Number.isSafeInteger(match.total) && match.total >= 0
        ? match.total
        : undefined;
      const number = step.depth === 0 ? step.top + 1 : step.index + 1;
      const stepNode = place({
        id: stepNodeId(step),
        kind: "step",
        label: step.spec?.title ?? "Unknown operation",
        detail: step.depth === 0 ? `Step ${number}` : `Branch step ${number}`,
        rowCount,
        selection: { kind: "step", id: step.id },
        sectionId,
      }, x, y);
      order(previous, stepNode);
      previous = stepNode;
      y = floor(previous) + ROW_GAP;
      if (step.op !== "if" && step.op !== "split") continue;
      const thenSteps = children(step, "then");
      const elseSteps = children(step, "else");
      const otherX = x + listWidth(thenSteps) + BRANCH_GAP;
      const branchTails = [thenSteps, elseSteps].map((steps, index) => {
        const laneX = index ? otherX : x;
        const branch = place({
          id: `branch.${step.id}.${index}`,
          kind: "branch",
          label: index ? "Else" : "Then",
          detail: steps.length ? (step.op === "split" ? (index ? "Remaining rows" : "Matching rows · first") : undefined) : "Pass through",
          sectionId,
        }, laneX, y);
        order(stepNode, branch);
        return placeSteps(steps, laneX, floor(branch) + ROW_GAP, branch, sectionId);
      });
      const merge = place({
        id: `merge.${step.id}`, kind: "merge",
        label: step.op === "split" ? "Combine branch rows" : "Continue after branch",
        sectionId,
      }, x, Math.max(...branchTails.map(floor)) + ROW_GAP);
      for (const tail of branchTails) order(tail, merge);
      previous = merge;
      y = floor(merge) + ROW_GAP;
    }
    return previous;
  }

  let y = MARGIN;
  for (const input of pipeline.inputs) {
    const node = place({
      id: inputIds.get(input.id)!, kind: "input",
      label: structuralLabel(input.id, "Input"), selection: { kind: "source", id: input.id },
    }, MARGIN, y);
    y = floor(node) + ROW_GAP;
  }
  let previous: DiagramNode | undefined;
  for (const section of sections) {
    const top = section.steps.filter((step) => step.depth === 0);
    const first = top[0].top + 1;
    const last = top.at(-1)!.top + 1;
    const sectionNode = place({
      id: section.id, kind: "section", label: section.label,
      detail: first === last ? `Step ${first}` : `Steps ${first}–${last}`,
      expanded: expanded.has(section.id), sectionId: section.id,
    }, MARGIN, y);
    order(previous, sectionNode);
    previous = sectionNode.expanded
      ? placeSteps(top, MARGIN, floor(sectionNode) + ROW_GAP, sectionNode, section.id)
      : sectionNode;
    y = floor(previous) + ROW_GAP;
  }
  for (const output of pipeline.outputs) {
    const node = place({
      id: outputNodeId(output.index), kind: "output",
      label: structuralLabel(output.name, "Output"), selection: { kind: "output", index: output.index },
    }, MARGIN, y);
    order(previous, node);
    previous = node;
    y = floor(node) + ROW_GAP;
  }
  // Each write gets a table node beside the workflow and multiple writes from
  // one collapsed section share a card. Keep cards disjoint when rows coincide.
  const tableX = right + TABLE_GAP;
  let tableY = MARGIN;
  for (const { node, writer } of tableNodes.values()) {
    const placed = place(node, tableX, Math.max(tableY, byId.get(writer)!.y));
    tableY = floor(placed) + ROW_GAP / 2;
  }
  const unique = new Map(relationships.map((edge) => [JSON.stringify(edge), edge]));
  const edges = [...unique.values()].map((edge, index): DiagramEdge => {
    const from = byId.get(edge.from)!;
    const to = byId.get(edge.to)!;
    const id = `edge.${index}`;
    if (edge.kind === "order") {
      const fromX = from.x + from.width / 2;
      const toX = to.x + to.width / 2;
      const middle = to.y - ROW_GAP / 2;
      const path = fromX === toX
        ? `M ${fromX} ${floor(from)} V ${to.y}`
        : `M ${fromX} ${floor(from)} V ${middle} H ${toX} V ${to.y}`;
      return { ...edge, id, path, labelX: middle, labelY: 0 };
    }
    const transfer = from.kind === "input";
    const direction = edge.kind;
    const lane = transfer ? tableX - TABLE_GAP + 30 : tableX - (direction === "write" ? 22 : 40);
    const startX = from.x + (from.kind === "table" ? 0 : from.width);
    const endX = to.x + (to.kind === "table" ? 0 : to.width);
    const startY = from.y + from.height / 2;
    const endY = to.y + to.height / 2;
    const path = `M ${startX} ${startY} H ${lane} V ${endY} H ${endX}`;
    // Keep edge captions next to the table card, or next to the input for an input read.
    const label = Array.from(edge.label ?? "");
    if (label.length > MAX_LABEL_LINE) label.splice(MAX_LABEL_LINE - 1, label.length, "…");
    const labelX = transfer ? startX + 90 : tableX - TABLE_GAP / 2;
    const labelY = direction === "write" ? endY - 7 : startY + 14;
    return { ...edge, id, label: label.join(""), path, labelX, labelY };
  });
  const width = Math.max(right + MARGIN, inputIds.size ? tableX - TABLE_GAP + 190 : 0);
  return {
    title: structuralLabel(pipeline.name, "Workflow"),
    width,
    height: Math.max(140, bottom + MARGIN),
    nodes,
    edges,
  };
}
