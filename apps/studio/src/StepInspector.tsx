import { useEffect, useState } from "react";
import { editArgument, stepDetail } from "./authoring";
export function StepInspector({
  source,
  selected,
  onChange,
  onSource,
  disabled = false,
}: {
  disabled?: boolean;
  source: string;
  selected?: string;
  onChange: (source: string) => void;
  onSource: () => void;
}) {
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
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
  useEffect(() => {
    setDraft(argument);
    setError("");
  }, [source, selected, argument]);
  return (
    <section className="step-inspector" aria-label="Step inspector">
      <strong>
        {selected
          ? `Step ${index === undefined ? selected : index + 1}`
          : "Step inspector"}
      </strong>
      {!selected ? (
        <p className="hint">Select a step to edit its argument.</p>
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
            Operation: <b>{detail.operation}</b>
          </p>
          <label>
            Argument (YAML scalar, mapping, or list)
            <textarea
              aria-label="Step argument"
              value={draft}
              disabled={disabled}
              onChange={(event) => setDraft(event.target.value)}
              spellCheck={false}
            />
          </label>
          <button
            disabled={disabled || draft === argument}
            onClick={() => {
              try {
                onChange(editArgument(source, index!, draft));
                setError("");
              } catch (e) {
                setError(String(e));
              }
            }}
          >
            Apply argument
          </button>
          <p className="hint">
            Apply records one undoable change. Use YAML source to change the
            operation.
          </p>
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
