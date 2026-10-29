// @vitest-environment jsdom
import { StrictMode, type PropsWithChildren } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { useDocument } from "./useDocument";

afterEach(cleanup);

const initial = "steps: []\n";
const strict = ({ children }: PropsWithChildren) => (
  <StrictMode>{children}</StrictMode>
);

describe("useDocument", () => {
  it("applies batched transactions to the latest source and undoes each once", () => {
    const { result } = renderHook(() => useDocument(initial), {
      wrapper: strict,
    });
    act(() => {
      result.current.transact("First", (source) => source + "# first\n");
      result.current.transact("Second", (source) => source + "# second\n");
    });
    expect(result.current.source).toBe(initial + "# first\n# second\n");
    expect(result.current.snapshot.revision).toBe(2);
    expect(result.current.dirty).toBe(true);
    act(() => result.current.undo());
    expect(result.current.source).toBe(initial + "# first\n");
    act(() => result.current.undo());
    expect(result.current.source).toBe(initial);
    expect(result.current.dirty).toBe(false);
    expect(result.current.canUndo).toBe(false);
    act(() => {
      result.current.redo();
      result.current.redo();
    });
    expect(result.current.source).toBe(initial + "# first\n# second\n");
    expect(result.current.snapshot.revision).toBe(6);
  });

  it("uses preceding batched edits, undo and load when transforming", () => {
    const { result } = renderHook(() => useDocument(initial));
    act(() => {
      result.current.setSource("steps: [\n");
      result.current.transact("Finish draft", (source) => source + "]\n");
    });
    expect(result.current.source).toBe("steps: [\n]\n");
    expect(result.current.snapshot.cst.errors).toEqual([]);
    act(() => {
      result.current.undo();
      result.current.transact("Replace draft", (source) => source + "  ]\n");
    });
    expect(result.current.source).toBe("steps: [\n  ]\n");
    expect(result.current.canRedo).toBe(false);
    act(() => {
      result.current.load(initial);
      result.current.transact("After load", (source) => source + "# loaded\n");
    });
    expect(result.current.source).toBe(initial + "# loaded\n");
    act(() => result.current.undo());
    expect(result.current.source).toBe(initial);
    expect(result.current.canUndo).toBe(false);
    expect(result.current.dirty).toBe(false);
  });

  it("reports a failed transaction synchronously without committing its transforms", () => {
    const { result } = renderHook(() => useDocument(initial), {
      wrapper: strict,
    });
    act(() => {
      result.current.transact("First", (source) => source + "# first\n");
      expect(() =>
        result.current.transact(
          "Fail",
          (source) => source + "# discarded\n",
          () => {
            throw new Error("Cannot edit");
          },
        ),
      ).toThrow("Cannot edit");
      result.current.transact("Second", (source) => source + "# second\n");
    });
    expect(result.current.source).toBe(initial + "# first\n# second\n");
    expect(result.current.snapshot.revision).toBe(2);
  });

  it("preserves an invalid draft and redo through a no-op transaction", () => {
    const { result } = renderHook(() => useDocument(initial));
    act(() => result.current.setSource("steps: [\n"));
    const invalid = result.current.snapshot;
    expect(invalid.cst.errors.length).toBeGreaterThan(0);
    act(() => result.current.setSource(initial));
    act(() => {
      result.current.undo();
      result.current.transact("No change", (source) => source);
    });
    expect(result.current.source).toBe(invalid.source);
    expect(result.current.snapshot.cst).toBe(invalid.cst);
    expect(result.current.snapshot.revision).toBe(3);
    expect(result.current.canRedo).toBe(true);
    act(() => result.current.redo());
    expect(result.current.source).toBe(initial);
    expect(result.current.dirty).toBe(false);
  });
});
