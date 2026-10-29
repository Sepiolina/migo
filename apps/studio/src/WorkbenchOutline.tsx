import { useEffect, useRef } from "react";
import type { WorkflowGraphNode } from "./model";
import "./workbench-outline.css";

export function WorkbenchOutline({
  nodes,
  selected,
  focusRequest,
  disabled,
  onSelect,
}: {
  nodes: WorkflowGraphNode[];
  selected?: string;
  focusRequest: number;
  disabled: boolean;
  onSelect: (id: string) => void;
}) {
  const outline = useRef<HTMLElement>(null);
  useEffect(() => {
    outline.current
      ?.querySelector<HTMLButtonElement>('[aria-pressed="true"]')
      ?.scrollIntoView?.({ block: "nearest" });
  }, [selected]);
  useEffect(() => {
    if (focusRequest > 0)
      outline.current
        ?.querySelector<HTMLButtonElement>('[aria-pressed="true"]')
        ?.focus();
  }, [focusRequest]);
  const steps = nodes.filter((node) => /^step\.\d+$/.test(node.id));
  return (
    <section
      ref={outline}
      className="workbench-outline"
      aria-label="Step outline"
    >
      <h2>Workflow steps</h2>
      <p>Select a step to edit its settings. Steps run in this order.</p>
      {steps.length ? (
        <ol>
          {steps.map((step) => (
            <li key={step.id}>
              <button
                type="button"
                className="outline-step"
                aria-pressed={selected === step.id}
                disabled={disabled}
                onClick={() => onSelect(step.id)}
              >
                <strong>{step.data.title}</strong>
                <span>{step.data.summary}</span>
                {step.data.branch && (
                  <small>
                    Contains branches; edit nested steps in Advanced YAML.
                  </small>
                )}
              </button>
            </li>
          ))}
        </ol>
      ) : (
        <p>No steps yet. Use Add step to start.</p>
      )}
    </section>
  );
}
