// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { Console } from "./Console";

afterEach(cleanup);

it("counts actionable problems and keeps navigation and retry independent", async () => {
  const onGoTo = vi.fn();
  const retry = vi.fn();
  const props = { log: [], source: "", onSource: vi.fn(), onGoTo, open: "problems" as const, onOpen: vi.fn() };
  const offline = { level: "info" as const, text: "Offline preview uses fallback values." };
  const { rerender } = render(<Console {...props} problems={[offline]} />);

  expect(screen.getByRole("tab", { name: "Problems" })).toBeTruthy();
  expect(screen.getByText("No problems.")).toBeTruthy();
  expect(screen.queryByText(offline.text)).toBeNull();

  rerender(<Console {...props} problems={[
    offline,
    { level: "warning", text: "Sample is missing." },
    { level: "error", text: "Step failed.", code: "TRACE", step: "steps.2", retry, retryLabel: "Retry preview" },
  ]} />);

  expect(screen.getByRole("tab", { name: "Problems 2" })).toBeTruthy();
  expect(screen.getByTitle("TRACE")).toBeTruthy();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: /Step failed/ }));
  expect(onGoTo).toHaveBeenCalledWith("steps.2");
  expect(retry).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Retry preview" }));
  expect(retry).toHaveBeenCalledTimes(1);
  expect(onGoTo).toHaveBeenCalledTimes(1);
});
