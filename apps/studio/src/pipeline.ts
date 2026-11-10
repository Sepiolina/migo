/**
 * The workflow model behind the editor, and every edit it makes.
 *
 * A workflow file is read into sources (inputs), a tree of steps and outputs.
 * Forms are generated from the step registry (stepRegistry.ts): readForm turns
 * a step's YAML into field values, writeField puts one field back. Edits work
 * on the YAML tree and keep the original text of everything they did not
 * touch (yamlSplice.ts), so a one-field change is a one-line diff, comments
 * included. Files with CRLF line endings keep them.
 */
import {
  Document,
  isMap,
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
  itemByType,
  stepByOp,
  type Field,
  type ItemSpec,
  type StepSpec,
} from "./stepRegistry";
import { preserveText } from "./yamlSplice";

export type Path = (string | number)[];
export const writeOptions = { lineWidth: 0, flowCollectionPadding: false } as const;

// ---- document plumbing ---------------------------------------------------------------

const lf = (source: string) => source.replace(/\r\n/g, "\n");
function open(source: string): Document.Parsed {
  const doc = parseDocument(lf(source), { keepSourceTokens: true, uniqueKeys: true });
  if (doc.errors.length) throw new Error(`The YAML has an error: ${doc.errors[0].message}`);
  return doc;
}
/** Print an edited document, keeping the text of unchanged parts. */
function finish(source: string, doc: Document.Parsed, commentsChanged = false): string {
  const text = preserveText(lf(source), doc.toString(writeOptions), commentsChanged);
  return source.includes("\r\n") ? text.replace(/\n/g, "\r\n") : text;
}
/** Text typed into a text box comes back with LF; keep the file's own line endings. */
export function keepLineEndings(previous: string, next: string): string {
  return previous.includes("\r\n") ? next.replace(/\r?\n/g, "\r\n") : next;
}
/** Apply an edit to the YAML tree and return the new source. */
export function edit(source: string, change: (doc: Document.Parsed) => void, commentsChanged = false): string {
  const doc = open(source);
  change(doc);
  return finish(source, doc, commentsChanged);
}
function nodeAt(doc: Document.Parsed, path: Path): unknown {
  return path.length ? doc.getIn(path, true) : doc.contents;
}
const str = (v: unknown): string | null =>
  isScalar(v) && typeof v.value === "string" ? v.value : null;
const js = (v: unknown): unknown => (v && typeof (v as Node).toJSON === "function" ? (v as Node).toJSON() : v ?? undefined);

/** A column as an expression: its bare name when that is an identifier. */
export function columnRef(name: string): string {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(name) &&
    !["true", "false", "nil", "index", "size", "group", "rows", "in", "not", "and", "or", "matches", "contains"].includes(name)
    ? name
    : `col(${JSON.stringify(name)})`;
}

// ---- the model -------------------------------------------------------------------------

export interface StepItem {
  /** "steps.3" or "steps.17.then.0": stable while the structure is unchanged. */
  id: string;
  path: Path;
  /** Where the step's settings live: the step map for if, else the argument. */
  argPath: Path;
  op: string;
  spec?: StepSpec;
  summary: string;
  depth: number;
  /** The top-level step this one belongs to (trace results are per top-level step). */
  top: number;
  branch?: string;
  /** The list holding this step and its index in it. */
  listPath: Path;
  index: number;
  /** The YAML comment above the step, used as its note. */
  note?: string;
}
export interface InputItem { id: string; label: string; match: string }
export interface OutputItem { index: number; name: string; from: string; detailed: boolean }
export interface Pipeline {
  name: string;
  inputs: InputItem[];
  steps: StepItem[];
  outputs: OutputItem[];
  /** output.sheets is the short {Sheet: dataset} form. */
  sheetsShort: boolean;
  format: string;
  service: Record<string, unknown>;
  /** Datasets saved by steps, in file order. */
  datasets: string[];
}

function stepOf(node: unknown): { op: string; arg: unknown } | null {
  if (!isMap(node)) return isScalar(node) ? { op: String(node.value), arg: undefined } : null;
  if (node.has("if")) return { op: "if", arg: node };
  if (node.items.length !== 1 || str(node.items[0].key) === null) return null;
  return { op: str(node.items[0].key)!, arg: node.items[0].value ?? undefined };
}

