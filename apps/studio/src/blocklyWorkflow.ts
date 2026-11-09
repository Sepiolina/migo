/**
 * The YAML <-> Blockly codec. It is generic: every step and nested item is
 * read and written from its registry spec (stepRegistry.ts). Unchanged parts
 * of the document, including comments and block scalars, are written back
 * exactly; changed fields are patched into the original mapping. Anything a
 * spec cannot represent opens as a preserved block and round-trips intact.
 */
import {
  parseVisualExpression,
  type VisualExpression,
} from "./blocklyExpressions";
import * as Blockly from "blockly/core";
import {
  Document,
  isAlias,
  isMap,
  isNode,
  isPair,
  isScalar,
  isSeq,
  Pair,
  parseDocument,
  Scalar,
  YAMLMap,
  YAMLSeq,
  type Node,
} from "yaml";

import {
  blockMemory as memory,
  refreshOptions,
  registerWorkflowBlocks,
} from "./blocklyBlocks";
import {
  fieldsOf,
  itemByType,
  stepByOp,
  stepByType,
  type Field,
  type ItemSpec,
  type StepSpec,
} from "./stepRegistry";
export { registerWorkflowBlocks, workflowToolbox } from "./blocklyBlocks";
import { preserveText } from "./yamlSplice";

type Doc = ReturnType<typeof parseDocument>;
/** A YAML shape that the block fields cannot show faithfully. */
class Unrepresentable extends Error {}

function document(source: string) {
  const doc = parseDocument(source, { keepSourceTokens: true, uniqueKeys: true });
  if (doc.errors.length)
    throw new Error(`Cannot open workflow YAML: ${doc.errors[0].message}`);
  if (doc.warnings.length)
    throw new Error(`Cannot safely edit workflow YAML: ${doc.warnings[0].message}`);
  const visit = (node: any): void => {
    if (!node) return;
    if (isAlias(node) || node.anchor || node.tag)
      throw new Error(
        "YAML aliases, anchors and custom tags cannot be edited safely in blocks. Use YAML source instead.",
      );
    if (isMap(node))
      node.items.forEach((pair) => {
        visit(pair.key);
        visit(pair.value);
      });
    else if (isSeq(node)) node.items.forEach(visit);
  };
  visit(doc.contents);
  if (!isMap(doc.contents))
    throw new Error("Workflow needs a YAML mapping with a steps list.");
  const steps = doc.get("steps", true);
  if (!isSeq(steps)) throw new Error("Workflow needs an ordered steps list.");
  return { doc, steps };
}

// ---- helpers -------------------------------------------------------------------

const str = (v: unknown): string | null =>
  isScalar(v) && typeof v.value === "string" ? v.value : null;
// Comma-separated fields cannot faithfully represent every valid column name.
const representable = (s: string) => !!s && s.trim() === s && !s.includes(",");
const same = (a: unknown, b: unknown) =>
  JSON.stringify(isNode(a) || isPair(a) ? (a as any).toJSON() : a) ===
  JSON.stringify(isNode(b) || isPair(b) ? (b as any).toJSON() : b);
function newBlock(ws: Blockly.Workspace, type: string) {
  const b = ws.newBlock(type);
  if (b instanceof Blockly.BlockSvg) b.initSvg();
  return b;
}
/** Read a dotted key ("header.search") from a mapping. */
function getPath(map: unknown, key: string): unknown {
  let node: unknown = map;
  for (const part of key.split(".")) {
    if (!isMap(node)) return undefined;
    node = node.get(part, true);
  }
  return node === null ? undefined : node;
}
function connectChain(input: Blockly.Input | null, blocks: Blockly.Block[]) {
  let connection = input?.connection ?? null;
  for (const block of blocks) {
    connection!.connect(block.previousConnection!);
    connection = block.nextConnection;
  }
}
function chain(first: Blockly.Block | null): Blockly.Block[] {
  const out: Blockly.Block[] = [];
  for (let b = first; b; b = b.getNextBlock()) out.push(b);
  return out;
}
function label(f: Field, _owner: string): string {
  const text = f.label.replace(/^[^a-z]*/i, "");
  return text || (f.kind === "list" ? "names" : f.kind === "expr" ? "an expression" : "a value");
}
function parseList(value: string, what: string): string[] {
  const parts = value.split(",").map((s) => s.trim());
  if (parts.some((s) => !s) || new Set(parts).size !== parts.length)
    throw new Error(`Enter valid, distinct ${what} separated by commas.`);
  return parts;
}
/** Keep the hand-written style: no folding, `{a: 1}` without padding. */
export const writeOptions = { lineWidth: 0, flowCollectionPadding: false } as const;
function flowText(node: Node): string {
  return new Document(node.toJSON())
    .toString({ ...writeOptions, collectionStyle: "flow" })
    .trim();
}

