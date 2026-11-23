import { useId, useMemo, useRef } from "react";
import { useDialogFocus } from "./useDialogFocus";

function firstChangedLines(saved: string, draft: string) {
  const savedLines = saved ? saved.split(/\r?\n/) : [];
  const draftLines = draft ? draft.split(/\r?\n/) : [];
  let start = 0;
  while (start < savedLines.length && start < draftLines.length && savedLines[start] === draftLines[start]) start++;
  let savedEnd = savedLines.length;
  let draftEnd = draftLines.length;
  if (start === savedEnd && start === draftEnd) return null;
  // Exclude the unchanged tail so an inserted or deleted line leaves its counterpart empty.
  while (savedEnd > start && draftEnd > start && savedLines[savedEnd - 1] === draftLines[draftEnd - 1]) {
    savedEnd--;
    draftEnd--;
  }
  return {
    number: start + 1,
    saved: start < savedEnd ? savedLines[start] : null,
    draft: start < draftEnd ? draftLines[start] : null,
  };
}

export function DraftRecoveryDialog({
  path,
  draftSource,
  savedSource,
  conflict,
  onRestore,
  onOpenSaved,
  onDiscard,
  onSaveCopy,
  onReloadDisk,
  onCancel,
  busy = false,
  error,
  updatedAt,
}: {
  path: string;
  draftSource: string;
  savedSource: string | null;
  conflict: boolean;
  onRestore?: () => void;
  onOpenSaved?: () => void;
  onDiscard?: () => void;
  onSaveCopy?: () => void;
  onReloadDisk?: () => void;
  onCancel: () => void;
  busy?: boolean;
  error?: string;
  updatedAt?: string | number;
}) {
  const dialog = useRef<HTMLElement>(null);
  const title = useId();
  const description = useId();
  const change = useMemo(() => savedSource === null ? null : firstChangedLines(savedSource, draftSource), [savedSource, draftSource]);
  const date = updatedAt === undefined ? null : new Date(updatedAt);
  useDialogFocus(dialog);

  return (
    <div className="dialog-backdrop">
      <section
        ref={dialog}
        tabIndex={-1}
        className="dialog draft-recovery-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={title}
        aria-describedby={description}
        onKeyDown={(event) => {
          event.stopPropagation();
          if (event.key === "Escape") {
            event.preventDefault();
            if (!busy) onCancel();
          }
          if (event.key !== "Tab") return;
          const controls = event.currentTarget.querySelectorAll<HTMLElement>("pre, button:not(:disabled)");
          const first = controls[0];
          const last = controls[controls.length - 1];
          if (!first) {
            event.preventDefault();
          } else if (event.shiftKey && (event.target === first || event.target === event.currentTarget)) {
            event.preventDefault();
            last.focus();
          } else if (!event.shiftKey && event.target === last) {
            event.preventDefault();
            first.focus();
          }
        }}
      >
        <header>
          <h2 id={title}>{conflict ? "Workflow save conflict" : "Recover workflow draft"}</h2>
          <p id={description}>
            <strong>{path}</strong>
            {conflict ? " changed on disk. Review both versions before continuing." : " has a recovery copy with unsaved edits."}
          </p>
          {date && Number.isFinite(date.getTime()) && (
            <p>Recovery copy from <time dateTime={date.toISOString()}>{date.toLocaleString()}</time></p>
          )}
        </header>
        {savedSource !== null && (
          <p>{change ? `First difference at line ${change.number}` : savedSource === draftSource ? "The YAML versions are identical." : "The YAML versions use different line endings."}</p>
        )}
        <div className="recovery-comparison">
          {savedSource !== null && (
            <section>
              <h3>Saved version</h3>
              {change && <p className="recovery-changed-line"><code>{change.saved === null ? "(No matching line)" : change.saved || "(Blank line)"}</code></p>}
              <pre role="region" aria-label="Saved version YAML" tabIndex={0}><code>{savedSource || "(Empty file)"}</code></pre>
            </section>
          )}
          <section>
            <h3>Draft version</h3>
            {change && <p className="recovery-changed-line"><code>{change.draft === null ? "(No matching line)" : change.draft || "(Blank line)"}</code></p>}
            <pre role="region" aria-label="Draft version YAML" tabIndex={0}><code>{draftSource || "(Empty draft)"}</code></pre>
          </section>
        </div>
        {error && <p className="recovery-error" role="alert">{error}</p>}
        {busy && <p role="status">Working…</p>}
        <footer className="actions">
          {onDiscard && <button type="button" className="btn danger recovery-discard" disabled={busy} onClick={onDiscard}>Discard recovery copy</button>}
          <button type="button" className="btn" disabled={busy} onClick={onCancel}>Cancel</button>
          {onOpenSaved && <button type="button" className="btn" disabled={busy} onClick={onOpenSaved}>Open saved version</button>}
          {onReloadDisk && <button type="button" className="btn" disabled={busy} onClick={onReloadDisk}>Reload disk version</button>}
          {onRestore && <button type="button" className="primary" disabled={busy} onClick={onRestore}>Restore draft</button>}
          {onSaveCopy && <button type="button" className="primary" disabled={busy} onClick={onSaveCopy}>Save copy</button>}
        </footer>
      </section>
    </div>
  );
}