export function readPipeline(source: string): Pipeline {
  const doc = open(source);
  const root = doc.contents;
  if (!isMap(root)) throw new Error("A workflow is a YAML mapping with name, input, steps and output.");
  const input = root.get("input", true);
  const inputs: InputItem[] = isMap(input)
    ? input.items.map((p) => {
        const v = js(p.value) as Record<string, unknown> | undefined;
        return { id: String(js(p.key)), label: String(v?.label ?? js(p.key)), match: String(v?.match ?? "") };
      })
    : [];
  const steps: StepItem[] = [];
  const datasets: string[] = [];
  const walk = (seq: unknown, listPath: Path, depth: number, top: number | null, branch?: string) => {
    if (!isSeq(seq)) return;
    seq.items.forEach((node, index) => {
      const path = [...listPath, index];
      const s = stepOf(node);
      const op = s?.op ?? "step";
      const t = top ?? index;
      const argPath = op === "if" ? path : [...path, op];
      const arg = s ? js(s.arg) : undefined;
      if (op === "save" && typeof arg === "string") datasets.push(arg);
      if ((op === "sql" || op === "rest") && arg && typeof arg === "object") {
        const save = (arg as Record<string, unknown>).save;
        if (typeof save === "string") datasets.push(save);
      }
      // The comment above the first item belongs to the list itself.
      const comment = ((node as Node | undefined)?.commentBefore ?? (index === 0 ? (seq as YAMLSeq).commentBefore : undefined))?.trim();
      steps.push({
        id: path.join("."),
        path,
        argPath,
        op,
        spec: stepByOp.get(op),
        summary: summarize(op, arg),
        depth,
        top: t,
        branch,
        listPath,
        index,
        note: comment ? comment.split("\n").map((l) => l.replace(/^\s*-*\s*/, "").replace(/-+\s*$/, "").trim()).filter(Boolean).join("\n") : undefined,
      });
      for (const key of ["then", "else"]) {
        const nested = op === "if" ? [...path, key] : op === "split" ? [...path, "split", key] : null;
        if (nested) walk(doc.getIn(nested, true), nested, depth + 1, t, key);
      }
    });
  };
  walk(root.get("steps", true), ["steps"], 0, null);
  const sheets = doc.getIn(["output", "sheets"], true);
  const outputs: OutputItem[] = isSeq(sheets)
    ? sheets.items.map((n, index) => {
        const v = js(n) as Record<string, unknown>;
        return { index, name: String(v?.name ?? ""), from: String(v?.from ?? v?.like ?? ""), detailed: Object.keys(v ?? {}).some((k) => !["name", "from"].includes(k)) };
      })
    : isMap(sheets)
      ? sheets.items.map((p, index) => ({ index, name: String(js(p.key)), from: String(js(p.value)), detailed: false }))
      : [];
  const service = (js(root.get("service", true)) ?? {}) as Record<string, unknown>;
  return {
    name: String(js(root.get("name", true)) ?? ""),
    inputs,
    steps,
    outputs,
    sheetsShort: isMap(sheets),
    format: String(js(doc.getIn(["output", "format"], true)) ?? "xlsx"),
    service: typeof service === "object" ? service : {},
    datasets: [...new Set(datasets)],
  };
}

// ---- summaries: each step as a short sentence ------------------------------------------

const names = (v: unknown) => (Array.isArray(v) ? v.join(", ") : v == null ? "" : String(v));
const clip = (s: string, n = 64) => (s.length > n ? s.slice(0, n - 1) + "…" : s);
const keys = (v: unknown) => (v && typeof v === "object" && !Array.isArray(v) ? Object.keys(v) : []);
const countOf = (list: string[], noun: string) =>
  list.length <= 3 ? list.join(", ") : `${list.slice(0, 2).join(", ")} and ${list.length - 2} more ${noun}`;
