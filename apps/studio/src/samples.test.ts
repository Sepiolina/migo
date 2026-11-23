import { describe, expect, it } from "vitest";
import { assignSamples, matcher } from "./samples";

const bom = { id: "bom", label: "Customer BOM", match: "" };
const priority = { id: "priority", label: "Priority", match: "(?i)prio|shortage" };
const inputs = [bom, priority];
const f = (name: string) => ({ name, path: `samples/${name}`, copied: true });

describe("samples", () => {
  it("reads Go-style patterns", () => {
    expect(matcher("(?i)prio")?.test("Priority_1.xlsx")).toBe(true);
    expect(matcher("prio")?.test("Priority_1.xlsx")).toBe(false);
    expect(matcher("")).toBeNull();
    expect(matcher("(")).toBeNull();
  });

  it("assigns both files of one import by name, in any order", () => {
    expect(assignSamples(inputs, {}, [f("Priority_1.xlsx"), f("1342824_B00.xls")])).toEqual({
      bind: { priority: "samples/Priority_1.xlsx", bom: "samples/1342824_B00.xls" },
      unassigned: [],
    });
  });

  it("gives an unmatched file to the chosen source, else to one without a sample", () => {
    expect(assignSamples(inputs, { bom: "a.xlsx", priority: "b.xlsx" }, [f("x.xlsx")], "priority").bind).toEqual({ priority: "samples/x.xlsx" });
    expect(assignSamples(inputs, { bom: "a.xlsx" }, [f("x.xlsx")]).bind).toEqual({ priority: "samples/x.xlsx" });
    // all bound, nothing chosen: the source without a pattern
    expect(assignSamples(inputs, { bom: "a.xlsx", priority: "b.xlsx" }, [f("x.xlsx")]).bind).toEqual({ bom: "samples/x.xlsx" });
  });

  it("never puts a file named for one source on another, and skips failed imports", () => {
    expect(assignSamples(inputs, {}, [f("prio_a.xlsx"), f("prio_b.xlsx"), { name: "bad.txt", error: "nope" }])).toEqual({
      bind: { priority: "samples/prio_a.xlsx" },
      unassigned: ["prio_b.xlsx"],
    });
  });
});