// ---- expressions ------------------------------------------------------------------

function expression(ws: Blockly.Workspace, text: string): Blockly.Block {
  const tree = parseVisualExpression(text) ?? { kind: "custom" as const, value: text };
  const build = (node: VisualExpression): Blockly.Block => {
    if (node.kind === "binary") {
      const type = ["&&", "||"].includes(node.op)
        ? "mai_logic"
        : ["+", "-", "*", "/"].includes(node.op)
          ? "mai_arithmetic"
          : "mai_compare";
      const block = newBlock(ws, type);
      block.setFieldValue(node.op, "OP");
      block.getInput("LEFT")!.connection!.connect(build(node.left).outputConnection!);
      block.getInput("RIGHT")!.connection!.connect(build(node.right).outputConnection!);
      return block;
    }
    if (node.kind === "not") {
      const block = newBlock(ws, "mai_not");
      block.getInput("VALUE")!.connection!.connect(build(node.value).outputConnection!);
      return block;
    }
    if (node.kind === "custom") {
      const block = newBlock(ws, "mai_code");
      block.setFieldValue(node.value, "CODE");
      return block;
    }
    const block = newBlock(ws, `mai_${node.kind}`);
    if (node.kind === "column") {
      block.setFieldValue(node.name, "NAME");
      block.setFieldValue(node.mode, "MODE");
    } else block.setFieldValue(node.value, "VALUE");
    return block;
  };
  const root = build(tree);
  let produced: string | undefined;
  try {
    produced = valueOf(root);
  } catch {
    /* an incomplete original */
  }
  memory.set(root, { source: text, produced });
  return root;
}
/** The expression text of a value block (and its connected blocks). */
export function valueOf(b: Blockly.Block | null): string {
  if (!b) throw new Error("Connect a value or condition to every empty socket.");
  const f = (name: string) => b.getFieldValue(name) as string;
  switch (b.type) {
    case "mai_code": {
      const code = f("CODE");
      if (!code.trim()) throw new Error("Enter an expression in every ƒx block.");
      return code;
    }
    case "mai_column": {
      const name = f("NAME");
      if (!name?.trim()) throw new Error("Enter a column name.");
      const column = `col(${JSON.stringify(name)})`;
      return f("MODE") === "number"
        ? `num(${column})`
        : f("MODE") === "text"
          ? `str(${column})`
          : column;
    }
    case "mai_number":
      return String(f("VALUE"));
    case "mai_text":
      return JSON.stringify(f("VALUE"));
    case "mai_boolean":
      return f("VALUE");
    case "mai_not":
      return `!(${valueOf(b.getInputTargetBlock("VALUE"))})`;
    case "mai_compare":
    case "mai_arithmetic":
    case "mai_logic":
      return `(${valueOf(b.getInputTargetBlock("LEFT"))} ${f("OP")} ${valueOf(b.getInputTargetBlock("RIGHT"))})`;
    default:
      throw new Error("This value block is not supported in workflows.");
  }
}
/** The text to write: the original text while the blocks are unchanged. */
export function expressionText(b: Blockly.Block | null): string {
  const produced = valueOf(b);
  const saved = memory.get(b!);
  return saved?.source !== undefined && saved.produced === produced
    ? saved.source
    : produced;
}

// ---- decode: YAML -> blocks ------------------------------------------------------

/** The values of a step's fields, keyed by field key. */
type View = Map<string, unknown>;

