import { describe, expect, it } from "vitest";
import { parseDocument } from "yaml";
import {
  changeSteps,
  editArgument,
  historyTransition,
  stepDetail,
} from "./authoring";
const fixture = `# workflow header
name: Demo
steps:
  # source comment
  - from: "parts[Parts]" # input comment
  - group:
      by: [ID] # key comment
      sum: [Quantity]
  - if: num(Quantity) > 0
    then:
      - save: yes # branch comment
    else:
      - save: no
output: {sheets: {Parts: yes}} # output comment
`;
const value = (source: string) => parseDocument(source).toJS();
describe("workflow authoring", () => {
  it("edits scalar arguments and preserves unrelated comments, quoted style, and branches", () => {
    const edited = editArgument(fixture, 0, '"other[Sheet]"');
    expect(value(edited).steps[0].from).toBe("other[Sheet]");
    for (const comment of [
      "workflow header",
      "source comment",
      "input comment",
      "key comment",
      "branch comment",
      "output comment",
    ])
      expect(edited).toContain(`# ${comment}`);
    expect(value(edited).steps[2]).toEqual(value(fixture).steps[2]);
  });
  it("provides structured YAML with comments and preserves them when applying", () => {
    const detail = stepDetail(fixture, 1);
    expect(detail.argument).toContain("# key comment");
    const edited = editArgument(
      fixture,
      1,
      detail.argument.replace("Quantity", "Amount"),
    );
    expect(value(edited).steps[1].group.sum).toEqual(["Amount"]);
    expect(edited).toContain("# key comment");
    expect(value(edited).output).toEqual(value(fixture).output);
  });
  it("returns quoted scalars as valid YAML arguments without changing their types", () => {
    const source = 'steps:\n - filter: "true"\n';
    expect(
      value(editArgument(source, 0, stepDetail(source, 0).argument)).steps[0]
        .filter,
    ).toBe("true");
  });
  it("uses replacement scalar types instead of inheriting explicit tags", () => {
    const integer = editArgument(
      "steps:\n - filter: !!int 2 # keep comment\n",
      0,
      "foo",
    );
    expect(value(integer).steps[0].filter).toBe("foo");
    expect(integer).not.toContain("!!int");
    expect(integer).toContain("# keep comment");
    const string = editArgument("steps:\n - filter: !!str 2\n", 0, "2");
    expect(value(string).steps[0].filter).toBe(2);
    expect(string).not.toContain("!!str");
    expect(
      value(editArgument("steps:\n - filter: 1\n", 0, "!!str 2")).steps[0]
        .filter,
    ).toBe("2");
  });
  it("moves whole step nodes with their branches and comments, then adds and deletes", () => {
    const moved = changeSteps(fixture, "up", 2);
    expect(value(moved).steps[1]).toEqual(value(fixture).steps[2]);
    expect(moved).toContain("# branch comment");
    const added = changeSteps(moved, "add");
    expect(value(added).steps).toHaveLength(4);
    expect(value(changeSteps(added, "delete", 3))).toEqual(value(moved));
  });
  it("requires source editing for compound branches and rejects malformed edits", () => {
    expect(stepDetail(fixture, 2).editable).toBe(false);
    expect(() => editArgument(fixture, 2, "true")).toThrow(/compound/);
    expect(() => editArgument(fixture, 0, "[")).toThrow();
    expect(() => changeSteps("steps: [", "add")).toThrow();
  });
  it("undoes and redoes exact source bytes and discards redo on a new edit", () => {
    let history = {
      past: [] as string[],
      present: fixture,
      future: [] as string[],
    };
    const changed = changeSteps(fixture, "up", 2);
    history = historyTransition(history, { type: "edit", source: changed });
    history = historyTransition(history, { type: "undo" });
    expect(history.present).toBe(fixture);
    history = historyTransition(history, { type: "redo" });
    expect(history.present).toBe(changed);
    history = historyTransition(historyTransition(history, { type: "undo" }), {
      type: "edit",
      source: "new",
    });
    expect(history.future).toEqual([]);
    expect(
      historyTransition(history, { type: "reset", source: fixture }),
    ).toEqual({ past: [], present: fixture, future: [] });
  });
});
