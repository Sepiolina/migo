import { useEffect, useState, type ReactNode } from "react";
import {
  addItem,
  deleteAt,
  itemForm,
  moveAt,
  readForm,
  writeField,
  type FormSpec,
  type FormValue,
  type Path,
  type Row,
} from "./pipeline";
import { itemByType, type Field } from "./stepRegistry";

/** A text control that applies on blur or Enter (⌘↵ in a box), so one edit is one undo step. */
export function Draft({
  id,
  value,
  onCommit,
  multiline,
  code,
  placeholder,
  label,
  disabled,
}: {
  id?: string;
  value: string;
  onCommit: (value: string) => void;
  multiline?: boolean;
  code?: boolean;
  placeholder?: string;
  label?: string;
  disabled?: boolean;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const commit = () => draft !== value && onCommit(draft);
  const common = {
    id,
    "aria-label": label,
    className: `ctl${code ? " code" : ""}`,
    value: draft,
    placeholder,
    disabled,
    spellCheck: false,
    onChange: (e: { target: { value: string } }) => setDraft(e.target.value),
    onBlur: commit,
    onKeyDown: (e: React.KeyboardEvent) => {
      if (e.key === "Escape") {
        setDraft(value);
        (e.target as HTMLElement).blur();
      }
      if (e.key === "Enter" && (!multiline || e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        commit();
      }
    },
  };
  return multiline || draft.length > 48 || draft.includes("\n") ? (
    <textarea rows={Math.min(8, Math.max(1, Math.ceil(draft.length / 34), draft.split("\n").length))} {...common} />
  ) : (
    <input {...common} />
  );
}

const scopeHint: Record<string, string> = {
  row: "Row formula: column names, index, prev(\"Col\"); in groups also group and size.",
  table: "Table formula: rows, table(\"name\"), sheets(\"input\"), config(\"key\").",
  name: "Sees size, best and tied.",
};

function label(f: Field): string {
  if (f.label && !["=", "←", "@", ":", "to", "at", "of", "titles"].includes(f.label)) return f.label[0].toUpperCase() + f.label.slice(1);
  return { NAME: "Name", VALUE: "Condition", COLUMNS: "Columns", DATASETS: "Tables", MESSAGE: "Message", QUERY: "Query", CONDITION: "Condition", ENTRIES: "Columns" }[f.name] ?? f.name[0] + f.name.slice(1).toLowerCase();
}

/** Insert a column name at the caret of the last focused formula box. */
let lastBox: HTMLTextAreaElement | HTMLInputElement | null = null;
function ColumnChips({ columns }: { columns: string[] }) {
  if (!columns.length) return null;
  return (
    <div className="chips" aria-label="Columns available here">
      {columns.slice(0, 40).map((c) => (
        <button
          key={c}
          className="chip"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => {
            const box = lastBox;
            if (!box || !box.isConnected) return;
            const text = /^[A-Za-z_][A-Za-z0-9_]*$/.test(c) ? c : `col(${JSON.stringify(c)})`;
            const s = box.selectionStart ?? box.value.length, e = box.selectionEnd ?? s;
            const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(box), "value")!.set!;
            setter.call(box, box.value.slice(0, s) + text + box.value.slice(e));
            box.dispatchEvent(new Event("input", { bubbles: true }));
            box.focus();
            box.setSelectionRange(s + text.length, s + text.length);
          }}
        >
          {c}
        </button>
      ))}
    </div>
  );
}