function viewOf(spec: StepSpec, node: YAMLMap, arg: unknown): View | null {
  const view: View = new Map();
  const fields = spec.fields;
  if (spec.form === "inline") {
    for (const f of fields) view.set(f.key, getPath(node, f.key));
    return view;
  }
  if (spec.form === "entries") {
    if (arg !== undefined && !isMap(arg)) return null;
    view.set("", arg ?? new YAMLMap());
    return view;
  }
  if (spec.scalar !== undefined && isScalar(arg) && arg.value !== null) {
    view.set(spec.scalar, arg);
    return view;
  }
  if (spec.list !== undefined && isSeq(arg)) {
    view.set(spec.list, arg);
    return view;
  }
  if (spec.op === "from" && str(arg) !== null) {
    // from: input[Sheet] is the shorthand of {input, sheet}.
    const match = str(arg)!.match(/^([^[\]]+?)(?:\[([^[\]]+)\])?$/);
    if (!match) return null;
    view.set("input", new Scalar(match[1]));
    if (match[2]) view.set("sheet", new Scalar(match[2]));
    return view;
  }
  if (arg !== undefined && !isMap(arg)) return null;
  const claimed = new Set<string>();
  for (const f of fields) {
    if (!f.key) continue;
    view.set(f.key, getPath(arg, f.key));
    claimed.add(f.key.split(".")[0]);
  }
  const rest = fields.find((f) => f.kind === "entries" && f.rest);
  if (rest) {
    const others = new YAMLMap();
    if (isMap(arg))
      others.items = arg.items.filter((p) => !claimed.has(str(p.key) ?? ""));
    view.set(rest.key, others);
  }
  return view;
}

function fill(
  ws: Blockly.Workspace,
  block: Blockly.Block,
  fields: Field[],
  view: View,
): void {
  const texts: Record<string, string> = {};
  for (const f of fields) {
    const n: any = view.get(f.key);
    const set = (value: string) => block.setFieldValue(value, f.name);
    switch (f.kind) {
      case "text":
      case "template":
        if (n === undefined) break;
        if (f.name === "SHEET" && isScalar(n) && typeof n.value === "number") {
          // from: a 1-based sheet number.
          if (!Number.isSafeInteger(n.value) || n.value < 1) throw new Unrepresentable();
          block.setFieldValue("number", "SHEET_KIND");
          set(String(n.value));
          break;
        }
        if (str(n) === null) throw new Unrepresentable();
        set(str(n)!);
        break;
      case "number":
        if (n === undefined) break;
        if (!isScalar(n) || typeof n.value !== "number") throw new Unrepresentable();
        set(String(n.value));
        break;
      case "bool":
        if (n === undefined) break;
        if (!isScalar(n) || typeof n.value !== "boolean") throw new Unrepresentable();
        set(n.value ? "TRUE" : "FALSE");
        break;
      case "enum": {
        if (f.virtual) break; // set by another field
        if (n === undefined) break;
        const value = isScalar(n) ? n.value : undefined;
        if (f.boolean ? typeof value !== "boolean" : typeof value !== "string")
          throw new Unrepresentable();
        if (!f.options.some(([, v]) => v === String(value))) throw new Unrepresentable();
        set(String(value));
        break;
      }
      case "list": {
        if (n === undefined) break;
        if (!isSeq(n)) throw new Unrepresentable();
        const names = n.items.map(str);
        if (names.some((x) => x === null || !representable(x))) throw new Unrepresentable();
        set(names.join(", "));
        break;
      }
      case "yaml": {
        if (n === undefined) break;
        const text = flowText(n);
        texts[f.name] = text;
        set(text);
        break;
      }
      case "expr": {
        if (n === undefined) break;
        if (!isScalar(n) || n.value === null || typeof n.value === "object")
          throw new Unrepresentable();
        block
          .getInput(f.name)!
          .connection!.connect(expression(ws, String(n.value)).outputConnection!);
        break;
      }
      case "entries": {
        if (n === undefined) break;
        if (!isMap(n)) throw new Unrepresentable();
        const item = itemByType.get(f.item)!;
        connectChain(
          block.getInput(f.name),
          n.items.map((pair) => decodeEntry(ws, item, pair)),
        );
        break;
      }
      case "items": {
        if (n === undefined) break;
        if (!isSeq(n)) throw new Unrepresentable();
        const item = itemByType.get(f.item)!;
        connectChain(
          block.getInput(f.name),
          n.items.map((node) => decodeObject(ws, item, node)),
        );
        break;
      }
      case "steps": {
        if (n === undefined) break;
        if (!isSeq(n)) throw new Unrepresentable();
        connectChain(
          block.getInput(f.name),
          n.items.map((node) => decodeStep(ws, node).block),
        );
        break;
      }
    }
  }
  const saved = memory.get(block) ?? {};
  memory.set(block, { ...saved, yaml: texts });
}

