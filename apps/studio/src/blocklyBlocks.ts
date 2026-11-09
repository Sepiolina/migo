import * as Blockly from "blockly/core";
import { Document, isNode, parseDocument, type Node } from "yaml";
import {
  categoryColours,
  colourOf,
  fieldsOf,
  itemByType,
  items,
  steps,
  stepByType,
  type Category,
  type Field,
} from "./stepRegistry";

/**
 * What a block remembers besides its fields: the original YAML (so unchanged
 * parts are written back byte for byte), the expression text it was opened
 * from, flow-YAML field text, and whether its optional settings are shown.
 */
export type Memory = {
  node?: Node;
  /** Expression roots: the original text and what the blocks produced from it. */
  source?: string;
  produced?: string;
  /** yaml fields: the text shown when the block was opened. */
  yaml?: Record<string, string>;
  /** Show every optional setting, not only the ones in use. */
  expanded?: boolean;
  /** The root block: the output sheets form, or "opaque" when not editable. */
  sheets?: "seq" | "map" | "opaque" | "none";
  /** The root block: a service section the service block cannot show. */
  serviceOpaque?: boolean;
};
export const blockMemory = new WeakMap<Blockly.Block, Memory>();

export const expressionTypes = [
  "mai_column",
  "mai_number",
  "mai_text",
  "mai_boolean",
  "mai_compare",
  "mai_arithmetic",
  "mai_logic",
  "mai_not",
  "mai_code",
];
const field = (name: string, text = "") => ({ type: "field_input", name, text });
const dropdown = (name: string, options: string[][]) => ({
  type: "field_dropdown",
  name,
  options,
});

/** The Blockly field for a registry field (inputs are added by `rows`). */
function fieldJson(f: Field): object | null {
  switch (f.kind) {
    case "text":
    case "template":
    case "list":
    case "number":
    case "yaml":
      return field(f.name);
    case "bool":
      return { type: "field_checkbox", name: f.name, checked: false };
    case "enum":
      return dropdown(f.name, f.options);
    default:
      return null;
  }
}
const statementCheck = (f: Field): string => {
  if (f.kind === "steps") return "MaiStep";
  if (f.kind === "entries" || f.kind === "items")
    return itemByType.get(f.item)!.check;
  return "";
};

/**
 * Lay fields out as rows. The first field shares the heading row; fields with
 * an empty label join the row before them. Every simple row ends in a named
 * dummy input (ROW_<last field>) so optional rows can be hidden.
 */
function rows(heading: string, fields: Field[]) {
  type Row = { message: string[]; args: object[]; last?: string };
  const out: Row[] = [];
  let cur: Row = { message: heading ? [heading.replace(/%/g, "%%")] : [], args: [] };
  let open = true;
  const close = () => {
    if (!cur.message.length && !cur.args.length) return;
    cur.args.push({ type: "input_dummy", name: cur.last ? `ROW_${cur.last}` : "HEAD" });
    cur.message.push(`%${cur.args.length}`);
    out.push(cur);
    cur = { message: [], args: [] };
  };
  const push = (row: Row, label: string, arg: object) => {
    if (label) row.message.push(label.replace(/%/g, "%%"));
    row.args.push(arg);
    row.message.push(`%${row.args.length}`);
  };
  fields.forEach((f, i) => {
    const joins = i === 0 || (open && !f.label);
    const statement = f.kind === "entries" || f.kind === "items" || f.kind === "steps";
    if (f.kind === "expr" || statement) {
      if (!joins) close();
      push(
        cur,
        f.label,
        statement
          ? { type: "input_statement", name: f.name, check: statementCheck(f) }
          : { type: "input_value", name: f.name, check: "MaiExpression" },
      );
      out.push(cur);
      cur = { message: [], args: [] };
      open = false;
      return;
    }
    if (!joins) close();
    push(cur, f.label, fieldJson(f)!);
    cur.last = f.name;
    open = true;
  });
  close();
  const json: Record<string, unknown> = {};
  out.forEach((row, index) => {
    json[`message${index}`] = row.message.join(" ");
    json[`args${index}`] = row.args;
  });
  return json;
}

function stepDefinitions(): object[] {
  return steps.map((spec) => ({
    type: spec.type,
    ...rows(spec.heading, spec.fields),
    previousStatement: "MaiStep",
    nextStatement: "MaiStep",
    colour: categoryColours[spec.category],
    tooltip: spec.help,
    inputsInline: false,
  }));
}
function itemDefinitions(): object[] {
  return items.map((spec) => ({
    type: spec.type,
    ...rows(spec.heading, fieldsOf(spec.type)),
    previousStatement: spec.check,
    nextStatement: spec.check,
    colour: spec.colour,
    tooltip: spec.help,
    inputsInline: false,
  }));
}

