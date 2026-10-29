import { useEffect, useRef } from "react";

/** Keep the destructive confirmation isolated from the workbench's shortcuts. */
export function DiscardChangesDialog({
  onCancel,
  onDiscard,
}: {
  onCancel: () => void;
  onDiscard: () => void;
}) {
  const panel = useRef<HTMLElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    panel.current?.querySelector<HTMLButtonElement>("button")?.focus();
    return () => {
      if (previous?.isConnected) previous.focus();
    };
  }, []);
  return (
    <div className="dialog-backdrop">
      <section
        ref={panel}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="discard-title"
        aria-describedby="discard-help"
        className="discard-dialog"
        onKeyDown={(event) => {
          event.stopPropagation();
          if (event.key === "Escape") {
            event.preventDefault();
            onCancel();
          }
          if (event.key !== "Tab") return;
          const buttons =
            panel.current?.querySelectorAll<HTMLButtonElement>("button");
          const first = buttons?.[0],
            last = buttons?.[1];
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last?.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first?.focus();
          }
        }}
      >
        <h2 id="discard-title">Discard unsaved workflow changes?</h2>
        <p id="discard-help">
          Save your workflow and apply any argument draft first to keep these
          edits.
        </p>
        <button onClick={onCancel}>Keep editing</button>
        <button onClick={onDiscard}>Discard changes and continue</button>
      </section>
    </div>
  );
}
