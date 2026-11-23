import { useEffect, useMemo, useRef, useState } from "react";
import { useDialogFocus } from "./useDialogFocus";

export type Command = {
  group: string;
  label: string;
  hint?: string;
  glyph?: string;
  kbd?: string;
  run: () => void;
};

/** ⌘K: one searchable list of every command, step type and place in the workflow. */
export function CommandMenu({ commands, placeholder, onClose }: { commands: Command[]; placeholder: string; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [hl, setHl] = useState(0);
  const menu = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);
  useDialogFocus(menu, search);
  const shown = useMemo(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    return commands.filter((c) => {
      const text = `${c.group} ${c.label} ${c.hint ?? ""}`.toLowerCase();
      return words.every((w) => text.includes(w));
    });
  }, [commands, query]);
  useEffect(() => setHl(0), [query]);
  useEffect(() => {
    list.current?.querySelector(".hl")?.scrollIntoView?.({ block: "nearest" });
  }, [hl]);
  const go = (i: number) => {
    const c = shown[i];
    onClose();
    c?.run();
  };
  let last = "";
  return (
    <div className="scrim" onMouseDown={(e) => {
      if (e.target !== e.currentTarget) return;
      e.preventDefault();
      onClose();
    }}>
      <div ref={menu} className="palette" role="dialog" aria-modal="true" aria-label="Command menu">
        <input
          ref={search}
          aria-label="Search commands"
          role="combobox"
          aria-expanded="true"
          aria-controls="command-list"
          aria-activedescendant={shown.length ? `cmd-${hl}` : undefined}
          placeholder={placeholder}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Tab") e.preventDefault();
            if (e.key === "ArrowDown") { e.preventDefault(); setHl(Math.max(0, Math.min(hl + 1, shown.length - 1))); }
            if (e.key === "ArrowUp") { e.preventDefault(); setHl(Math.max(hl - 1, 0)); }
            if (e.key === "Enter") {
              e.preventDefault();
              if (!e.ctrlKey && !e.metaKey) go(hl);
            }
            if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onClose(); }
          }}
        />
        <div className="list" id="command-list" role="listbox" ref={list}>
          {shown.map((c, i) => {
            const head = c.group !== last ? <div className="h" key={`h-${c.group}`}>{c.group}</div> : null;
            last = c.group;
            return [
              head,
              <button key={`${c.group}-${c.label}-${i}`} id={`cmd-${i}`} tabIndex={-1} role="option" aria-selected={i === hl} className={`mi ${i === hl ? "hl" : ""}`} onMouseDown={(e) => e.preventDefault()} onMouseMove={() => setHl(i)} onClick={() => go(i)}>
                <span className="gi" aria-hidden="true">{c.glyph ?? "•"}</span>
                <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {c.label}
                  {c.hint && <small>{c.hint}</small>}
                </span>
                <span className="kbd">{c.kbd ?? ""}</span>
              </button>,
            ];
          })}
          {!shown.length && <div className="h">Nothing matches “{query}”.</div>}
        </div>
      </div>
    </div>
  );
}
