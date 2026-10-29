// @vitest-environment jsdom
import { useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { DiscardChangesDialog } from "./DiscardChangesDialog";
afterEach(cleanup);
it("contains keyboard focus, cancels with Escape, and restores the opener without discarding", async () => {
  const user = userEvent.setup();
  const discard = vi.fn();
  function Harness() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button onClick={() => setOpen(true)}>Open other workflow</button>
        {open && (
          <DiscardChangesDialog
            onCancel={() => setOpen(false)}
            onDiscard={discard}
          />
        )}
      </>
    );
  }
  render(<Harness />);
  await user.click(screen.getByText("Open other workflow"));
  expect(document.activeElement).toBe(screen.getByText("Keep editing"));
  await user.tab({ shift: true });
  expect(document.activeElement).toBe(
    screen.getByText("Discard changes and continue"),
  );
  await user.tab();
  expect(document.activeElement).toBe(screen.getByText("Keep editing"));
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(document.activeElement).toBe(screen.getByText("Open other workflow"));
  expect(discard).not.toHaveBeenCalled();
});
