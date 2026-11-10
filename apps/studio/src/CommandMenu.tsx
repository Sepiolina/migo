import { useEffect, useMemo, useRef, useState } from "react";

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
  const list = useRef<HTMLDivElement>(null);
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
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette" role="dialog" aria-label="Command menu">
        <input
          autoFocus
          aria-label="Search commands"
          role="combobox"
          aria-expanded="true"
          aria-controls="command-list"
          aria-activedescendant={shown.length ? `cmd-${hl}` : undefined}
          placeholder={placeholder}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") { e.preventDefault(); setHl(Math.min(hl + 1, shown.length - 1)); }
            if (e.key === "ArrowUp") { e.preventDefault(); setHl(Math.max(hl - 1, 0)); }
            if (e.key === "Enter") { e.preventDefault(); go(hl); }
            if (e.key === "Escape") { e.preventDefault(); onClose(); }
          }}
        />
        <div className="list" id="command-list" role="listbox" ref={list}>
          {shown.map((c, i) => {
            const head = c.group !== last ? <div className="h" key={`h-${c.group}`}>{c.group}</div> : null;
            last = c.group;
            return [
              head,
              <button key={`${c.group}-${c.label}-${i}`} id={`cmd-${i}`} role="option" aria-selected={i === hl} className={`mi ${i === hl ? "hl" : ""}`} onMouseMove={() => setHl(i)} onClick={() => go(i)}>
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
