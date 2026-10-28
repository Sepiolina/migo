import { useEffect, useRef, useState } from "react";
import {
  readWorkbookSettings,
  validateWorkbookSettings,
  writeWorkbookSettings,
  type WorkbookSettings,
} from "./workbookSettings";
import "./workflow-setup.css";

export function WorkflowSetup({
  source,
  onChange,
  disabled = false,
  onDraftChange,
}: {
  source: string;
  onChange: (source: string) => void;
  disabled?: boolean;
  onDraftChange: (pending: boolean) => void;
}) {
  const [baseline, setBaseline] = useState(() => readWorkbookSettings(source));
  const [draft, setDraft] = useState(() => readWorkbookSettings(source));
  const counter = useRef(0);
  const callbacks = useRef(onDraftChange);
  callbacks.current = onDraftChange;
  useEffect(() => {
    const next = readWorkbookSettings(source);
    setBaseline(next);
    setDraft(next);
    callbacks.current(false);
  }, [source]);
  const pending = JSON.stringify(draft) !== JSON.stringify(baseline);
  const errors = draft ? validateWorkbookSettings(draft) : [];
  const update = (next: WorkbookSettings) => {
    setDraft(next);
    callbacks.current(JSON.stringify(next) !== JSON.stringify(baseline));
  };
  const discard = () => {
    setDraft(baseline);
    callbacks.current(false);
  };
  const apply = () => {
    if (!draft || errors.length || !pending) return;
    const next = writeWorkbookSettings(source, draft);
    setBaseline(draft);
    callbacks.current(false);
    onChange(next);
  };
  return (
    <section className="workflow-setup" aria-label="Workbook setup">
      <h2>Workbook setup</h2>
      {!draft ? (
        <p>
          These workbook settings cannot be edited safely here. The existing
          settings will be preserved; use the advanced editor to change them.
        </p>
      ) : (
        <>
          <p>
            Choose the inputs and output sheets for this workflow. Set file
            paths in Project &amp; inputs.
          </p>
          <fieldset disabled={disabled}>
            <label>
              Workflow display name
              <input
                value={draft.name}
                onChange={(event) =>
                  update({ ...draft, name: event.target.value })
                }
              />
            </label>
            <h3>Inputs</h3>
            {draft.inputs.map((row, index) => (
              <div className="workflow-setup-row" key={row.key}>
                <label>
                  Identifier
                  <input
                    value={row.id}
                    onChange={(event) =>
                      update({
                        ...draft,
                        inputs: draft.inputs.map((item, i) =>
                          i === index
                            ? { ...item, id: event.target.value }
                            : item,
                        ),
                      })
                    }
                  />
                </label>
                <label>
                  Label
                  <input
                    value={row.label}
                    onChange={(event) =>
                      update({
                        ...draft,
                        inputs: draft.inputs.map((item, i) =>
                          i === index
                            ? { ...item, label: event.target.value }
                            : item,
                        ),
                      })
                    }
                  />
                </label>
                <button
                  type="button"
                  onClick={() =>
                    update({
                      ...draft,
                      inputs: draft.inputs.filter((_, i) => i !== index),
                    })
                  }
                  aria-label={`Remove input ${index + 1}`}
                >
                  Remove input
                </button>
              </div>
            ))}
            <button
              type="button"
              onClick={() =>
                update({
                  ...draft,
                  inputs: [
                    ...draft.inputs,
                    {
                      key: `new-input-${++counter.current}`,
                      id: "",
                      label: "",
                    },
                  ],
                })
              }
            >
              Add input
            </button>
            <h3>Output sheets</h3>
            <p>
              Use the saved table name from a Save table block for each output
              sheet.
            </p>
            {draft.outputs.map((row, index) => (
              <div className="workflow-setup-row" key={row.key}>
                <label>
                  Sheet name
                  <input
                    value={row.name}
                    onChange={(event) =>
                      update({
                        ...draft,
                        outputs: draft.outputs.map((item, i) =>
                          i === index
                            ? { ...item, name: event.target.value }
                            : item,
                        ),
                      })
                    }
                  />
                </label>
                <label>
                  Saved table name
                  <input
                    value={row.table}
                    onChange={(event) =>
                      update({
                        ...draft,
                        outputs: draft.outputs.map((item, i) =>
                          i === index
                            ? { ...item, table: event.target.value }
                            : item,
                        ),
                      })
                    }
                  />
                </label>
                <button
                  type="button"
                  onClick={() =>
                    update({
                      ...draft,
                      outputs: draft.outputs.filter((_, i) => i !== index),
                    })
                  }
                  aria-label={`Remove output sheet ${index + 1}`}
                >
                  Remove sheet
                </button>
              </div>
            ))}
            <button
              type="button"
              onClick={() =>
                update({
                  ...draft,
                  outputs: [
                    ...draft.outputs,
                    {
                      key: `new-output-${++counter.current}`,
                      name: "",
                      table: "",
                    },
                  ],
                })
              }
            >
              Add output sheet
            </button>
            <p>
              After renaming or removing input identifiers or table names,
              update any matching blocks yourself.
            </p>
            {pending && errors.length > 0 && (
              <ul className="workflow-setup-errors" role="alert">
                {errors.map((error) => (
                  <li key={error}>{error}</li>
                ))}
              </ul>
            )}
            <div className="workflow-setup-actions">
              <button
                type="button"
                disabled={!pending || errors.length > 0}
                onClick={apply}
              >
                Apply workbook settings
              </button>
              <button type="button" disabled={!pending} onClick={discard}>
                Discard setup changes
              </button>
            </div>
          </fieldset>
        </>
      )}
    </section>
  );
}
