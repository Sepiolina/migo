// @vitest-environment jsdom
import { useState } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it } from "vitest";
import { parse } from "yaml";
import { StepInspector } from "./StepInspector";
afterEach(cleanup);

it("retains invalid local edits when another setting is changed back to its saved value", async () => {
  const user = userEvent.setup();
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={"steps:\n  - from: {input: parts, sheet: 1, trim: false}\n"}
        selected="step.0"
        draft={draft}
        onDraftChange={setDraft}
        onChange={() => {}}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  fireEvent.change(screen.getByLabelText("Sheet number (starting at 1)"), {
    target: { value: "" },
  });
  await user.click(screen.getByLabelText("Trim whitespace"));
  await user.click(screen.getByLabelText("Trim whitespace"));
  expect(
    screen.getByText("Enter a sheet number of 1 or greater."),
  ).toBeTruthy();
  expect(screen.getByRole("button", { name: "Discard changes" })).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Apply changes" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.change(screen.getByLabelText("Sheet number (starting at 1)"), {
    target: { value: "1" },
  });
  expect(screen.queryByRole("button", { name: "Discard changes" })).toBeNull();
});

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
  expect(screen.getByRole("button", { name: "Discard changes" })).toBeTruthy();
  await user.clear(key);
  await user.type(key, "Third");
  await user.click(screen.getByRole("button", { name: "Apply changes" }));
  expect(Object.keys(parse(result).steps[0].select)).toEqual([
    "First",
    "Third",
  ]);
});

it("shows a suggested column in the field and applies the same configuration", async () => {
  const user = userEvent.setup();
  let saved = "";
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={"steps:\n  - group: {by: [Customer], sum: [Quantity]}\n"}
        selected="step.0"
        columnSuggestions={["Customer", "Quantity", "Line total"]}
        draft={draft}
        onDraftChange={setDraft}
        onChange={(value) => {
          saved = value;
        }}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  await user.click(screen.getAllByRole("button", { name: "+ Line total" })[1]);
  expect(screen.getByDisplayValue("Quantity, Line total")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Apply changes" }));
  expect(parse(saved).steps[0].group.sum).toEqual(["Quantity", "Line total"]);
});

it("does not offer column names the comma-separated editor cannot represent", async () => {
  const user = userEvent.setup();
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={"steps:\n  - group: {by: [Customer], sum: [Quantity]}\n"}
        selected="step.0"
        columnSuggestions={["Last, First", "Quantity"]}
        draft={draft}
        onDraftChange={setDraft}
        onChange={() => {}}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  expect(screen.queryByRole("button", { name: "+ Last, First" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Advanced YAML" }));
  expect(screen.getByLabelText("Step argument")).toBeTruthy();
});

it("protects an invalid row name from view changes and removal of a different row", async () => {
  const user = userEvent.setup();
  function Harness() {
    const [draft, setDraft] = useState<string>();
    return (
      <StepInspector
        source={"steps:\n  - select: {First: A, Second: B}\n"}
        selected="step.0"
        draft={draft}
        onDraftChange={setDraft}
        onChange={() => {}}
        onSource={() => {}}
      />
    );
  }
  render(<Harness />);
  const key = screen.getByLabelText("New column name 2") as HTMLInputElement;
  await user.clear(key);
  expect(
    (screen.getByRole("button", { name: "Advanced YAML" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(
    (
      screen.getByRole("button", {
        name: "Remove Columns to keep row 1",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(key.value).toBe("");
  await user.type(key, "Third");
  expect(
    (screen.getByRole("button", { name: "Advanced YAML" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
});