export function summarize(op: string, arg: unknown): string {
  const a = (arg ?? {}) as Record<string, any>;
  switch (op) {
    case "from": {
      if (typeof arg === "string") return `Read ${arg.replace(/\[(.*)\]$/, " › $1")}`;
      const sheet = a.sheet != null ? ` › ${typeof a.sheet === "number" ? `sheet ${a.sheet}` : a.sheet}` : "";
      const how = a.header ? " · header found by title" : a.columns ? " · fixed columns" : "";
      return `Read ${a.input ?? ""}${sheet}${how}`;
    }
    case "save": return `Save as ${arg}`;
    case "filter": return `Keep rows where ${clip(String(arg ?? ""))}`;
    case "set": return `Set ${countOf(keys(arg), "columns")}`;
    case "select": return `Keep columns ${countOf(keys(arg), "columns")}`;
    case "rename": return typeof arg === "string" ? `Rename columns by ${clip(arg)}` : `Rename ${keys(arg).map((k) => `${k} → ${a[k]}`).join(", ")}`;
    case "fill": return `Fill blanks down in ${countOf(Array.isArray(arg) ? arg : [], "columns")}`;
    case "drop": return `Remove ${countOf(Array.isArray(arg) ? arg : [], "columns")}`;
    case "dedupe": return `Remove duplicate ${names(Array.isArray(arg) ? arg : a.by)}${a.keep === "last" ? ", keep last" : ""}`;
    case "sort": return `Sort by ${names(a.by)}${a.desc ? ", largest first" : ""}`;
    case "group": {
      const aggs = keys(arg).filter((k) => k !== "by").map((k) => `${k} ${names(a[k])}`);
      return `Group by ${names(a.by) || "everything"}${aggs.length ? ` → ${aggs.join(", ")}` : ""}`;
    }
    case "window": return `Per ${names(a.by) || "table"}: set ${countOf(keys(a.set), "columns")}`;
    case "pick": return `Pick one per ${names(a.by) || "table"} → ${a.mark ?? ""} (${(a.rules ?? []).length} rules)`;
    case "join": return `Look up ${a.with ?? ""} on ${names(a.on)}${a.take ? ` → ${keys(a.take).join(", ")}` : ""}`;
    case "append": return `Append ${names(arg)}`;
    case "stack": return `Stack ${names(a.columns)} into ${a.into ?? ""}`;
    case "compare": return `Compare with ${a.with ?? ""} by ${names(a.by)}`;
    case "if": return `If ${clip(String(a.if ?? ""))}`;
    case "split": return `Split rows where ${clip(String(a.where ?? ""))}`;
    case "log": return `Log “${clip(typeof arg === "string" ? arg : String(a.message ?? ""), 48)}”`;
    case "fail": return `Stop if ${clip(typeof arg === "string" ? "always" : String(a.when ?? "always"), 48)}`;
    case "sql": return `SQL → ${a.save ?? ""}`;
    case "rest": return `${a.method ?? "GET"} ${clip(String(a.url ?? ""), 40)} → ${a.save ?? ""}`;
  }
  return stepByOp.get(op)?.title ?? op;
}

// ---- forms -----------------------------------------------------------------------------

/** A form: registry fields plus where the argument may be a short form. */
export interface FormSpec {
  fields: Field[];
  scalar?: string;
  list?: string;
  entriesOnly?: boolean;
  op?: string;
}
export type Row = [key: string, value: string];
export type FormValue = string | boolean | Row[] | number;

export function stepForm(spec: StepSpec): FormSpec {
  return { fields: spec.fields, scalar: spec.scalar, list: spec.list, entriesOnly: spec.form === "entries", op: spec.op };
}
export function itemForm(type: string): FormSpec {
  const item = itemByType.get(type) as ItemSpec;
  return { fields: item.fields ?? [] };
}

