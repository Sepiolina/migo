import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getCurrentWindow } from "@tauri-apps/api/window";
import "./studio.css";
import { isDesktop, native, request, type Inspection, type RunResult, type Trace, type Validation } from "./api";
import { useDocument } from "./useDocument";
import { readPipeline, insertStep, addInput, expandSheets, addItem, type Pipeline } from "./pipeline";
import { Navigator, type Selection } from "./Navigator";
import { Inspector } from "./Inspector";
import { DataGrid, profileRows, type ColumnAction } from "./DataGrid";
import { Console, type LogLine, type Problem } from "./Console";
import { CommandMenu, type Command } from "./CommandMenu";
import { Home, type WorkflowEntry } from "./Home";
import { PackageBuilder } from "./PackageBuilder";
import { DiscardChangesDialog } from "./DiscardChangesDialog";
import { steps as stepSpecs } from "./stepRegistry";
import { workflowTemplates, type Template } from "./workflowTemplates";
import { parse } from "yaml";

type Theme = "system" | "light" | "dark";
type Toast = { text: string; undo?: boolean } | null;
type Panel = "steps" | "table" | "insp";

const letters = (i: number): string => (i >= 26 ? letters(Math.floor(i / 26) - 1) : "") + String.fromCharCode(65 + (i % 26));
const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40) || "workflow";
const base = (path: string) => path.split("/").pop()!.replace(/\.ya?ml$/, "");
const now = () => new Date().toTimeString().slice(0, 8);
const store = {
  get<T>(key: string, fallback: T): T {
    try {
      const v = localStorage.getItem(key);
      return v ? (JSON.parse(v) as T) : fallback;
    } catch {
      return fallback;
    }
  },
  set(key: string, value: unknown) {
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      /* storage unavailable */
    }
  },
};
const icons = {
  nav: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="2" /><path d="M6 2.5v11" /></svg>,
  insp: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="2" /><path d="M10 2.5v11" /></svg>,
  undo: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4"><path d="M5 4 2 7l3 3" /><path d="M2 7h7.5a4 4 0 0 1 0 8H7" /></svg>,
  redo: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4"><path d="m11 4 3 3-3 3" /><path d="M14 7H6.5a4 4 0 0 0 0 8H9" /></svg>,
  theme: <svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.4" /><path d="M8 2a6 6 0 0 1 0 12z" fill="currentColor" /></svg>,
};

