import {
  parseVisualExpression,
  type VisualExpression,
} from "./blocklyExpressions";
import * as Blockly from "blockly/core";
import {
  isAlias,
  isMap,
  isNode,
  isScalar,
  isSeq,
  parseDocument,
  type Node,
} from "yaml";

import { blockMemory as memory, registerWorkflowBlocks } from "./blocklyBlocks";
export { registerWorkflowBlocks, workflowToolbox } from "./blocklyBlocks";

function document(source: string) {
  const doc = parseDocument(source, {
    keepSourceTokens: true,
    uniqueKeys: true,
  });
  if (doc.errors.length)
    throw new Error(`Cannot open workflow YAML: ${doc.errors[0].message}`);
  if (doc.warnings.length)
    throw new Error(
      `Cannot safely edit workflow YAML: ${doc.warnings[0].message}`,
    );
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
const scalar = (v: any): string | null =>
  isScalar(v) && typeof v.value === "string" ? v.value : null;
// Comma-separated fields cannot faithfully represent every valid column name.
const representable = (s: string) => !!s && s.trim() === s && !s.includes(",");
const list = (v: any): string[] | null =>
  isSeq(v) &&
  v.items.every((x) => scalar(x) !== null && representable(scalar(x)!))
    ? v.items.map((x) => scalar(x)!)
    : null;
const get = (m: any, key: string) => (isMap(m) ? m.get(key, true) : undefined);
function listField(value: string[], b: Blockly.Block, name: string) {
  b.setFieldValue(value.join(", "), name);
}
function newBlock(ws: Blockly.Workspace, type: string) {
  const b = ws.newBlock(type);
  if (b instanceof Blockly.BlockSvg) b.initSvg();
  return b;
}
function expression(ws: Blockly.Workspace, text: string): Blockly.Block {
  const tree = parseVisualExpression(text) ?? {
    kind: "custom" as const,
    value: text,
  };
  const build = (node: VisualExpression): Blockly.Block => {
    if (node.kind === "binary") {
      const type = ["&&", "||"].includes(node.op)
        ? "mai_logic"
        : ["+", "-", "*", "/"].includes(node.op)
          ? "mai_arithmetic"
          : "mai_compare";
      const block = newBlock(ws, type);
      block.setFieldValue(node.op, "OP");
      block
        .getInput("LEFT")!
        .connection!.connect(build(node.left).outputConnection!);
      block
        .getInput("RIGHT")!
        .connection!.connect(build(node.right).outputConnection!);
      return block;
    }
    if (node.kind === "not") {
      const block = newBlock(ws, "mai_not");
      block
        .getInput("VALUE")!
        .connection!.connect(build(node.value).outputConnection!);
      return block;
    }
    const type =
      node.kind === "custom" ? "mai_custom_expression" : `mai_${node.kind}`;
    const block = newBlock(ws, type);
    if (node.kind === "custom") {
      memory.set(block, { custom: node.value });
      block.setEditable(false);
    } else if (node.kind === "column") {
      block.setFieldValue(node.name, "NAME");
      block.setFieldValue(node.mode, "MODE");
    } else block.setFieldValue(node.value, "VALUE");
    return block;
  };
  return build(tree);
}
function valueOf(b: Blockly.Block | null): string {
  if (!b)
    throw new Error("Connect a value or condition to every empty socket.");
  const f = (name: string) => b.getFieldValue(name) as string;
  switch (b.type) {
    case "mai_custom_expression": {
      const v = memory.get(b)?.custom;
      if (v === undefined)
        throw new Error(
          "The preserved expression is unavailable. Reload the workflow.",
        );
      return v;
    }
    case "mai_column": {
      const name = f("NAME");
      required(name, "column name");
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
function required(value: string, label: string): string {
  if (!value?.trim()) throw new Error(`Enter a ${label}.`);
  return value;
}
function columns(value: string, label: string, optional = false): string[] {
  if (!value.trim() && optional) return [];
  const parts = value.split(",").map((s) => s.trim());
  if (parts.some((s) => !s) || new Set(parts).size !== parts.length)
    throw new Error(`Enter valid, distinct ${label} separated by commas.`);
  return parts;
}
function values(b: Blockly.Block): any {
  const f = (name: string) => b.getFieldValue(name) as string;
  switch (b.type) {
    case "mai_from": {
      const numbered = f("SHEET_KIND") === "number";
      const sheet = numbered ? Number(f("SHEET")) : f("SHEET");
      if (
        numbered &&
        (!/^\d+$/.test(f("SHEET")) ||
          !Number.isSafeInteger(sheet) ||
          Number(sheet) < 1)
      )
        throw new Error("Enter a sheet number of 1 or greater.");
      return {
        source: required(f("SOURCE"), "source name"),
        sheet,
      };
    }
    case "mai_filter": {
      const condition = b.getInputTargetBlock("VALUE");
      if (
        condition &&
        ["mai_number", "mai_text", "mai_arithmetic"].includes(condition.type)
      )
        throw new Error("Use a true-or-false condition for filtering rows.");
      return { value: valueOf(condition) };
    }
    case "mai_set":
      return {
        column: required(f("COLUMN"), "column name"),
        value: valueOf(b.getInputTargetBlock("VALUE")),
      };
    case "mai_rename":
      return {
        from: required(f("FROM"), "column name"),
        to: required(f("TO"), "new column name"),
      };
    case "mai_select":
      return { columns: columns(f("COLUMNS"), "columns") };
    case "mai_sort":
      return {
        by: columns(f("COLUMNS"), "sort columns"),
        desc: f("DIRECTION") === "true",
        numeric: f("NUMERIC") === "true",
      };
    case "mai_group":
      return {
        by: columns(f("BY"), "group columns", true),
        sum: columns(f("SUM"), "sum columns", true),
      };
    case "mai_save":
      return { name: required(f("NAME"), "dataset name") };
    case "mai_join":
      return {
        with: required(f("WITH"), "dataset name"),
        on: columns(f("ON"), "join columns"),
        how: f("HOW"),
      };
    case "mai_dedupe":
      return { by: columns(f("BY"), "duplicate columns"), keep: f("KEEP") };
    case "mai_preserved":
      return {};
    default:
      throw new Error("This step block is not supported in workflows.");
  }
}
function decode(
  ws: Blockly.Workspace,
  node: any,
): { block: Blockly.Block; preserved: boolean } {
  let type = "mai_preserved",
    data: Record<string, string> = {},
    expr: string | null = null,
    op = "step";
  if (isMap(node) && node.items.length === 1 && scalar(node.items[0].key)) {
    op = scalar(node.items[0].key)!;
    const arg: any = node.items[0].value,
      str = scalar(arg),
      by = list(get(arg, "by"));
    const sheetNode = get(arg, "sheet");
    const indexedSheet =
      isScalar(sheetNode) &&
      typeof sheetNode.value === "number" &&
      Number.isSafeInteger(sheetNode.value) &&
      sheetNode.value > 0;
    const validBool = (v: any) =>
      v === undefined || (isScalar(v) && typeof v.value === "boolean");
    if (op === "from") {
      const match = str?.match(/^([^\[\]]+?)(?:\[([^\[\]]+)\])?$/);
      if (match) {
        type = "mai_from";
        data = { SOURCE: match[1], SHEET: match[2] || "", SHEET_KIND: "name" };
      } else if (
        isMap(arg) &&
        scalar(get(arg, "input")) !== null &&
        (sheetNode === undefined || scalar(sheetNode) !== null || indexedSheet)
      ) {
        type = "mai_from";
        data = {
          SOURCE: scalar(get(arg, "input"))!,
          SHEET: indexedSheet
            ? String(sheetNode.value)
            : scalar(sheetNode) || "",
          SHEET_KIND: indexedSheet ? "number" : "name",
        };
      }
    } else if (op === "save" && str !== null) {
      type = "mai_save";
      data = { NAME: str };
    } else if (op === "filter" && str !== null) {
      type = "mai_filter";
      expr = str;
    } else if (
      op === "set" &&
      isMap(arg) &&
      arg.items.length === 1 &&
      scalar(arg.items[0].key) !== null &&
      scalar(arg.items[0].value) !== null
    ) {
      type = "mai_set";
      data = { COLUMN: scalar(arg.items[0].key)! };
      expr = scalar(arg.items[0].value);
    } else if (
      op === "rename" &&
      isMap(arg) &&
      arg.items.length === 1 &&
      scalar(arg.items[0].key) !== null &&
      scalar(arg.items[0].value) !== null
    ) {
      type = "mai_rename";
      data = {
        FROM: scalar(arg.items[0].key)!,
        TO: scalar(arg.items[0].value)!,
      };
    } else if (
      op === "select" &&
      isMap(arg) &&
      arg.items.length &&
      arg.items.every(
        (p) =>
          scalar(p.key) !== null &&
          representable(scalar(p.key)!) &&
          scalar(p.key) === scalar(p.value),
      )
    ) {
      type = "mai_select";
      data = { COLUMNS: arg.items.map((p) => scalar(p.key)).join(", ") };
    } else if (
      op === "sort" &&
      by &&
      validBool(get(arg, "desc")) &&
      validBool(get(arg, "numeric"))
    ) {
      type = "mai_sort";
      data = {
        COLUMNS: by.join(", "),
        DIRECTION: String((get(arg, "desc") as any)?.value ?? false),
        NUMERIC: String((get(arg, "numeric") as any)?.value ?? false),
      };
    } else if (
      op === "group" &&
      isMap(arg) &&
      (get(arg, "by") === undefined || by) &&
      (get(arg, "sum") === undefined || list(get(arg, "sum")))
    ) {
      type = "mai_group";
      data = {
        BY: (by || []).join(", "),
        SUM: (list(get(arg, "sum")) || []).join(", "),
      };
    } else if (
      op === "join" &&
      isMap(arg) &&
      scalar(get(arg, "with")) !== null &&
      list(get(arg, "on")) &&
      (get(arg, "how") === undefined ||
        ["left", "inner"].includes(scalar(get(arg, "how")) || ""))
    ) {
      type = "mai_join";
      data = {
        WITH: scalar(get(arg, "with"))!,
        ON: list(get(arg, "on"))!.join(", "),
        HOW: scalar(get(arg, "how")) || "left",
      };
    } else if (
      op === "dedupe" &&
      isMap(arg) &&
      by &&
      (get(arg, "keep") === undefined ||
        ["first", "last"].includes(scalar(get(arg, "keep")) || ""))
    ) {
      type = "mai_dedupe";
      data = { BY: by.join(", "), KEEP: scalar(get(arg, "keep")) || "first" };
    }
  }
  const block = newBlock(ws, type);
  if (type === "mai_preserved") {
    block.setFieldValue(
      `Existing ${op.toUpperCase()} step · preserved`,
      "LABEL",
    );
    block.setEditable(false);
  } else
    Object.entries(data).forEach(([key, value]) =>
      block.setFieldValue(value, key),
    );
  if (expr !== null)
    block
      .getInput("VALUE")!
      .connection!.connect(expression(ws, expr).outputConnection!);
  let signature: string | undefined;
  try {
    signature = JSON.stringify(values(block));
  } catch {
    /* invalid original */
  }
  memory.set(block, {
    node: isNode(node) ? (node.clone() as Node) : undefined,
    signature,
  });
  return { block, preserved: type === "mai_preserved" };
}
export function loadWorkflow(
  workspace: Blockly.Workspace,
  source: string,
): { preservedSteps: number } {
  registerWorkflowBlocks();
  const { steps } = document(source);
  workspace.clear();
  const root = newBlock(workspace, "mai_workflow");
  root.setDeletable(false);
  root.setMovable(false);
  if (root instanceof Blockly.BlockSvg) root.moveBy(24, 24);
  let connection = root.getInput("STEPS")!.connection!;
  let preservedSteps = 0;
  for (const node of steps.items) {
    const decoded = decode(workspace, node);
    if (decoded.preserved) preservedSteps++;
    connection.connect(decoded.block.previousConnection!);
    connection = decoded.block.nextConnection!;
  }
  for (const block of workspace.getAllBlocks(false).reverse()) {
    if (block instanceof Blockly.BlockSvg) block.render();
  }
  return { preservedSteps };
}
function writeMapValue(
  doc: ReturnType<typeof parseDocument>,
  map: any,
  key: string,
  value: unknown,
) {
  const existing = map.get(key, true);
  if (
    isScalar(existing) &&
    (typeof value === "string" || typeof value === "boolean")
  )
    existing.value = value;
  else {
    const replacement = doc.createNode(value);
    if (isNode(existing)) {
      replacement.commentBefore = existing.commentBefore;
      replacement.comment = existing.comment;
      replacement.spaceBefore = existing.spaceBefore;
    }
    map.set(key, replacement);
  }
}
function nodeFor(
  doc: ReturnType<typeof parseDocument>,
  block: Blockly.Block,
): Node {
  const v = values(block),
    saved = memory.get(block);
  if (
    saved?.node &&
    (block.type === "mai_preserved" || saved.signature === JSON.stringify(v))
  )
    return saved.node.clone() as Node;
  if (block.type === "mai_preserved")
    throw new Error("The preserved step is unavailable. Reload the workflow.");
  if (block.type === "mai_group") {
    const group = isMap(saved?.node) ? get(saved.node, "group") : undefined;
    const other = [
      "count",
      "mean",
      "min",
      "max",
      "first",
      "last",
      "join",
    ].flatMap((key) => {
      const names = get(group, key);
      return isSeq(names)
        ? names.items
            .map(scalar)
            .filter((name): name is string => name !== null)
        : [];
    });
    const aggregates = [...v.sum, ...other];
    if (!aggregates.length)
      throw new Error("Enter at least one sum column for the group.");
    if (
      new Set(aggregates).size !== aggregates.length ||
      v.by.some((name: string) => aggregates.includes(name))
    )
      throw new Error("Group keys and aggregated columns must be distinct.");
  }
  const op = block.type.slice(4);
  const original: any = saved?.node?.clone();
  const node: any = isMap(original) ? original : doc.createNode({ [op]: null });
  let arg: any = node.get(op, true);
  const mapArg = () => {
    if (!isMap(arg)) {
      arg = doc.createNode({});
      node.set(op, arg);
    }
    return arg;
  };
  const oldValues = saved?.signature ? JSON.parse(saved.signature) : undefined;
  const put = (key: string, value: unknown, field = key) => {
    if (
      !oldValues ||
      JSON.stringify(oldValues[field]) !== JSON.stringify(value)
    )
      writeMapValue(doc, mapArg(), key, value);
  };
  switch (op) {
    case "from":
      if (isMap(arg)) {
        put("input", v.source, "source");
        if (v.sheet || arg.has("sheet")) put("sheet", v.sheet);
      } else if (typeof v.sheet === "number") {
        writeMapValue(doc, node, op, { input: v.source, sheet: v.sheet });
      } else
        writeMapValue(
          doc,
          node,
          op,
          v.source + (v.sheet ? `[${v.sheet}]` : ""),
        );
      break;
    case "save":
      writeMapValue(doc, node, op, v.name);
      break;
    case "filter":
      writeMapValue(doc, node, op, v.value);
      break;
    case "set":
    case "rename": {
      const m = mapArg();
      const oldKey = m.items[0]?.key;
      if (isScalar(oldKey)) oldKey.value = op === "set" ? v.column : v.from;
      else m.set(op === "set" ? v.column : v.from, "");
      writeMapValue(
        doc,
        m,
        op === "set" ? v.column : v.from,
        op === "set" ? v.value : v.to,
      );
      break;
    }
    case "select": {
      const m = mapArg();
      m.items = [];
      v.columns.forEach((col: string) => m.set(col, col));
      break;
    }
    case "sort":
      put("by", v.by);
      put("desc", v.desc);
      put("numeric", v.numeric);
      break;
    case "group":
      put("by", v.by);
      put("sum", v.sum);
      break;
    case "join":
      put("with", v.with);
      put("on", v.on);
      put("how", v.how);
      break;
    case "dedupe":
      put("by", v.by);
      put("keep", v.keep);
      break;
  }
  return node;
}
export function serializeWorkflow(
  workspace: Blockly.Workspace,
  baseSource: string,
): string {
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
  const nodes: Node[] = [];
  let block = root.getInputTargetBlock("STEPS");
  while (block) {
    nodes.push(nodeFor(doc, block));
    block = block.getNextBlock();
  }
  steps.items = nodes;
  return doc.toString();
}
