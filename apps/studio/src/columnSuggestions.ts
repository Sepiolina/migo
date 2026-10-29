import { parseDocument } from "yaml";
import type { Inspection } from "./api";

const map = (value: unknown): Record<string, unknown> | undefined =>
  value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
const names = (value: unknown): string[] | undefined =>
  Array.isArray(value) && value.every((item) => typeof item === "string")
    ? value
    : undefined;

/** Advisory names only, never a replacement for Go validation or type inference. */
export function suggestColumns(
  source: string,
  selected: string | undefined,
  inspection: Inspection | undefined,
  loadedPath: string,
  bindings: Record<string, string>,
): string[] {
  if (!selected || !/^step\.\d+$/.test(selected)) return [];
  try {
    const doc = parseDocument(source);
    if (doc.errors.length) return [];
    const root = map(doc.toJS({ maxAliasCount: 0 }));
    if (!root || !Array.isArray(root.steps)) return [];
    let columns: string[] = [];
    const saved = new Map<string, string[]>();
    for (const raw of root.steps.slice(0, Number(selected.slice(5)))) {
      const step = map(raw);
      if (!step || Object.keys(step).length !== 1) {
        columns = [];
        continue;
      }
      const [op, value] = Object.entries(step)[0];
      const arg = map(value);
      if (op === "from") {
        columns = [];
        const match =
          typeof value === "string"
            ? /^([^\[\]]+)(?:\[([^\]]+)\])?$/.exec(value)
            : null;
        const input = match?.[1] ?? arg?.input;
        const sheet = match?.[2] ?? arg?.sheet;
        if (typeof input !== "string") continue;
        if (!sheet && saved.has(input)) {
          columns = [...saved.get(input)!];
          continue;
        }
        if (
          !inspection ||
          inspection.offset !== 0 ||
          bindings[input] !== loadedPath
        )
          continue;
        const expected =
          typeof sheet === "number"
            ? inspection.sheets[sheet - 1]
            : sheet || inspection.sheets[0];
        if (
          inspection.sheet !== expected ||
          arg?.header ||
          arg?.columns ||
          arg?.start
        )
          continue;
        columns = (inspection.rows[0] ?? []).filter(Boolean);
        if (typeof arg?.row_number === "string") columns.push(arg.row_number);
      } else if (op === "save" && typeof value === "string")
        saved.set(value, [...columns]);
      else if (["filter", "sort", "dedupe", "log"].includes(op)) continue;
      else if (op === "set" && arg && columns.length)
        columns = [...columns, ...Object.keys(arg)];
      else if (op === "rename" && arg && columns.length)
        columns = columns.map((name) =>
          typeof arg[name] === "string" ? (arg[name] as string) : name,
        );
      else if (
        op === "group" &&
        arg &&
        columns.length &&
        Object.keys(arg).every((key) => ["by", "sum"].includes(key))
      )
        columns = [...(names(arg.by) ?? []), ...(names(arg.sum) ?? [])];
      else columns = []; // Unknown transformations invalidate guesses rather than inventing a schema.
    }
    return [...new Set(columns)];
  } catch {
    return [];
  }
}
