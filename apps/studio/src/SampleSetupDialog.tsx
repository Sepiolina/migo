import { useCallback, useEffect, useId, useRef, useState } from "react";
import { native, request, type Inspection } from "./api";
import { useDialogFocus } from "./useDialogFocus";

type Sheet = string | number;
type Input = { id: string; label: string; sheetRequirements: Sheet[][] };
type Check =
  | { kind: "checking" | "missing" }
  | { kind: "error"; message: string }
  | { kind: "read"; sheets: string[] };

function hasSheet(sheets: string[], sheet: Sheet) {
  if (typeof sheet === "number") return Number.isInteger(sheet) && sheet >= 1 && sheet <= sheets.length;
  return sheets.some((name) => name.trim().toLowerCase() === sheet.trim().toLowerCase());
}

function sheetLabel(sheet: Sheet) {
  return typeof sheet === "number" ? `#${sheet}` : `“${sheet}”`;
}

async function inspectSample(workspace: string, path: string): Promise<Check> {
  try {
    const revisions = await native<Record<string, string | null>>("file_revisions", { paths: [path] });
    if (!Object.hasOwn(revisions, path) || revisions[path] === null) return { kind: "missing" };
    const inspection = await request<Inspection>("inspectWorkbook", { workspace, path, limit: 1 });
    return { kind: "read", sheets: inspection.sheets };
  } catch (error) {
    return { kind: "error", message: error instanceof Error ? error.message : String(error) };
  }
}

function sampleStatus(path: string | undefined, check: Check | undefined, requirements: Sheet[][]) {
  if (!path) return { text: "No sample file selected." };
  if (!check || check.kind === "checking") return { text: "Checking sample file…" };
  if (check.kind === "missing") return { text: "Sample file is missing or inaccessible.", invalid: true };
  if (check.kind === "error") return { text: "Could not read the workbook.", detail: check.message, invalid: true };
  if (check.kind === "read") {
    const missing = requirements.filter((group) => !group.some((sheet) => hasSheet(check.sheets, sheet)));
    if (missing.length) {
      const detail = missing.map((group) => group.map(sheetLabel).join(" or ")).join(", ");
      return { text: `Missing required ${missing.length === 1 ? "sheet" : "sheets"} ${detail}.`, invalid: true };
    }
  }
  return { text: "Ready.", ready: true };
}

