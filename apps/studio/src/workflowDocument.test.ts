import { describe, expect, it } from "vitest";
import { isMap } from "yaml";
import { changeSteps, editArgument } from "./authoring";
import {
  createWorkflowDocument,
  sourceTransaction,
  workflowDocumentTransition as transition,
} from "./workflowDocument";

const source =
  '# header\nsteps:\n  - filter: "true" # keep\n  - save: Result\n';

describe("workflow document commands", () => {
  it("owns an exact source and source-token CST snapshot", () => {
    const state = createWorkflowDocument(source);
    expect(state.present.source).toBe(source);
    expect(state.present.revision).toBe(0);
    expect(state.present.cst.errors).toEqual([]);
    expect(isMap(state.present.cst.contents)).toBe(true);
    if (isMap(state.present.cst.contents)) {
      expect(state.present.cst.contents.srcToken).toBeDefined();
    }
    const edited = transition(
      state,
      sourceTransaction(source, "Change filter", (draft) =>
        editArgument(draft, 0, '"false"'),
      ),
    );
    expect(edited.present.cst.toJS().steps[0].filter).toBe("false");
    expect(edited.present.cst.toString()).toBe(edited.present.source);
    expect(state.present.cst.toJS().steps[0].filter).toBe("true");
    expect(edited.present.revision).toBe(1);
  });

  it("ignores a no-op command without discarding redo or advancing revision", () => {
    const initial = createWorkflowDocument(source);
    const edited = transition(initial, {
      type: "edit-source",
      source: "steps: []\n",
    });
    const undone = transition(edited, { type: "undo" });
    const noop = sourceTransaction(source, "Move first up", (draft) =>
      changeSteps(draft, "up", 0),
    );
    expect(transition(undone, noop)).toBe(undone);
    expect(transition(initial, { type: "undo" })).toBe(initial);
    expect(transition(initial, { type: "redo" })).toBe(initial);
  });

  it("groups transforms into one undo entry and restores exact bytes", () => {
    const initial = createWorkflowDocument(source);
    const command = sourceTransaction(
      source,
      "Edit and move",
      (draft) => editArgument(draft, 0, '"false"'),
      (draft) => changeSteps(draft, "down", 0),
    );
    const edited = transition(initial, command);
    expect(edited.past).toHaveLength(1);
    expect(edited.present.cst.toJS().steps[1].filter).toBe("false");
    const undone = transition(edited, { type: "undo" });
    expect(undone.present.source).toBe(source);
    expect(undone.present.cst).toBe(initial.present.cst);
    expect(undone.present.revision).toBe(2);
    const redone = transition(undone, { type: "redo" });
    expect(redone.present.source).toBe(command.source);
    expect(redone.present.cst).toBe(edited.present.cst);
    expect(redone.present.revision).toBe(3);
    expect(
      transition(undone, { type: "edit-source", source: "steps: []" }).future,
    ).toEqual([]);
  });

  it("retains invalid YAML drafts and their errors through undo and redo", () => {
    const initial = createWorkflowDocument(source);
    const draft = "steps: [\n";
    const invalid = transition(initial, { type: "edit-source", source: draft });
    expect(invalid.present.source).toBe(draft);
    expect(invalid.present.cst.errors.length).toBeGreaterThan(0);
    expect(initial.present.cst.errors).toEqual([]);
    const undone = transition(invalid, { type: "undo" });
    expect(undone.present.source).toBe(source);
    expect(undone.present.cst.errors).toEqual([]);
    expect(transition(undone, { type: "redo" }).present.source).toBe(draft);
  });

  it("does not commit any part of a failed transaction", () => {
    const state = createWorkflowDocument(source);
    expect(() =>
      sourceTransaction(
        source,
        "Invalid edit",
        (draft) => changeSteps(draft, "add"),
        (draft) => editArgument(draft, 0, "["),
      ),
    ).toThrow();
    expect(state.present.source).toBe(source);
    expect(state.present.cst.toJS().steps).toHaveLength(2);
    expect(state.past).toEqual([]);
  });

  it("bounds history at 200 changes and resets saved source with a load", () => {
    let state = createWorkflowDocument(source);
    for (let index = 0; index < 205; index++) {
      state = transition(state, {
        type: "edit-source",
        source: `steps: [] # ${index}\n`,
      });
    }
    expect(state.past).toHaveLength(200);
    expect(state.past[0].source).toBe("steps: [] # 4\n");
    state = transition(state, {
      type: "mark-saved",
      source: state.present.source,
    });
    expect(state.savedSource).toBe(state.present.source);
    const revision = state.present.revision;
    state = transition(state, { type: "load", source });
    expect(state.past).toEqual([]);
    expect(state.future).toEqual([]);
    expect(state.savedSource).toBe(source);
    expect(state.present.revision).toBe(revision + 1);
  });
});
