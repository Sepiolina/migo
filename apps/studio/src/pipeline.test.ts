import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import {
  addInput,
  deleteAt,
  duplicateAt,
  expandSheets,
  insertStep,
  itemForm,
  moveAt,
  readForm,
  readPipeline,
  renameInput,
  setNote,
  stepForm,
  writeField,
} from "./pipeline";
import { itemByType, stepByOp, steps as registry } from "./stepRegistry";

const bom = readFileSync("../../workflows/bom.yaml", "utf8");
const changed = (a: string, b: string) => {
  const x = a.split("\n"), y = b.split("\n");
  return y.filter((l, i) => l !== x[i]).length + Math.abs(x.length - y.length);
};
const field = (op: string, name: string) => stepByOp.get(op)!.fields.find((f) => f.name === name)!;

describe("pipeline model", () => {
  it("reads every BOM step with a registry spec, summary and section note", () => {
    const p = readPipeline(bom);
    expect(p.name).toBe("BOM");
    expect(p.inputs.map((i) => i.id)).toEqual(["bom", "priority"]);
    expect(p.outputs.map((o) => o.name)).toEqual(["BOM", "debug"]);
    expect(p.steps.every((s) => s.spec)).toBe(true);
    expect(p.steps.filter((s) => s.depth > 0).length).toBeGreaterThan(10);
    expect(p.steps[0].note).toContain("read the customer BOM");
    expect(p.steps[0].summary).toBe("Read bom · header found by title");
    expect(p.datasets).toContain("priorities");
    for (const s of p.steps) expect(() => readForm(bom, s.argPath, stepForm(s.spec!))).not.toThrow();
  });
  it("reads forms: short forms, entries, nested keys and aggregates", () => {
    const p = readPipeline(bom);
    const read = (i: number) => readForm(bom, p.steps[i].argPath, stepForm(p.steps[i].spec!));
    expect(read(0).HEADER_COLUMNS).toContainEqual(["Level", "level, lvl, item level"]);
    expect(read(0)["HEADER.SEARCH"]).toBe("15");
    expect(read(4).NAME).toBe("parts");
    const log = p.steps.findIndex((s) => s.op === "log" && s.summary.includes("Loaded"));
    expect(read(log).MESSAGE).toBe("Loaded {len(rows)} Priority entries.");
    const g = readPipeline("name: x\nsteps:\n  - group: {by: [K], sum: [Q], first: [D]}\n");
    expect(readForm("steps:\n  - group: {by: [K], sum: [Q], first: [D]}\n", g.steps[0].argPath, stepForm(g.steps[0].spec!)).AGGREGATES).toEqual([["sum", "Q"], ["first", "D"]]);
  });
  it("writes one field with a one-line diff, keeping comments and block scalars", () => {
    const p = readPipeline(bom);
    const dedupe = p.steps.find((s) => s.op === "dedupe" && s.summary.includes("keep last"))!;
    const out = writeField(bom, dedupe.argPath, stepForm(dedupe.spec!), field("dedupe", "KEEP"), "first");
    expect(changed(bom, out)).toBe(1);
    expect(out).toContain("- dedupe: {by: [Key], keep: first}");
    const set = p.steps.find((s) => s.op === "set" && s.summary.includes("Brand"))!;
    const rows = readForm(bom, set.argPath, stepForm(set.spec!)).ENTRIES as [string, string][];
    const next = rows.map(([k, v]) => [k, k === "Desc" ? v.replace("norm(", "trim(") : v] as [string, string]);
    const out2 = writeField(bom, set.argPath, stepForm(set.spec!), set.spec!.fields[0], next);
    expect(changed(bom, out2)).toBe(1);
    expect(out2).toContain("# Brands that are banned unless the row is allowed:");
  });
  it("keeps and restores short forms", () => {
    const src = "steps:\n  - from: parts[Parts]\n  - log: hello\n  - save: out\n";
    const p = readPipeline(src);
    let out = writeField(src, p.steps[0].argPath, stepForm(p.steps[0].spec!), field("from", "SHEET"), "Other");
    expect(parse(out).steps[0]).toEqual({ from: "parts[Other]" });
    out = writeField(out, p.steps[0].argPath, stepForm(p.steps[0].spec!), field("from", "TRIM"), true);
    expect(parse(out).steps[0]).toEqual({ from: { input: "parts", sheet: "Other", trim: true } });
    out = writeField(out, p.steps[1].argPath, stepForm(p.steps[1].spec!), field("log", "WHEN"), "len(rows) > 0");
    expect(parse(out).steps[1]).toEqual({ log: { message: "hello", when: "len(rows) > 0" } });
    out = writeField(out, p.steps[1].argPath, stepForm(p.steps[1].spec!), field("log", "WHEN"), "");
    expect(parse(out).steps[1]).toEqual({ log: { message: "hello" } });
    out = writeField(out, p.steps[2].argPath, stepForm(p.steps[2].spec!), field("save", "NAME"), "result");
    expect(parse(out).steps[2]).toEqual({ save: "result" });
    expect(() => writeField(out, p.steps[2].argPath, stepForm(p.steps[2].spec!), field("save", "NAME"), "")).toThrow(/empty/);
  });
  it("edits group aggregates, nested items and the service section", () => {
    const src = "name: x\nsteps:\n  - group: {by: [K], sum: [Q]}\n  - pick: {by: [G], mark: M, rules: [{rows: 'true'}]}\n";
    const p = readPipeline(src);
    let out = writeField(src, p.steps[0].argPath, stepForm(p.steps[0].spec!), field("group", "AGGREGATES"), [["sum", "Q"], ["count", "ID"]]);
    expect(parse(out).steps[0].group).toEqual({ by: ["K"], sum: ["Q"], count: ["ID"] });
    const rule = itemByType.get("mai_pick_rule")!.fields!.find((f) => f.key === "reason")!;
    out = writeField(out, [...p.steps[1].argPath, "rules", 0], itemForm("mai_pick_rule"), rule, "Only one");
    expect(parse(out).steps[1].pick.rules[0]).toEqual({ rows: "true", reason: "Only one" });
    const route = itemByType.get("mai_service")!.fields!.find((f) => f.key === "route")!;
    out = writeField(out, ["service"], itemForm("mai_service"), route, "orders-v2");
    expect(parse(out).service).toEqual({ route: "orders-v2" });
  });
  it("inserts, moves, duplicates, deletes and annotates steps", () => {
    const src = "name: x\ninput: {parts: {}}\nsteps:\n  # read\n  - from: parts\n  - save: out\noutput: {sheets: {Out: out}}\n";
    let out = insertStep(src, ["steps"], 1, { sort: { by: ["Qty"], desc: true } });
    expect(out).toContain("  - sort: {by: [Qty], desc: true}\n");
    out = moveAt(out, ["steps", 1], -1);
    expect(parse(out).steps.map((s: object) => Object.keys(s)[0])).toEqual(["sort", "from", "save"]);
    out = duplicateAt(out, ["steps", 0]);
    out = deleteAt(out, ["steps", 0]);
    expect(parse(out).steps).toHaveLength(3);
    out = setNote(out, ["steps", 2], "Keep the result");
    expect(out).toContain("# Keep the result");
    expect(out).toContain("# read");
  });
  it("renames inputs with the steps that read them, and expands short sheets", () => {
    const src = "name: x\ninput: {parts: {label: Parts}}\nsteps:\n  - from: parts[Sheet1]\n  - save: out\noutput: {sheets: {Out: out}}\n";
    let out = renameInput(addInput(src, "prices", "Prices"), "parts", "orders");
    expect(parse(out).input).toEqual({ orders: { label: "Parts" }, prices: { label: "Prices" } });
    expect(parse(out).steps[0]).toEqual({ from: "orders[Sheet1]" });
    out = expandSheets(out);
    expect(parse(out).output.sheets).toEqual([{ name: "Out", from: "out" }]);
  });
  it("round-trips every registry example through its form", () => {
    for (const spec of registry) {
      const src = spec.op === "if" ? "steps:\n  - if: 'true'\n    then: []\n" : `steps:\n  - ${spec.op}: ${spec.example}\n`;
      const p = readPipeline(src);
      const values = readForm(src, p.steps[0].argPath, stepForm(spec));
      let out = src;
      for (const f of spec.fields) {
        if (f.kind === "items" || f.kind === "steps" || f.virtual) continue;
        out = writeField(out, p.steps[0].argPath, stepForm(spec), f, values[f.name]);
      }
      expect(parse(out), spec.op).toEqual(parse(src));
    }
  });
  it("keeps CRLF line endings", () => {
    const crlf = bom.replace(/\r?\n/g, "\r\n");
    const p = readPipeline(crlf);
    const out = writeField(crlf, p.steps[4].argPath, stepForm(p.steps[4].spec!), field("save", "NAME"), "parts2");
    expect(out.replace(/\r\n/g, "").includes("\n")).toBe(false);
    expect(changed(crlf.replace(/\r\n/g, "\n"), out.replace(/\r\n/g, "\n"))).toBe(1);
  });
});
