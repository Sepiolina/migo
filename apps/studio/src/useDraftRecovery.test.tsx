// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { draftDebounceMs, draftRetentionMs, readDrafts, writeDraft } from "./draftRecovery";
import { useDraftRecovery } from "./useDraftRecovery";

const initial = {
  workspace: "/project",
  workflowPath: "workflows/test.yaml",
  draftId: "",
  baseRevision: "rev1",
  source: "draft",
  savedSource: "disk",
  dirty: true,
  enabled: true,
};

function settle() {
  act(() => vi.advanceTimersByTime(draftDebounceMs));
}

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("draft recovery", () => {
  it("debounces successive edits and recovers a checkpoint after the editor closes", () => {
    const editor = renderHook(useDraftRecovery, { initialProps: initial });
    act(() => vi.advanceTimersByTime(draftDebounceMs - 1));
    expect(editor.result.current.drafts).toEqual([]);
    editor.rerender({ ...initial, source: "newer draft" });
    settle();
    editor.unmount();

    const otherWorkspace = renderHook(useDraftRecovery, { initialProps: { ...initial, workspace: "/elsewhere", dirty: false } });
    expect(otherWorkspace.result.current.drafts).toEqual([]);
    otherWorkspace.rerender({ ...initial, dirty: false });
    expect(otherWorkspace.result.current.drafts).toEqual([
      expect.objectContaining({ version: 1, source: "newer draft", baseRevision: "rev1", savedSource: "disk" }),
    ]);
  });

  it("keeps independent drafts for concurrent editors and removes only the selected record", () => {
    const first = renderHook(useDraftRecovery, { initialProps: initial });
    const second = renderHook(useDraftRecovery, { initialProps: { ...initial, source: "second" } });
    settle();
    expect(first.result.current.drafts.map(draft => draft.source).sort()).toEqual(["draft", "second"]);
    const outdated = first.result.current.drafts.find(draft => draft.source === "second")!;
    second.rerender({ ...initial, source: "second edited again" });
    settle();

    act(() => first.result.current.discard(outdated));
    expect(first.result.current.drafts.map(draft => draft.source).sort()).toEqual(["draft", "second edited again"]);
    act(() => first.result.current.clearCurrent());
    expect(second.result.current.drafts.map(draft => draft.source)).toEqual(["second edited again"]);
  });

  it("expires old checkpoints and preserves the baseline of an untitled template", () => {
    const untitled = { ...initial, workflowPath: "", draftId: "template", baseRevision: null };
    const editor = renderHook(useDraftRecovery, { initialProps: untitled });
    settle();
    expect(editor.result.current.drafts[0]).toMatchObject({
      workflowPath: "", draftId: "template", savedSource: "disk", baseRevision: null,
    });
    const checkpoint = editor.result.current.drafts[0];
    editor.unmount();
    vi.setSystemTime(Date.now() + draftRetentionMs);
    writeDraft(localStorage, { ...checkpoint, workspace: "/other", timestamp: Date.now() });
    expect(readDrafts(localStorage, "/open")).toEqual([]);
    expect(readDrafts(localStorage, initial.workspace)).toEqual([]);
    expect(readDrafts(localStorage, "/other")).toHaveLength(1);
    expect(localStorage.length).toBe(1);
  });

  it("preserves edits made during an in-flight save and updates their base revision immediately", () => {
    const editor = renderHook(useDraftRecovery, { initialProps: initial });
    settle();
    const { clearCurrent } = editor.result.current;
    editor.rerender({ ...initial, source: "written during save" });

    act(() => clearCurrent(initial.source));
    expect(editor.result.current.drafts[0].source).toBe(initial.source);
    editor.rerender({ ...initial, source: "written during save", baseRevision: "rev2", savedSource: initial.source });
    expect(editor.result.current.drafts[0]).toMatchObject({
      source: "written during save", baseRevision: "rev2", savedSource: "draft",
    });
    act(() => clearCurrent(initial.source));
    expect(editor.result.current.drafts).toHaveLength(1);
  });

  it("cancels a pending checkpoint on discard and scopes delayed cleanup to its original slot", () => {
    const editor = renderHook(useDraftRecovery, { initialProps: initial });
    settle();
    const old = editor.result.current;
    editor.rerender({ ...initial, source: "pending edit" });
    act(() => old.clearCurrent());
    settle();
    expect(editor.result.current.drafts).toEqual([]);

    editor.rerender({ ...initial, workflowPath: "another.yaml", source: "another edit" });
    act(() => old.clearCurrent());
    settle();
    expect(editor.result.current.drafts.map(draft => draft.source)).toEqual(["another edit"]);
  });

  it("surfaces an unavailable checkpoint without blocking a later retry", () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementationOnce(() => {
      throw new DOMException("Quota exceeded", "QuotaExceededError");
    });
    const editor = renderHook(useDraftRecovery, { initialProps: initial });
    settle();
    expect(setItem).toHaveBeenCalledOnce();
    expect(editor.result.current.storageUnavailable).toBe(true);
    expect(editor.result.current.drafts).toEqual([]);

    editor.rerender({ ...initial, source: "retry" });
    settle();
    expect(editor.result.current.storageUnavailable).toBe(false);
    expect(editor.result.current.drafts[0].source).toBe("retry");
  });

  it("supersedes an older scheduled checkpoint through a synchronous save handoff", () => {
    const editor = renderHook(useDraftRecovery, { initialProps: initial });
    act(() => editor.result.current.writeCurrent({
      workflowPath: initial.workflowPath, draftId: "", source: "newer", savedSource: "draft", baseRevision: "rev2",
    }));
    settle();
    expect(editor.result.current.drafts[0]).toMatchObject({ source: "newer", baseRevision: "rev2" });
  });
});