export function SampleSetupDialog({ inputs, bindings, workspace, onApply, onClose }: {
  inputs: Input[];
  bindings: Record<string, string>;
  workspace: string;
  onApply: (bindings: Record<string, string>) => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLElement>(null);
  const title = useId();
  useDialogFocus(dialog);
  const [original] = useState(() => ({ inputs, bindings }));
  const [draft, setDraft] = useState(() => ({ ...bindings }));
  const [checks, setChecks] = useState<Record<string, Check>>({});
  const [pickerErrors, setPickerErrors] = useState<Record<string, string>>({});
  const [picking, setPicking] = useState<string | null>(null);
  const pickerOpen = useRef(false);
  const mounted = useRef(false);
  const queue = useRef(Promise.resolve());
  const currentChecks = useRef(new Map<string, symbol>());
  const editTimers = useRef(new Map<string, ReturnType<typeof setTimeout>>());

  const checkPath = useCallback((id: string, path: string) => {
    const attempt = Symbol();
    currentChecks.current.set(id, attempt);
    setChecks((previous) => ({ ...previous, [id]: { kind: "checking" } }));
    queue.current = queue.current.then(async () => {
      if (currentChecks.current.get(id) !== attempt) return;
      const result = await inspectSample(workspace, path);
      if (currentChecks.current.get(id) === attempt) {
        setChecks((previous) => ({ ...previous, [id]: result }));
      }
    });
  }, [workspace]);

  useEffect(() => {
    mounted.current = true;
    for (const { id } of original.inputs) {
      const path = original.bindings[id];
      if (path) checkPath(id, path);
    }
    const attempts = currentChecks.current;
    const timers = editTimers.current;
    return () => {
      mounted.current = false;
      attempts.clear();
      timers.forEach(clearTimeout);
      timers.clear();
    };
  }, [checkPath, original]);

  const flushEdit = (id: string, path: string) => {
    const timer = editTimers.current.get(id);
    if (timer === undefined) return;
    clearTimeout(timer);
    editTimers.current.delete(id);
    if (path) checkPath(id, path);
  };

  const editPath = (id: string, path: string) => {
    setDraft((previous) => ({ ...previous, [id]: path }));
    currentChecks.current.delete(id);
    setChecks((previous) => ({ ...previous, [id]: { kind: "checking" } }));
    setPickerErrors((previous) => ({ ...previous, [id]: "" }));
    clearTimeout(editTimers.current.get(id));
    editTimers.current.set(id, setTimeout(() => flushEdit(id, path), 200));
  };

  const browse = async (id: string) => {
    if (pickerOpen.current) return;
    pickerOpen.current = true;
    setPicking(id);
    setPickerErrors((previous) => ({ ...previous, [id]: "" }));
    try {
      const picked = await native<{ path: string } | null>("choose_sample_file");
      if (!picked || !mounted.current) return;
      clearTimeout(editTimers.current.get(id));
      editTimers.current.delete(id);
      setDraft((previous) => ({ ...previous, [id]: picked.path }));
      checkPath(id, picked.path);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (mounted.current) setPickerErrors((previous) => ({ ...previous, [id]: message }));
    } finally {
      pickerOpen.current = false;
      if (mounted.current) setPicking(null);
    }
  };

  const rows = inputs.map((input) => {
    const path = draft[input.id];
    const check = checks[input.id];
    return { input, path, status: sampleStatus(path, check, input.sheetRequirements), sheets: check?.kind === "read" ? check.sheets : [] };
  });
  const next = rows.find(({ status }) => !status.ready);

  return (
    <div className="dialog-backdrop sample-setup-backdrop">
      <section
        ref={dialog}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title}
        className="dialog sample-setup"
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            onClose();
          }
          if (event.key !== "Tab") return;
          const controls = [...event.currentTarget.querySelectorAll<HTMLElement>("input, button:not(:disabled)")];
          const first = controls[0], last = controls.at(-1);
          if (event.shiftKey && (event.target === first || event.target === event.currentTarget)) {
            event.preventDefault();
            last?.focus();
          } else if (!event.shiftKey && event.target === last) {
            event.preventDefault();
            first?.focus();
          }
        }}
      >
        <h2 id={title}>Set up samples</h2>
        <div className="sample-setup-inputs">
          {rows.map(({ input, path, status, sheets }) => (
            <section className="sample-setup-row" key={input.id} aria-label={input.label || input.id}>
              <div className="sample-setup-source">
                <strong>{input.label || input.id}</strong>
                {input.label && input.label !== input.id && <code>{input.id}</code>}
              </div>
              <input
                className="ctl sample-setup-path"
                aria-label={`Sample path for ${input.label || input.id}`}
                placeholder="Workspace-relative path"
                spellCheck={false}
                value={path ?? ""}
                onChange={(event) => editPath(input.id, event.target.value)}
                onBlur={(event) => flushEdit(input.id, event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") flushEdit(input.id, event.currentTarget.value);
                }}
              />
              <p className={status.invalid ? "sample-setup-invalid" : ""}>
                {status.text}
                {status.detail && <> {status.detail}</>}
              </p>
              {sheets.length > 0 && <p>Available sheets <strong>{sheets.join(", ")}</strong></p>}
              {pickerErrors[input.id] && <p className="sample-setup-invalid" role="alert">{pickerErrors[input.id]}</p>}
              <button
                className="btn"
                aria-disabled={picking !== null}
                onClick={() => void browse(input.id)}
              >
                {picking === input.id ? "Opening file picker…" : status.invalid ? "Locate file…" : "Browse…"}
              </button>
            </section>
          ))}
        </div>
        <p role="status">{next ? `Next input: ${next.input.label || next.input.id}` : "All samples are ready."}</p>
        <div className="actions">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="primary" disabled={!!next || picking !== null} onClick={() => onApply(draft)}>Finish</button>
        </div>
      </section>
    </div>
  );
}