/** Rows of key → value, for set/select/rename/params and the like. */
function EntriesEditor({ field, rows, onCommit, columns }: { field: Field & { kind: "entries" }; rows: Row[]; onCommit: (rows: Row[]) => void; columns: string[] }) {
  const item = itemByType.get(field.item)!;
  const keyField = item.entryKey!, valueField = item.entryValue!;
  const [adding, setAdding] = useState(false);
  const code = valueField.kind === "expr";
  const update = (i: number, k: string, v: string) => onCommit(rows.map((r, j) => (j === i ? [k, v] : r)));
  return (
    <div style={{ display: "grid", gap: 4 }}>
      {rows.map(([k, v], i) => (
        <div className="kv" key={`${i}:${k}`}>
          {keyField.kind === "enum" ? (
            <select className="ctl" aria-label={`${label(field)} ${i + 1} name`} value={k} onChange={(e) => update(i, e.target.value, v)}>
              {keyField.options.map(([t, val]) => <option key={val} value={val}>{t}</option>)}
            </select>
          ) : (
            <Draft label={`${label(field)} ${i + 1} name`} value={k} onCommit={(nk) => update(i, nk, v)} />
          )}
          <span onFocusCapture={(e) => (lastBox = e.target as unknown as HTMLTextAreaElement)}>
            <Draft label={`${k} value`} value={v} code={code} multiline={code && v.length > 30} onCommit={(nv) => update(i, k, nv)} />
          </span>
          <button className="tb" aria-label={`Remove ${k}`} title="Remove" onClick={() => onCommit(rows.filter((_, j) => j !== i))}>✕</button>
        </div>
      ))}
      {adding ? (
        <div className="kv">
          <Draft label="New name" value="" placeholder={keyField.kind === "enum" ? keyField.options[0][1] : "Name"} onCommit={(k) => { setAdding(false); if (k.trim()) onCommit([...rows, [k.trim(), valueField.kind === "expr" ? '""' : ""]]); }} />
          <span />
          <button className="tb" aria-label="Cancel" onClick={() => setAdding(false)}>✕</button>
        </div>
      ) : (
        <button className="linkish" onClick={() => (keyField.kind === "enum" ? onCommit([...rows, [keyField.options[0][1], columns[0] ?? ""]]) : setAdding(true))}>
          + Add {item.title.toLowerCase()}
        </button>
      )}
    </div>
  );
}

export interface FormProps {
  source: string;
  argPath: Path;
  form: FormSpec;
  onChange: (source: string, label: string) => void;
  onError: (message: string) => void;
  columns: string[];
  showAll?: boolean;
  depth?: number;
}

