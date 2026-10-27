import { useEffect, useState } from "react";
import { editArgument, stepDetail } from "./authoring";
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
  useEffect(() => {
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
              onChange={(event) =>
                onDraftChange(
                  event.target.value === argument
                    ? undefined
                    : event.target.value,
                )
              }
              spellCheck={false}
            />
          </label>
          <button
            disabled={disabled || draft === argument}
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
            Apply argument
          </button>
          {pendingDraft !== undefined && (
            <button
              onClick={() => {
                onDraftChange(undefined);
                setError("");
              }}
            >
              Discard argument draft
            </button>
          )}
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