const values = categoryColours.Values;
const logic = categoryColours.Logic;
const fixedDefinitions: object[] = [
  {
    type: "mai_workflow",
    message0: "Workflow %1",
    args0: [{ type: "input_dummy", name: "HEAD" }],
    message1: "steps %1",
    args1: [{ type: "input_statement", name: "STEPS", check: "MaiStep" }],
    message2: "output sheets %1",
    args2: [{ type: "input_dummy", name: "LABEL_SHEETS" }],
    message3: "%1",
    args3: [{ type: "input_statement", name: "SHEETS", check: "MaiSheet" }],
    message4: "service %1",
    args4: [{ type: "input_statement", name: "SERVICE", check: "MaiService" }],
    colour: "#334155",
    tooltip:
      "The workflow runs connected steps from top to bottom. Start with Read, transform the rows, save a dataset, then write it as an output sheet.",
  },
  {
    type: "mai_preserved",
    message0: "%1",
    args0: [
      {
        type: "field_label_serializable",
        name: "LABEL",
        text: "Existing step · preserved",
      },
    ],
    previousStatement: "MaiStep",
    nextStatement: "MaiStep",
    colour: "#8a94a6",
    tooltip:
      "This step is kept intact. It can be moved or deleted; use Advanced YAML to change its definition.",
  },
  {
    type: "mai_column",
    message0: "column %1 as %2",
    args0: [
      field("NAME"),
      dropdown("MODE", [
        ["original value", "raw"],
        ["number", "number"],
        ["text", "text"],
      ]),
    ],
    colour: values,
    tooltip:
      "Enter the exact Excel column title. Choose number for arithmetic and numeric comparisons, or text for labels.",
  },
  {
    type: "mai_number",
    message0: "%1",
    args0: [{ type: "field_number", name: "VALUE", value: 0 }],
    colour: values,
    tooltip: "A numeric value for a calculation or comparison.",
  },
  {
    type: "mai_text",
    message0: "“ %1 ”",
    args0: [field("VALUE")],
    colour: values,
    tooltip: "A text value. Enter text without quotation marks.",
  },
  {
    type: "mai_boolean",
    message0: "%1",
    args0: [dropdown("VALUE", [["true", "true"], ["false", "false"]])],
    colour: logic,
    tooltip: "A true or false value.",
  },
  {
    type: "mai_compare",
    message0: "%1 %2 %3",
    args0: [
      { type: "input_value", name: "LEFT", check: "MaiExpression" },
      dropdown("OP", [
        ["=", "=="],
        ["≠", "!="],
        [">", ">"],
        ["≥", ">="],
        ["<", "<"],
        ["≤", "<="],
      ]),
      { type: "input_value", name: "RIGHT", check: "MaiExpression" },
    ],
    inputsInline: true,
    colour: logic,
    tooltip: "Compare two values. Convert a column to number before comparing quantities.",
  },
  {
    type: "mai_arithmetic",
    message0: "%1 %2 %3",
    args0: [
      { type: "input_value", name: "LEFT", check: "MaiExpression" },
      dropdown("OP", [
        ["+", "+"],
        ["−", "-"],
        ["×", "*"],
        ["÷", "/"],
      ]),
      { type: "input_value", name: "RIGHT", check: "MaiExpression" },
    ],
    inputsInline: true,
    colour: values,
    tooltip: "Connect numeric values. For an Excel column, choose its number conversion first.",
  },
  {
    type: "mai_logic",
    message0: "%1 %2 %3",
    args0: [
      { type: "input_value", name: "LEFT", check: "MaiExpression" },
      dropdown("OP", [
        ["and", "&&"],
        ["or", "||"],
      ]),
      { type: "input_value", name: "RIGHT", check: "MaiExpression" },
    ],
    inputsInline: true,
    colour: logic,
    tooltip: "Combine two conditions. AND requires both to match; OR requires at least one.",
  },
  {
    type: "mai_not",
    message0: "not %1",
    args0: [{ type: "input_value", name: "VALUE", check: "MaiExpression" }],
    inputsInline: true,
    colour: logic,
    tooltip: "Reverse a condition: true becomes false and false becomes true.",
  },
  {
    type: "mai_code",
    message0: "ƒx %1",
    args0: [{ type: "field_input", name: "CODE", text: "" }],
    colour: "#475569",
    tooltip:
      "Any expression, written as text: functions such as norm(), prev(), first(filter(group, …)), ?: and let. Select the block to edit it in a larger box.",
  },
].map((def: any) =>
  expressionTypes.includes(def.type) ? { ...def, output: "MaiExpression" } : def,
);

