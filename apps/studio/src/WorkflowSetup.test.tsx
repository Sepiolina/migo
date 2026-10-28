// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { WorkflowSetup } from "./WorkflowSetup";

const source = `name: Report\ninput: {parts: {label: Parts}}\nsteps: []\noutput: {sheets: {Parts: result}}\n`;
describe("WorkbookSetup", () => {
  it("keeps invalid drafts local, supports discard, and applies valid changes", () => {
    const onChange = vi.fn(),
      onDraftChange = vi.fn();
    render(
      <WorkflowSetup
        source={source}
        onChange={onChange}
        onDraftChange={onDraftChange}
      />,
    );
    fireEvent.change(screen.getByLabelText("Identifier"), {
      target: { value: "invalid id" },
    });
    expect(onDraftChange).toHaveBeenLastCalledWith(true);
    expect(
      (
        screen.getByRole("button", {
          name: "Apply workbook settings",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Discard setup changes" }),
    );
    expect(
      (screen.getByLabelText("Identifier") as HTMLInputElement).value,
    ).toBe("parts");
    expect(onDraftChange).toHaveBeenLastCalledWith(false);
    fireEvent.change(screen.getByLabelText("Workflow display name"), {
      target: { value: "New report" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Apply workbook settings" }),
    );
    expect(onChange).toHaveBeenCalledOnce();
    expect(onChange.mock.calls[0][0]).toContain("New report");
    expect(onDraftChange).toHaveBeenLastCalledWith(false);
  });
});
