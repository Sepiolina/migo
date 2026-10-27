import "./guided-inspector.css";
import { useEffect, useState } from "react";
import { editArgument, stepDetail } from "./authoring";
import { isIntegerKey, readGuided, updateGuided } from "./guidedArgument";
import { getOperation } from "./operationCatalog";
import { SimpleCondition } from "./SimpleCondition";

type Field = NonNullable<
  NonNullable<ReturnType<typeof readGuided>>["spec"]["fields"]
>[number];

function RowKeyInput({
  name,
  label,
  disabled,
  names,
  onChange,
  onValidityChange,
}: {
  name: string;
  label: string;
  disabled: boolean;
  names: string[];
  onChange: (name: string) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const [text, setText] = useState(name);
  const [error, setError] = useState("");
  return (
    <label>
      {label}
      <input
        aria-label={label}
        value={text}
        disabled={disabled}
        onChange={(event) => {
          const next = event.target.value;
          setText(next);
          const message = !next
            ? "Enter a column name."
            : isIntegerKey(next)
              ? "Use Advanced YAML for numeric column names to preserve their order."
              : next !== name && names.includes(next)
                ? "Column names must be unique."
                : "";
          setError(message);
          onValidityChange(!message);
          if (!message) onChange(next);
        }}
      />
      {error && <small className="inline-error">{error}</small>}
    </label>
  );
}
function RowsEditor({
  label,
  keyLabel,
  valueLabel,
  value,
  disabled,
  onChange,
  onValidityChange,
}: {
  label: string;
  keyLabel: string;
  valueLabel: string;
  value: Record<string, string>;
  disabled: boolean;
  onChange: (value: Record<string, string>) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const entries = Object.entries(value);
  const [invalidRows, setInvalidRows] = useState<number[]>([]);
  const [revision, setRevision] = useState(0);
  const setRowValid = (index: number, valid: boolean) => {
    const next = valid
      ? invalidRows.filter((i) => i !== index)
      : [...new Set([...invalidRows, index])];
    setInvalidRows(next);
    onValidityChange(next.length === 0);
  };
  return (
    <fieldset className="guided-rows">
      <legend>{label}</legend>
      {entries.map(([key, item], index) => (
        <div className="guided-row" key={`${revision}:${index}`}>
          <RowKeyInput
            label={`${keyLabel} ${index + 1}`}
            name={key}
            names={entries.map(([name]) => name)}
            disabled={disabled}
            onValidityChange={(valid) => setRowValid(index, valid)}
            onChange={(next) =>
              onChange(
                Object.fromEntries(
                  entries.map(([k, v], i) => [i === index ? next : k, v]),
                ),
              )
            }
          />
          <label>
            {valueLabel}
            <input
              aria-label={`${valueLabel} ${index + 1}`}
              value={item}
              disabled={disabled}
              onChange={(event) =>
                onChange(
                  Object.fromEntries(
                    entries.map(([k, v], i) => [
                      k,
                      i === index ? event.target.value : v,
                    ]),
                  ),
                )
              }
            />
          </label>
          <button
            type="button"
            disabled={disabled}
            aria-label={`Remove ${label} row ${index + 1}`}
            onClick={() => {
              setInvalidRows([]);
              setRevision((n) => n + 1);
              onValidityChange(true);
              onChange(
                Object.fromEntries(entries.filter((_, i) => i !== index)),
              );
            }}
          >
            Remove
          </button>
        </div>
      ))}
      <button
        type="button"
        disabled={disabled}
        onClick={() => {
          let name = "New column";
          let n = 2;
          while (Object.hasOwn(value, name)) name = `New column ${n++}`;
          onChange({ ...value, [name]: "" });
        }}
      >
        Add row
      </button>
    </fieldset>
  );
}
function ColumnsInput({
  value,
  disabled,
  onChange,
}: {
  value: string[];
  disabled: boolean;
  onChange: (value: string[]) => void;
}) {
  const [text, setText] = useState(value.join(", "));
  return (
    <input
      value={text}
      disabled={disabled}
      onChange={(event) => {
        const next = event.target.value;
        setText(next);
        onChange(
          next
            .split(",")
            .map((part) => part.trim())
            .filter(Boolean),
        );
      }}
    />
  );
}
function SheetInput({
  value,
  disabled,
  onChange,
  onValidityChange,
}: {
  value: unknown;
  disabled: boolean;
  onChange: (value: unknown) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const isNumber = typeof value === "number";
  const [numberText, setNumberText] = useState(isNumber ? String(value) : "1");
  const invalid =
    isNumber &&
    (!/^\d+$/.test(numberText) ||
      !Number.isSafeInteger(Number(numberText)) ||
      Number(numberText) < 1);
  return (
    <div className="sheet-input">
      <label>
        Sheet reference
        <select
          value={isNumber ? "number" : "name"}
          disabled={disabled}
          onChange={(event) => {
            const number = event.target.value === "number";
            setNumberText("1");
            onValidityChange(true);
            onChange(number ? 1 : "");
          }}
        >
          <option value="name">Sheet name</option>
          <option value="number">Sheet number</option>
        </select>
      </label>
      <label>
        {isNumber ? "Sheet number (starting at 1)" : "Sheet name"}
        <input
          type={isNumber ? "number" : "text"}
          min={isNumber ? 1 : undefined}
          step={isNumber ? 1 : undefined}
          value={isNumber ? numberText : ((value as string | undefined) ?? "")}
          disabled={disabled}
          onChange={(event) => {
            if (!isNumber) onChange(event.target.value);
            else {
              const text = event.target.value;
              const n = Number(text);
              const valid =
                /^\d+$/.test(text) && Number.isSafeInteger(n) && n > 0;
              setNumberText(text);
              onValidityChange(valid);
              if (valid) onChange(n);
            }
          }}
        />
      </label>
      {invalid && (
        <p className="inline-error">Enter a sheet number of 1 or greater.</p>
      )}
    </div>
  );
}
function GuidedField({
  field,
  value,
  disabled,
  onChange,
  onValidityChange,
}: {
  field: Field;
  value: unknown;
  disabled: boolean;
  onChange: (value: unknown) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  if (field.kind === "sheet")
    return (
      <SheetInput
        value={value}
        disabled={disabled}
        onChange={onChange}
        onValidityChange={onValidityChange}
      />
    );
  if (field.kind === "rows")
    return (
      <RowsEditor
        label={field.label}
        keyLabel={field.keyLabel!}
        valueLabel={field.valueLabel!}
        value={(value ?? {}) as Record<string, string>}
        disabled={disabled}
        onChange={onChange}
        onValidityChange={onValidityChange}
      />
    );
  if (field.kind === "boolean")
    return (
      <label className="guided-toggle">
        <input
          type="checkbox"
          checked={value === true}
          disabled={disabled}
          onChange={(event) => onChange(event.target.checked)}
        />{" "}
        {field.label}
      </label>
    );
  return (
    <label>
      {field.label}
      {field.kind === "enum" ? (
        <select
          value={(value as string | undefined) ?? ""}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value || undefined)}
        >
          <option value="">Default</option>
          {field.options?.map((option) => (
            <option key={option} value={option}>
              {option === "left"
                ? "All current rows (left)"
                : option === "inner"
                  ? "Matching rows only (inner)"
                  : option}
            </option>
          ))}
        </select>
      ) : field.kind === "columns" ? (
        <ColumnsInput
          value={(value ?? []) as string[]}
          disabled={disabled}
          onChange={onChange}
        />
      ) : (
        <input
          value={(value as string | undefined) ?? ""}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
      {field.help && <small className="hint">{field.help}</small>}
    </label>
  );
}
export function StepInspector({
  source,
  selected,
  onChange,
  onSource,
  disabled = false,
  draft: pendingDraft,
  onDraftChange,
}: {
  disabled?: boolean;
  draft?: string;
  onDraftChange: (draft: string | undefined) => void;
  source: string;
  selected?: string;
  onChange: (source: string) => void;
  onSource: () => void;
}) {
  const [error, setError] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [validity, setValidity] = useState<Record<string, boolean>>({});
  const valid = Object.values(validity).every(Boolean);
  const setFieldValid = (key: string, value: boolean) => {
    setValidity((previous) => ({ ...previous, [key]: value }));
    // Invalid local field edits still count as unsaved work in the shell.
    if (!value && pendingDraft === undefined) onDraftChange(argument);
  };
  const [revision, setRevision] = useState(0);
  const index =
    selected && /^step\.\d+$/.test(selected)
      ? Number(selected.split(".")[1])
      : undefined;
  let detail;
  try {
    if (index !== undefined) detail = stepDetail(source, index);
  } catch {
    /* Source error appears on canvas. */
  }
  const argument = detail?.argument ?? "";
  const draft = pendingDraft ?? argument;
  const guided = detail && readGuided(detail.operation, draft);
  useEffect(() => {
    setError("");
  }, [source, selected, argument]);
  useEffect(() => {
    setAdvanced(false);
    setValidity({});
  }, [selected]);
  const changeDraft = (next: string) =>
    onDraftChange(next === argument ? undefined : next);
  const changeGuided = (key: string | null, value: unknown) => {
    try {
      changeDraft(updateGuided(draft, detail!.operation, key, value));
      setError("");
    } catch (e) {
      setError(String(e));
    }
  };
  return (
    <section className="step-inspector" aria-label="Step inspector">
      <strong>
        {selected
          ? `Step ${index === undefined ? selected : index + 1}`
          : "Step inspector"}
      </strong>
      {!selected ? (
        <p className="hint">Select a step to edit its settings.</p>
      ) : !detail?.editable ? (
        <>
          <p className="hint">
            Nested, shorthand, and compound steps can be edited in YAML source.
            Their branches stay intact.
          </p>
          <button onClick={onSource}>Edit YAML source</button>
        </>
      ) : (
        <>
          <p>
            <b>{getOperation(detail.operation).title}</b>
          </p>
          <div className="inspector-tabs" aria-label="Settings view">
            <button
              type="button"
              aria-pressed={!advanced}
              onClick={() => setAdvanced(false)}
            >
              Settings
            </button>
            <button
              type="button"
              aria-pressed={advanced}
              onClick={() => {
                setAdvanced(true);
                setValidity({});
              }}
            >
              Advanced YAML
            </button>
          </div>
          {!advanced && guided ? (
            <div
              className="guided-settings"
              key={`${selected}:${argument}:${revision}`}
            >
              {detail.operation === "filter" &&
              typeof guided.value === "string" ? (
                <SimpleCondition
                  expression={guided.value}
                  disabled={disabled}
                  onChange={(value) => changeGuided(null, value)}
                  onValidityChange={(value) =>
                    setFieldValid("condition", value)
                  }
                />
              ) : typeof guided.value === "string" && guided.spec.scalar ? (
                <label>
                  {guided.spec.scalar.label}
                  <input
                    value={guided.value}
                    disabled={disabled}
                    onChange={(event) => changeGuided(null, event.target.value)}
                  />
                  <small className="hint">{guided.spec.scalar.help}</small>
                </label>
              ) : guided.spec.rootRows ? (
                <RowsEditor
                  label={guided.spec.rootRows.label}
                  keyLabel={guided.spec.rootRows.keyLabel!}
                  valueLabel={guided.spec.rootRows.valueLabel!}
                  value={guided.value as Record<string, string>}
                  disabled={disabled}
                  onChange={(value) => changeGuided(null, value)}
                  onValidityChange={(value) => setFieldValid("root", value)}
                />
              ) : (
                guided.spec.fields?.map((field) => (
                  <GuidedField
                    key={field.key}
                    field={field}
                    value={(guided.value as Record<string, unknown>)[field.key]}
                    disabled={disabled}
                    onChange={(value) => changeGuided(field.key, value)}
                    onValidityChange={(value) =>
                      setFieldValid(field.key, value)
                    }
                  />
                ))
              )}
              {guided.spec.fields && (
                <p className="hint">
                  Other existing options are preserved. Use Advanced YAML to
                  edit them.
                </p>
              )}
            </div>
          ) : !advanced ? (
            <>
              <p className="hint">
                These settings need the Advanced YAML editor.
              </p>
              <button type="button" onClick={() => setAdvanced(true)}>
                Open Advanced YAML
              </button>
            </>
          ) : (
            <label>
              Step argument
              <textarea
                aria-label="Step argument"
                value={draft}
                disabled={disabled}
                onChange={(event) => changeDraft(event.target.value)}
                spellCheck={false}
              />
            </label>
          )}
          <button
            disabled={disabled || !valid || draft === argument}
            onClick={() => {
              try {
                onChange(editArgument(source, index!, draft));
                onDraftChange(undefined);
                setError("");
              } catch (e) {
                setError(String(e));
              }
            }}
          >
            Apply changes
          </button>
          {pendingDraft !== undefined && (
            <button
              onClick={() => {
                onDraftChange(undefined);
                setError("");
                setValidity({});
                setRevision((n) => n + 1);
              }}
            >
              Discard changes
            </button>
          )}
          <p className="hint">Apply records one undoable change.</p>
          {error && (
            <p role="alert" className="inline-error">
              {error}
            </p>
          )}
        </>
      )}
    </section>
  );
}
