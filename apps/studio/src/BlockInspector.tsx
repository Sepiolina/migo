import { useEffect, useState } from "react";
import * as Blockly from "blockly/core";
import { advancedFields, blockMemory, toggleOptions } from "./blocklyBlocks";
import { expressionOf, setExpression } from "./blocklyWorkflow";
import {
  fieldsOf,
  itemByType,
  stepByType,
  type Field,
} from "./stepRegistry";

interface BlockInspectorProps {
  block: Blockly.Block | null;
  /** Changes whenever the workspace changes, so values stay current. */
  revision: number;
  disabled?: boolean;
  onClose: () => void;
}

const scopeHelp: Record<string, string> = {
  row: "Row expression: columns by name (or col(\"Name\")), index, prev(\"Col\"), group and size in groups.",
  table: "Table expression: rows, table(\"name\"), sheets(\"input\"), config(\"key\").",
  name: "Sees the rule's size, best and tied values.",
};

function titleOf(block: Blockly.Block): string {
  const step = stepByType.get(block.type);
  if (step) return step.title;
  const item = itemByType.get(block.type);
  if (item) return item.title;
  if (block.type === "mai_code") return "Expression";
  return block.type.replace(/^mai_/, "").replace(/_/g, " ");
}

/** A text box that applies on blur or Ctrl/Cmd+Enter, giving one undo step per edit. */
function DraftText({
  value,
  multiline,
  onCommit,
  disabled,
  label,
  placeholder,
  code,
}: {
  value: string;
  multiline?: boolean;
  onCommit: (value: string) => void;
  disabled?: boolean;
  label: string;
  placeholder?: string;
  code?: boolean;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const commit = () => {
    if (draft !== value) onCommit(draft);
  };
  const props = {
    "aria-label": label,
    value: draft,
    disabled,
    placeholder,
    spellCheck: false,
    className: code ? "code" : undefined,
    onChange: (e: { target: { value: string } }) => setDraft(e.target.value),
    onBlur: commit,
    onKeyDown: (e: React.KeyboardEvent) => {
      if (e.key === "Escape") setDraft(value);
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey || !multiline)) {
        e.preventDefault();
        commit();
      }
    },
  };
  return multiline ? (
    <textarea rows={Math.min(10, Math.max(2, draft.split("\n").length + 1))} {...props} />
  ) : (
    <input {...props} />
  );
}

/** Edit the selected block's settings with full-size controls. */
export function BlockInspector({ block, revision, disabled, onClose }: BlockInspectorProps) {
  const [, setTick] = useState(0);
  void revision;
  if (!block || block.isDeadOrDying()) return null;
  const fields = fieldsOf(block.type);
  const help = block.getTooltip();
  const setField = (name: string, value: string) => {
    try {
      block.setFieldValue(value, name);
    } catch {
      /* the field validator rejected the value */
    }
  };
  const advanced = advancedFields(block.type).length > 0;
  const expanded = !!blockMemory.get(block)?.expanded;
  const control = (f: Field) => {
    const id = `${block.id}-${f.name}`;
    const label = f.label || (f.kind === "list" ? "Columns" : "Value");
    switch (f.kind) {
      case "entries":
      case "items":
      case "steps":
        return null;
      case "bool":
        return (
          <label key={id} className="inspector-check">
            <input
              type="checkbox"
              disabled={disabled}
              checked={block.getFieldValue(f.name) === "TRUE"}
              onChange={(e) => setField(f.name, e.target.checked ? "TRUE" : "FALSE")}
            />
            {label}
          </label>
        );
      case "enum":
        return (
          <label key={id}>
            {label}
            <select
              disabled={disabled}
              value={block.getFieldValue(f.name)}
              onChange={(e) => setField(f.name, e.target.value)}
            >
              {f.options.map(([text, value]) => (
                <option key={value} value={value}>
                  {text}
                </option>
              ))}
            </select>
          </label>
        );
      case "expr":
        return (
          <label key={id}>
            {label === "=" || label === "Value" ? "Expression" : label}
            <DraftText
              label={label}
              multiline
              code
              disabled={disabled}
              value={expressionOf(block, f.name)}
              placeholder={f.optional ? "Optional" : "Required"}
              onCommit={(text) => setExpression(block, f.name, text)}
            />
            <small>{scopeHelp[f.scope]}</small>
          </label>
        );
      default:
        return (
          <label key={id}>
            {label}
            <DraftText
              label={label}
              multiline={
                ("multiline" in f && f.multiline) || f.kind === "template" || f.kind === "yaml"
              }
              code={f.kind === "yaml" || ("multiline" in f && !!f.multiline)}
              disabled={disabled}
              value={String(block.getFieldValue(f.name) ?? "")}
              placeholder={
                f.kind === "list"
                  ? "Comma-separated names"
                  : f.kind === "yaml"
                    ? "{bold: true}"
                    : f.optional
                      ? "Optional"
                      : undefined
              }
              onCommit={(value) => setField(f.name, value)}
            />
            {f.hint && <small>{f.hint}</small>}
          </label>
        );
    }
  };
  return (
    <aside className="block-inspector" aria-label="Selected block settings">
      <header>
        <div>
          <span className="eyebrow">Selected block</span>
          <h3>{titleOf(block)}</h3>
        </div>
        <button type="button" className="ghost" aria-label="Close block settings" onClick={onClose}>
          ✕
        </button>
      </header>
      {help && <p className="inspector-help">{help}</p>}
      <div className="inspector-fields">
        {block.type === "mai_code" ? (
          <label>
            Expression
            <DraftText
              label="Expression"
              multiline
              code
              disabled={disabled}
              value={String(block.getFieldValue("CODE") ?? "")}
              onCommit={(value) => setField("CODE", value)}
            />
            <small>Ctrl/Cmd+Enter applies; Escape reverts.</small>
          </label>
        ) : (
          fields.map(control)
        )}
        {!fields.length && block.type !== "mai_code" && (
          <p className="inspector-help">Edit this block directly on the canvas.</p>
        )}
      </div>
      {advanced && (
        <button
          type="button"
          className="ghost"
          onClick={() => {
            toggleOptions(block);
            setTick((t) => t + 1);
          }}
          aria-pressed={expanded}
        >
          {expanded ? "Hide unused options on canvas" : "Show all options on canvas"}
        </button>
      )}
    </aside>
  );
}