function getKey(map: unknown, key: string): unknown {
  let node: unknown = map;
  for (const part of key.split(".")) {
    if (!isMap(node)) return undefined;
    node = node.get(part, true);
  }
  return node ?? undefined;
}
/** The argument as a mapping view: short forms become their mapping. */
function view(arg: unknown, form: FormSpec): unknown {
  if (form.op === "from" && str(arg) !== null) {
    const m = str(arg)!.match(/^([^[\]]+?)(?:\[([^[\]]+)\])?$/);
    const map = new YAMLMap();
    map.set("input", new Scalar(m?.[1] ?? str(arg)));
    if (m?.[2]) map.set("sheet", new Scalar(m[2]));
    return map;
  }
  if (form.scalar && isScalar(arg)) {
    const map = new YAMLMap();
    map.set(form.scalar, arg);
    return map;
  }
  if (form.list && isSeq(arg)) {
    const map = new YAMLMap();
    map.set(form.list, arg);
    return map;
  }
  return arg;
}
function flowText(node: unknown): string {
  return parseDocument(JSON.stringify(js(node)))
    .toString({ ...writeOptions, collectionStyle: "flow" })
    .trim();
}
function valueText(f: Field, node: unknown): string {
  if (node === undefined) return "";
  if (f.kind === "list") return isSeq(node) ? node.items.map((x) => String(js(x))).join(", ") : String(js(node) ?? "");
  if (f.kind === "yaml") return flowText(node);
  const v = js(node);
  return v == null ? "" : typeof v === "object" ? flowText(node) : String(v);
}

/** Read every field of a form. The shorthand field of a scalar argument has key "". */
export function readForm(source: string, argPath: Path, form: FormSpec): Record<string, FormValue> {
  const doc = open(source);
  const arg = nodeAt(doc, argPath);
  const out: Record<string, FormValue> = {};
  const m = view(arg, form);
  const claimed = new Set(form.fields.filter((f) => f.key).map((f) => f.key.split(".")[0]));
  for (const f of form.fields) {
    const node = f.key === "" ? (f.kind === "entries" ? (f.rest ? undefined : arg) : arg) : getKey(m, f.key);
    switch (f.kind) {
      case "bool":
        out[f.name] = isScalar(node) && node.value === true;
        break;
      case "enum":
        out[f.name] = f.virtual
          ? isScalar(getKey(m, "sheet")) && typeof (getKey(m, "sheet") as Scalar).value === "number" ? "number" : "name"
          : node === undefined ? f.fallback : String(js(node));
        break;
      case "entries": {
        const item = itemByType.get(f.item)!;
        const valueField = item.entryValue!;
        const pairs = f.rest
          ? (isMap(m) ? m.items.filter((p) => !claimed.has(String(js(p.key)))) : [])
          : isMap(node) ? node.items : [];
        out[f.name] = pairs.map((p) => [String(js(p.key)), valueText(valueField, p.value ?? undefined)] as Row);
        break;
      }
      case "items":
      case "steps":
        out[f.name] = isSeq(node) ? node.items.length : 0;
        break;
      default:
        out[f.name] = valueText(f, node);
    }
  }
  return out;
}