function decodeEntry(ws: Blockly.Workspace, item: ItemSpec, pair: any): Blockly.Block {
  if (str(pair.key) === null) throw new Unrepresentable();
  const block = newBlock(ws, item.type);
  const view: View = new Map();
  // Both halves have the key "": fill them one at a time.
  fill(ws, block, [item.entryKey!], new Map([["", pair.key]]));
  const keyTexts = memory.get(block)?.yaml ?? {};
  view.set("", pair.value ?? undefined);
  fill(ws, block, [item.entryValue!], view);
  const saved = memory.get(block)!;
  const map = new YAMLMap();
  map.items = [pair.clone()];
  memory.set(block, { ...saved, yaml: { ...keyTexts, ...saved.yaml }, node: map });
  refreshOptions(block);
  return block;
}

function decodeObject(ws: Blockly.Workspace, item: ItemSpec, node: unknown): Blockly.Block {
  if (!isMap(node)) throw new Unrepresentable();
  const block = newBlock(ws, item.type);
  const view: View = new Map();
  for (const f of item.fields!) view.set(f.key, getPath(node, f.key));
  fill(ws, block, item.fields!, view);
  memory.set(block, { ...memory.get(block), node: node.clone() as Node });
  refreshOptions(block);
  return block;
}

function stepOf(node: unknown): { op: string; arg: unknown } | null {
  if (!isMap(node)) return null;
  if (node.has("if")) return { op: "if", arg: undefined };
  if (node.items.length !== 1 || str(node.items[0].key) === null) return null;
  return { op: str(node.items[0].key)!, arg: node.items[0].value ?? undefined };
}

function decodeStep(
  ws: Blockly.Workspace,
  node: unknown,
): { block: Blockly.Block; preserved: boolean } {
  const step = stepOf(node);
  const spec = step && stepByOp.get(step.op);
  const view = spec && viewOf(spec, node as YAMLMap, step.arg);
  if (spec && view) {
    const block = newBlock(ws, spec.type);
    try {
      fill(ws, block, spec.fields, view);
      memory.set(block, { ...memory.get(block), node: (node as Node).clone() as Node });
      refreshOptions(block);
      return { block, preserved: false };
    } catch (error) {
      if (!(error instanceof Unrepresentable)) throw error;
      block.dispose(false);
    }
  }
  const block = newBlock(ws, "mai_preserved");
  const op = step?.op ?? (isScalar(node) ? String(node.value) : "step");
  block.setFieldValue(`Existing ${op.toUpperCase()} step · preserved`, "LABEL");
  block.setEditable(false);
  memory.set(block, { node: isNode(node) ? (node.clone() as Node) : undefined });
  return { block, preserved: true };
}

export function loadWorkflow(
  workspace: Blockly.Workspace,
  source: string,
): { preservedSteps: number } {
  registerWorkflowBlocks();
  const { doc, steps } = document(source);
  workspace.clear();
  const root = newBlock(workspace, "mai_workflow");
  root.setDeletable(false);
  root.setMovable(false);
  if (root instanceof Blockly.BlockSvg) root.moveBy(24, 24);
  let preservedSteps = 0;
  const blocks = steps.items.map((node) => {
    const decoded = decodeStep(workspace, node);
    if (decoded.preserved) preservedSteps++;
    return decoded.block;
  });
  connectChain(root.getInput("STEPS"), blocks);

  // Output sheets: a list of sheet maps, or the short form {Sheet: dataset}.
  const sheets: any = doc.getIn(["output", "sheets"], true);
  const sheetSpec = itemByType.get("mai_sheet")!;
  let form: "seq" | "map" | "opaque" | "none" = sheets == null ? "none" : "opaque";
  try {
    if (isSeq(sheets)) {
      connectChain(
        root.getInput("SHEETS"),
        sheets.items.map((n) => decodeObject(workspace, sheetSpec, n)),
      );
      form = "seq";
    } else if (isMap(sheets)) {
      connectChain(
        root.getInput("SHEETS"),
        sheets.items.map((pair) => {
          if (str(pair.key) === null || str(pair.value) === null)
            throw new Unrepresentable();
          const map = new YAMLMap();
          map.set("name", new Scalar(str(pair.key)));
          map.set("from", new Scalar(str(pair.value)));
          return decodeObject(workspace, sheetSpec, map);
        }),
      );
      form = "map";
    }
  } catch (error) {
    if (!(error instanceof Unrepresentable)) throw error;
    for (const b of chain(root.getInputTargetBlock("SHEETS"))) b.dispose(false);
    form = "opaque";
  }
  memory.set(root, { sheets: form });
  if (form === "opaque") {
    root.getInput("SHEETS")!.setVisible(false);
    root.getInput("LABEL_SHEETS")!.appendField("· preserved, edit in YAML");
  }
  for (const block of workspace.getAllBlocks(false).reverse()) {
    if (block instanceof Blockly.BlockSvg) block.render();
  }
  return { preservedSteps };
}

