/**
 * The Studio step registry: one spec per workflow step or nested item.
 *
 * The specs are declared once, in Go (internal/workflow/steps_info.go), and
 * reach Studio as steps.generated.json, written by `make describe`. Never
 * edit that file by hand: to add or change a step, edit its Go descriptor and
 * regenerate (docs/MAI_STUDIO_DEVELOPMENT.md, "Add a step to Studio"). The
 * step list, the add-step menu, the inspector forms and the column menu are
 * generated from the specs, and pipeline.ts reads and writes YAML from them.
 * A step without a spec still opens, shows its YAML and is saved unchanged.
 */
import catalog from "./steps.generated.json";

/** Where an expression runs, which decides the variables it can see. */
export type ExpressionScope = "row" | "table" | "name";

interface Base {
  /** The YAML key of the field inside the step argument (or item map). */
  key: string;
  /** The form field name; unique within a step. */
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
  /** A map edited as rows of `item` entries (a key and a value per row). */
  | (Base & { kind: "entries"; item: string; rest?: boolean })
  /** A list of maps, each edited as an `item` sub-form. */
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
  /** A stable id for the step kind. */
  type: string;
  title: string;
  /** A short heading. */
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

/** A nested item: one map entry, or one map inside a list. */
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

const groupValidate = (values: Record<string, unknown>) => {
  const aggregates = (values.aggregates ?? {}) as Record<string, string[]>;
  const names = Object.values(aggregates).flat();
  if (!names.length) throw new Error("Add at least one summary column to the group.");
  const by = (values.by as string[] | undefined) ?? [];
  if (new Set(names).size !== names.length || by.some((name) => names.includes(name)))
    throw new Error("Group keys and aggregated columns must be distinct.");
};

/** Checks that need code; everything else about a step is declared in Go. */
const validators: Record<string, StepSpec["validate"]> = { group: groupValidate };

/** A field as `migo describe` writes it: an enum without a fallback falls back to "". */
const field = (f: Record<string, unknown>): Field =>
  (f.kind === "enum" ? { fallback: "", ...f } : f) as unknown as Field;
const fieldList = (list: Record<string, unknown>[] | undefined) => list?.map(field);

export const items: ItemSpec[] = catalog.items.map(
  (item) =>
    ({
      ...item,
      colour: categoryColours[item.category as Category],
      entryKey: item.entryKey && field(item.entryKey),
      entryValue: item.entryValue && field(item.entryValue),
      fields: fieldList(item.fields),
    }) as ItemSpec,
);

export const steps: StepSpec[] = catalog.steps.map(
  (step) =>
    ({ ...step, fields: fieldList(step.fields), validate: validators[step.op] }) as StepSpec,
);

/** The expression helpers, for reference and completion. */
export const functions: { name: string; signature: string; doc: string }[] = catalog.functions;

export const stepByOp = new Map(steps.map((s) => [s.op, s]));
export const stepByType = new Map(steps.map((s) => [s.type, s]));
export const itemByType = new Map(items.map((s) => [s.type, s]));

/** The fields of a step or item. */
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
