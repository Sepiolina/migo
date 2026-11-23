// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useFileRevisions } from "./useFileRevisions";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("ignores a delayed fingerprint after switching workspaces with identical relative paths", async () => {
  type Files = Record<string, string>;
  const calls: ((files: Files) => void)[] = [];
  vi.stubGlobal("__maiBridge", {
    invoke: () => new Promise<Files>((resolve) => calls.push(resolve)),
  });
  const { result, rerender } = renderHook(
    (workspace) => useFileRevisions(workspace, ["sample.xlsx"]),
    { initialProps: "/first" },
  );
  await waitFor(() => expect(calls).toHaveLength(1));
  rerender("/second");
  await waitFor(() => expect(calls).toHaveLength(2));
  await act(async () => calls[1]({ "sample.xlsx": "second" }));
  await act(async () => calls[0]({ "sample.xlsx": "first" }));
  expect(result.current.revisions).toEqual({ "sample.xlsx": "second" });

  act(() => { window.dispatchEvent(new Event("focus")); });
  await waitFor(() => expect(calls).toHaveLength(3));
  await act(async () => calls[2]({ "sample.xlsx": "updated" }));
  expect(result.current.revisions).toEqual({ "sample.xlsx": "updated" });
});
