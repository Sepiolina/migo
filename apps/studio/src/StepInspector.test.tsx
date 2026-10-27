// @vitest-environment jsdom
import { useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it } from "vitest";
import { parse } from "yaml";
import { StepInspector } from "./StepInspector";
afterEach(cleanup);

it("edits a guided setting and preserves additional options when applied", async () => {
  const user = userEvent.setup();
  let result = "";
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={
          "steps:\n  - join: {with: parts, on: [Part], custom: retained}\n"
        }
        selected="step.0"
        draft={draft}
        onDraftChange={setDraft}
        onChange={(source) => {
          result = source;
        }}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  const field = screen.getByDisplayValue("Part");
  await user.clear(field);
  await user.type(field, "SKU, Site");
  await user.click(screen.getByRole("button", { name: "Apply changes" }));
  expect(parse(result).steps[0].join).toEqual({
    with: "parts",
    on: ["SKU", "Site"],
    custom: "retained",
  });
});

it("requires a column before applying a built filter condition", async () => {
  const user = userEvent.setup();
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={'steps:\n  - filter: "true"\n'}
        selected="step.0"
        draft={draft}
        onDraftChange={setDraft}
        onChange={() => {}}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  await user.type(screen.getByLabelText("Value"), "active");
  expect(
    (screen.getByRole("button", { name: "Apply changes" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.type(screen.getByLabelText("Column"), "Status");
  expect(
    (screen.getByRole("button", { name: "Apply changes" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
});

it("lets users choose a sheet index or a numeric-looking sheet name", async () => {
  const user = userEvent.setup();
  let result = "";
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={"steps:\n  - from: {input: parts, sheet: 1}\n"}
        selected="step.0"
        draft={draft}
        onDraftChange={setDraft}
        onChange={(source) => {
          result = source;
        }}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  expect(
    (screen.getByLabelText("Sheet reference") as HTMLSelectElement).value,
  ).toBe("number");
  await user.selectOptions(screen.getByLabelText("Sheet reference"), "name");
  await user.type(screen.getByLabelText("Sheet name"), "1");
  await user.click(screen.getByRole("button", { name: "Apply changes" }));
  expect(parse(result).steps[0].from.sheet).toBe("1");
});

it("rejects integer-like row keys before they can reorder a selection", async () => {
  const user = userEvent.setup();
  let result = "";
  const source = "steps:\n  - select: {First: A, Second: B}\n";
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={source}
        selected="step.0"
        draft={draft}
        onDraftChange={setDraft}
        onChange={(value) => {
          result = value;
        }}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  const key = screen.getByLabelText("New column name 2");
  await user.clear(key);
  await user.type(key, "2");
  expect(
    screen.getByText(
      "Use Advanced YAML for numeric column names to preserve their order.",
    ),
  ).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Apply changes" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(result).toBe("");
  await user.clear(key);
  await user.type(key, "Third");
  await user.click(screen.getByRole("button", { name: "Apply changes" }));
  expect(Object.keys(parse(result).steps[0].select)).toEqual([
    "First",
    "Third",
  ]);
});