// ---- encode: blocks -> YAML ------------------------------------------------------

/** The YAML value of one field, or undefined to omit its key. */
function fieldNode(
  doc: Doc,
  block: Blockly.Block,
  f: Field,
  original: unknown,
  owner: string,
  keep = false,
): Node | undefined {
  const raw = () => block.getFieldValue(f.name) as string;
  const scalar = (value: unknown): Node => {
    if (isScalar(original) && original.value === value) return original;
    if (isScalar(original) && typeof original.value === typeof value) {
      const copy = original.clone() as Scalar;
      copy.value = value;
      return copy;
    }
    return new Scalar(value);
  };
  const missing = (what = f.label ? `a ${label(f, owner)}` : label(f, owner)) => {
    throw new Error(`Enter ${what} in ${owner}.`);
  };
  switch (f.kind) {
    case "text":
    case "template": {
      const value = raw();
      if (f.name === "SHEET" && block.getFieldValue("SHEET_KIND") === "number") {
        const n = Number(value);
        if (!/^\d+$/.test(value) || !Number.isSafeInteger(n) || n < 1)
          throw new Error("Enter a sheet number of 1 or greater.");
        return scalar(n);
      }
      if (value === "") {
        // An explicit empty text, such as a blank column header, stays.
        if (isScalar(original) && original.value === "") return original;
        if (f.optional) return undefined;
        missing();
      }
      return scalar(value);
    }
    case "number": {
      const value = raw().trim();
      if (value === "") {
        if (f.optional) return undefined;
        missing();
      }
      const n = Number(value);
      if (
        !Number.isFinite(n) ||
        (f.integer && !Number.isSafeInteger(n)) ||
        (f.min !== undefined && n < f.min)
      )
        throw new Error(
          `Enter ${f.integer ? "a whole number" : "a number"}${f.min !== undefined ? ` of ${f.min} or more` : ""} for ${label(f, owner)} in ${owner}.`,
        );
      return scalar(n);
    }
    case "bool": {
      const value = raw() === "TRUE";
      if (!value && original === undefined) return undefined;
      return scalar(value);
    }
    case "enum": {
      const value = raw();
      if (value === f.fallback && original === undefined && !keep) return undefined;
      return scalar(f.boolean ? value === "true" : value);
    }
    case "list": {
      const value = raw();
      if (!value.trim()) {
        if (f.optional) return undefined;
        missing(`${label(f, owner)} (separated by commas)`);
      }
      const names = parseList(value, label(f, owner));
      if (isSeq(original) && same(original, names)) return original;
      const seq = doc.createNode(names) as YAMLSeq;
      seq.flow = true;
      return seq;
    }
    case "yaml": {
      const value = raw().trim();
      if (!value) return undefined;
      if (original !== undefined && memory.get(block)?.yaml?.[f.name] === value)
        return original as Node;
      const parsed = parseDocument(value);
      if (parsed.errors.length)
        throw new Error(`${label(f, owner)} in ${owner} is not valid YAML: ${parsed.errors[0].message}`);
      const node = doc.createNode(parsed.toJSON());
      if (isMap(node) || isSeq(node)) node.flow = true;
      return node as Node;
    }
    case "expr": {
      const child = block.getInputTargetBlock(f.name);
      if (!child && f.optional) return undefined;
      if (
        f.condition &&
        child &&
        ["mai_number", "mai_text", "mai_arithmetic"].includes(child.type)
      )
        throw new Error("Use a true-or-false condition here.");
      const text = expressionText(child);
      if (isScalar(original) && String(original.value) === text) return original;
      return scalar(text);
    }
    case "entries": {
      const item = itemByType.get(f.item)!;
      const pairs = chain(block.getInputTargetBlock(f.name)).map((b) =>
        encodeEntry(doc, b, item),
      );
      if (!pairs.length) {
        if (f.optional || f.rest) return f.rest ? new YAMLMap() : undefined;
        throw new Error(`Add at least one entry to ${owner}.`);
      }
      const keys = pairs.map((p) => JSON.stringify((p.key as Node).toJSON()));
      if (new Set(keys).size !== keys.length)
        throw new Error(`Each name can appear only once in ${owner}.`);
      const map = new YAMLMap();
      if (isMap(original)) {
        map.flow = original.flow;
        map.commentBefore = original.commentBefore;
        map.comment = original.comment;
      } else map.flow = pairs.length <= 3 && pairs.every((p) => isScalar(p.value));
      map.items = pairs;
      return isMap(original) && same(original, map) ? original : map;
    }
    case "items": {
      const item = itemByType.get(f.item)!;
      const nodes = chain(block.getInputTargetBlock(f.name)).map((b) =>
        encodeObject(doc, b, item),
      );
      if (!nodes.length) {
        if (f.optional) return undefined;
        throw new Error(`Add at least one ${item.heading.toLowerCase() || "item"} to ${owner}.`);
      }
      return sequence(nodes, original);
    }
    case "steps": {
      const nodes = chain(block.getInputTargetBlock(f.name)).map((b) => encodeStep(doc, b));
      if (!nodes.length && f.optional && original === undefined) return undefined;
      return sequence(nodes, original);
    }
  }
}
function sequence(nodes: Node[], original: unknown): YAMLSeq {
  const seq = new YAMLSeq();
  if (isSeq(original)) {
    seq.flow = original.flow && nodes.length > 0;
    seq.commentBefore = original.commentBefore;
    seq.comment = original.comment;
  }
  seq.items = nodes;
  return isSeq(original) && same(original, seq) ? original : seq;
}

