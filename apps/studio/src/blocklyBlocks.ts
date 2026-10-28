import * as Blockly from "blockly/core";
import { Document, isNode, parseDocument, type Node } from "yaml";

type Memory = { node?: Node; signature?: string; custom?: string };
export const blockMemory = new WeakMap<Blockly.Block, Memory>();
const stepTypes = [
  "mai_from",
  "mai_filter",
  "mai_set",
  "mai_rename",
  "mai_select",
  "mai_sort",
  "mai_group",
  "mai_save",
  "mai_join",
  "mai_dedupe",
];
const expressionTypes = [
  "mai_column",
  "mai_number",
  "mai_text",
  "mai_boolean",
  "mai_compare",
  "mai_arithmetic",
  "mai_logic",
  "mai_not",
];
const field = (name: string, text = "") => ({
  type: "field_input",
  name,
  text,
});
const dropdown = (name: string, options: string[][]) => ({
  type: "field_dropdown",
  name,
  options,
});
const definitions: object[] = [
  {
    type: "mai_workflow",
    message0: "Workflow %1 %2",
    args0: [
      { type: "input_dummy" },
      { type: "input_statement", name: "STEPS", check: "MaiStep" },
    ],
    colour: 220,
  },
  {
    type: "mai_from",
    message0: "Read source %1 sheet %2 %3",
    args0: [
      field("SOURCE"),
      dropdown("SHEET_KIND", [
        ["name / first", "name"],
        ["number", "number"],
      ]),
      field("SHEET"),
    ],
    colour: 205,
  },
  {
    type: "mai_filter",
    message0: "Keep rows where %1",
    args0: [{ type: "input_value", name: "VALUE", check: "MaiExpression" }],
    colour: 160,
  },
  {
    type: "mai_set",
    message0: "Set column %1 to %2",
    args0: [
      field("COLUMN"),
      { type: "input_value", name: "VALUE", check: "MaiExpression" },
    ],
    colour: 160,
  },
  {
    type: "mai_rename",
    message0: "Rename column %1 to %2",
    args0: [field("FROM"), field("TO")],
    colour: 160,
  },
  {
    type: "mai_select",
    message0: "Keep columns %1",
    args0: [field("COLUMNS")],
    colour: 160,
  },
  {
    type: "mai_sort",
    message0: "Sort columns %1 %2 %3",
    args0: [
      field("COLUMNS"),
      dropdown("DIRECTION", [
        ["ascending", "false"],
        ["descending", "true"],
      ]),
      dropdown("NUMERIC", [
        ["as text", "false"],
        ["as numbers", "true"],
      ]),
    ],
    colour: 160,
  },
  {
    type: "mai_group",
    message0: "Group by %1 sum columns %2",
    args0: [field("BY"), field("SUM")],
    colour: 160,
  },
  {
    type: "mai_save",
    message0: "Save dataset as %1",
    args0: [field("NAME")],
    colour: 205,
  },
  {
    type: "mai_join",
    message0: "Join dataset %1 on columns %2 %3",
    args0: [
      field("WITH"),
      field("ON"),
      dropdown("HOW", [
        ["keep all rows", "left"],
        ["matching rows only", "inner"],
      ]),
    ],
    colour: 205,
  },
  {
    type: "mai_dedupe",
    message0: "Remove duplicates by %1 keep %2",
    args0: [
      field("BY"),
      dropdown("KEEP", [
        ["first", "first"],
        ["last", "last"],
      ]),
    ],
    colour: 160,
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
    colour: 40,
  },
  {
    type: "mai_column",
    message0: "Column %1 as %2",
    args0: [
      field("NAME"),
      dropdown("MODE", [
        ["original value", "raw"],
        ["number", "number"],
        ["text", "text"],
      ]),
    ],
    colour: 270,
  },
  {
    type: "mai_number",
    message0: "Number %1",
    args0: [{ type: "field_number", name: "VALUE", value: 0 }],
    colour: 270,
  },
  {
    type: "mai_text",
    message0: "Text %1",
    args0: [field("VALUE")],
    colour: 270,
  },
  {
    type: "mai_boolean",
    message0: "%1",
    args0: [
      dropdown("VALUE", [
        ["true", "true"],
        ["false", "false"],
      ]),
    ],
    colour: 270,
  },
  {
    type: "mai_compare",
    message0: "%1 %2 %3",
    args0: [
      { type: "input_value", name: "LEFT", check: "MaiExpression" },
      dropdown("OP", [
        ["equals", "=="],
        ["does not equal", "!="],
        ["greater than", ">"],
        ["at least", ">="],
        ["less than", "<"],
        ["at most", "<="],
      ]),
      { type: "input_value", name: "RIGHT", check: "MaiExpression" },
    ],
    colour: 270,
  },
  {
    type: "mai_arithmetic",
    message0: "%1 %2 %3",
    args0: [
      { type: "input_value", name: "LEFT", check: "MaiExpression" },
      dropdown("OP", [
        ["plus", "+"],
        ["minus", "-"],
        ["times", "*"],
        ["divided by", "/"],
      ]),
      { type: "input_value", name: "RIGHT", check: "MaiExpression" },
    ],
    colour: 270,
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
    colour: 270,
  },
  {
    type: "mai_not",
    message0: "not %1",
    args0: [{ type: "input_value", name: "VALUE", check: "MaiExpression" }],
    colour: 270,
  },
  {
    type: "mai_custom_expression",
    message0: "Custom expression · preserved",
    colour: 40,
  },
].map((def: any) =>
  stepTypes.includes(def.type) || def.type === "mai_preserved"
    ? { ...def, previousStatement: "MaiStep", nextStatement: "MaiStep" }
    : expressionTypes.includes(def.type) || def.type === "mai_custom_expression"
      ? { ...def, output: "MaiExpression" }
      : def,
);
const colours: Record<string, number> = {
  mai_set: 270,
  mai_rename: 270,
  mai_select: 270,
  mai_column: 38,
  mai_number: 38,
  mai_text: 38,
  mai_arithmetic: 38,
  mai_boolean: 350,
  mai_compare: 350,
  mai_logic: 350,
  mai_not: 350,
};
const help: Record<string, string> = {
  mai_from:
    "Use an input identifier from Workbook setup, or a saved table name. Choose a sheet name or number (starting at 1). Leave the name blank for the first sheet.",
  mai_filter:
    "Keep only rows whose connected condition is true. Use a comparison from Logic and column values from Values.",
  mai_set:
    "Create or replace a column. Snap a number, text value, column, or calculation into the value socket.",
  mai_rename: "Enter the exact current column title and its new title.",
  mai_select:
    "Keep and order these column titles. Separate multiple titles with commas.",
  mai_sort:
    "Separate column titles with commas. Choose numeric sorting for quantities and prices.",
  mai_group:
    "Separate column titles with commas. Group keys and sum columns must be different. Leave Group by empty to total the whole table.",
  mai_save:
    "Keep the current table with this name. Use the same name in Workbook setup for an output sheet.",
  mai_join:
    "Choose a table saved by an earlier block. Separate matching column titles with commas.",
  mai_dedupe:
    "Keep one row for each unique combination of these column titles, separated by commas.",
  mai_column:
    "Enter the exact Excel column title. Choose number for arithmetic and numeric comparisons, or text for labels.",
  mai_compare:
    "Compare two values. Convert a column to number before comparing quantities.",
  mai_arithmetic:
    "Connect numeric values. For an Excel column, choose its number conversion first.",
  mai_preserved:
    "This existing step is kept intact. It can be moved or deleted; use Advanced YAML to change its definition.",
  mai_custom_expression:
    "This existing calculation is kept intact. Use Advanced YAML to change its definition.",
};
let registered = false;
export function registerWorkflowBlocks(): void {
  if (registered) return;
  Blockly.defineBlocksWithJsonArray(definitions);
  // Blockly duplicate/copy reconstructs blocks from serialized state. Carry the
  // original YAML and opaque expressions with the block, including hidden options.
  for (const type of [
    ...stepTypes,
    ...expressionTypes,
    "mai_preserved",
    "mai_custom_expression",
  ]) {
    const definition = Blockly.Blocks[type];
    const init = definition.init;
    definition.init = function (this: Blockly.Block) {
      init.call(this);
      if (colours[type] !== undefined) this.setColour(colours[type]);
      if (help[type]) this.setTooltip(help[type]);
      this.saveExtraState = () => {
        const saved = blockMemory.get(this);
        if (!saved) return null;
        return {
          yaml: saved.node
            ? new Document(saved.node.clone()).toString()
            : undefined,
          signature: saved.signature,
          custom: saved.custom,
        };
      };
      this.loadExtraState = (state: {
        yaml?: string;
        signature?: string;
        custom?: string;
      }) => {
        const doc =
          state.yaml === undefined ? undefined : parseDocument(state.yaml);
        if (doc?.errors.length || (doc && !isNode(doc.contents)))
          throw new Error("Cannot restore preserved workflow block.");
        blockMemory.set(this, {
          node: doc?.contents as Node | undefined,
          signature: state.signature,
          custom: state.custom,
        });
        if (
          this.type === "mai_preserved" ||
          this.type === "mai_custom_expression"
        )
          this.setEditable(false);
      };
    };
  }
  registered = true;
}
const item = (
  type: string,
  inputs?: Blockly.utils.toolbox.BlockInfo["inputs"],
) => ({ kind: "block", type, ...(inputs ? { inputs } : {}) });
const shadow = (type: string, fields: Record<string, string | number>) => ({
  shadow: { type, fields },
});
const columnShadow = () => shadow("mai_column", { NAME: "Column" });
const conditionShadow = () => ({
  block: {
    type: "mai_compare",
    inputs: { LEFT: columnShadow(), RIGHT: shadow("mai_text", { VALUE: "" }) },
  },
});
export const workflowToolbox: Blockly.utils.toolbox.ToolboxDefinition = {
  kind: "categoryToolbox",
  contents: [
    {
      kind: "category",
      name: "Data",
      colour: "205",
      contents: [item("mai_from"), item("mai_save"), item("mai_join")],
    },
    {
      kind: "category",
      name: "Rows",
      colour: "160",
      contents: [
        item("mai_filter", { VALUE: conditionShadow() }),
        item("mai_sort"),
        item("mai_group"),
        item("mai_dedupe"),
      ],
    },
    {
      kind: "category",
      name: "Columns",
      colour: "270",
      contents: [
        item("mai_set", { VALUE: columnShadow() }),
        item("mai_rename"),
        item("mai_select"),
      ],
    },
    {
      kind: "category",
      name: "Values",
      colour: "38",
      contents: [
        item("mai_column"),
        item("mai_number"),
        item("mai_text"),
        item("mai_boolean"),
        item("mai_arithmetic", {
          LEFT: shadow("mai_number", { VALUE: 0 }),
          RIGHT: shadow("mai_number", { VALUE: 0 }),
        }),
      ],
    },
    {
      kind: "category",
      name: "Logic",
      colour: "350",
      contents: [
        item("mai_compare", {
          LEFT: columnShadow(),
          RIGHT: shadow("mai_text", { VALUE: "" }),
        }),
        item("mai_logic", {
          LEFT: conditionShadow(),
          RIGHT: conditionShadow(),
        }),
        item("mai_not", { VALUE: conditionShadow() }),
      ],
    },
  ],
};
