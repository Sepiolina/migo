import {
  Document,
  type Node,
  isMap,
  isNode,
  isScalar,
  isSeq,
  parseDocument,
} from "yaml";

function editable(source: string) {
  const doc = parseDocument(source, { keepSourceTokens: true });
  if (doc.errors.length) throw new Error(doc.errors[0].message);
  doc.toJS({ maxAliasCount: 0 });
  const steps = doc.get("steps", true);
  if (!isSeq(steps)) throw new Error("Workflow needs an ordered steps list.");
  return { doc, steps };
}
export type StepDetail = {
  operation: string;
  argument: string;
  editable: boolean;
};
export function stepDetail(source: string, index: number): StepDetail {
  const { doc, steps } = editable(source);
  const step = steps.items[index];
  if (!isMap(step) || step.items.length !== 1 || !isScalar(step.items[0].key))
    return {
      operation: "Compound or shorthand step",
      argument: "",
      editable: false,
    };
  const pair = step.items[0];
  const argumentDoc = new Document();
  argumentDoc.contents = isNode(pair.value)
    ? (pair.value.clone() as Node)
    : doc.createNode(pair.value);
  argumentDoc.commentBefore = null;
  argumentDoc.comment = null;
  return {
    operation: String(pair.key),
    argument: argumentDoc.toString().trimEnd(),
    editable: true,
  };
}
export function editArgument(
  source: string,
  index: number,
  argument: string,
): string {
  const { doc, steps } = editable(source);
  const step = steps.items[index];
  if (!isMap(step) || step.items.length !== 1)
    throw new Error("Edit compound steps in YAML source.");
  const parsed = parseDocument(argument, { keepSourceTokens: true });
  if (parsed.errors.length) throw new Error(parsed.errors[0].message);
  parsed.toJS({ maxAliasCount: 0 });
  const old = step.items[0].value;
  if (isScalar(old) && isScalar(parsed.contents))
    old.value = parsed.contents.value;
  else {
    const replacement = parsed.contents ?? doc.createNode(null);
    if (isNode(old)) {
      replacement.commentBefore = old.commentBefore;
      replacement.comment = old.comment;
      replacement.spaceBefore = old.spaceBefore;
    }
    step.items[0].value = replacement;
  }
  return doc.toString();
}
export function changeSteps(
  source: string,
  action: "add" | "delete" | "up" | "down",
  index = 0,
): string {
  const { doc, steps } = editable(source);
  if (action === "add") steps.add(doc.createNode({ filter: "true" }));
  else {
    if (index < 0 || index >= steps.items.length)
      throw new Error("Select a top-level step first.");
    if (action === "delete") steps.items.splice(index, 1);
    else {
      const next = index + (action === "up" ? -1 : 1);
      if (next < 0 || next >= steps.items.length) return source;
      [steps.items[index], steps.items[next]] = [
        steps.items[next],
        steps.items[index],
      ];
    }
  }
  return doc.toString();
}
export function topLevelCount(source: string): number {
  try {
    return editable(source).steps.items.length;
  } catch {
    return 0;
  }
}
export type History = { past: string[]; present: string; future: string[] };
export function historyTransition(
  state: History,
  action:
    { type: "edit" | "reset"; source: string } | { type: "undo" | "redo" },
): History {
  if (action.type === "reset")
    return { past: [], present: action.source, future: [] };
  if (action.type === "edit")
    return action.source === state.present
      ? state
      : {
          past: [...state.past, state.present].slice(-200),
          present: action.source,
          future: [],
        };
  if (action.type === "undo")
    return state.past.length
      ? {
          past: state.past.slice(0, -1),
          present: state.past.at(-1)!,
          future: [state.present, ...state.future],
        }
      : state;
  return state.future.length
    ? {
        past: [...state.past, state.present],
        present: state.future[0],
        future: state.future.slice(1),
      }
    : state;
}
