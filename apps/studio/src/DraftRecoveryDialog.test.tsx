// @vitest-environment jsdom
import { useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { DraftRecoveryDialog } from "./DraftRecoveryDialog";

afterEach(cleanup);

it.each([
  { saved: "", draft: "  inserted: true\n", changed: "inserted: true" },
  { saved: "  deleted: true\n", draft: "", changed: "deleted: true" },
])("compares a line insertion or deletion far into the file", ({ saved, draft, changed }) => {
  const prefix = "  unchanged: true\n".repeat(400);
  const suffix = "  end: true\n";
  render(
    <DraftRecoveryDialog
      path="workflows/orders.yaml"
      savedSource={prefix + saved + suffix}
      draftSource={prefix + draft + suffix}
      conflict
      onSaveCopy={vi.fn()}
      onReloadDisk={vi.fn()}
      onCancel={vi.fn()}
    />,
  );
  expect(screen.getByText("First difference at line 401")).toBeTruthy();
  expect(screen.getByText("(No matching line)")).toBeTruthy();
  expect(screen.getByText(changed, { exact: true })).toBeTruthy();
  expect(screen.getByRole("region", { name: "Saved version YAML" }).textContent).toBe(prefix + saved + suffix);
  expect(screen.getByRole("region", { name: "Draft version YAML" }).textContent).toBe(prefix + draft + suffix);
  expect(screen.queryByRole("button", { name: "Restore draft" })).toBeNull();
});

it("contains keyboard focus and restores the opener on cancel", async () => {
  const user = userEvent.setup();
  const discard = vi.fn();
  function Harness() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button onClick={() => setOpen(true)}>Open recovery copy</button>
        {open && (
          <DraftRecoveryDialog
            path="Untitled workflow"
            draftSource="name: draft"
            savedSource={null}
            conflict={false}
            onRestore={vi.fn()}
            onDiscard={discard}
            onCancel={() => setOpen(false)}
          />
        )}
      </>
    );
  }
  render(<Harness />);
  const opener = screen.getByRole("button", { name: "Open recovery copy" });
  await user.click(opener);
  expect(document.activeElement).toBe(screen.getByRole("dialog", { name: "Recover workflow draft" }));
  await user.tab({ shift: true });
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Restore draft" }));
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("region", { name: "Draft version YAML" }));
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(document.activeElement).toBe(opener);
  expect(discard).not.toHaveBeenCalled();
});
