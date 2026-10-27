import { isMap, isScalar, parseDocument, type Document } from "yaml";

type Field = {
  key: string;
  label: string;
  help?: string;
  kind: "text" | "sheet" | "columns" | "boolean" | "enum" | "rows";
  options?: string[];
  keyLabel?: string;
  valueLabel?: string;
};
export type GuidedSpec = {
  scalar?: { label: string; help: string };
  fields?: Field[];
  rootRows?: Field;
};
const columns = (key: string, label: string): Field => ({
  key,
  label,
  kind: "columns",
  help: "Separate column names with commas.",
});
const rows = (
  key: string,
  label: string,
  keyLabel: string,
  valueLabel: string,
): Field => ({ key, label, kind: "rows", keyLabel, valueLabel });
export const specs: Record<string, GuidedSpec> = {
  from: {
    scalar: {
      label: "Input or saved table",
      help: "For example, parts or parts[Sheet1].",
    },
    fields: [
      {
        key: "input",
        label: "Input or saved table",
        kind: "text",
        help: "The input name, for example parts.",
      },
      {
        key: "sheet",
        label: "Sheet",
        kind: "sheet",
        help: "Sheet name or number; leave blank for the first sheet.",
      },
      rows("columns", "Column positions", "Column name", "Sheet column"),
      { key: "trim", label: "Trim whitespace", kind: "boolean" },
      { key: "skip_blank", label: "Skip blank rows", kind: "boolean" },
      { key: "row_number", label: "Row number column", kind: "text" },
      { key: "empty", label: "Message if empty", kind: "text" },
    ],
  },
  filter: {
    scalar: {
      label: "Keep rows when",
      help: "For example, num(Quantity) > 0.",
    },
  },
  save: {
    scalar: {
      label: "Table name",
      help: "Use this name to refer to the table in a later step.",
    },
  },
  set: {
    rootRows: rows("", "Columns to calculate", "Column name", "Expression"),
  },
  select: {
    rootRows: rows("", "Columns to keep", "New column name", "Source column"),
  },
  rename: {
    scalar: { label: "Name expression", help: "For example, lower(name)." },
    rootRows: rows("", "Columns to rename", "Current name", "New name"),
  },
  sort: {
    fields: [
      columns("by", "Sort by"),
      { key: "desc", label: "Descending", kind: "boolean" },
      { key: "numeric", label: "Sort as numbers", kind: "boolean" },
    ],
  },
  group: {
    fields: [
      columns("by", "Group by"),
      ...["sum", "count", "mean", "min", "max", "first", "last", "join"].map(
        (key) => columns(key, `${key[0].toUpperCase()}${key.slice(1)} columns`),
      ),
    ],
  },
  join: {
    fields: [
      {
        key: "with",
        label: "Saved table",
        kind: "text",
        help: "Name of the table to join.",
      },
      columns("on", "Matching columns"),
      columns("right_on", "Matching columns in saved table"),
      {
        key: "how",
        label: "Keep rows",
        kind: "enum",
        options: ["left", "inner"],
        help: "Left keeps all current rows; inner keeps matches only.",
      },
      { key: "normalize", label: "Normalize matching values", kind: "boolean" },
      rows("take", "Columns to add", "New column name", "Saved table column"),
    ],
  },
};

function stringMap(value: unknown): value is Record<string, string> {
  return (
    !!value &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.values(value).every((v) => typeof v === "string")
  );
}
// JavaScript objects reorder integer-index keys, but YAML mappings can be ordered.
export function isIntegerKey(name: string): boolean {
  const number = Number(name);
  return Number.isInteger(number) && number >= 0 && number < 4294967295 && String(number) === name;
}
function unsafeRowKeys(node: unknown): boolean {
  if (!isMap(node)) return true;
  return node.items.some(({ key }) => {
    if (!isScalar(key) || typeof key.value !== "string") return true;
    const name = key.value;
    const number = Number(name);
    return (
      Number.isInteger(number) &&
      number >= 0 &&
      number < 4294967295 &&
      String(number) === name
    );
  });
}
export function readGuided(
  operation: string,
  argument: string,
): { doc: Document; value: unknown; spec: GuidedSpec } | undefined {
  const spec = specs[operation];
  if (!spec) return;
  try {
    const doc = parseDocument(argument, {
      keepSourceTokens: true,
      uniqueKeys: true,
    });
    if (doc.errors.length) return;
    const value: unknown = doc.toJS({ maxAliasCount: 0 });
    if (typeof value === "string" && spec.scalar) return { doc, value, spec };
    if (
      !isMap(doc.contents) ||
      !value ||
      typeof value !== "object" ||
      Array.isArray(value)
    )
      return;
    // Non-string keys and duplicate keys cannot be safely represented by the form.
    if (
      doc.contents.items.some(
        (pair) => !isScalar(pair.key) || typeof pair.key.value !== "string",
      )
    )
      return;
    if (spec.rootRows)
      return stringMap(value) && !unsafeRowKeys(doc.contents)
        ? { doc, value, spec }
        : undefined;
    if (!spec.fields) return;
    const map = value as Record<string, unknown>;
    for (const field of spec.fields) {
      const v = map[field.key];
      if (v === undefined) continue;
      if (field.kind === "text" && typeof v !== "string") return;
      if (
        field.kind === "sheet" &&
        typeof v !== "string" &&
        !(typeof v === "number" && Number.isSafeInteger(v) && v > 0)
      )
        return;
      if (field.kind === "boolean" && typeof v !== "boolean") return;
      if (
        field.kind === "columns" &&
        (!Array.isArray(v) ||
          !v.every(
            (x) =>
              typeof x === "string" &&
              x.trim() === x &&
              x.length > 0 &&
              !x.includes(","),
          ))
      )
        return;
      if (
        field.kind === "rows" &&
        (!stringMap(v) || unsafeRowKeys(doc.get(field.key, true)))
      )
        return;
      if (
        field.kind === "enum" &&
        (typeof v !== "string" || !field.options?.includes(v))
      )
        return;
    }
    return { doc, value, spec };
  } catch {
    return;
  }
}
export function updateGuided(
  argument: string,
  operation: string,
  key: string | null,
  value: unknown,
): string {
  const parsed = readGuided(operation, argument);
  if (!parsed) throw new Error("These settings must be edited in YAML.");
  if (key === null) parsed.doc.contents = parsed.doc.createNode(value);
  else if (value === undefined) parsed.doc.delete(key);
  else parsed.doc.set(key, value);
  return parsed.doc.toString().trimEnd();
}
