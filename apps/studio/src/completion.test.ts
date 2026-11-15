import { describe, expect, it } from "vitest";
import { suggest } from "./completion";

const columns = ["Quantity", "Unit price", "Qty"];
const at = (text: string) => suggest(text, text.length, columns);

describe("formula completion", () => {
  it("offers columns and helpers for the word being typed", () => {
    expect(at("num(Q")).toEqual({
      from: 4,
      list: [
        { label: "Quantity", insert: "Quantity", detail: "column" },
        { label: "Qty", insert: "Qty", detail: "column" },
      ],
    });
    expect(at("to")!.list[0]).toMatchObject({ label: "toNumber(v, d)", insert: "toNumber(" });
  });

  it("writes a column that is not an identifier as col(...)", () => {
    expect(at("uni")!.list[0].insert).toBe('col("Unit price")');
  });

  it("stays quiet in text, after a dot and for a finished word", () => {
    expect(at('Status == "Q')).toBeNull();
    expect(at("first(group).Q")).toBeNull();
    expect(at("Qty")).toBeNull();
    expect(at("zzz")).toBeNull();
    expect(at("1 + ")).toBeNull();
  });

  it("completes in the middle of a formula", () => {
    expect(suggest("bl + 1", 2, columns)).toMatchObject({ from: 0, list: [{ insert: "blank(" }] });
  });
});