function scalarNode(value: unknown, before: unknown): Node {
  if (isScalar(before) && before.value === value) return before;
  if (isScalar(before) && typeof before.value === typeof value) {
    const copy = before.clone() as Scalar;
    copy.value = value;
    return copy;
  }
  return new Scalar(value);
}
function listNode(doc: Document.Parsed, text: string, before: unknown): Node | undefined {
  const parts = text.split(",").map((s) => s.trim()).filter(Boolean);
  if (!parts.length) return undefined;
  if (isSeq(before) && JSON.stringify(js(before)) === JSON.stringify(parts)) return before;
  const seq = doc.createNode(parts) as YAMLSeq;
  seq.flow = true;
  return seq;
}
/** The node for one field value, or undefined to remove the key. */
function fieldNode(doc: Document.Parsed, f: Field, value: FormValue, before: unknown): Node | undefined {
  switch (f.kind) {
    case "bool":
      return value ? scalarNode(true, before) : before !== undefined ? scalarNode(false, before) : undefined;
    case "enum":
      if (value === f.fallback && before === undefined) return undefined;
      return scalarNode(f.boolean ? value === "true" : value, before);
    case "number": {
      const text = String(value).trim();
      if (!text) return undefined;
      const n = Number(text);
      if (!Number.isFinite(n)) throw new Error(`${f.label || "This field"} needs a number.`);
      return scalarNode(n, before);
    }
    case "list":
      return listNode(doc, String(value), before);
    case "yaml": {
      const text = String(value).trim();
      if (!text) return undefined;
      const parsed = parseDocument(text);
      if (parsed.errors.length) throw new Error(`Not valid YAML: ${parsed.errors[0].message}`);
      if (before !== undefined && JSON.stringify(js(before)) === JSON.stringify(parsed.toJSON())) return before as Node;
      const node = doc.createNode(parsed.toJSON());
      if (isMap(node) || isSeq(node)) node.flow = true;
      return node as Node;
    }
    case "entries": {
      const item = itemByType.get(f.item)!;
      const rows = (value as Row[]).filter(([k]) => k.trim() !== "");
      if (!rows.length) return undefined;
      const seen = new Set<string>();
      const map = new YAMLMap();
      map.flow = isMap(before) ? before.flow : rows.length <= 3 && rows.every(([, v]) => v.length < 30);
      if (isMap(before)) {
        map.commentBefore = before.commentBefore;
        map.comment = before.comment;
      }
      for (const [k, v] of rows) {
        if (seen.has(k)) throw new Error(`“${k}” appears twice.`);
        seen.add(k);
        const old = isMap(before) ? before.items.find((p) => String(js(p.key)) === k) : undefined;
        const valueNode =
          item.entryValue!.kind === "list"
            ? listNode(doc, v, old?.value) ?? new YAMLSeq()
            : old && valueText(item.entryValue!, old.value ?? undefined) === v
              ? (old.value as Node)
              : new Scalar(item.entryValue!.kind === "number" ? Number(v) : v);
        map.items.push(old && old.value === valueNode ? old : new Pair(old?.key ?? new Scalar(k), valueNode));
      }
      return map;
    }
    default: {
      const text = String(value);
      if (text === "") return isScalar(before) && before.value === "" && !f.optional ? before : undefined;
      if (f.name === "SHEET" && /^\d+$/.test(text) && isScalar(before) && typeof before.value === "number")
        return scalarNode(Number(text), before);
      return scalarNode(text, before);
    }
  }
}
function setKey(doc: Document.Parsed, map: YAMLMap, key: string, node: Node | undefined) {
  const [head, ...rest] = key.split(".");
  if (rest.length) {
    let child: unknown = map.get(head, true);
    if (!isMap(child)) {
      if (node === undefined) return;
      child = new YAMLMap();
      map.set(head, child);
    }
    setKey(doc, child as YAMLMap, rest.join("."), node);
    if (!(child as YAMLMap).items.length) map.delete(head);
    return;
  }
  if (node === undefined) {
    map.delete(head);
    return;
  }
  const pair = map.items.find((p) => String(js(p.key)) === head);
  if (pair) {
    if (pair.value !== node && isScalar(node) && isScalar(pair.value)) {
      node.commentBefore ??= (pair.value as Node).commentBefore;
      node.comment ??= (pair.value as Node).comment;
    }
    pair.value = node;
  } else map.items.push(new Pair(new Scalar(head), node));
}