/** A form generated from registry fields, including nested item lists. */
export function StepForm({ source, argPath, form, onChange, onError, columns, showAll, depth = 0 }: FormProps) {
  let values: Record<string, FormValue>;
  try {
    values = readForm(source, argPath, form);
  } catch (e) {
    return <p className="hint err-text">{String((e as Error).message)}</p>;
  }
  const commit = (f: Field, v: FormValue) => {
    try {
      onChange(writeField(source, argPath, form, f, v), `Edit ${label(f).toLowerCase()}`);
    } catch (e) {
      onError((e as Error).message);
    }
  };
  const used = (f: Field) => {
    const v = values[f.name];
    return !(v === "" || v === false || v === 0 || (Array.isArray(v) && !v.length) || (f.kind === "enum" && v === f.fallback));
  };
  const visible = form.fields.filter((f) => !f.advanced || showAll || used(f));
  const hidden = form.fields.length - visible.length;
  const out: ReactNode[] = [];
  let needChips = false;
  for (const f of visible) {
    const id = `f-${argPath.join("-")}-${f.name}`;
    const v = values[f.name];
    switch (f.kind) {
      case "steps":
        out.push(<div className="prop" key={f.name}><span>{label(f)}</span><span className="hint" style={{ paddingTop: 5 }}>{v} step{v === 1 ? "" : "s"}, listed under this one</span></div>);
        break;
      case "items": {
        const item = itemByType.get(f.item)!;
        const count = v as number;
        const listPath = [...argPath, f.key];
        out.push(
          <div key={f.name} style={{ display: "grid", gap: 6 }}>
            <div className="hint">{label(f)}</div>
            {Array.from({ length: count }, (_, i) => (
              <div className="subitem" key={i}>
                <div className="lbl">
                  {item.title} {i + 1}
                  <span>
                    <button className="tb" aria-label={`Move ${item.title} ${i + 1} up`} disabled={i === 0} onClick={() => onChange(moveAt(source, [...listPath, i], -1), "Move")}>↑</button>
                    <button className="tb" aria-label={`Move ${item.title} ${i + 1} down`} disabled={i === count - 1} onClick={() => onChange(moveAt(source, [...listPath, i], 1), "Move")}>↓</button>
                    <button className="tb" aria-label={`Remove ${item.title} ${i + 1}`} onClick={() => onChange(deleteAt(source, [...listPath, i]), `Remove ${item.title.toLowerCase()}`)}>✕</button>
                  </span>
                </div>
                <StepForm source={source} argPath={[...listPath, i]} form={itemForm(f.item)} onChange={onChange} onError={onError} columns={columns} showAll={showAll} depth={depth + 1} />
              </div>
            ))}
            <button
              className="linkish"
              onClick={() => {
                const first = item.fields?.find((x) => !x.optional);
                const init = first ? { [first.key]: first.kind === "expr" ? "true" : first.key === "header" || first.key === "name" ? "New" : columns[0] ?? "Column" } : {};
                onChange(addItem(source, listPath, init), `Add ${item.title.toLowerCase()}`);
              }}
            >
              + Add {item.title.toLowerCase()}
            </button>
          </div>,
        );
        break;
      }
      case "entries":
        needChips ||= itemByType.get(f.item)!.entryValue!.kind === "expr";
        out.push(
          <div key={f.name} style={{ display: "grid", gap: 4 }}>
            {(f.label || form.fields.length > 1) && <div className="hint">{label(f)}</div>}
            <EntriesEditor field={f} rows={v as Row[]} columns={columns} onCommit={(rows) => commit(f, rows)} />
          </div>,
        );
        break;
      case "bool":
        out.push(
          <label className="prop check" key={f.name} htmlFor={id}>
            <span>{label(f)}</span>
            <input id={id} type="checkbox" checked={v as boolean} onChange={(e) => commit(f, e.target.checked)} />
          </label>,
        );
        break;
      case "enum":
        out.push(
          <label className="prop" key={f.name} htmlFor={id}>
            <span>{label(f)}</span>
            <select id={id} className="ctl" value={String(v)} onChange={(e) => commit(f, e.target.value)}>
              {f.options.map(([t, val]) => <option key={val} value={val}>{t || "(none)"}</option>)}
            </select>
          </label>,
        );
        break;
      default: {
        const isCode = f.kind === "expr" || f.kind === "yaml" || ("multiline" in f && !!f.multiline);
        needChips ||= f.kind === "expr" || f.kind === "list";
        out.push(
          <div className="prop" key={f.name}>
            <span><label htmlFor={id}>{label(f)}</label></span>
            <div style={{ display: "grid", gap: 3, minWidth: 0 }} onFocusCapture={(e) => (lastBox = e.target as unknown as HTMLTextAreaElement)}>
              <Draft
                id={id}
                value={String(v)}
                code={isCode}
                multiline={isCode && ("multiline" in f ? !!f.multiline : false)}
                placeholder={f.kind === "list" ? "Names, separated by commas" : f.kind === "yaml" ? "{bold: true}" : f.optional ? "Optional" : "Required"}
                onCommit={(nv) => commit(f, nv)}
              />
              {f.kind === "expr" && !f.hint && <span className="hint">{scopeHint[f.scope]}</span>}
              {f.kind === "template" && <span className="hint">Text with {"{formula}"} parts.</span>}
              {f.hint && <span className="hint">{f.hint}</span>}
            </div>
          </div>,
        );
      }
    }
  }
  return (
    <div style={{ display: "grid", gap: 8, minWidth: 0 }}>
      {out}
      {depth === 0 && needChips && <ColumnChips columns={columns} />}
      {hidden > 0 && depth === 0 && <span className="hint">{hidden} more setting{hidden === 1 ? "" : "s"} unused. Use Show all to see them.</span>}
    </div>
  );
}