export default function App() {
  const doc = useDocument(workflowTemplates[1].source);
  const { source } = doc;
  const desktop = isDesktop();
  const [view, setView] = useState<"home" | "editor">("home");
  const [workspace, setWorkspace] = useState("");
  const [entries, setEntries] = useState<WorkflowEntry[]>([]);
  const [workflowPath, setWorkflowPath] = useState("");
  const [revision, setRevision] = useState<string | null>(null);
  const [selection, setSelection] = useState<Selection>({ kind: "step", id: "steps.0" });
  const [showNav, setShowNav] = useState(true);
  const [showInsp, setShowInsp] = useState(true);
  const [panel, setPanel] = useState<Panel>("table");
  const [consoleTab, setConsoleTab] = useState<"problems" | "log" | "yaml" | null>("problems");
  const [bindings, setBindings] = useState<Record<string, string>>({});
  const [skipped, setSkipped] = useState<Set<number>>(new Set());
  const [trace, setTrace] = useState<{ key: string; data?: Trace; error?: string; busy: boolean }>({ key: "", busy: false });
  const [inspection, setInspection] = useState<{ key: string; data?: Inspection; error?: string }>({ key: "" });
  const [validation, setValidation] = useState<{ source: string; error?: string }>({ source: "" });
  const [run, setRun] = useState<{ source: string; result: RunResult } | null>(null);
  const [log, setLog] = useState<LogLine[]>([]);
  const [busy, setBusy] = useState("");
  const [toast, setToast] = useState<Toast>(null);
  const [palette, setPalette] = useState<null | "all" | "add">(null);
  const [saveAs, setSaveAs] = useState<string | null>(null);
  const [packaging, setPackaging] = useState(false);
  const [packagePending, setPackagePending] = useState(false);
  const [pendingDiscard, setPendingDiscard] = useState<(() => void) | null>(null);
  const [theme, setTheme] = useState<Theme>(() => store.get<Theme>("mai.theme", "system"));

  useEffect(() => {
    if (theme === "system") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = theme;
    store.set("mai.theme", theme);
  }, [theme]);

  // ---- the model ------------------------------------------------------------------
  const parsed = useMemo(() => {
    try {
      return { pipeline: readPipeline(source) };
    } catch (e) {
      return { error: (e as Error).message };
    }
  }, [source]);
  const lastGood = useRef<Pipeline | null>(null);
  if (parsed.pipeline) lastGood.current = parsed.pipeline;
  const pipeline = parsed.pipeline ?? lastGood.current ?? readPipeline(workflowTemplates[1].source);
  const route = String(pipeline.service.route ?? "") || (workflowPath ? base(workflowPath) : slug(pipeline.name));
  const selectedStep = selection.kind === "step" ? pipeline.steps.find((s) => s.id === selection.id) : undefined;
  const bound = pipeline.inputs.every((i) => bindings[i.id]);
  const bindingKey = `mai.bindings:${workspace}:${workflowPath || pipeline.name}`;

  const say = useCallback((text: string, undo = false) => {
    setToast({ text, undo });
  }, []);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 3600);
    return () => clearTimeout(t);
  }, [toast]);
  const addLog = (text: string, warn = false) => setLog((l) => [...l.slice(-400), { time: now(), text, warn }]);
  const fail = (e: unknown) => {
    const text = e instanceof Error ? e.message : String(e);
    say(text);
    addLog(text, true);
  };

  /** Every edit goes through here: one undo step, with an Undo toast. Undo
   * also restores the selection from before the edit. */
  const selections = useRef<{ undo: Selection[]; redo: Selection[] }>({ undo: [], redo: [] });
  const change = useCallback((next: string, label: string) => {
    if (next === source) return;
    selections.current.undo = [...selections.current.undo.slice(-199), selection];
    selections.current.redo = [];
    doc.transact(label, () => next);
    say(label, true);
  }, [doc, source, say, selection]);
  const undo = () => {
    if (!doc.canUndo) return;
    doc.undo();
    const s = selections.current.undo.pop();
    if (s) {
      selections.current.redo.push(selection);
      setSelection(s);
    }
  };
  const redo = () => {
    if (!doc.canRedo) return;
    doc.redo();
    const s = selections.current.redo.pop();
    if (s) {
      selections.current.undo.push(selection);
      setSelection(s);
    }
  };

  // Keep the selection valid after edits.
  useEffect(() => {
    if (selection.kind === "step" && !pipeline.steps.some((s) => s.id === selection.id))
      setSelection(pipeline.steps.length ? { kind: "step", id: pipeline.steps[Math.min(pipeline.steps.length - 1, Number(selection.id.split(".")[1]) || 0)].id } : { kind: "endpoint" });
    if (selection.kind === "source" && !pipeline.inputs.some((i) => i.id === selection.id)) setSelection({ kind: "endpoint" });
    if (selection.kind === "output" && !pipeline.outputs[selection.index]) setSelection({ kind: "endpoint" });
  }, [pipeline, selection]);

  // ---- bindings ---------------------------------------------------------------------
  useEffect(() => setBindings(store.get(bindingKey, {})), [bindingKey]);
  const bind = (id: string, path: string) => {
    const next = { ...bindings, [id]: path };
    if (!path) delete next[id];
    setBindings(next);
    store.set(bindingKey, next);
  };

  // ---- live preview: trace the workflow on the sample files ----------------------------
  const focus = selectedStep?.top ?? -1;
  const traceKey = JSON.stringify({ source, bindings, focus, skip: [...skipped].sort(), workspace });
  const canPreview = desktop && !!workspace && bound && !parsed.error && view === "editor";
  useEffect(() => {
    if (!canPreview || trace.key === traceKey) return;
    let live = true;
    setTrace((t) => ({ ...t, busy: true }));
    const timer = setTimeout(() => {
      request<Trace>("trace", { workspace, yaml: source, inputs: bindings, step: focus, skip: [...skipped], limit: 200 })
        .then((data) => {
          if (!live) return;
          setTrace({ key: traceKey, data, busy: false });
          const lines = data.steps.flatMap((s) => (s.log ?? []).map((l) => `${s.step + 1}. ${l}`));
          if (lines.length) setLog((l) => [...l.slice(-300), ...lines.map((text) => ({ time: now(), text }))]);
        })
        .catch((e) => live && setTrace({ key: traceKey, error: (e as Error).message, busy: false }));
    }, 280);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [canPreview, traceKey]); // eslint-disable-line react-hooks/exhaustive-deps

  // Source and output previews read files directly.
  const fileToInspect =
    selection.kind === "source" ? bindings[selection.id]
    : selection.kind === "output" && run?.source === source ? run.result.path
    : undefined;
  const sheetToInspect = selection.kind === "output" ? (pipeline.format === "csv" ? undefined : pipeline.outputs[selection.index]?.name) : undefined;
  const inspectKey = JSON.stringify({ fileToInspect, sheetToInspect, workspace });
  useEffect(() => {
    if (!desktop || !workspace || !fileToInspect || inspection.key === inspectKey) return;
    let live = true;
    request<Inspection>("inspectWorkbook", { workspace, path: fileToInspect, sheet: sheetToInspect, limit: 200 })
      .then((data) => live && setInspection({ key: inspectKey, data }))
      .catch((e) => live && setInspection({ key: inspectKey, error: (e as Error).message }));
    return () => {
      live = false;
    };
  }, [inspectKey]); // eslint-disable-line react-hooks/exhaustive-deps

  // Validate with the production parser shortly after each edit.
  useEffect(() => {
    if (!desktop || !workspace || parsed.error || validation.source === source) return;
    const t = setTimeout(() => {
      request<Validation>("validate", { workspace, yaml: source })
        .then(() => setValidation({ source }))
        .catch((e) => setValidation({ source, error: (e as Error).message }));
    }, 450);
    return () => clearTimeout(t);
  }, [source, workspace]); // eslint-disable-line react-hooks/exhaustive-deps

  // ---- problems -------------------------------------------------------------------------------
  const problems: Problem[] = [];
  if (parsed.error) problems.push({ level: "error", text: parsed.error });
  if (validation.source === source && validation.error) {
    const m = validation.error.match(/^step (\d+)/);
    problems.push({ level: "error", text: validation.error, step: m ? `steps.${Number(m[1]) - 1}` : undefined });
  }
  trace.data?.steps.forEach((s) => s.error && problems.push({ level: "error", text: `Step ${s.step + 1}: ${s.error}`, step: `steps.${s.step}` }));
  if (trace.error) problems.push({ level: "error", text: trace.error });
  pipeline.inputs.forEach((i) => !bindings[i.id] && problems.push({ level: "warning", text: `Source “${i.id}” has no sample file, so the table cannot be previewed.` }));
  // The parser and the trace can report the same error.
  for (let i = problems.length - 1; i > 0; i--) if (problems.slice(0, i).some((p) => p.text.endsWith(problems[i].text.replace(/^Step \d+: /, "")) || problems[i].text.endsWith(p.text))) problems.splice(i, 1);
  if (pipeline.steps.some((s) => s.spec?.service)) problems.push({ level: "info", text: "SQL and REST steps run on the server only. Local previews skip them and use the fallback values." });

  // ---- actions -------------------------------------------------------------------------------
  const insertAfter = (step: Record<string, unknown>, label: string) => {
    const s = selectedStep;
    const listPath = s ? s.listPath : ["steps"];
    const at = s ? s.index + 1 : pipeline.steps.filter((x) => x.depth === 0).length;
    try {
      change(insertStep(source, listPath, at, step), `Added: ${label}`);
      setSelection({ kind: "step", id: [...listPath, at].join(".") });
    } catch (e) {
      fail(e);
    }
  };
  const onColumnAction = (a: ColumnAction) => insertAfter(a.step, a.label);

  async function chooseWorkspace() {
    try {
      const picked = await native<{ path: string } | null>("choose_workspace");
      if (!picked) return;
      setWorkspace(picked.path);
      await loadEntries();
      addLog(`Opened folder ${picked.path}`);
    } catch (e) {
      fail(e);
    }
  }
  async function loadEntries() {
    const list = await native<{ paths: string[] }>("list_workflows");
    const out: WorkflowEntry[] = [];
    for (const path of list.paths) {
      try {
        const file = await native<{ source: string }>("read_workflow", { path });
        const y = parse(file.source) as { name?: string; service?: { route?: string } };
        out.push({ path, name: y?.name ?? "", route: y?.service?.route ?? base(path) });
      } catch {
        out.push({ path, name: "", route: base(path) });
      }
    }
    setEntries(out);
  }
  const guard = (action: () => void) => (doc.dirty ? setPendingDiscard(() => action) : action());
  const resetEditor = () => {
    selections.current = { undo: [], redo: [] };
    setSkipped(new Set());
    setTrace({ key: "", busy: false });
    setRun(null);
    setValidation({ source: "" });
    setSelection({ kind: "step", id: "steps.0" });
    setView("editor");
    setPanel("table");
  };
  async function openWorkflow(path: string) {
    try {
      const file = await native<{ path: string; source: string; revision: string }>("read_workflow", { path });
      doc.load(file.source);
      setWorkflowPath(file.path);
      setRevision(file.revision);
      resetEditor();
      addLog(`Opened ${file.path}`);
    } catch (e) {
      fail(e);
    }
  }
  function startTemplate(t: Template) {
    doc.load(t.source);
    doc.markSaved("");
    setWorkflowPath("");
    setRevision(null);
    resetEditor();
    const key = `mai.bindings:${workspace}:${readPipeline(t.source).name}`;
    if (!Object.keys(store.get(key, {})).length && Object.keys(t.samples).length) store.set(key, t.samples);
    setBindings(store.get(key, t.samples));
    say(`New workflow from “${t.title}”. Save it to serve it as /api/${slug(readPipeline(t.source).name)}.`);
  }
  async function save(path = workflowPath) {
    if (!desktop || !workspace) return say("Open a project folder to save workflows.");
    if (!path) return setSaveAs(`workflows/${slug(pipeline.name)}.yaml`);
    try {
      setBusy("Saving");
      const saved = await native<{ path: string; source: string; revision: string }>("save_workflow", {
        path,
        source,
        expectedRevision: path === workflowPath ? revision : null,
      });
      doc.markSaved(saved.source);
      const key = `mai.bindings:${workspace}:${saved.path}`;
      store.set(key, bindings);
      setWorkflowPath(saved.path);
      setRevision(saved.revision);
      setSaveAs(null);
      say(`Saved ${saved.path}`);
      addLog(`Saved ${saved.path}`);
      loadEntries().catch(() => undefined);
    } catch (e) {
      fail(e);
    } finally {
      setBusy("");
    }
  }
  async function runWorkflow() {
    if (!canPreview) return say(!desktop ? "Running needs the desktop app." : !workspace ? "Open a project folder first." : !bound ? "Give every source a sample file first." : "Fix the YAML first.");
    try {
      setBusy("Running");
      addLog(`Run started: ${pipeline.inputs.map((i) => bindings[i.id]).join(", ")}`);
      const result = await request<RunResult>("run", { workspace, yaml: source, inputs: bindings });
      setRun({ source, result });
      addLog(`Wrote ${result.path}`);
      say(`Run finished. Output written to ${result.path}`);
      setSelection({ kind: "output", index: 0 });
    } catch (e) {
      fail(e);
      setConsoleTab("problems");
    } finally {
      setBusy("");
    }
  }
  const toggleSkip = (top: number) => {
    const next = new Set(skipped);
    if (next.has(top)) next.delete(top);
    else next.add(top);
    setSkipped(next);
  };

  // ---- close guard and shortcuts ------------------------------------------------------------
  const dirtyRef = useRef(false);
  dirtyRef.current = doc.dirty;
  useEffect(() => {
    if (!("__TAURI_INTERNALS__" in window)) return;
    let stop: (() => void) | undefined;
    const w = getCurrentWindow();
    w.onCloseRequested((e) => {
      if (!dirtyRef.current) return;
      e.preventDefault();
      setPendingDiscard(() => () => void w.destroy());
    }).then((s) => (stop = s), () => undefined);
    return () => stop?.();
  }, []);
  const keys = useRef<(e: KeyboardEvent) => void>(() => undefined);
  keys.current = (e: KeyboardEvent) => {
    const mod = e.metaKey || e.ctrlKey;
    const typing = /INPUT|TEXTAREA|SELECT/.test((document.activeElement as HTMLElement)?.tagName ?? "");
    const k = e.key.toLowerCase();
    if (mod && k === "k") { e.preventDefault(); setPalette("all"); return; }
    if (mod && k === "s") { e.preventDefault(); (document.activeElement as HTMLElement)?.blur?.(); setTimeout(() => void save(), 0); return; }
    if (view !== "editor") return;
    if (mod && e.key === "Enter") { e.preventDefault(); void runWorkflow(); return; }
    if (mod && e.shiftKey && k === "a") { e.preventDefault(); setPalette("add"); return; }
    if (mod && k === "1") { e.preventDefault(); setShowNav((v) => !v); return; }
    if (mod && k === "2") { e.preventDefault(); setShowInsp((v) => !v); return; }
    if (typing || palette) return;
    if (mod && k === "z") { e.preventDefault(); if (e.shiftKey) redo(); else undo(); return; }
    if (mod && k === "y") { e.preventDefault(); redo(); return; }
    if (mod && k === "/" && selectedStep) { e.preventDefault(); toggleSkip(selectedStep.top); return; }
    if ((e.key === "ArrowDown" || e.key === "ArrowUp") && !mod && (document.activeElement?.closest(".nav") || document.activeElement === document.body)) {
      e.preventDefault();
      const i = selectedStep ? pipeline.steps.indexOf(selectedStep) : -1;
      const next = pipeline.steps[Math.max(0, Math.min(pipeline.steps.length - 1, i + (e.key === "ArrowDown" ? 1 : -1)))];
      if (next) setSelection({ kind: "step", id: next.id });
    }
  };
  useEffect(() => {
    const h = (e: KeyboardEvent) => keys.current(e);
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);
  // Focus the selected row when it changes from the keyboard.
  useEffect(() => {
    document.querySelector<HTMLElement>(".nav .row.sel")?.scrollIntoView?.({ block: "nearest" });
  }, [selection]);

  // ---- commands -------------------------------------------------------------------------------
  const cols = (() => {
    const steps = trace.data?.steps;
    if (!steps || !selectedStep) return [] as string[];
    const t = steps[selectedStep.top - 1] ?? steps[selectedStep.top];
    return t?.columns.map((c) => c.name) ?? [];
  })();
  const addCommands: Command[] = stepSpecs.map((s) => ({
    group: "Add step",
    label: s.title,
    hint: s.description,
    glyph: s.icon,
    run: () => {
      const arg = parse(s.example || "true");
      insertAfter(s.op === "if" ? { if: arg, then: [] } : { [s.op]: arg }, s.title);
    },
  }));
  const commands: Command[] = palette === "add" ? addCommands : [
    { group: "Workflow", label: "Run with sample files", glyph: "▶", kbd: "⌘↵", run: () => void runWorkflow() },
    { group: "Workflow", label: "Save", glyph: "↓", kbd: "⌘S", run: () => void save() },
    { group: "Workflow", label: "Save as…", glyph: "↓", run: () => setSaveAs(workflowPath || `workflows/${slug(pipeline.name)}.yaml`) },
    { group: "Workflow", label: "API endpoint and distribution", glyph: "⇄", run: () => setSelection({ kind: "endpoint" }) },
    { group: "Workflow", label: "Build Linux package…", glyph: "▣", run: () => setPackaging(true) },
    { group: "Workflow", label: "All workflows", glyph: "⌂", run: () => setView("home") },
    { group: "Edit", label: "Undo", glyph: "↶", kbd: "⌘Z", run: undo },
    { group: "Edit", label: "Redo", glyph: "↷", kbd: "⇧⌘Z", run: redo },
    ...addCommands,
    ...pipeline.steps.map((s) => ({ group: "Go to", label: `${s.depth ? "  " : `${s.top + 1}. `}${s.summary}`, glyph: s.spec?.icon, run: () => setSelection({ kind: "step", id: s.id }) })),
    ...pipeline.inputs.map((i) => ({ group: "Go to", label: `Source ${i.id}`, glyph: "↳", run: () => setSelection({ kind: "source", id: i.id }) })),
    ...entries.filter((w) => w.path !== workflowPath).map((w) => ({ group: "Open workflow", label: w.name || w.path, hint: w.path, glyph: "▤", run: () => guard(() => void openWorkflow(w.path)) })),
    ...workflowTemplates.map((t) => ({ group: "New from template", label: t.title, hint: t.description, glyph: "+", run: () => guard(() => startTemplate(t)) })),
    { group: "View", label: "Toggle steps panel", glyph: "◧", kbd: "⌘1", run: () => setShowNav((v) => !v) },
    { group: "View", label: "Toggle inspector", glyph: "◨", kbd: "⌘2", run: () => setShowInsp((v) => !v) },
    { group: "View", label: "Show YAML", glyph: "{}", run: () => setConsoleTab("yaml") },
    { group: "View", label: "Switch appearance", glyph: "◐", run: () => setTheme(theme === "system" ? "light" : theme === "light" ? "dark" : "system") },
  ];

  // ---- the stage: what the table shows ---------------------------------------------------------
  const stage = (() => {
    const notReady = (title: string, body: React.ReactNode, action?: React.ReactNode) => (
      <DataGrid columns={[]} rows={[]} total={0} header={<b>{title}</b>} empty={<div className="empty"><b>{title}</b><span>{body}</span>{action}</div>} />
    );
    if (selection.kind === "endpoint")
      return notReady("API endpoint", <>MaiService serves this workflow at <code>/api/{route}</code>. Its settings and request format are in the inspector.</>);
    if (selection.kind === "source" || selection.kind === "output") {
      const name = selection.kind === "source" ? `Source ${selection.id}` : `Output ${pipeline.outputs[selection.index]?.name}`;
      if (!desktop) return notReady(name, "Open the desktop app to read files.");
      if (!fileToInspect) return notReady(name, selection.kind === "source" ? "Set a sample file in the inspector to see its contents." : "Run the workflow (⌘↵) to write this output and see it here.", selection.kind === "output" ? <button className="primary" onClick={() => void runWorkflow()}>▶ Run</button> : undefined);
      if (inspection.key !== inspectKey) return notReady(name, "Reading…");
      if (inspection.error) return notReady(name, <span className="err">{inspection.error}</span>);
      const d = inspection.data!;
      const all = (d.rows ?? []).map((r) => r ?? []);
      // Sources show the raw sheet with column letters, so header rows and
      // fixed positions can be read off directly; outputs show their header.
      const raw = selection.kind === "source";
      const width = Math.max(0, ...all.map((r) => r.length));
      const head = raw ? Array.from({ length: width }, (_, i) => letters(i)) : all[0] ?? [];
      const rows = raw ? all : all.slice(1);
      const total = raw ? d.totalRows : Math.max(0, d.totalRows - 1);
      return (
        <DataGrid
          columns={profileRows(head, rows)}
          rows={rows}
          total={total}
          header={<><b>{name}</b><span className="meta mono">{fileToInspect}{d.sheets.length > 1 || d.sheet !== "Sheet1" ? ` › ${d.sheet}` : ""}</span><span className="meta">{total.toLocaleString()} rows · {head.length} columns</span></>}
        />
      );
    }
    const s = selectedStep;
    if (!s) return notReady("No step selected", "Choose a step on the left.");
    if (!desktop) return notReady(`${s.top + 1}. ${s.summary}`, "The browser preview edits YAML only. Open the desktop app to see the table after each step.");
    if (!workspace) return notReady("Open a project folder", "Studio reads sample files from a folder on this computer.", <button className="primary" onClick={() => void chooseWorkspace()}>Open folder…</button>);
    if (!bound) {
      const missing = pipeline.inputs.find((i) => !bindings[i.id])!;
      return notReady("Add a sample file", <>Choose a sample file for source <b>{missing.id}</b> to see the data at every step.</>, <button className="primary" onClick={() => setSelection({ kind: "source", id: missing.id })}>Choose sample file</button>);
    }
    if (parsed.error) return notReady("The YAML has an error", <span className="err">{parsed.error}</span>);
    const steps = trace.data?.steps;
    if (!steps) return notReady(`${s.top + 1}. ${s.summary}`, trace.error ? <span className="err">{trace.error}</span> : "Running the steps on the sample files…");
    const t = steps[s.top];
    if (!t) {
      const failed = steps.at(-1)!;
      return notReady(`${s.top + 1}. ${s.summary}`, <>Step {failed.step + 1} failed, so this step did not run.<span className="err">{failed.error}</span></>, <button className="btn" onClick={() => setSelection({ kind: "step", id: `steps.${failed.step}` })}>Go to step {failed.step + 1}</button>);
    }
    const prev = s.top > 0 ? steps[s.top - 1] : undefined;
    const added = new Set(t.columns.filter((c) => prev && !prev.columns.some((p) => p.name === c.name)).map((c) => c.name));
    const delta = prev ? t.total - prev.total : 0;
    const header = (
      <>
        <b>{s.depth ? `In step ${s.top + 1}` : `${s.top + 1}.`} {s.summary}</b>
        <span className="meta">{t.total.toLocaleString()} rows · {t.columns.length} columns</span>
        {delta !== 0 && <span className={`tag ${delta < 0 ? "minus" : "plus"}`}>{delta > 0 ? "+" : "−"}{Math.abs(delta).toLocaleString()} rows</span>}
        {added.size > 0 && <span className="tag plus">+{[...added].join(", ")}</span>}
        {t.skipped && <span className="tag mod">left out of preview</span>}
        {s.depth > 0 && <span className="tag">shows the table after step {s.top + 1}</span>}
        <span className="spacer" />
        {trace.busy && <span className="meta">Updating…</span>}
      </>
    );
    if (t.error) return notReady(`${s.top + 1}. ${s.summary}`, <span className="err">{t.error}</span>);
    if (!t.columns.length) return notReady(`${s.top + 1}. ${s.summary}`, "There is no table yet at this step. Start with a Read step.");
    return <DataGrid columns={t.columns} rows={t.rows ?? []} total={t.total} added={added} header={header} onAction={onColumnAction} />;
  })();

  // ---- render -------------------------------------------------------------------------------------
  const status = busy
    ? { cls: "busy", text: `${busy}…` }
    : parsed.error || (validation.source === source && validation.error) || trace.data?.steps.some((s) => s.error)
      ? { cls: "bad", text: "Has errors" }
      : !desktop ? { cls: "", text: "Browser preview" }
      : !workspace ? { cls: "", text: "No folder open" }
      : !bound ? { cls: "warn", text: "Needs sample files" }
      : trace.busy ? { cls: "busy", text: "Previewing…" }
      : trace.data ? { cls: "ok", text: `Valid · ${trace.data.steps.at(-1)?.total.toLocaleString() ?? 0} rows out` }
      : { cls: "", text: "Ready" };
  const panes = `panes${showNav ? "" : " no-nav"}${showInsp ? "" : " no-insp"}`;
  return (
    <div className="app" data-m={panel}>
      <header className="toolbar">
        <div className="tl">
          {view === "editor" && <button className="tb hide-sm" aria-label="Steps panel" aria-pressed={showNav} title="Steps panel (⌘1)" onClick={() => setShowNav(!showNav)}>{icons.nav}</button>}
          <div className="title">
            <button onClick={() => guard(() => setView("home"))} title="All workflows">Workflows</button>
            {view === "editor" && (
              <>
                <span className="kbd">/</span>
                <b title={workflowPath || "Not saved yet"}>{pipeline.name || "Untitled"}</b>
                <span className="edited">{doc.dirty ? (workflowPath ? "— Edited" : "— Not saved") : ""}</span>
              </>
            )}
          </div>
        </div>
        <div className="center">{view === "editor" && <span className={`pill ${status.cls}`} role="status"><i />{status.text}</span>}</div>
        <div className="tr">
          <button className="tb hide-sm" onClick={() => setPalette("all")} title="Command menu">Search <span className="kbd">⌘K</span></button>
          {view === "editor" && (
            <>
              <button className="tb" aria-label="Undo" title="Undo (⌘Z)" disabled={!doc.canUndo} onClick={undo}>{icons.undo}</button>
              <button className="tb hide-sm" aria-label="Redo" title="Redo (⇧⌘Z)" disabled={!doc.canRedo} onClick={redo}>{icons.redo}</button>
            </>
          )}
          <button className="tb" aria-label={`Appearance: ${theme}`} title={`Appearance: ${theme}`} onClick={() => setTheme(theme === "system" ? "light" : theme === "light" ? "dark" : "system")}>{icons.theme}</button>
          {view === "editor" && (
            <>
              <button className="tb hide-sm" aria-label="Inspector" aria-pressed={showInsp} title="Inspector (⌘2)" onClick={() => setShowInsp(!showInsp)}>{icons.insp}</button>
              <button className="tb hide-sm" onClick={() => void save()} disabled={!!busy} title="Save (⌘S)">Save</button>
              <button className="primary" onClick={() => void runWorkflow()} disabled={!!busy} title="Run with sample files (⌘↵)">▶ Run <span className="kbd hide-sm">⌘↵</span></button>
            </>
          )}
        </div>
      </header>

      {view === "home" ? (
        <Home
          workspace={workspace}
          desktop={desktop}
          workflows={entries}
          onOpenWorkspace={() => guard(() => void chooseWorkspace())}
          onOpen={(p) => guard(() => void openWorkflow(p))}
          onTemplate={(t) => guard(() => startTemplate(t))}
          onBack={doc.dirty || workflowPath ? () => setView("editor") : undefined}
        />
      ) : (
        <div className={panes}>
          <Navigator
            hidden={!showNav}
            pipeline={pipeline}
            selection={selection}
            onSelect={(s) => { setSelection(s); if (panel === "steps") setPanel("table"); }}
            trace={trace.data?.steps}
            skipped={skipped}
            onToggleSkip={toggleSkip}
            onAdd={() => setPalette("add")}
            onAddInput={() => {
              const id = ["data", "lookup", "extra", "more"].find((n) => !pipeline.inputs.some((i) => i.id === n)) ?? `input${pipeline.inputs.length + 1}`;
              try {
                change(addInput(source, id, id[0].toUpperCase() + id.slice(1)), "Add source");
                setSelection({ kind: "source", id });
              } catch (e) { fail(e); }
            }}
            onAddOutput={() => {
              const from = pipeline.datasets.at(-1) ?? "result";
              const next = addItem(expandSheets(source), ["output", "sheets"], { name: `Sheet${pipeline.outputs.length + 1}`, from });
              change(next, "Add output sheet");
              setSelection({ kind: "output", index: pipeline.outputs.length });
            }}
            route={route}
            bindings={bindings}
          />
          {stage}
          {showInsp && (
            <Inspector
              source={source}
              pipeline={pipeline}
              selection={selection}
              onChange={change}
              onError={fail}
              onSelect={setSelection}
              onClose={() => (panel === "insp" ? setPanel("table") : setShowInsp(false))}
              trace={trace.data?.steps}
              columns={cols}
              bindings={bindings}
              onBind={bind}
              inspection={selection.kind === "source" && inspection.key === inspectKey ? inspection.data : undefined}
              route={route}
              workflowPath={workflowPath}
              canPackage={desktop && !!workspace && !!workflowPath && !doc.dirty}
              onPackage={() => setPackaging(true)}
              onAddRead={(input) => {
                change(insertStep(source, ["steps"], 0, { from: input }), `Read ${input}`);
                setSelection({ kind: "step", id: "steps.0" });
              }}
            />
          )}
        </div>
      )}
      {view === "editor" && (
        <Console
          problems={problems}
          log={log}
          source={source}
          onSource={(s) => change(s, "Edit YAML")}
          onGoTo={(id) => setSelection({ kind: "step", id })}
          open={consoleTab}
          onOpen={setConsoleTab}
        />
      )}
      {view === "editor" && (
        <nav className="mtabs" aria-label="Panels">
          {(["steps", "table", "insp"] as Panel[]).map((p) => (
            <button key={p} aria-pressed={panel === p} onClick={() => { setPanel(p); if (p === "insp") setShowInsp(true); }}>
              {p === "steps" ? "Steps" : p === "table" ? "Table" : "Settings"}
            </button>
          ))}
        </nav>
      )}

      {palette && <CommandMenu commands={commands} placeholder={palette === "add" ? "Add a step after the selected one…" : "Search commands, steps and workflows…"} onClose={() => setPalette(null)} />}
      {saveAs !== null && (
        <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && setSaveAs(null)}>
          <form className="dialog" role="dialog" aria-label="Save workflow" onSubmit={(e) => { e.preventDefault(); void save(saveAs); }}>
            <h2>Save workflow</h2>
            <p>Files in <code>workflows/</code> become API endpoints. The file name is the route unless the service settings name another.</p>
            <input className="ctl" aria-label="File path" autoFocus value={saveAs} onChange={(e) => setSaveAs(e.target.value)} />
            <div className="hint">Serves <code>/api/{String(pipeline.service.route ?? "") || base(saveAs)}</code></div>
            <div className="actions">
              <button type="button" className="btn" onClick={() => setSaveAs(null)}>Cancel</button>
              <button className="primary" disabled={!/\.ya?ml$/.test(saveAs)}>Save</button>
            </div>
          </form>
        </div>
      )}
      {packaging && (
        <PackageBuilder
          workspace={workspace}
          workflows={entries.map((e) => e.path)}
          blocked={doc.dirty}
          onPending={setPackagePending}
          onClose={() => !packagePending && setPackaging(false)}
        />
      )}
      {pendingDiscard && (
        <DiscardChangesDialog
          onCancel={() => setPendingDiscard(null)}
          onDiscard={() => {
            const a = pendingDiscard;
            setPendingDiscard(null);
            a();
          }}
        />
      )}
      {toast && (
        <div className="toast" role="status">
          <span>{toast.text}</span>
          {toast.undo && doc.canUndo && <button onClick={() => { undo(); setToast(null); }}>Undo</button>}
        </div>
      )}
    </div>
  );
}