/** Blocks whose optional settings can be shown or hidden. */
export function advancedFields(type: string): Field[] {
  return fieldsOf(type).filter((f) => f.advanced);
}
function inUse(block: Blockly.Block, f: Field): boolean {
  if (f.kind === "expr" || f.kind === "entries" || f.kind === "items" || f.kind === "steps")
    return !!block.getInput(f.name)?.connection?.targetBlock();
  const value = block.getFieldValue(f.name);
  if (f.kind === "bool") return value === "TRUE";
  if (f.kind === "enum") return value !== f.fallback;
  return value !== "" && value !== null && value !== undefined;
}
/** The input that holds a field: its own row, or the row it joins. */
function inputOf(block: Blockly.Block, f: Field): Blockly.Input | null {
  if (f.kind === "expr" || f.kind === "entries" || f.kind === "items" || f.kind === "steps")
    return block.getInput(f.name);
  return block.getField(f.name)?.getParentInput() ?? null;
}
/** Hide optional rows that are not in use unless the block is expanded. */
export function refreshOptions(block: Blockly.Block): void {
  const advanced = advancedFields(block.type);
  if (!advanced.length) return;
  const expanded = !!blockMemory.get(block)?.expanded;
  for (const f of advanced) {
    const input = inputOf(block, f);
    if (!input) continue;
    const show = expanded || inUse(block, f);
    if (input.isVisible() !== show) input.setVisible(show);
  }
  if (block instanceof Blockly.BlockSvg && block.rendered) block.queueRender();
}
export function toggleOptions(block: Blockly.Block, expanded?: boolean): void {
  const memory = blockMemory.get(block) ?? {};
  memory.expanded = expanded ?? !memory.expanded;
  blockMemory.set(block, memory);
  refreshOptions(block);
}

const optionsIcon =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><circle cx="3" cy="8" r="1.6" fill="white"/><circle cx="8" cy="8" r="1.6" fill="white"/><circle cx="13" cy="8" r="1.6" fill="white"/></svg>',
  );

let registered = false;
export function registerWorkflowBlocks(): void {
  if (registered) return;
  Blockly.defineBlocksWithJsonArray([
    ...fixedDefinitions,
    ...stepDefinitions(),
    ...itemDefinitions(),
  ]);
  const types = [
    ...fixedDefinitions.map((d: any) => d.type as string),
    ...steps.map((s) => s.type),
    ...items.map((s) => s.type),
  ];
  // Blockly duplicate/copy reconstructs blocks from serialized state. Carry the
  // original YAML and expression text with the block, including hidden options.
  for (const type of types) {
    const definition = Blockly.Blocks[type];
    const init = definition.init;
    definition.init = function (this: Blockly.Block) {
      init.call(this);
      const optional = advancedFields(type);
      if (optional.length) {
        this.inputList[0]?.appendField(
          new Blockly.FieldImage(optionsIcon, 16, 16, "Show or hide options", (f) => {
            const block = f.getSourceBlock();
            if (block) toggleOptions(block);
          }),
          "OPTIONS",
        );
        (this as Blockly.BlockSvg).customContextMenu = (
          menu: Array<Blockly.ContextMenuRegistry.ContextMenuOption | Blockly.ContextMenuRegistry.LegacyContextMenuOption>,
        ) => {
          const expanded = !!blockMemory.get(this)?.expanded;
          menu.push({
            text: expanded ? "Hide unused options" : "Show all options",
            enabled: true,
            callback: () => toggleOptions(this),
          } as Blockly.ContextMenuRegistry.LegacyContextMenuOption);
        };
        refreshOptions(this);
      }
      this.saveExtraState = () => {
        const saved = blockMemory.get(this);
        if (!saved) return null;
        return {
          yaml: saved.node ? new Document(saved.node.clone()).toString() : undefined,
          source: saved.source,
          produced: saved.produced,
          texts: saved.yaml,
          expanded: saved.expanded,
          sheets: saved.sheets,
          serviceOpaque: saved.serviceOpaque,
        };
      };
      this.loadExtraState = (state: {
        yaml?: string;
        source?: string;
        produced?: string;
        texts?: Record<string, string>;
        expanded?: boolean;
        sheets?: Memory["sheets"];
      }) => {
        const doc = state.yaml === undefined ? undefined : parseDocument(state.yaml);
        if (doc?.errors.length || (doc && !isNode(doc.contents)))
          throw new Error("Cannot restore preserved workflow block.");
        blockMemory.set(this, {
          node: doc?.contents as Node | undefined,
          source: state.source,
          produced: state.produced,
          yaml: state.texts,
          expanded: state.expanded,
          sheets: state.sheets,
        });
        if (this.type === "mai_preserved") this.setEditable(false);
        refreshOptions(this);
      };
    };
  }
  registered = true;
}