/** Write field values into a mapping, keeping unknown keys and key order. */
function patch(
  doc: Doc,
  block: Blockly.Block,
  fields: Field[],
  original: unknown,
  owner: string,
): { map: YAMLMap; values: Record<string, unknown> } {
  const map: YAMLMap = isMap(original) ? (original.clone() as YAMLMap) : new YAMLMap();
  const values: Record<string, unknown> = {};
  let nested = false;
  for (const f of fields) {
    if (!f.key && !(f.kind === "entries" && f.rest)) continue;
    const before = f.key ? getPath(map, f.key) : undefined;
    if (f.kind === "entries" && f.rest) {
      // The remaining keys of the mapping (group aggregates).
      const claimed = new Set(fields.filter((x) => x.key).map((x) => x.key.split(".")[0]));
      const others = new YAMLMap();
      others.items = map.items.filter((p) => !claimed.has(str(p.key) ?? ""));
      const node = fieldNode(doc, block, f, others, owner) as YAMLMap;
      values[f.name.toLowerCase()] = node.toJSON();
      const kept = new Set(node.items.map((p) => str(p.key)));
      map.items = map.items.filter(
        (p) => claimed.has(str(p.key) ?? "") || kept.has(str(p.key)),
      );
      for (const pair of node.items) {
        const existing = map.items.find((p) => str(p.key) === str(pair.key));
        if (existing) existing.value = pair.value;
        else map.items.push(pair);
      }
      continue;
    }
    const node = fieldNode(doc, block, f, before, owner);
    values[f.name.toLowerCase()] = node?.toJSON();
    if (node && (isMap(node) || isSeq(node)) && !node.flow) nested = true;
    const [head, tail] = f.key.split(".", 2);
    if (tail === undefined) {
      if (node === undefined) map.delete(head);
      else if (node !== before) setKeepingComments(map, head, node, before);
    } else {
      let parent: any = map.get(head, true);
      if (node === undefined) {
        if (isMap(parent)) {
          parent.delete(tail);
          if (!parent.items.length) map.delete(head);
        }
        continue;
      }
      if (!isMap(parent)) {
        parent = new YAMLMap();
        map.set(head, parent);
      }
      if (node !== before) setKeepingComments(parent, tail, node, before);
    }
  }
  if (!isMap(original)) map.flow = !nested && map.items.length <= 4;
  return { map, values };
}
function setKeepingComments(map: YAMLMap, key: string, node: Node, before: unknown) {
  if (isNode(before)) {
    node.commentBefore ??= before.commentBefore;
    node.comment ??= before.comment;
    node.spaceBefore ??= before.spaceBefore;
  }
  const existing = map.items.find((p) => str(p.key) === key);
  if (existing) existing.value = node;
  else map.items.push(new Pair(new Scalar(key), node));
}