/** Write one field of a form and return the new source. */
export function writeField(source: string, argPath: Path, form: FormSpec, field: Field, value: FormValue): string {
  return edit(source, (doc) => {
    const arg = nodeAt(doc, argPath);
    const replaceArg = (node: Node | undefined) => doc.setIn(argPath, node ?? null);
    // A whole-argument field: save: name, filter: cond, fill: [..], set: {..}
    if (field.key === "" && !(field.kind === "entries" && field.rest)) {
      const node = fieldNode(doc, field, value, arg);
      if (node === undefined) throw new Error(`${field.label || "This field"} cannot be empty.`);
      replaceArg(node);
      return;
    }
    const wasShort = !isMap(arg);
    let map = view(arg, form);
    if (!isMap(map)) map = new YAMLMap();
    else if (map === arg) map = arg as YAMLMap;
    const m = map as YAMLMap;
    if (form.op === "from" && field.name === "SHEET_KIND") {
      const sheet = m.get("sheet", true);
      const text = sheet === undefined ? "" : String(js(sheet));
      if (value === "number" && !/^\d+$/.test(text)) throw new Error("Enter a sheet number first, then choose number.");
      setKey(doc, m, "sheet", text ? new Scalar(value === "number" ? Number(text) : text) : undefined);
    } else if (field.kind === "entries" && field.rest) {
      const claimed = new Set(form.fields.filter((f) => f.key).map((f) => f.key.split(".")[0]));
      const rows = value as Row[];
      m.items = m.items.filter((p) => claimed.has(String(js(p.key))) || rows.some(([k]) => k === String(js(p.key))));
      const item = itemByType.get(field.item)!;
      for (const [k, v] of rows) if (k) setKey(doc, m, k, listNode(doc, v, m.get(k, true)) ?? fieldNode(doc, item.entryValue!, v, m.get(k, true)));
    } else {
      setKey(doc, m, field.key, fieldNode(doc, field, value, getKey(m, field.key)));
    }
    // Keep or restore the short form when only its field is left.
    const only = m.items.map((p) => String(js(p.key)));
    if (wasShort || arg === undefined) {
      if (form.op === "from" && only.every((k) => k === "input" || k === "sheet") && typeof js(m.get("sheet")) !== "number" && (arg === undefined || str(arg) !== null)) {
        const sheet = js(m.get("sheet"));
        replaceArg(new Scalar(`${js(m.get("input"))}${sheet ? `[${sheet}]` : ""}`));
        return;
      }
      if (form.scalar && only.length === 1 && only[0] === form.scalar && (arg === undefined || isScalar(arg))) {
        replaceArg(m.get(form.scalar, true) as Node);
        return;
      }
      if (form.list && only.length === 1 && only[0] === form.list && (arg === undefined || isSeq(arg))) {
        replaceArg(m.get(form.list, true) as Node);
        return;
      }
      if (!only.some((k) => { const v: unknown = m.get(k, true); return isMap(v) || (isSeq(v) && !v.flow); })) m.flow = true;
    }
    if (m !== arg) replaceArg(m);
  });
}

// ---- structure: steps, items, inputs, outputs ----------------------------------------------

/** A step from JS: small arguments are written inline, like hand-written files. */
export function stepNode(doc: Document.Parsed, step: Record<string, unknown>): Node {
  const node = doc.createNode(step) as Node;
  const flow = (n: unknown) => {
    if (isMap(n) || isSeq(n)) {
      const text = JSON.stringify(js(n));
      if (text.length <= 70) n.flow = true;
      else (n.items as unknown[]).forEach((x) => flow(isMap(n) ? (x as Pair).value : x));
    }
  };
  if (isMap(node)) node.items.forEach((p) => flow(p.value));
  return node;
}
export function insertStep(source: string, listPath: Path, index: number, step: Record<string, unknown>): string {
  return edit(source, (doc) => {
    let seq = nodeAt(doc, listPath);
    if (!isSeq(seq)) {
      seq = new YAMLSeq();
      doc.setIn(listPath, seq);
    }
    (seq as YAMLSeq).items.splice(index, 0, stepNode(doc, step));
  });
}
export function deleteAt(source: string, path: Path): string {
  return edit(source, (doc) => {
    const seq = nodeAt(doc, path.slice(0, -1));
    if (!isSeq(seq)) throw new Error("Nothing to delete here.");
    seq.items.splice(Number(path.at(-1)), 1);
  });
}
export function moveAt(source: string, path: Path, delta: number): string {
  return edit(source, (doc) => {
    const seq = nodeAt(doc, path.slice(0, -1));
    const i = Number(path.at(-1)), j = i + delta;
    if (!isSeq(seq) || j < 0 || j >= seq.items.length) return;
    const [item] = seq.items.splice(i, 1);
    seq.items.splice(j, 0, item);
  });
}
export function duplicateAt(source: string, path: Path): string {
  return edit(source, (doc) => {
    const seq = nodeAt(doc, path.slice(0, -1));
    const i = Number(path.at(-1));
    if (!isSeq(seq)) return;
    const copy = (seq.items[i] as Node).clone() as Node;
    copy.commentBefore = undefined;
    copy.spaceBefore = undefined;
    seq.items.splice(i + 1, 0, copy);
  });
}
/** Set (or with "" remove) the YAML comment above a step: its note. */
export function setNote(source: string, path: Path, note: string): string {
  return edit(source, (doc) => {
    const node = nodeAt(doc, path) as Node | undefined;
    if (!node) return;
    const text = note.trim() ? note.trim().split("\n").map((l) => ` ${l}`).join("\n") : undefined;
    const seq = nodeAt(doc, path.slice(0, -1)) as YAMLSeq;
    if (Number(path.at(-1)) === 0 && !node.commentBefore && isSeq(seq) && seq.commentBefore !== undefined) seq.commentBefore = text;
    else node.commentBefore = text;
  }, true);
}
export function addItem(source: string, listPath: Path, item: Record<string, unknown>): string {
  return edit(source, (doc) => {
    let seq = nodeAt(doc, listPath);
    if (!isSeq(seq)) {
      seq = new YAMLSeq();
      doc.setIn(listPath, seq);
    }
    const node = doc.createNode(item) as YAMLMap;
    node.flow = JSON.stringify(item).length <= 70;
    (seq as YAMLSeq).items.push(node);
  });
}
/** Set a value anywhere (undefined or "" removes it). */
export function setValue(source: string, path: Path, value: unknown): string {
  return edit(source, (doc) => {
    if (value === undefined || value === "") {
      doc.deleteIn(path);
      return;
    }
    const before = doc.getIn(path, true);
    if (isScalar(before) && before.value === value) return;
    if (isScalar(before) && typeof value !== "object") {
      const copy = before.clone() as Scalar;
      copy.value = value;
      doc.setIn(path, copy);
    } else doc.setIn(path, doc.createNode(value));
  });
}