// ---- toolbox ------------------------------------------------------------------

type ToolboxBlock = Blockly.utils.toolbox.BlockInfo;
const shadow = (type: string, fields: Record<string, string | number>) => ({
  shadow: { type, fields },
});
const columnShadow = () => shadow("mai_column", { NAME: "Column" });
const conditionBlock = () => ({
  block: {
    type: "mai_compare",
    inputs: { LEFT: columnShadow(), RIGHT: shadow("mai_text", { VALUE: "" }) },
  },
});
function sample(type: string, depth = 0): ToolboxBlock {
  const inputs: Record<string, object> = {};
  for (const f of fieldsOf(type)) {
    if (f.kind === "expr" && !f.optional)
      inputs[f.name] = f.condition
        ? f.scope === "table"
          ? shadow("mai_boolean", { VALUE: "true" })
          : conditionBlock()
        : columnShadow();
    if ((f.kind === "entries" || f.kind === "items") && !f.optional && depth < 2)
      inputs[f.name] = { block: sample(f.item, depth + 1) };
  }
  if (type === "mai_sheet")
    inputs.COLUMNS = { block: sample("mai_out_column", depth + 1) };
  const extra: Record<string, string> = {};
  if (type === "mai_out_column") extra.HEADER = "Column";
  if (type === "mai_sheet") extra.NAME = "Sheet1";
  return {
    kind: "block",
    type,
    ...(Object.keys(inputs).length ? { inputs } : {}),
    ...(Object.keys(extra).length ? { fields: extra } : {}),
  } as ToolboxBlock;
}
const itemsFor: Partial<Record<Category, string[]>> = {
  Data: ["mai_alias", "mai_position", "mai_pair"],
  Columns: ["mai_assign", "mai_rename_pair"],
  Groups: ["mai_aggregate", "mai_pick_rule"],
  Services: ["mai_param", "mai_header"],
};
const order: Category[] = ["Data", "Rows", "Columns", "Groups", "Control", "Services"];
export const workflowToolbox: Blockly.utils.toolbox.ToolboxDefinition = {
  kind: "categoryToolbox",
  contents: [
    ...order.map((category) => ({
      kind: "category",
      name: category,
      colour: categoryColours[category],
      contents: [
        ...steps.filter((s) => s.category === category).map((s) => sample(s.type)),
        ...(itemsFor[category] ?? []).map((t) => sample(t)),
      ],
    })),
    {
      kind: "category",
      name: "Output",
      colour: categoryColours.Output,
      contents: ["mai_sheet", "mai_out_column", "mai_out_style", "mai_service"].map((t) => sample(t)),
    },
    {
      kind: "category",
      name: "Values",
      colour: values,
      contents: [
        sample("mai_column"),
        sample("mai_number"),
        sample("mai_text"),
        {
          kind: "block",
          type: "mai_arithmetic",
          inputs: {
            LEFT: shadow("mai_number", { VALUE: 0 }),
            RIGHT: shadow("mai_number", { VALUE: 0 }),
          },
        } as ToolboxBlock,
        { kind: "block", type: "mai_code", fields: { CODE: 'norm(Column)' } } as ToolboxBlock,
      ],
    },
    {
      kind: "category",
      name: "Logic",
      colour: logic,
      contents: [
        {
          kind: "block",
          type: "mai_compare",
          inputs: { LEFT: columnShadow(), RIGHT: shadow("mai_text", { VALUE: "" }) },
        } as ToolboxBlock,
        {
          kind: "block",
          type: "mai_logic",
          inputs: { LEFT: conditionBlock(), RIGHT: conditionBlock() },
        } as ToolboxBlock,
        { kind: "block", type: "mai_not", inputs: { VALUE: conditionBlock() } } as ToolboxBlock,
        sample("mai_boolean"),
      ],
    },
  ],
};

export { colourOf, stepByType };