function encodeEntry(doc: Doc, block: Blockly.Block, item: ItemSpec): Pair {
  const original: any = memory.get(block)?.node;
  const pair = isMap(original) ? original.items[0] : undefined;
  const owner = "the entry";
  const key = fieldNode(doc, block, item.entryKey!, pair?.key, owner, true)!;
  const value = fieldNode(doc, block, item.entryValue!, pair?.value ?? undefined, owner)!;
  const result = new Pair(key, value);
  if (pair && same(pair, result)) return pair.clone();
  if (pair) {
    // Keep the comments written above and beside the entry.
    (key as Node).commentBefore ??= (pair.key as Node | undefined)?.commentBefore;
    (key as Node).spaceBefore ??= (pair.key as Node | undefined)?.spaceBefore;
  }
  return result;
}

function encodeObject(doc: Doc, block: Blockly.Block, item: ItemSpec): Node {
  const original = memory.get(block)?.node;
  const { map } = patch(doc, block, item.fields!, original, item.heading || "the item");
  if (!isMap(original) && item.fields!.some((f) => f.kind === "items")) map.flow = false;
  return isMap(original) && same(original, map) ? (original.clone() as Node) : map;
}

function encodeStep(doc: Doc, block: Blockly.Block): Node {
  const saved = memory.get(block);
  if (block.type === "mai_preserved") {
    if (!saved?.node) throw new Error("The preserved step is unavailable. Reload the workflow.");
    return saved.node.clone() as Node;
  }
  const spec = stepByType.get(block.type);
  if (!spec) throw new Error("This step block is not supported in workflows.");
  const original: any = saved?.node;
  const owner = `“${spec.title}”`;
  let result: Node;
  if (spec.form === "inline") {
    const { map, values } = patch(doc, block, spec.fields, original, owner);
    spec.validate?.(values);
    map.flow = false;
    result = map;
  } else {
    const step = stepOf(original);
    const arg = step?.op === spec.op ? step.arg : undefined;
    let value: Node;
    if (spec.form === "entries") {
      value = fieldNode(doc, block, spec.fields[0], arg, owner)!;
    } else {
      const shorthand = spec.scalar ?? spec.list;
      const view = isMap(arg) ? arg : undefined;
      const { map, values } = patch(doc, block, spec.fields, view, owner);
      spec.validate?.(values);
      const present = spec.fields.filter(
        (f) => (f.key || (f as { rest?: boolean }).rest) && values[f.name.toLowerCase()] !== undefined,
      );
      if (
        shorthand !== undefined &&
        !isMap(arg) &&
        present.every((f) => f.key === shorthand)
      ) {
        const f = spec.fields.find((x) => x.key === shorthand)!;
        value = fieldNode(doc, block, f, arg, owner)!;
      } else if (
        spec.op === "from" &&
        (arg === undefined || str(arg) !== null) &&
        present.every((f) => f.key === "input" || f.key === "sheet") &&
        typeof values.sheet !== "number"
      ) {
        const text = `${values.source}${values.sheet ? `[${values.sheet}]` : ""}`;
        value =
          isScalar(arg) && arg.value === text
            ? (arg as Node)
            : isScalar(arg)
              ? Object.assign(arg.clone() as Scalar, { value: text })
              : new Scalar(text);
      } else value = map;
    }
    if (isMap(original) && step?.op === spec.op) {
      const copy = original.clone() as YAMLMap;
      if (value !== arg && isNode(arg) && isNode(value)) value.comment ??= arg.comment;
      copy.items[0].value = value;
      result = copy;
    } else {
      const map = new YAMLMap();
      map.items = [new Pair(new Scalar(spec.op), value)];
      result = map;
    }
  }
  return isMap(original) && same(original, result) ? (original.clone() as Node) : result;
}

