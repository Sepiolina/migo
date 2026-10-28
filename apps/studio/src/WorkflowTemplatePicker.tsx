import { useEffect, useRef } from "react";
import { workflowTemplates } from "./workflowTemplates";

export function WorkflowTemplatePicker({
  onSelect,
  onClose,
}: {
  onSelect: (template: (typeof workflowTemplates)[number]) => void;
  onClose: () => void;
}) {
  const panel = useRef<HTMLElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    panel.current?.querySelector<HTMLButtonElement>(".template-card")?.focus();
    return () => previous?.focus();
  }, []);
  return (
    <div className="dialog-backdrop">
      <section
        ref={panel}
        className="template-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="template-title"
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.stopPropagation();
            onClose();
          }
          if (event.key !== "Tab") return;
          const buttons = Array.from(
            panel.current?.querySelectorAll<HTMLButtonElement>("button") ?? [],
          );
          const first = buttons[0],
            last = buttons.at(-1);
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last?.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first?.focus();
          }
        }}
      >
        <div className="template-heading">
          <div>
            <h2 id="template-title">What would you like to build?</h2>
            <p>
              Start with connected blocks. You can change the common settings
              visually.
            </p>
          </div>
          <button onClick={onClose}>Close templates</button>
        </div>
        <div className="template-grid">
          {workflowTemplates.map((template) => (
            <button
              key={template.id}
              className="template-card"
              onClick={() => onSelect(template)}
            >
              <span aria-hidden="true">{template.icon}</span>
              <strong>{template.title}</strong>
              <small>{template.description}</small>
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}
