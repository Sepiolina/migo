import { describe, expect, it } from "vitest";
import { parse } from "yaml";
import { addOperation } from "./nodeAuthoring";
import { operations } from "./operationCatalog";

const source = `# workflow comment
name: Library append
steps:
  - from: 'parts[Parts]' # source comment
  - if: "num(Quantity) > 0"
    then:
      - save: positive # branch comment
    else:
      - save: other
output: {sheets: {Parts: positive}} # output comment
`;
const defaults: Record<string, unknown> = {
  from: "",
  filter: "true",
  set: {},
  select: {},
  rename: {},
  sort: { by: [] },
  group: { by: [] },
  join: { with: "", on: [] },
  save: "",
};
describe("node library append", () => {
  it.each(operations)(
    "appends $operation with its intended YAML type and preserves the existing workflow",
    (operation) => {
      const appended = addOperation(
        source,
        operation.operation,
        operation.defaultArgument,
      );
      const before = parse(source);
      const after = parse(appended);
      expect(after.steps.slice(0, -1)).toEqual(before.steps);
      expect(after.steps.at(-1)).toEqual({
        [operation.operation]: defaults[operation.operation],
      });
      expect(after.output).toEqual(before.output);
      for (const comment of [
        "workflow comment",
        "source comment",
        "branch comment",
        "output comment",
      ])
        expect(appended).toContain(`# ${comment}`);
      expect(appended).toContain("'parts[Parts]'");
    },
  );
  it("rejects malformed source, malformed defaults, and aliases before appending", () => {
    expect(() => addOperation("steps: [", "save", '""')).toThrow();
    expect(() => addOperation(source, "group", "[")).toThrow();
    expect(() =>
      addOperation("steps: &steps [*steps]", "save", '""'),
    ).toThrow();
    expect(() => addOperation(source, "save", "&loop [*loop]")).toThrow();
  });
});
