import { useRef } from "react";
import { useDialogFocus } from "./useDialogFocus";

export function SaveWorkflowDialog({ path, route, returnFocusTo, onChange, onClose, onSave }: {
  path: string;
  route: string;
  returnFocusTo?: HTMLElement | null;
  onChange: (path: string) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const dialog = useRef<HTMLFormElement>(null);
  const input = useRef<HTMLInputElement>(null);
  useDialogFocus(dialog, input, returnFocusTo);

  return (
    <div className="scrim" onMouseDown={(e) => {
      if (e.target !== e.currentTarget) return;
      e.preventDefault();
      onClose();
    }}>
      <form
        ref={dialog}
        className="dialog"
        role="dialog"
        aria-modal="true"
        aria-label="Save workflow"
        onSubmit={(e) => { e.preventDefault(); onSave(); }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) e.preventDefault();
          if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onClose();
          }
          if (e.key !== "Tab") return;
          const buttons = [...e.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")];
          const last = buttons.at(-1);
          if (e.shiftKey && e.target === input.current) {
            e.preventDefault();
            last?.focus();
          } else if (!e.shiftKey && e.target === last) {
            e.preventDefault();
            input.current?.focus();
          }
        }}
      >
        <h2>Save workflow</h2>
        <p>Files in <code>workflows/</code> become API endpoints. The file name is the route unless the service settings name another.</p>
        <input ref={input} className="ctl" aria-label="File path" value={path} onChange={(e) => onChange(e.target.value)} />
        <div className="hint">Serves <code>/api/{route}</code></div>
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className="primary" disabled={!/\.ya?ml$/.test(path)}>Save</button>
        </div>
      </form>
    </div>
  );
}
