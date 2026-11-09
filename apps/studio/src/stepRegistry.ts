/**
 * The Studio step registry: one declaration per workflow step or nested item.
 *
 * Every block on the canvas, every toolbox entry, the step library and the
 * selected-block inspector are generated from these specs, and the generic
 * codec in blocklyWorkflow.ts reads and writes YAML from them. To make a new
 * Go step editable as blocks, add one entry to `steps` (see
 * docs/MAI_STUDIO_DEVELOPMENT.md, "Add a step to Studio"). Unknown or
 * unrepresentable steps still round-trip intact as preserved blocks.
 */

/** Where an expression runs, which decides the variables it can see. */
export type ExpressionScope = "row" | "table" | "name";

interface Base {
  /** The YAML key of the field inside the step argument (or item map). */
  key: string;
  /** The Blockly field or input name; unique within a block. */
  name: string;
  /** The label shown before the field on the canvas. */
  label: string;
  /** Omitted from YAML while empty (or at its default). */
  optional?: boolean;
  /** Shown only after "Show options" unless the loaded YAML sets it. */
  advanced?: boolean;
  /** One line of guidance for the inspector. */
  hint?: string;
  /** A UI-only field with no YAML key of its own (from: sheet kind). */
  virtual?: boolean;
}
export type Field =
  | (Base & { kind: "text"; placeholder?: string; multiline?: boolean })
  /** Text with {expression} parts, e.g. log messages and reasons. */
  | (Base & { kind: "template"; multiline?: boolean })
  | (Base & { kind: "number"; integer?: boolean; min?: number })
  | (Base & { kind: "list" })
  | (Base & { kind: "bool" })
  | (Base & {
      kind: "enum";
      options: [label: string, value: string][];
      /** The value used when the key is absent. */
      fallback: string;
      /** Write true/false instead of text. */
      boolean?: boolean;
    })
  | (Base & { kind: "expr"; scope: ExpressionScope; condition?: boolean })
  /** A map edited as a stack of `item` blocks (key and value per block). */
  | (Base & { kind: "entries"; item: string; rest?: boolean })
  /** A list of maps edited as a stack of `item` blocks. */
  | (Base & { kind: "items"; item: string })
  /** A nested list of steps (if/then/else, split). */
  | (Base & { kind: "steps" })
  /** Any YAML value written as flow YAML, e.g. `{bold: true}`. */
  | (Base & { kind: "yaml" });

export type Category =
  | "Data"
  | "Rows"
  | "Columns"
  | "Groups"
  | "Control"
  | "Services"
  | "Output";

export interface StepSpec {
  /** The YAML step name. */
  op: string;
  /** The Blockly block type. */
  type: string;
  title: string;
  /** The block's first line. */
  heading: string;
  category: Category;
  icon: string;
  description: string;
  help: string;
  fields: Field[];
  /**
   * How the YAML argument maps to fields:
   * - map: `op: {key: value, ...}`
   * - entries: the argument itself is the map of the single entries field
   * - inline: the step node holds the keys (`if:`, `then:`, `else:`)
   */
  form: "map" | "entries" | "inline";
  /** The field a scalar argument fills (`save: name`, `log: text`). */
  scalar?: string;
  /** The field a list argument fills (`fill: [A, B]`). */
  list?: string;
  /** The default YAML argument when inserted from the step library. */
  example: string;
  /** Extra checks before writing YAML. */
  validate?: (values: Record<string, unknown>) => void;
  /** Needs a service connection; blocked in local runs. */
  service?: boolean;
}

/** A nested block: one map entry, or one map inside a list. */
export interface ItemSpec {
  type: string;
  /** The inspector title. */
  title: string;
  heading: string;
  colour: string;
  help: string;
  /** entry: one key/value pair of an `entries` field; object: one list map. */
  shape: "entry" | "object";
  /** entry only: the key field (key "") and value field (key ""). */
  entryKey?: Field;
  entryValue?: Field;
  /** object only. */
  fields?: Field[];
  /** The statement check the parent's input accepts. */
  check: string;
}