/** The YAML text of one node, for display. */
export function yamlOf(source: string, path: Path): string {
  const doc = open(source);
  const node = nodeAt(doc, path);
  if (node === undefined) return "";
  return new Document(js(node)).toString({ ...writeOptions, flowCollectionPadding: false }).trim();
}
/** Rename a short-form output sheet or change its dataset. */
export function setShortSheet(source: string, index: number, name: string, from: string): string {
  return edit(source, (doc) => {
    const sheets = doc.getIn(["output", "sheets"], true);
    if (!isMap(sheets)) return;
    const pair = sheets.items[index];
    if (String(js(pair.key)) !== name) pair.key = new Scalar(name);
    if (String(js(pair.value)) !== from) pair.value = new Scalar(from);
  });
}

/** Inputs: add, rename (with the steps that read it) and remove. */
export function addInput(source: string, id: string, label: string): string {
  return edit(source, (doc) => {
    let input = doc.get("input", true);
    if (!isMap(input)) {
      input = new YAMLMap();
      doc.set("input", input);
    }
    if ((input as YAMLMap).has(id)) throw new Error(`An input named ${id} already exists.`);
    const node = doc.createNode({ label }) as YAMLMap;
    node.flow = true;
    (input as YAMLMap).set(id, node);
  });
}
export function renameInput(source: string, from: string, to: string): string {
  if (!/^[A-Za-z_][A-Za-z0-9_-]*$/.test(to)) throw new Error("Use letters, digits, - and _ for an input name, starting with a letter.");
  const p = readPipeline(source);
  if (p.inputs.some((i) => i.id === to)) throw new Error(`An input named ${to} already exists.`);
  let next = edit(source, (doc) => {
    const input = doc.get("input", true) as YAMLMap;
    const pair = input.items.find((x) => String(js(x.key)) === from);
    if (pair) pair.key = new Scalar(to);
  });
  for (const s of p.steps.filter((s) => s.op === "from")) {
    const arg = readForm(next, s.argPath, stepForm(s.spec!));
    if (arg.SOURCE === from) next = writeField(next, s.argPath, stepForm(s.spec!), s.spec!.fields[0], to);
  }
  return next;
}
export function removeInput(source: string, id: string): string {
  return edit(source, (doc) => {
    const input = doc.get("input", true);
    if (isMap(input)) input.delete(id);
  });
}
/** Outputs are edited in list form; the short {Sheet: dataset} form is expanded first. */
export function expandSheets(source: string): string {
  return edit(source, (doc) => {
    const sheets = doc.getIn(["output", "sheets"], true);
    if (!isMap(sheets)) return;
    const seq = new YAMLSeq();
    for (const p of sheets.items) {
      const m = doc.createNode({ name: js(p.key), from: js(p.value) }) as YAMLMap;
      m.flow = true;
      seq.items.push(m);
    }
    doc.setIn(["output", "sheets"], seq);
  });
}