function encodeSheets(doc: Doc, root: Blockly.Block): void {
  const form = memory.get(root)?.sheets ?? "none";
  if (form === "opaque") return;
  const sheetSpec = itemByType.get("mai_sheet")!;
  const original: any = doc.getIn(["output", "sheets"], true);
  const blocks = chain(root.getInputTargetBlock("SHEETS"));
  const nodes = blocks.map((b) => encodeObject(doc, b, sheetSpec));
  const names = nodes.map((n) => (n as any).toJSON().name);
  if (new Set(names).size !== names.length)
    throw new Error("Each output sheet needs a distinct name.");
  let next: Node;
  const short = nodes.every((n) => {
    const keys = Object.keys((n as any).toJSON());
    return keys.length === 2 && keys.includes("name") && keys.includes("from");
  });
  if ((form === "map" || (form === "none" && nodes.length)) && short) {
    const map = new YAMLMap();
    map.flow = isMap(original) ? original.flow : true;
    map.items = nodes.map((n) => {
      const js = (n as any).toJSON();
      const old = isMap(original)
        ? original.items.find((p: Pair) => str(p.key) === js.name)
        : undefined;
      return old && str(old.value) === js.from
        ? old
        : new Pair(new Scalar(js.name), new Scalar(js.from));
    });
    next = map;
  } else if (!nodes.length && form === "none") return;
  else next = sequence(nodes, isSeq(original) ? original : undefined);
  if (original !== undefined && same(original, next)) return;
  if (!isMap(doc.get("output", true))) doc.set("output", doc.createNode({}));
  (doc.get("output", true) as YAMLMap).set("sheets", next);
}

export function serializeWorkflow(workspace: Blockly.Workspace, baseSource: string): string {
  const { doc, steps } = document(baseSource);
  const all = workspace.getAllBlocks(false),
    roots = all.filter((b) => b.type === "mai_workflow");
  if (roots.length !== 1 || roots[0].getParent())
    throw new Error("The workspace must contain exactly one workflow block.");
  const root = roots[0];
  if (workspace.getTopBlocks(false).some((b) => b !== root))
    throw new Error("Connect or remove every detached block before saving.");
  if (all.some((b) => !b.isEnabled()))
    throw new Error("Enable or remove disabled blocks before saving.");
  steps.items = chain(root.getInputTargetBlock("STEPS")).map((b) => encodeStep(doc, b));
  encodeSheets(doc, root);
  return preserveText(baseSource, doc.toString(writeOptions));
}

/** The registry fields of a block, for the selected-block inspector. */
export { fieldsOf };

/**
 * Replace the expression in a value input with blocks parsed from text (or a
 * ƒx code block when the text is beyond the visual grammar).
 */
export function setExpression(block: Blockly.Block, input: string, text: string): void {
  const ws = block.workspace;
  Blockly.Events.setGroup(true);
  try {
    const old = block.getInputTargetBlock(input);
    if (old && !old.isShadow()) old.dispose(false);
    if (text.trim()) {
      const next = expression(ws, text);
      // A new expression is the user's text: write it as typed.
      memory.set(next, { source: text, produced: valueOf(next) });
      block.getInput(input)!.connection!.connect(next.outputConnection!);
      if (next instanceof Blockly.BlockSvg) {
        for (const b of next.getDescendants(false)) (b as Blockly.BlockSvg).render();
      }
    }
  } finally {
    Blockly.Events.setGroup(false);
  }
}

/** The current expression text of a value input, or "" when empty. */
export function expressionOf(block: Blockly.Block, input: string): string {
  const child = block.getInputTargetBlock(input);
  if (!child) return "";
  try {
    return expressionText(child);
  } catch {
    return "";
  }
}