const text = (
  key: string,
  label: string,
  more: Partial<Base> & { placeholder?: string; multiline?: boolean } = {},
): Field => ({ kind: "text", key, name: key.toUpperCase(), label, ...more });
const template = (
  key: string,
  label: string,
  more: Partial<Base> & { multiline?: boolean } = {},
): Field => ({ kind: "template", key, name: key.toUpperCase(), label, ...more });
const list = (key: string, label: string, more: Partial<Base> = {}): Field => ({
  kind: "list",
  key,
  name: key.toUpperCase(),
  label,
  ...more,
});
const bool = (key: string, label: string, more: Partial<Base> = {}): Field => ({
  kind: "bool",
  key,
  name: key.toUpperCase(),
  label,
  optional: true,
  ...more,
});
const number = (
  key: string,
  label: string,
  more: Partial<Base> & { integer?: boolean; min?: number } = {},
): Field => ({ kind: "number", key, name: key.toUpperCase(), label, ...more });
const expr = (
  key: string,
  label: string,
  scope: ExpressionScope,
  more: Partial<Base> & { condition?: boolean } = {},
): Field => ({ kind: "expr", key, name: key.toUpperCase(), label, scope, ...more });
const yamlField = (key: string, label: string, more: Partial<Base> = {}): Field => ({
  kind: "yaml",
  key,
  name: key.toUpperCase(),
  label,
  optional: true,
  ...more,
});
const steps_ = (key: string, label: string, more: Partial<Base> = {}): Field => ({
  kind: "steps",
  key,
  name: key.toUpperCase(),
  label,
  ...more,
});

export const categoryColours: Record<Category | "Values" | "Logic", string> = {
  Data: "#3b7ddd",
  Rows: "#1f9d8b",
  Columns: "#7a5af5",
  Groups: "#c2569b",
  Control: "#d08a1e",
  Services: "#5b6b82",
  Output: "#2e8b57",
  Values: "#e0912f",
  Logic: "#d9534f",
};

export const items: ItemSpec[] = [
  {
    type: "mai_assign",
    title: "Column calculation",
    heading: "",
    colour: categoryColours.Columns,
    help: "Create or replace this column. Columns are calculated in order, so later ones can use earlier ones.",
    shape: "entry",
    entryKey: text("", "column", { name: "COLUMN" }),
    entryValue: expr("", "=", "row", { name: "VALUE" }),
    check: "MaiAssign",
  },
  {
    type: "mai_pair",
    title: "Column mapping",
    heading: "",
    colour: categoryColours.Columns,
    help: "Map one name to another: the left name is written, the right name is read.",
    shape: "entry",
    entryKey: text("", "", { name: "FROM" }),
    entryValue: text("", "←", { name: "TO" }),
    check: "MaiPair",
  },
  {
    type: "mai_rename_pair",
    title: "Rename",
    heading: "",
    colour: categoryColours.Columns,
    help: "Enter the exact current column title and its new title.",
    shape: "entry",
    entryKey: text("", "column", { name: "FROM" }),
    entryValue: text("", "to", { name: "TO" }),
    check: "MaiRenamePair",
  },
  {
    type: "mai_param",
    title: "Query parameter",
    heading: "",
    colour: categoryColours.Services,
    help: "A query parameter (@name). The table expression sees rows; a list expands to several values.",
    shape: "entry",
    entryKey: text("", "@", { name: "NAME" }),
    entryValue: expr("", "=", "table", { name: "VALUE" }),
    check: "MaiParam",
  },
  {
    type: "mai_header",
    title: "HTTP header",
    heading: "",
    colour: categoryColours.Services,
    help: "An HTTP header. The value is text with {expression} parts.",
    shape: "entry",
    entryKey: text("", "header", { name: "NAME" }),
    entryValue: template("", ":", { name: "VALUE" }),
    check: "MaiHeader",
  },
  {
    type: "mai_alias",
    title: "Header column",
    heading: "",
    colour: categoryColours.Data,
    help: "A column found by any of these header titles (case-insensitive), separated by commas.",
    shape: "entry",
    entryKey: text("", "column", { name: "COLUMN" }),
    entryValue: list("", "titles", { name: "TITLES" }),
    check: "MaiAlias",
  },
  {
    type: "mai_position",
    title: "Column position",
    heading: "",
    colour: categoryColours.Data,
    help: "A column read from a fixed sheet column letter, such as A or BA.",
    shape: "entry",
    entryKey: text("", "column", { name: "COLUMN" }),
    entryValue: text("", "at", { name: "LETTER" }),
    check: "MaiPosition",
  },
  {
    type: "mai_aggregate",
    title: "Summary",
    heading: "",
    colour: categoryColours.Groups,
    help: "Summarize these columns in each group.",
    shape: "entry",
    entryKey: {
      kind: "enum",
      key: "",
      name: "AGGREGATE",
      label: "",
      fallback: "sum",
      options: [
        ["sum", "sum"],
        ["count", "count"],
        ["mean", "mean"],
        ["min", "min"],
        ["max", "max"],
        ["first", "first"],
        ["last", "last"],
        ["join", "join"],
      ],
    },
    entryValue: list("", "of", { name: "COLUMNS" }),
    check: "MaiAggregate",
  },
  {
    type: "mai_pick_rule",
    title: "Pick rule",
    heading: "Rule",
    colour: categoryColours.Groups,
    help: "The first rule with matching rows chooses one row per group. Ties go to the top-most row.",
    shape: "object",
    fields: [
      expr("rows", "candidates", "row", { condition: true }),
      text("best", "highest", { optional: true, hint: "A numeric column; the highest value wins." }),
      expr("when", "applies when", "name", {
        optional: true,
        hint: "Sees size, best and tied. Default: size > 0.",
      }),
      template("reason", "reason", { optional: true }),
    ],
    check: "MaiPickRule",
  },
  {
    type: "mai_sheet",
    title: "Output sheet",
    heading: "Sheet",
    colour: categoryColours.Output,
    help: "One worksheet of the output workbook, written from a saved dataset.",
    shape: "object",
    fields: [
      text("name", "name"),
      text("from", "from dataset", { optional: true }),
      text("like", "like sheet", { optional: true, advanced: true }),
      bool("freeze", "freeze header", { advanced: true }),
      number("header_height", "header height", { optional: true, advanced: true }),
      yamlField("header", "header style", { advanced: true }),
      yamlField("style", "cell style", { advanced: true }),
      { kind: "items", key: "columns", name: "COLUMNS", label: "columns", item: "mai_out_column", optional: true },
      { kind: "items", key: "styles", name: "STYLES", label: "styles", item: "mai_out_style", optional: true },
    ],
    check: "MaiSheet",
  },
  {
    type: "mai_out_column",
    title: "Output column",
    heading: "Column",
    colour: categoryColours.Output,
    help: "One output column: a dataset column, or a value calculated per row.",
    shape: "object",
    fields: [
      text("header", "header"),
      text("id", "id", { optional: true, advanced: true }),
      text("column", "from column", { optional: true }),
      expr("value", "value", "row", { optional: true }),
      number("width", "width", { optional: true }),
      text("number_format", "number format", { optional: true, advanced: true }),
      yamlField("style", "style", { advanced: true }),
      yamlField("header_style", "header style", { advanced: true }),
    ],
    check: "MaiOutColumn",
  },
  {
    type: "mai_out_style",
    title: "Conditional style",
    heading: "Style",
    colour: categoryColours.Output,
    help: "Conditional formatting. Later styles win over earlier ones.",
    shape: "object",
    fields: [
      expr("when", "when", "row", { optional: true, condition: true }),
      list("columns", "columns", { optional: true }),
      text("fill", "fill", { optional: true, placeholder: "#FFFF00" }),
      yamlField("font", "font"),
      yamlField("border", "border"),
      text("number_format", "number format", { optional: true, advanced: true }),
      text("align", "align", { optional: true, advanced: true }),
      bool("bold", "bold", { advanced: true }),
    ],
    check: "MaiOutStyle",
  },
];

const entries = (
  key: string,
  label: string,
  item: string,
  more: Partial<Base> & { rest?: boolean } = {},
): Field => ({ kind: "entries", key, name: key.toUpperCase() || "ENTRIES", label, item, ...more });

const groupValidate = (values: Record<string, unknown>) => {
  const aggregates = (values.aggregates ?? {}) as Record<string, string[]>;
  const names = Object.values(aggregates).flat();
  if (!names.length) throw new Error("Add at least one summary column to the group.");
  const by = (values.by as string[] | undefined) ?? [];
  if (new Set(names).size !== names.length || by.some((name) => names.includes(name)))
    throw new Error("Group keys and aggregated columns must be distinct.");
};

export const steps: StepSpec[] = [
  {
    op: "from",
    type: "mai_from",
    title: "Read data",
    heading: "Read",
    category: "Data",
    icon: "↳",
    description: "Start with an input workbook or a saved table.",
    help: "Use an input identifier from Workbook setup, or a saved table name. Choose a sheet name or number (starting at 1). Leave the name blank for the first sheet.",
    form: "map",
    fields: [
      text("input", "source", { name: "SOURCE" }),
      {
        kind: "enum",
        key: "",
        name: "SHEET_KIND",
        label: "sheet",
        virtual: true,
        fallback: "name",
        options: [
          ["name / first", "name"],
          ["number", "number"],
        ],
      },
      text("sheet", "", { name: "SHEET", optional: true }),
      number("start", "first data row", { optional: true, integer: true, min: 1, advanced: true }),
      bool("trim", "trim cells", { advanced: true }),
      bool("skip_blank", "skip blank rows", { advanced: true }),
      text("row_number", "row number column", { optional: true, advanced: true }),
      template("empty", "fail if empty", { optional: true, advanced: true }),
      entries("columns", "fixed columns", "mai_position", { optional: true, advanced: true }),
      number("header.search", "find header in first", { optional: true, integer: true, min: 1, advanced: true }),
      number("header.min", "with at least", { optional: true, integer: true, min: 1, advanced: true }),
      entries("header.columns", "header titles", "mai_alias", { name: "HEADER_COLUMNS", optional: true, advanced: true }),
      entries("header.fallback", "fallback positions", "mai_position", { name: "HEADER_FALLBACK", optional: true, advanced: true }),
    ],
    example: "{input: input}",
  },
  {
    op: "save",
    type: "mai_save",
    title: "Save table",
    heading: "Save dataset as",
    category: "Data",
    icon: "▣",
    description: "Keep a copy of the current table for later steps or output sheets.",
    help: "Keep the current table with this name. Use the same name in Workbook setup for an output sheet.",
    form: "map",
    scalar: "",
    fields: [text("", "", { name: "NAME" })],
    example: "result",
  },
  {
    op: "append",
    type: "mai_append",
    title: "Append tables",
    heading: "Append datasets",
    category: "Data",
    icon: "+",
    description: "Add the rows of saved tables; columns are matched by name.",
    help: "Saved dataset names separated by commas. Their rows are added below the current table.",
    form: "map",
    list: "",
    fields: [list("", "", { name: "DATASETS" })],
    example: "[other]",
  },
  {
    op: "join",
    type: "mai_join",
    title: "Join tables",
    heading: "Join dataset",
    category: "Data",
    icon: "⋈",
    description: "Add columns from a saved table by matching keys.",
    help: "Choose a table saved by an earlier block. Separate matching column titles with commas. Take chooses and names the added columns.",
    form: "map",
    fields: [
      text("with", "", { name: "WITH" }),
      list("on", "on columns", { name: "ON" }),
      {
        kind: "enum",
        key: "how",
        name: "HOW",
        label: "",
        fallback: "left",
        options: [
          ["keep all rows", "left"],
          ["matching rows only", "inner"],
        ],
      },
      list("right_on", "matching their", { optional: true, advanced: true }),
      bool("normalize", "ignore case and spaces", { advanced: true }),
      text("suffix", "suffix for clashes", { optional: true, advanced: true }),
      entries("take", "take", "mai_pair", { optional: true, advanced: true }),
    ],
    example: "{with: other, on: [Key]}",
  },
  {
    op: "filter",
    type: "mai_filter",
    title: "Filter rows",
    heading: "Keep rows where",
    category: "Rows",
    icon: "◇",
    description: "Keep rows that match a condition.",
    help: "Keep only rows whose connected condition is true. Use a comparison from Logic and column values from Values.",
    form: "map",
    scalar: "",
    fields: [expr("", "", "row", { name: "VALUE", condition: true })],
    example: '"true"',
  },
  {
    op: "sort",
    type: "mai_sort",
    title: "Sort rows",
    heading: "Sort by",
    category: "Rows",
    icon: "⇅",
    description: "Order rows by columns (stable).",
    help: "Separate column titles with commas. Choose numeric sorting for quantities and prices.",
    form: "map",
    fields: [
      list("by", "", { name: "COLUMNS" }),
      {
        kind: "enum",
        key: "desc",
        name: "DIRECTION",
        label: "",
        fallback: "false",
        boolean: true,
        options: [
          ["ascending", "false"],
          ["descending", "true"],
        ],
      },
      {
        kind: "enum",
        key: "numeric",
        name: "NUMERIC",
        label: "",
        fallback: "false",
        boolean: true,
        options: [
          ["as text", "false"],
          ["as numbers", "true"],
        ],
      },
    ],
    example: "{by: [Column]}",
  },
  {
    op: "dedupe",
    type: "mai_dedupe",
    title: "Remove duplicates",
    heading: "Remove duplicates by",
    category: "Rows",
    icon: "≠",
    description: "Keep one row per key.",
    help: "Keep one row for each unique combination of these column titles, separated by commas.",
    form: "map",
    list: "by",
    fields: [
      list("by", "", { name: "BY" }),
      {
        kind: "enum",
        key: "keep",
        name: "KEEP",
        label: "keep",
        fallback: "first",
        options: [
          ["first", "first"],
          ["last", "last"],
        ],
      },
      bool("normalize", "ignore case and spaces", { advanced: true }),
    ],
    example: "[Column]",
  },
  {
    op: "stack",
    type: "mai_stack",
    title: "Stack columns",
    heading: "Stack columns",
    category: "Rows",
    icon: "⇵",
    description: "One row per listed column, its value in a new column.",
    help: "Each row is repeated once per listed column, with that column's value in the new column.",
    form: "map",
    fields: [list("columns", "", { name: "COLUMNS" }), text("into", "into", { name: "INTO" })],
    example: "{columns: [A, B], into: Value}",
  },
  {
    op: "compare",
    type: "mai_compare_tables",
    title: "Compare tables",
    heading: "Compare with",
    category: "Rows",
    icon: "⇄",
    description: "Mark rows added, removed, changed or unchanged against a saved table.",
    help: "Compares the current table with a saved baseline by key columns and writes a status column.",
    form: "map",
    fields: [
      text("with", "", { name: "WITH" }),
      list("by", "by keys"),
      list("columns", "compare columns", { optional: true }),
      text("status", "status column", { optional: true }),
      bool("normalize", "ignore case and spaces", { advanced: true }),
    ],
    example: "{with: baseline, by: [Key]}",
  },
  {
    op: "set",
    type: "mai_set",
    title: "Calculate columns",
    heading: "Set columns",
    category: "Columns",
    icon: "ƒ",
    description: "Create or update columns with expressions, in order.",
    help: "Create or replace columns. Snap a number, text, column or calculation into each value socket. prev(\"Col\") reads the row above.",
    form: "entries",
    fields: [entries("", "", "mai_assign")],
    example: '{Column: "\\"\\""}',
  },
  {
    op: "select",
    type: "mai_select",
    title: "Select columns",
    heading: "Keep columns",
    category: "Columns",
    icon: "☷",
    description: "Choose, rename and order columns.",
    help: "Keep only these columns, in this order. The left name is the output, the right name the source column.",
    form: "entries",
    fields: [entries("", "", "mai_pair")],
    example: "{Column: Column}",
  },
  {
    op: "rename",
    type: "mai_rename",
    title: "Rename columns",
    heading: "Rename columns",
    category: "Columns",
    icon: "✎",
    description: "Give columns new names.",
    help: "Enter the exact current column title, then its new title.",
    form: "entries",
    fields: [entries("", "", "mai_rename_pair")],
    example: "{Old: New}",
  },
  {
    op: "fill",
    type: "mai_fill",
    title: "Fill blanks down",
    heading: "Fill blanks from above in",
    category: "Columns",
    icon: "↓",
    description: "Fill blank cells from the row above.",
    help: "Column titles separated by commas. A blank cell takes the last non-blank value above it.",
    form: "map",
    list: "",
    fields: [list("", "", { name: "COLUMNS" })],
    example: "[Column]",
  },
  {
    op: "group",
    type: "mai_group",
    title: "Group rows",
    heading: "Group by",
    category: "Groups",
    icon: "⊞",
    description: "Summarize rows by key: one row per group.",
    help: "Separate column titles with commas. Leave Group by empty to total the whole table. Add one summary per aggregate.",
    form: "map",
    fields: [
      list("by", "", { name: "BY", optional: true }),
      entries("", "summaries", "mai_aggregate", { name: "AGGREGATES", rest: true }),
    ],
    example: "{by: [Key], sum: [Quantity]}",
    validate: groupValidate,
  },
  {
    op: "window",
    type: "mai_window",
    title: "Calculate by group",
    heading: "For each group of",
    category: "Groups",
    icon: "▤",
    description: "Compute columns per group, keeping every row.",
    help: "Expressions see group (the group's rows), index (position in group) and size. Leave by empty for one group.",
    form: "map",
    fields: [
      list("by", "", { optional: true }),
      expr("where", "only rows where", "row", { optional: true, condition: true }),
      entries("set", "set", "mai_assign"),
    ],
    example: "{by: [Key], set: {Column: '\"\"'}}",
  },
  {
    op: "pick",
    type: "mai_pick",
    title: "Pick rows",
    heading: "Pick one row per",
    category: "Groups",
    icon: "✓",
    description: "Choose one row per group with ordered rules.",
    help: "Rules run in order; the first rule with candidate rows chooses. The chosen row is marked true and gets the rule's reason.",
    form: "map",
    fields: [
      list("by", "", { optional: true }),
      expr("where", "taking part", "row", { optional: true, condition: true }),
      text("mark", "mark column"),
      text("reason", "reason column", { optional: true }),
      { kind: "items", key: "rules", name: "RULES", label: "rules", item: "mai_pick_rule" },
    ],
    example: "{by: [Key], mark: Chosen, rules: [{rows: 'true'}]}",
  },
  {
    op: "if",
    type: "mai_if",
    title: "If / else",
    heading: "If",
    category: "Control",
    icon: "⑂",
    description: "Run steps only when a table condition holds.",
    help: "A table expression: it sees rows, table(\"name\"), sheets(\"input\") and config(\"key\").",
    form: "inline",
    fields: [
      expr("if", "", "table", { name: "CONDITION", condition: true }),
      steps_("then", "then"),
      steps_("else", "else", { optional: true }),
    ],
    example: "",
  },
  {
    op: "split",
    type: "mai_split",
    title: "Split rows",
    heading: "Split rows where",
    category: "Control",
    icon: "⫘",
    description: "Route matching rows and the others through different steps.",
    help: "Matching rows run through then, the others through else; the results are appended (then first).",
    form: "map",
    fields: [
      expr("where", "", "row", { condition: true }),
      steps_("then", "then", { optional: true }),
      steps_("else", "else", { optional: true }),
    ],
    example: "{where: 'true', then: [], else: []}",
  },
  {
    op: "log",
    type: "mai_log",
    title: "Log message",
    heading: "Log",
    category: "Control",
    icon: "☰",
    description: "Write a message to the run log.",
    help: "Text with {expression} parts, e.g. Loaded {len(rows)} rows. Add a condition to log only sometimes.",
    form: "map",
    scalar: "message",
    fields: [
      template("message", "", { name: "MESSAGE" }),
      expr("when", "when", "table", { optional: true, condition: true }),
    ],
    example: '"Loaded {len(rows)} rows."',
  },
  {
    op: "fail",
    type: "mai_fail",
    title: "Stop with error",
    heading: "Stop with error",
    category: "Control",
    icon: "!",
    description: "Stop the run; the message becomes the API error.",
    help: "Text with {expression} parts. With a condition, the run stops only when it is true.",
    form: "map",
    scalar: "message",
    fields: [
      template("message", "", { name: "MESSAGE" }),
      expr("when", "when", "table", { optional: true, condition: true }),
    ],
    example: "{when: 'len(rows) == 0', message: The file is empty.}",
  },
  {
    op: "sql",
    type: "mai_sql",
    title: "SQL query",
    heading: "SQL query",
    category: "Services",
    icon: "⌘",
    description: "Query a configured database and save the result.",
    help: "Parameters are table expressions; a list expands to @p1, @p2… The current table is unchanged.",
    service: true,
    form: "map",
    fields: [
      text("query", "", { name: "QUERY", multiline: true }),
      text("save", "save as"),
      entries("params", "parameters", "mai_param", { optional: true }),
      list("columns", "columns", { optional: true }),
      text("connection", "connection", { optional: true, advanced: true }),
      number("batch", "values per query", { optional: true, integer: true, min: 1, advanced: true }),
      template("on_error", "on error, log", { optional: true, advanced: true }),
    ],
    example: "{query: SELECT 1 AS One, save: result}",
  },
  {
    op: "rest",
    type: "mai_rest",
    title: "REST request",
    heading: "REST",
    category: "Services",
    icon: "↗",
    description: "Call an HTTP API in batches and save the returned rows.",
    help: "url, headers and on_error are text with {expression} parts. body sees batch and batch_number.",
    service: true,
    form: "map",
    fields: [
      {
        kind: "enum",
        key: "method",
        name: "METHOD",
        label: "",
        fallback: "GET",
        options: ["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => [m, m] as [string, string]),
      },
      template("url", "url"),
      text("save", "save as"),
      expr("each", "for each value of", "table", { optional: true }),
      expr("batch", "batch size", "table", { optional: true }),
      expr("body", "body", "table", { optional: true }),
      expr("rows", "rows from response", "table", { optional: true }),
      list("columns", "columns", { optional: true }),
      entries("headers", "headers", "mai_header", { optional: true, advanced: true }),
      expr("retries", "retries", "table", { optional: true, advanced: true }),
      {
        kind: "enum",
        key: "keys",
        name: "KEYS",
        label: "key case",
        fallback: "",
        advanced: true,
        options: [
          ["as sent", ""],
          ["lower", "lower"],
          ["upper", "upper"],
        ],
      },
      template("on_error", "on error, log", { optional: true, advanced: true }),
    ],
    example: "{url: 'http://localhost/api', save: result}",
  },
];

export const stepByOp = new Map(steps.map((s) => [s.op, s]));
export const stepByType = new Map(steps.map((s) => [s.type, s]));
export const itemByType = new Map(items.map((s) => [s.type, s]));

/** The fields of a step or object item block. */
export function fieldsOf(type: string): Field[] {
  const step = stepByType.get(type);
  if (step) return step.fields;
  const item = itemByType.get(type);
  if (!item) return [];
  if (item.shape === "object") return item.fields ?? [];
  return [item.entryKey!, item.entryValue!];
}

/** The colour of a step or item block. */
export function colourOf(type: string): string {
  const step = stepByType.get(type);
  if (step) return categoryColours[step.category];
  return itemByType.get(type)?.colour ?? "#8a94a6";
}
