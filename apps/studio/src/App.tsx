import { SampleSetupDialog } from "./SampleSetupDialog";
import { WorkbookPreview } from "./WorkbookPreview";
import { StepPreview } from "./StepPreview";
import { shortcut } from "./shortcut";
import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { getCurrentWindow } from "@tauri-apps/api/window";
import "./studio.css";
import { isDesktop, isWebTool, listenNative, native, request, type Inspection, type RunnerError, type RunResult, type Trace, type Validation } from "./api";
import { asRunnerError, useCurrentRequest } from "./useCurrentRequest";
import { RequestCache } from "./RequestCache";
import { useFileRevisions } from "./useFileRevisions";
import { useDocument } from "./useDocument";
import { readPipeline, insertStep, addInput, expandSheets, addItem, keepLineEndings, type Pipeline } from "./pipeline";
import { Navigator, type Selection } from "./Navigator";
import { Inspector } from "./Inspector";
import { PaneSplitter, paneSizes, splitterWidth, type PanePreferences, type SidePane } from "./PaneSplitter";
import { DataGrid, profileRows, type ColumnAction } from "./DataGrid";
import { Console, type LogLine, type Problem } from "./Console";
import { Splitter } from "./Splitter";
import { assignSamples, type ImportedFile } from "./samples";

/** Files picked or dropped; the browser build also imports workflows and tests. */
type Imported = { files: ImportedFile[]; workflows?: string[]; tests?: string[]; error?: string };
import { CommandMenu, type Command } from "./CommandMenu";
import { Home, type WorkflowEntry } from "./Home";
import { DiscardChangesDialog } from "./DiscardChangesDialog";
import { SaveWorkflowDialog } from "./SaveWorkflowDialog";
import { createDraftId, draftKey, type RecoveryDraft } from "./draftRecovery";
import { useDraftRecovery } from "./useDraftRecovery";
import { steps as stepSpecs } from "./stepRegistry";
import { workflowTemplates, type Template } from "./workflowTemplates";
import { parse } from "yaml";

const WorkflowTests = lazy(() => import("./WorkflowTests").then(({ WorkflowTests }) => ({ default: WorkflowTests })));
const RunsPanel = lazy(() => import("./RunsPanel").then(({ RunsPanel }) => ({ default: RunsPanel })));
const WorkflowDiagram = lazy(() => import("./WorkflowDiagram").then(({ WorkflowDiagram }) => ({ default: WorkflowDiagram })));
const DraftRecoveryDialog = lazy(() => import("./DraftRecoveryDialog").then(({ DraftRecoveryDialog }) => ({ default: DraftRecoveryDialog })));
const PackageBuilder = lazy(() => import("./PackageBuilder").then(({ PackageBuilder }) => ({ default: PackageBuilder })));

type Theme = "system" | "light" | "dark";
type Toast = { text: string; undo?: boolean } | null;
type Panel = "steps" | "table" | "insp";
type RunIdentity = { key: string };
type WorkflowFile = { path: string; source: string; revision: string };
type RecoveryReview = {
  path: string;
  disk: WorkflowFile | null;
  record?: RecoveryDraft;
  conflict: boolean;
  setup?: boolean;
};
type YamlDraft = { revision: number; source: string };
const unassignedWorkspace = "unassigned";
const errorMessage = (error: unknown) => error instanceof Error ? error.message : String(error);

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
  /** The browser build: an in-memory workspace, files imported and downloaded. */
  const web = isWebTool();
  const [view, setView] = useState<"home" | "editor">("home");
  const [workspacePanel, setWorkspacePanel] = useState<"runs" | "tests" | null>(null);
  const panelOpen = workspacePanel !== null;
  const testsButton = useRef<HTMLButtonElement>(null);
  const runsButton = useRef<HTMLButtonElement>(null);
  const [workspace, setWorkspace] = useState("");
  const currentWorkspace = useRef("");
  const choosingWorkspace = useRef(false);
  const openRequest = useRef(0);
  const [entries, setEntries] = useState<WorkflowEntry[]>([]);
  const [workflowPath, setWorkflowPath] = useState("");
  const [revision, setRevision] = useState<string | null>(null);
  const [draftId, setDraftId] = useState("");
  const restoredDraft = useRef<RecoveryDraft | undefined>(undefined);
  const clearUnassignedDraft = useRef<(() => void) | undefined>(undefined);
  const [yamlDraft, setYamlDraft] = useState<YamlDraft>();
  const latestYamlDraft = useRef<YamlDraft | undefined>(undefined);
  const updateYamlDraft = (next?: YamlDraft) => {
    latestYamlDraft.current = next;
    setYamlDraft(next);
  };
  const draftSource = yamlDraft?.revision === doc.snapshot.revision ? yamlDraft.source : source;
  const dirty = draftSource !== doc.savedSource;
  const latestEditor = () => {
    const snapshot = doc.getCurrent();
    const raw = latestYamlDraft.current;
    return {
      source: raw?.revision === snapshot.present.revision ? raw.source : snapshot.present.source,
      savedSource: snapshot.savedSource,
    };
  };
  const recovery = useDraftRecovery({
    workspace: workspace || unassignedWorkspace,
    workflowPath,
    draftId,
    baseRevision: revision,
    source: draftSource,
    savedSource: doc.savedSource,
    dirty,
    enabled: true,
  });
  const [selection, setSelection] = useState<Selection>({ kind: "step", id: "steps.0" });
  const [showNav, setShowNav] = useState(true);
  const [showInsp, setShowInsp] = useState(true);
  const [panePreferences, setPanePreferences] = useState<PanePreferences>(() => ({
    navigator: 260,
    inspector: 300,
    priority: "navigator",
    ...store.get<Partial<PanePreferences>>("mai.paneWidths", {}),
  }));
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth);
  const [panel, setPanel] = useState<Panel>("table");
  const [diagram, setDiagram] = useState<"unopened" | "open" | "closed">("unopened");
  const showDiagram = () => { setDiagram("open"); setPanel("table"); };
  const showTable = () => {
    setDiagram((previous) => previous === "open" ? "closed" : previous);
    setPanel("table");
  };
  const appRef = useRef<HTMLDivElement>(null);
  const panelFocus = useRef<"table" | "insp" | null>(null);
  useLayoutEffect(() => {
    const target = panelFocus.current;
    if (!target) return;
    panelFocus.current = null;
    const app = appRef.current;
    const visibleStage = app?.querySelector<HTMLElement>(".editor-stage-body:not([hidden])");
    const element = target === "insp" ? app?.querySelector<HTMLElement>('.insp button[aria-label="Close inspector"]') :
      visibleStage?.querySelector<HTMLElement>("[role=grid], [tabindex='0']") ?? visibleStage?.querySelector<HTMLElement>(".stage");
    element?.focus({ preventScroll: true });
  }, [panel, showInsp]);
  const [consoleH, setConsoleH] = useState<number | null>(() => store.get<number | null>("mai.layout.console", null));
  useEffect(() => store.set("mai.layout.console", consoleH), [consoleH]);
  const [dropping, setDropping] = useState(false);
  const [consoleTab, setConsoleTab] = useState<"problems" | "log" | "yaml" | null>("problems");
  const [bindingState, setBindingState] = useState<{ key: string; values: Record<string, string> }>();
  const [documentEpoch, setDocumentEpoch] = useState(0);
  const [sampleSetup, setSampleSetup] = useState(false);
  const [worksheetPosition, setWorksheetPosition] = useState<{ key: string; offset: number }>();
  const [bindingEpoch, setBindingEpoch] = useState(0);
  const [skipped, setSkipped] = useState<Set<number>>(new Set());
  const [columnJump, setColumnJump] = useState<{ name: string; request: number; stepId: string; trace: Trace }>();
  const jumpRequest = useRef(0);
  const [run, setRun] = useState<{ key: string; workspace: string; result: RunResult; completedAt: number }>();
  const artifact = run?.workspace === workspace ? run : undefined;
  const workbookOutput = !!artifact && /\.xlsx$/i.test(artifact.result.path);
  const [running, setRunning] = useState<RunIdentity>();
  const [runFailure, setRunFailure] = useState<{ identity: RunIdentity; error: RunnerError }>();
  const [log, setLog] = useState<LogLine[]>([]);
  const [saving, setSaving] = useState(false);
  const savePending = useRef(false);
  const [toast, setToast] = useState<Toast>(null);
  const [palette, setPalette] = useState<null | "all" | "add">(null);
  const [saveAs, setSaveAs] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string>();
  const [recoveryReview, setRecoveryReview] = useState<RecoveryReview>();
  const [reviewError, setReviewError] = useState<string>();
  const [reviewBusy, setReviewBusy] = useState(false);
  const saveOpener = useRef<HTMLElement | null>(null);
  /** The name being typed for a new workflow test, while its dialog is open. */
  const [testName, setTestName] = useState<string | null>(null);
  const [savingTest, setSavingTest] = useState(false);
  const [packaging, setPackaging] = useState(false);
  const [packagePending, setPackagePending] = useState(false);
  const [pendingDiscard, setPendingDiscard] = useState<(() => void) | null>(null);
  const [theme, setTheme] = useState<Theme>(() => store.get<Theme>("mai.theme", "system"));

  useEffect(() => {
    if (theme === "system") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = theme;
    store.set("mai.theme", theme);
  }, [theme]);
  useEffect(() => {
    const resize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  useEffect(() => store.set("mai.paneWidths", panePreferences), [panePreferences]);

  const paneWidths = paneSizes(viewportWidth, showNav, showInsp, panePreferences);
  const resizePane = (pane: SidePane, width: number) => {
    setPanePreferences((previous) => previous[pane] === width && previous.priority === pane
      ? previous
      : { ...previous, [pane]: width, priority: pane });
  };
  const paneStyle = {
    "--navigator-width": `${paneWidths.navigator.width}px`,
    "--inspector-width": `${paneWidths.inspector.width}px`,
    "--splitter-width": `${splitterWidth}px`,
  } as React.CSSProperties;

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
  const bindingKey = `mai.bindings:${workspace}:${workflowPath || pipeline.name}`;
  const bindings = bindingState?.key === bindingKey ? bindingState.values : store.get<Record<string, string>>(bindingKey, {});
  const bound = pipeline.inputs.every((i) => bindings[i.id]);

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
    if (next === doc.getCurrent().present.source) return;
    selections.current.undo = [...selections.current.undo.slice(-199), selection];
    selections.current.redo = [];
    doc.transact(label, () => next);
    say(label, true);
  }, [doc, say, selection]);
  const commitYamlDraft = () => {
    if (!latestYamlDraft.current) return;
    const current = latestEditor();
    updateYamlDraft();
    change(current.source, "Edit YAML");
  };
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
    if (selection.kind === "output" && !pipeline.outputs[selection.index] && !workbookOutput) setSelection({ kind: "endpoint" });
  }, [pipeline, selection, workbookOutput]);

  // ---- bindings ---------------------------------------------------------------------
  const applyBindings = (next: Record<string, string>) => {
    setBindingState({ key: bindingKey, values: next });
    setBindingEpoch((value) => value + 1);
    store.set(bindingKey, next);
  };
  const bind = (id: string, path: string) => {
    const next = { ...bindings, [id]: path };
    if (!path) delete next[id];
    applyBindings(next);
    if (path === bindings[id]) fileRevisions.refresh();
  };

  async function browseSample(id: string) {
    try {
      const picked = await native<{ path: string; copied: boolean } | null>("choose_sample_file");
      if (!picked) return;
      bind(id, picked.path);
      addLog(picked.copied ? `Copied the sample file into ${picked.path}` : `Using ${picked.path} for ${id}`);
    } catch (e) {
      fail(e);
    }
  }

  /** Binds imported files to sources: all to `only`, or by file name (see assignSamples). */
  function applySamples(files: ImportedFile[], opts: { only?: string; preferred?: string } = {}) {
    for (const f of files) if (f.error) addLog(`Could not import ${f.name}: ${f.error}`, true);
    const ok = files.filter((f) => f.path);
    if (!ok.length) return;
    const { bind: pairs, unassigned } = opts.only
      ? { bind: { [opts.only]: ok[0].path! }, unassigned: ok.slice(1).map((f) => f.name) }
      : assignSamples(pipeline.inputs, bindings, ok, opts.preferred);
    const next = { ...bindings, ...pairs };
    applyBindings(next);
    fileRevisions.refresh();
    for (const [id, path] of Object.entries(pairs)) {
      const f = ok.find((x) => x.path === path)!;
      addLog(`${id} ← ${path}${f.copied && !web ? " (copied into the folder)" : ""}`);
    }
    if (unassigned.length) addLog(`Not used: ${unassigned.join(", ")}. Select a source and import again, or name the file after it.`, true);
    const first = Object.keys(pairs)[0];
    if (first) setSelection({ kind: "source", id: first });
  }
  /** Opens the file picker: one file for source `id`, or several matched to sources by name. */
  async function importSamples(id?: string) {
    try {
      const picked = await native<{ files: ImportedFile[] }>("choose_sample_files", { multiple: !id });
      applySamples(picked.files, { only: id });
    } catch (e) {
      fail(e);
    }
  }
  // Files dropped on the window. The handler reads the latest state. The
  // browser build also takes workflows: the first one opens, and the sample
  // files that came with it are bound once it has (pendingSamples).
  const pendingSamples = useRef<{ path: string; files: ImportedFile[] } | undefined>(undefined);
  const onDrop = useRef<(p: Imported) => void>(() => undefined);
  onDrop.current = (p) => {
    if (p.error) return fail(new Error(p.error));
    for (const path of p.tests ?? []) addLog(`Added ${path}`);
    const opened = p.workflows?.[0];
    if (opened) {
      for (const path of p.workflows!.slice(1)) addLog(`Added ${path}`);
      void loadEntries(currentWorkspace.current);
      return guard(() => {
        pendingSamples.current = p.files.length ? { path: opened, files: p.files } : undefined;
        void openWorkflow(opened);
      });
    }
    if (!p.files.length) {
      if (p.tests?.length) void loadEntries(currentWorkspace.current);
      return;
    }
    if (view !== "editor") return addLog("Open a workflow, then drop its sample files.", true);
    applySamples(p.files, { preferred: selection.kind === "source" ? selection.id : undefined });
  };
  useEffect(() => {
    const pending = pendingSamples.current;
    if (!pending || view !== "editor" || workflowPath !== pending.path || parsed.error) return;
    pendingSamples.current = undefined;
    applySamples(pending.files);
  });
  async function importFiles() {
    try {
      onDrop.current(await native<Imported>("import_files"));
    } catch (e) {
      fail(e);
    }
  }
  async function clearWebWorkspace() {
    try {
      await native("clear_workspace");
      doc.load(workflowTemplates[1].source);
      doc.markSaved(workflowTemplates[1].source);
      updateYamlDraft();
      setWorkflowPath("");
      setRevision(null);
      setRun(undefined);
      setView("home");
      await loadEntries(currentWorkspace.current);
      say("Removed your workflows, samples and runs from this browser.");
    } catch (e) {
      fail(e);
    }
  }
  async function downloadYaml() {
    const name = workflowPath ? workflowPath.split("/").pop()! : `${slug(pipeline.name) || "workflow"}.yaml`;
    try {
      await native("download_text", { name, text: latestEditor().source });
    } catch (e) {
      fail(e);
    }
  }
  async function downloadOutput() {
    if (!artifact) return;
    const ext = artifact.result.path.split(".").pop();
    try {
      await native("download_file", { path: artifact.result.path, name: `${slug(pipeline.name) || "output"}.${ext}` });
    } catch (e) {
      fail(e);
    }
  }
  // The browser build has its workspace from the start: no folder to choose.
  useEffect(() => {
    if (web) void chooseWorkspace();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [web]);
  useEffect(() => {
    let alive = true;
    const offs: (() => void)[] = [];
    void listenNative<{ files: ImportedFile[]; error?: string }>("samples-dropped", (p) => onDrop.current(p)).then((off) => (alive ? offs.push(off) : off()));
    void listenNative<boolean>("samples-drag", (on) => setDropping(on)).then((off) => (alive ? offs.push(off) : off()));
    return () => {
      alive = false;
      offs.forEach((off) => off());
    };
  }, []);

  // ---- live preview: trace the workflow on the sample files ----------------------------
  const focus = selectedStep?.top ?? -1;
  // Recorded answers for sql and rest steps live beside the tests, by convention.
  const mocksPath = workflowPath ? `tests/${base(workflowPath)}.mocks.yaml` : "";
  const skip = [...skipped].sort((a, b) => a - b);
  const editorOpen = desktop && !!workspace && view === "editor" && !sampleSetup;
  const documentKey = JSON.stringify([workspace, workflowPath, documentEpoch]);
  const samplePaths = pipeline.inputs.map((input) => bindings[input.id]).filter(Boolean);
  const fileRevisions = useFileRevisions(editorOpen ? workspace : "", [...samplePaths, ...(mocksPath ? [mocksPath] : []), ...(artifact ? [artifact.result.path] : [])]);
  const openRuns = () => {
    fileRevisions.refresh();
    setWorkspacePanel("runs");
  };
  const hasRevision = (path: string) => Object.hasOwn(fileRevisions.revisions ?? {}, path);
  const filesReady = samplePaths.every(hasRevision) && !fileRevisions.error;
  const samples = pipeline.inputs.map(({ id }) => [id, bindings[id], fileRevisions.revisions?.[bindings[id]]]);
  const runKey = JSON.stringify([workspace, documentEpoch, source, samples, mocksPath, fileRevisions.revisions?.[mocksPath]]);
  const runIdentity = useMemo(() => ({ key: runKey }), [runKey, bindingEpoch, view, workflowPath]);
  const navigation = useMemo(() => ({ selection, panel, diagram }), [selection, panel, diagram]);
  const runContext = useRef<{ identity: RunIdentity; navigation: typeof navigation; panelOpen: boolean } | undefined>(undefined);
  useLayoutEffect(() => {
    runContext.current = { identity: runIdentity, navigation, panelOpen };
    return () => { runContext.current = undefined; };
  }, [runIdentity, navigation, panelOpen]);
  const runRequest = useRef(0);
  const canPreview = editorOpen && bound && !parsed.error;
  const canRun = canPreview && filesReady;
  const previousRun = !!artifact && artifact.key !== runKey;
  const currentRunFailure = runFailure?.identity === runIdentity ? runFailure.error : undefined;
  const busy = savingTest ? "Saving test" : saving ? "Saving" : running === runIdentity ? "Running" : "";
  const previewLimit = 200;
  const traceScope = JSON.stringify([documentKey, selectedStep?.id, selectedStep?.op]);
  const compare = selectedStep?.depth === 0 && (selectedStep.op === "fill" || selectedStep.op === "filter");
  // Source, bindings, and file revisions belong to the run identity. A new
  // identity or skip set discards both completed and in-flight trace entries.
  const traceCache = useMemo(() => new RequestCache<Trace>(
    4 * 1024 * 1024, (data) => !data.steps.some((step) => step.error),
  ), [runIdentity, skipped]);
  const trace = useCurrentRequest(
    canRun ? JSON.stringify([focus, compare, previewLimit]) : null,
    traceScope,
    () => request<Trace>("trace", { workspace, yaml: source, inputs: bindings, step: focus, skip, compare, mocks: mocksPath, limit: previewLimit }),
    280,
    (data) => {
      const lines = data.steps.flatMap((s) => (s.log ?? []).map((l) => `${s.step + 1}. ${l}`));
      if (lines.length) setLog((l) => [...l.slice(-300), ...lines.map((text) => ({ time: now(), text }))]);
    },
    traceCache,
  );
  const currentTrace = trace.data;
  const traceUpdating = canPreview && !fileRevisions.error && (!filesReady || trace.pending);
  const selectedStepId = selectedStep?.id;
  // The grid handles each request in a layout effect before this cleanup.
  useEffect(() => {
    if (columnJump) setColumnJump(undefined);
  }, [columnJump]);

  const stepPreview = selectedStep ? currentTrace?.steps[selectedStep.top] : undefined;
  const jumpColumns = stepPreview?.rows?.length && !stepPreview.error && !stepPreview.skipped
    ? stepPreview.columns.map((c) => c.name)
    : [];
  const goToColumn = (name: string) => {
    if (!currentTrace || !selectedStepId || !jumpColumns.includes(name)) return;
    showTable();
    setColumnJump({ name, request: ++jumpRequest.current, stepId: selectedStepId, trace: currentTrace });
  };
  // Source and output previews read files directly.
  const fileToInspect =
    selection.kind === "source" ? bindings[selection.id]
    : selection.kind === "output" && !workbookOutput ? artifact?.result.path
    : undefined;
  const inspectedRevision = fileToInspect ? fileRevisions.revisions?.[fileToInspect] : undefined;
  const worksheetKey = selection.kind === "source" && fileToInspect
    ? JSON.stringify([documentKey, selection.id, fileToInspect, inspectedRevision, bindingEpoch])
    : undefined;
  const inspectScope = worksheetKey ?? JSON.stringify([documentKey, fileToInspect]);
  const inspectOffset = worksheetPosition?.key === worksheetKey ? worksheetPosition?.offset ?? 0 : 0;
  useLayoutEffect(() => {
    setWorksheetPosition((position) => position?.key === worksheetKey ? position : undefined);
  }, [worksheetKey]);
  const inspection = useCurrentRequest(
    editorOpen && fileToInspect && hasRevision(fileToInspect) && !fileRevisions.error
      ? JSON.stringify([inspectScope, inspectedRevision, bindingEpoch, inspectOffset, previewLimit])
      : null,
    inspectScope,
    () => request<Inspection>("inspectWorkbook", { workspace, path: fileToInspect, offset: inspectOffset, limit: previewLimit }),
  );

  // Validate with the production parser shortly after each edit.
  const validation = useCurrentRequest(
    editorOpen && !parsed.error ? JSON.stringify([documentKey, source]) : null,
    documentKey,
    () => request<Validation>("validate", { workspace, yaml: source }),
    450,
  );
  const retryTrace = () => { fileRevisions.refresh(); trace.retry(); };
  const retryInspection = () => { fileRevisions.refresh(); inspection.retry(); };

  // ---- problems -------------------------------------------------------------------------------
  const problems: Problem[] = [];
  let validationProblem: Problem | undefined;
  if (parsed.error) problems.push({ level: "error", text: parsed.error });
  if (validation.error) {
    const m = validation.error.message.match(/^step (\d+)/i);
    validationProblem = {
      level: "error",
      text: validation.error.message,
      code: validation.error.code,
      step: m ? `steps.${Number(m[1]) - 1}` : undefined,
      retry: validation.retry,
      retryLabel: "Retry validation",
    };
    problems.push(validationProblem);
  }
  // Validation and preview can report the same parser failure.
  const addTraceProblem = (text: string, code?: string, step?: string) => {
    const message = (value: string) => value.replace(/^step \d+:\s*/i, "");
    if (validationProblem && message(validationProblem.text) === message(text)
        && (!validationProblem.step || !step || validationProblem.step === step)) {
      validationProblem.step ??= step;
      validationProblem.code ??= code;
      return;
    }
    problems.push({ level: "error", text, code, step, retry: retryTrace, retryLabel: "Retry preview" });
  };
  currentTrace?.steps.forEach((s) => s.error && addTraceProblem(`Step ${s.step + 1}: ${s.error}`, undefined, `steps.${s.step}`));
  if (trace.error) addTraceProblem(trace.error.message, trace.error.code);
  if (inspection.error) problems.push({ level: "error", text: inspection.error.message, code: inspection.error.code, retry: retryInspection, retryLabel: "Retry file preview" });
  if (fileRevisions.error) problems.push({ level: "error", text: fileRevisions.error, retry: fileRevisions.refresh, retryLabel: "Retry checking files" });
  if (currentRunFailure) problems.push({ level: "error", text: currentRunFailure.message, code: currentRunFailure.code, retry: () => void runWorkflow(), retryLabel: "Retry run" });
  pipeline.inputs.forEach((i) => !bindings[i.id] && problems.push({ level: "warning", text: `Source “${i.id}” has no sample file, so the table cannot be previewed.` }));

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

  const reportStorageError = () => say("Draft recovery storage is unavailable.");
  function discardEditorChanges() {
    const current = latestEditor();
    clearUnassignedDraft.current?.();
    const removed = recovery.clearCurrent();
    const originalRemoved = !restoredDraft.current || recovery.discard(restoredDraft.current);
    if (!removed || !originalRemoved) reportStorageError();
    restoredDraft.current = undefined;
    updateYamlDraft();
    doc.load(current.savedSource);
    if (!workflowPath) setDraftId("");
    selections.current = { undo: [], redo: [] };
  }
  function guard(action: () => void) {
    if (savePending.current || choosingWorkspace.current) return;
    const current = latestEditor();
    if (current.source !== current.savedSource) {
      setPendingDiscard(() => action);
    } else {
      openRequest.current++;
      action();
    }
  }
  async function chooseWorkspace() {
    if (savePending.current || choosingWorkspace.current) return;
    choosingWorkspace.current = true;
    openRequest.current++;
    try {
      const picked = await native<{ path: string } | null>("choose_workspace");
      if (!picked) return;
      const previousWorkspace = currentWorkspace.current;
      if (!previousWorkspace && draftId) {
        const original = restoredDraft.current;
        clearUnassignedDraft.current = () => {
          const activeRemoved = recovery.clearCurrent();
          const originalRemoved = !original || recovery.discard(original);
          if (activeRemoved && originalRemoved) clearUnassignedDraft.current = undefined;
          else reportStorageError();
        };
        const current = latestEditor();
        if (current.source === current.savedSource || recovery.writeCurrent({
          ...current, workflowPath: "", draftId, baseRevision: null,
        }, picked.path)) {
          clearUnassignedDraft.current();
        }
        restoredDraft.current = undefined;
      }
      if (previousWorkspace && previousWorkspace !== picked.path) {
        doc.load(workflowTemplates[1].source);
        updateYamlDraft();
        restoredDraft.current = undefined;
        setWorkflowPath("");
        setRevision(null);
        setDraftId("");
        setView("home");
        setEntries([]);
      }
      currentWorkspace.current = picked.path;
      setWorkspace(picked.path);
      await loadEntries(picked.path);
      addLog(web ? "Workspace in this browser: nothing leaves this computer" : `Opened folder ${picked.path}`);
    } catch (e) {
      fail(e);
    } finally {
      choosingWorkspace.current = false;
    }
  }
  async function loadEntries(root: string) {
    const list = await native<{ paths: string[] }>("list_workflows");
    const out: WorkflowEntry[] = [];
    for (const path of list.paths) {
      if (currentWorkspace.current !== root) return;
      try {
        const file = await native<WorkflowFile>("read_workflow", { path });
        const y = parse(file.source) as { name?: string; service?: { route?: string } };
        out.push({ path, name: y?.name ?? "", route: y?.service?.route ?? base(path) });
      } catch {
        out.push({ path, name: "", route: base(path) });
      }
    }
    if (currentWorkspace.current === root) setEntries(out);
  }
  const resetEditor = () => {
    updateYamlDraft();
    selections.current = { undo: [], redo: [] };
    setDocumentEpoch((value) => value + 1);
    setSkipped(new Set());
    setRun(undefined);
    setRunFailure(undefined);
    setSelection({ kind: "step", id: "steps.0" });
    setView("editor");
    setDiagram("unopened");
    setPanel("table");
    setSampleSetup(false);
  };
  function loadWorkflow(file: WorkflowFile, setup = false, record?: RecoveryDraft) {
    doc.load(file.source);
    setWorkflowPath(file.path);
    setRevision(file.revision);
    setDraftId("");
    resetEditor();
    restoredDraft.current = record;
    if (record) doc.setSource(record.source);
    if (setup && !record) {
      readPipeline(file.source);
      setSampleSetup(true);
    }
    addLog(record ? `Restored draft for ${file.path}` : `Opened ${file.path}`);
  }
  function showRecoveryReview(next: RecoveryReview) {
    setReviewError(undefined);
    setRecoveryReview(next);
  }
  async function reviewDraft(record: RecoveryDraft, setup = false) {
    const id = ++openRequest.current;
    let disk: WorkflowFile | null = null;
    if (record.workflowPath) {
      try {
        disk = await native<WorkflowFile>("read_workflow", { path: record.workflowPath });
      } catch {
        // An inaccessible or removed saved file can still be recovered to a different path.
      }
    }
    if (id !== openRequest.current) return;
    showRecoveryReview({
      path: record.workflowPath,
      disk,
      record,
      conflict: !!record.workflowPath && disk?.revision !== record.baseRevision,
      setup,
    });
  }
  async function openWorkflow(path: string, setup = false) {
    const record = recovery.refresh().find((draft) => draft.workflowPath === path);
    if (record) return reviewDraft(record, setup);
    const id = ++openRequest.current;
    try {
      const file = await native<WorkflowFile>("read_workflow", { path });
      if (id !== openRequest.current) return;
      const canonicalDraft = recovery.refresh().find((draft) => draft.workflowPath === file.path);
      if (canonicalDraft) {
        showRecoveryReview({
          path: file.path, disk: file, record: canonicalDraft,
          conflict: file.revision !== canonicalDraft.baseRevision, setup,
        });
      } else {
        loadWorkflow(file, setup);
      }
    } catch (e) {
      if (id === openRequest.current) fail(e);
    }
  }
  function startTemplate(t: Template) {
    doc.load(t.source);
    doc.markSaved("");
    setWorkflowPath("");
    setRevision(null);
    setDraftId(createDraftId());
    restoredDraft.current = undefined;
    resetEditor();
    const key = `mai.bindings:${workspace}:${readPipeline(t.source).name}`;
    if (!Object.keys(store.get(key, {})).length && Object.keys(t.samples).length) store.set(key, t.samples);
    setBindingState({ key, values: store.get(key, t.samples) });
    say(`New workflow from “${t.title}”. Save it to serve it as /api/${slug(readPipeline(t.source).name)}.`);
  }
  function restoreRecovery() {
    const record = recoveryReview?.record;
    if (!record) return;
    if (record.workflowPath) return void openDiskVersion("restore");
    doc.load(record.savedSource);
    doc.setSource(record.source);
    setWorkflowPath("");
    setRevision(null);
    setDraftId(record.draftId);
    resetEditor();
    restoredDraft.current = record;
    setRecoveryReview(undefined);
  }
  async function openDiskVersion(action: "open" | "restore" | "reload") {
    const review = recoveryReview;
    if (!review || reviewBusy) return;
    setReviewBusy(true);
    setReviewError(undefined);
    try {
      const file = await native<WorkflowFile>("read_workflow", { path: review.path });
      if (action === "restore" && file.revision !== review.record?.baseRevision) {
        showRecoveryReview({ ...review, disk: file, conflict: true });
        return;
      }
      if (action === "reload") {
        if (review.record && !recovery.discard(review.record)) {
          setReviewError("Draft recovery storage is unavailable.");
          return;
        }
        if (!review.record) discardEditorChanges();
      }
      loadWorkflow(file, review.setup, action === "restore" ? review.record : undefined);
      setRecoveryReview(undefined);
    } catch (error) {
      if (action === "restore") showRecoveryReview({ ...review, disk: null, conflict: true });
      setReviewError(errorMessage(error));
    } finally {
      setReviewBusy(false);
    }
  }
  function discardRecoveryCopy() {
    const record = recoveryReview?.record;
    if (record && !recovery.discard(record)) {
      setReviewError("Draft recovery storage is unavailable.");
      return;
    }
    setRecoveryReview(undefined);
  }
  function beginSaveAs(path: string, opener: HTMLElement | null = null) {
    if (savePending.current) return;
    saveOpener.current = opener;
    setSaveError(undefined);
    setSaveAs(path);
  }
  function saveCopy() {
    const path = recoveryReview?.path;
    const extension = path?.match(/\.ya?ml$/i)?.[0] ?? ".yaml";
    beginSaveAs(path ? `${path.slice(0, -extension.length)}-copy${extension}` : `workflows/${slug(pipeline.name)}-copy.yaml`);
  }
  async function save(path = workflowPath, opener: HTMLElement | null = null) {
    if (!desktop || !workspace) { say("Open a project folder to save workflows."); return null; }
    if (savePending.current || choosingWorkspace.current) return null;
    if (!path) {
      beginSaveAs(`workflows/${slug(pipeline.name)}.yaml`, opener);
      return null;
    }
    const review = recoveryReview;
    const copy = !!review && saveAs !== null;
    const recoveredCopy = copy ? review.record : undefined;
    const record = recoveredCopy ?? restoredDraft.current;
    if (!recoveredCopy) commitYamlDraft();
    const submitted = recoveredCopy?.source ?? latestEditor().source;
    const expectedRevision = !copy && path === workflowPath ? revision : null;
    const saveBindings = bindings;
    openRequest.current++;
    try {
      savePending.current = true;
      setSaving(true);
      setSaveError(undefined);
      const saved = await native<WorkflowFile>("save_workflow", {
        path, source: submitted, expectedRevision,
      });
      if (recoveredCopy) {
        loadWorkflow(saved);
      } else {
        const latest = latestEditor().source;
        const pendingDraftWritten = latest === saved.source || recovery.writeCurrent({
          workflowPath: saved.path,
          draftId: "",
          baseRevision: saved.revision,
          source: latest,
          savedSource: saved.source,
        });
        if (pendingDraftWritten && (saved.path !== workflowPath || latest === saved.source)) {
          recovery.clearCurrent();
        }
        if (pendingDraftWritten) clearUnassignedDraft.current?.();
        doc.markSaved(saved.source);
        store.set(`mai.bindings:${workspace}:${saved.path}`, saveBindings);
        setWorkflowPath(saved.path);
        setRevision(saved.revision);
        setDraftId("");
      }
      if (record) recovery.discard(record);
      restoredDraft.current = undefined;
      setSaveAs(null);
      setRecoveryReview(undefined);
      say(`Saved ${saved.path}`);
      addLog(`Saved ${saved.path}`);
      loadEntries(workspace).catch(() => undefined);
      return saved.revision;
    } catch (error) {
      const message = errorMessage(error);
      if (message.startsWith("Workflow conflict:") && !copy) {
        let disk: WorkflowFile | null = null;
        try {
          disk = await native<WorkflowFile>("read_workflow", { path });
        } catch {
          // The workflow may have been removed since opening.
        }
        recovery.flush();
        setSaveAs(null);
        showRecoveryReview({ path, disk, conflict: true });
      } else if (saveAs !== null) {
        setSaveError(message);
      } else {
        fail(error);
      }
      return null;
    } finally {
      savePending.current = false;
      setSaving(false);
    }
  }
  /** Why a run cannot be kept as a test right now ("" when it can). */
  const testBlocker = !desktop ? "Saving a test needs the desktop app." : !workspace ? "Open a project folder first." : !workflowPath || doc.dirty ? "Save the workflow first: a test runs the saved file." : !bound ? "Give every source a sample file first." : "";
  async function saveTest(name: string) {
    try {
      setSavingTest(true);
      const saved = await request<{ file: string; expected: string }>("saveTest", { workspace, path: workflowPath, inputs: bindings, mocks: mocksPath, name });
      setTestName(null);
      addLog(`Test “${name}” added to ${saved.file}; expected output ${saved.expected}`);
      say(`Test saved to ${saved.file}. Run the tests with: migo test tests`);
    } catch (e) {
      fail(e);
    } finally {
      setSavingTest(false);
    }
  }
  async function runWorkflow() {
    if (!canPreview) return say(!desktop ? "Running needs the desktop app." : !workspace ? "Open a project folder first." : !bound ? "Give every source a sample file first." : "Fix the YAML first.");
    if (!filesReady) return say(fileRevisions.error ?? "Checking sample files…");
    const id = ++runRequest.current;
    const isCurrent = () => runRequest.current === id && runContext.current?.identity === runIdentity;
    try {
      setRunning(runIdentity);
      setRunFailure(undefined);
      addLog(`Run started: ${pipeline.inputs.map((i) => bindings[i.id]).join(", ")}`);
      const result = await request<RunResult>("run", { workspace, workflowPath, yaml: source, inputs: bindings, omitPreview: true, mocks: mocksPath });
      if (!isCurrent()) return;
      setRun({ key: runKey, workspace, completedAt: Date.now(), result });
      addLog(`Wrote ${result.path}`);
      say(`Run finished. Output written to ${result.path}`);
      if (pipeline.outputs.length && !runContext.current?.panelOpen && runContext.current?.navigation === navigation) {
        setSelection({ kind: "output", index: 0 });
        showTable();
      }
    } catch (e) {
      if (!isCurrent()) return;
      setRunFailure({ identity: runIdentity, error: asRunnerError(e) });
      fail(e);
      setConsoleTab("problems");
    } finally {
      if (runRequest.current === id) setRunning(undefined);
    }
  }
  const toggleSkip = (top: number) => {
    const next = new Set(skipped);
    if (next.has(top)) next.delete(top);
    else next.add(top);
    setSkipped(next);
  };

  // ---- close guard and shortcuts ------------------------------------------------------------
  const closeGuard = useRef<() => boolean>(() => false);
  closeGuard.current = () => {
    if (savePending.current) return true;
    const editor = latestEditor();
    return editor.source !== editor.savedSource;
  };
  useEffect(() => {
    if (!("__TAURI_INTERNALS__" in window)) return;
    let stop: (() => void) | undefined;
    let disposed = false;
    const w = getCurrentWindow();
    w.onCloseRequested((e) => {
      if (!closeGuard.current()) return;
      e.preventDefault();
      if (!savePending.current) setPendingDiscard(() => () => void w.destroy());
    }).then((s) => { if (disposed) s(); else stop = s; }, () => undefined);
    return () => {
      disposed = true;
      stop?.();
    };
  }, []);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!closeGuard.current()) return;
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, []);
  const keys = useRef<(e: KeyboardEvent) => void>(() => undefined);
  const saveCurrent = useRef(save);
  saveCurrent.current = save;
  const compactLayout = () => window.matchMedia?.("(max-width: 720px)").matches;
  const closeInspector = () => {
    panelFocus.current = "table";
    if (!compactLayout()) setShowInsp(false);
    if (panel !== "table") setPanel("table");
  };
  const toggleInspector = () => {
    const compact = compactLayout();
    if (showInsp && (!compact || panel === "insp")) {
      closeInspector();
    } else {
      setShowInsp(true);
      if (compact) {
        panelFocus.current = "insp";
        setPanel("insp");
      }
    }
  };
  keys.current = (e: KeyboardEvent) => {
    if (panelOpen || palette || saveAs !== null || recoveryReview || packaging || pendingDiscard || sampleSetup) return;
    const mod = e.metaKey || e.ctrlKey;
    const active = document.activeElement;
    const typing = active instanceof HTMLElement && /INPUT|TEXTAREA|SELECT/.test(active.tagName);
    const k = e.key.toLowerCase();
    if (mod && k === "k") { e.preventDefault(); setPalette("all"); return; }
    if (mod && k === "s") {
      if (view !== "editor") return;
      e.preventDefault();
      if (typing) active.blur();
      setTimeout(() => void saveCurrent.current(undefined, active instanceof HTMLElement ? active : null), 0);
      return;
    }
    if (view !== "editor") return;
    if (mod && e.key === "Enter") { e.preventDefault(); void runWorkflow(); return; }
    if (mod && e.shiftKey && k === "a") { e.preventDefault(); setPalette("add"); return; }
    if (mod && k === "1") { e.preventDefault(); setShowNav((v) => !v); return; }
    if (mod && k === "2") { e.preventDefault(); toggleInspector(); return; }
    if (typing) return;
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
    const steps = currentTrace?.steps;
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
    ...(editorOpen && workflowPath ? [{ group: "View", label: "Workflow tests", glyph: "✓", run: () => setWorkspacePanel("tests") }] : []),
    ...(desktop && workspace ? [{ group: "View", label: "Runs", glyph: "▤", run: openRuns }] : []),
    ...(editorOpen && workbookOutput ? [{ group: "View", label: "Open workbook preview", glyph: "▤", run: () => {
      setSelection({ kind: "output", index: 0 });
      showTable();
    } }] : []),
    ...(editorOpen && !parsed.error ? [{group: "Workflow", label: "Set up samples", glyph: "↳", run: () => setSampleSetup(true)}] : []),
    { group: "Workflow", label: "Run with sample files", glyph: "▶", kbd: shortcut("Enter"), run: () => void runWorkflow() },
    ...(view === "editor" ? [
      ...(desktop && !workspace ? [{ group: "Workflow", label: "Open project folder…", glyph: "▤", run: () => void chooseWorkspace() }] : []),
      { group: "Workflow", label: "Save", glyph: "↓", kbd: shortcut("S"), run: () => void save() },
      { group: "Workflow", label: "Save as…", glyph: "↓", run: () => beginSaveAs(workflowPath || `workflows/${slug(pipeline.name)}.yaml`) },
      ...(web ? [{ group: "Workflow", label: "Download YAML", glyph: "↓", run: () => void downloadYaml() }] : []),
      ...(web && artifact ? [{ group: "Workflow", label: "Download run output", glyph: "↓", run: () => void downloadOutput() }] : []),
      ...(web ? [{ group: "Workflow", label: "Open files…", glyph: "▤", run: () => guard(() => void importFiles()) }] : []),
      ...(dirty ? [{ group: "Workflow", label: "Discard recovery copy", glyph: "×", run: () => {
        clearUnassignedDraft.current?.();
        const removed = recovery.clearCurrent();
        const originalRemoved = !restoredDraft.current || recovery.discard(restoredDraft.current);
        if (originalRemoved) restoredDraft.current = undefined;
        if (removed && originalRemoved) say("Discarded recovery copy.");
        else reportStorageError();
      } }] : []),
    ] : []),
    { group: "Workflow", label: "API endpoint and distribution", glyph: "⇄", run: () => setSelection({ kind: "endpoint" }) },
    { group: "Workflow", label: "Save this run as a test…", hint: "Keep the sample files and the output as a regression test", glyph: "✓", run: () => (testBlocker ? say(testBlocker) : setTestName("")) },
    { group: "Workflow", label: "Build Linux package…", glyph: "▣", run: () => setPackaging(true) },
    { group: "Workflow", label: "All workflows", glyph: "⌂", run: () => guard(() => setView("home")) },
    { group: "Edit", label: "Undo", glyph: "↶", kbd: shortcut("Z"), run: undo },
    { group: "Edit", label: "Redo", glyph: "↷", kbd: shortcut("Z", true), run: redo },
    ...addCommands,
    ...pipeline.steps.map((s) => ({ group: "Go to", label: `${s.depth ? "  " : `${s.top + 1}. `}${s.summary}`, glyph: s.spec?.icon, run: () => setSelection({ kind: "step", id: s.id }) })),
    ...pipeline.inputs.map((i) => ({ group: "Go to", label: `Source ${i.id}`, glyph: "↳", run: () => setSelection({ kind: "source", id: i.id }) })),
    ...entries.filter((w) => w.path !== workflowPath).map((w) => ({ group: "Open workflow", label: w.name || w.path, hint: w.path, glyph: "▤", run: () => guard(() => void openWorkflow(w.path)) })),
    ...workflowTemplates.map((t) => ({ group: "New from template", label: t.title, hint: t.description, glyph: "+", run: () => guard(() => startTemplate(t)) })),
    { group: "View", label: "Toggle steps panel", glyph: "◧", kbd: shortcut("1"), run: () => setShowNav((v) => !v) },
    { group: "View", label: "Toggle inspector", glyph: "◨", kbd: shortcut("2"), run: toggleInspector },
    ...(view === "editor" ? [{ group: "View", label: "Show workflow diagram", glyph: "◇", run: showDiagram }] : []),
    { group: "View", label: "Show YAML", glyph: "{}", run: () => setConsoleTab("yaml") },
    { group: "View", label: "Switch appearance", glyph: "◐", run: () => setTheme(theme === "system" ? "light" : theme === "light" ? "dark" : "system") },
  ];

  // ---- the stage: what the table shows ---------------------------------------------------------
  const stepTitle = selectedStep && `${selectedStep.depth ? `In step ${selectedStep.top + 1}` : `${selectedStep.top + 1}.`} ${selectedStep.summary}`;
  const setupButton = workspace && !parsed.error && <button className="btn" onClick={() => setSampleSetup(true)}>Set up samples</button>;
  const stage = (() => {
    const notReady = (title: string, body: React.ReactNode, action?: React.ReactNode) => (
      <DataGrid columns={[]} rows={[]} total={0} header={<b>{stepTitle ?? title}</b>} empty={<div className="empty">{title !== stepTitle && <b>{title}</b>}<span>{body}</span>{action}</div>} />
    );
    if (selection.kind === "endpoint")
      return notReady("API endpoint", <>MaiService serves this workflow at <code>/api/{route}</code>. Its settings and request format are in the inspector.</>);
    if (selection.kind === "source" || selection.kind === "output") {
      if (selection.kind === "output" && workbookOutput && artifact) {
        return <WorkbookPreview
          key={JSON.stringify([workspace, artifact.result.path])}
          workspace={workspace}
          path={artifact.result.path}
          revision={fileRevisions.revisions?.[artifact.result.path]}
          revisionError={fileRevisions.error}
          onRetryRevision={fileRevisions.refresh}
          initialSheet={pipeline.outputs[selection.index]?.name}
          completedAt={artifact.completedAt}
          stale={previousRun}
          canRefresh={canRun && running !== runIdentity}
          onRefresh={() => void runWorkflow()}
          onSheetChange={(sheet) => {
            const output = pipeline.outputs.find((item) => item.name === sheet);
            if (output) setSelection({ kind: "output", index: output.index });
          }}
        />;
      }
      const name = selection.kind === "source" ? `Source ${selection.id}` : `Output ${pipeline.outputs[selection.index]?.name}`;
      if (!desktop) return notReady(name, "Open the desktop app to read files.");
      if (!workspace) return notReady(name, "Open a project folder to read its files.", <button className="primary" onClick={() => void chooseWorkspace()}>Open folder…</button>);
      if (!fileToInspect) return notReady(name, selection.kind === "source" ? "Choose a sample file to preview this source." : `Run the workflow (${shortcut("Enter")}) to write this output and see it here.`, selection.kind === "output" ? <button className="primary" onClick={() => void runWorkflow()}>▶ Run</button> : setupButton);
      if (fileRevisions.error) return notReady(name, <span className="err">{fileRevisions.error}</span>, <button className="btn" onClick={fileRevisions.refresh}>Retry checking files</button>);
      if (inspection.error) return notReady(name, <span className="err">{inspection.error.message}</span>, <div className="actions-row"><button className="btn" onClick={retryInspection}>Retry file preview</button>{selection.kind === "source" && setupButton}</div>);
      const d = inspection.data ?? inspection.previous;
      if (!d) return notReady(name, "Reading…");
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
          rowOffset={raw ? d.offset : undefined}
          hideProfiles={raw}
          header={<><b>{name}</b><span className="meta mono">{fileToInspect}{d.sheets.length > 1 || d.sheet !== "Sheet1" ? ` › ${d.sheet}` : ""}</span><span className="meta">{total.toLocaleString()} rows · {head.length} columns</span>{!inspection.data && <span className="meta">Updating…</span>}{!raw && previousRun && <><span className="tag warn">Previous run</span><button className="btn" disabled={!canRun || running === runIdentity} onClick={() => void runWorkflow()}>Run to refresh</button></>}</>}
          controls={worksheetKey && total > 0 && (
            <form className="worksheet-row-controls" key={worksheetKey} onSubmit={(event) => {
              event.preventDefault();
              const row = Number(new FormData(event.currentTarget).get("row"));
              if (Number.isSafeInteger(row) && row >= 1 && row <= total) {
                setWorksheetPosition({ key: worksheetKey, offset: row - 1 });
              }
            }}>
              <label htmlFor="worksheet-row">Worksheet row</label>
              <input id="worksheet-row" name="row" className="ctl" type="number" min={1} max={total} required defaultValue={1} />
              <button className="btn" type="submit">Go to row</button>
              <button className="btn" type="button" onClick={(event) => {
                event.currentTarget.form?.reset();
                setWorksheetPosition({ key: worksheetKey, offset: 0 });
              }}>First row</button>
            </form>
          )}
        />
      );
    }
    const s = selectedStep;
    if (!s) return notReady("No step selected", "Choose a step on the left.");
    const title = stepTitle ?? s.summary;
    if (!desktop) return notReady(title, "Open the desktop app to see the table after each step.");
    if (!workspace) return notReady("Open a project folder", "Studio reads sample files from a folder on this computer.", <button className="primary" onClick={() => void chooseWorkspace()}>Open folder…</button>);
    if (!bound && !parsed.error) {
      const missing = pipeline.inputs.filter((i) => !bindings[i.id]);
      const many = missing.length > 1;
      return notReady(many ? "Add sample files" : "Add a sample file",
        <>Choose or drop sample files for <b>{missing.map(i => i.id).join(", ")}</b> to preview the workflow.</>,
        <div className="actions-row"><button className="primary" onClick={() => void importSamples(many ? undefined : missing[0].id)}>{many ? "Import sample files…" : "Import sample file…"}</button>{setupButton}</div>);
    }
    if (parsed.error) return notReady("The YAML has an error", <span className="err">{parsed.error}</span>);
    const retryPreview = <button className="btn" onClick={retryTrace}>Retry preview</button>;
    if (fileRevisions.error) return notReady(title, <span className="err">{fileRevisions.error}</span>, <button className="btn" onClick={fileRevisions.refresh}>Retry checking files</button>);
    if (trace.error) return notReady(title, <span className="err">{trace.error.message}</span>, <div className="actions-row">{retryPreview}{setupButton}</div>);
    const steps = currentTrace?.steps ?? (traceUpdating ? trace.previous?.steps : undefined);
    const t = steps?.[s.top];
    if (!currentTrace && (!t || t.error || !t.columns.length)) return notReady(title, "Running the steps on the sample files…");
    if (!t) {
      const failed = steps?.at(-1);
      if (!failed?.error) return notReady(title, "There is no preview for this step.", retryPreview);
      return notReady(title, <>Step {failed.step + 1} failed, so this step did not run.<span className="err">{failed.error}</span></>, <div className="actions-row"><button className="btn" onClick={() => setSelection({ kind: "step", id: `steps.${failed.step}` })}>Go to step {failed.step + 1}</button>{retryPreview}</div>);
    }
    const prev = s.top > 0 ? steps?.[s.top - 1] : undefined;
    const added = new Set(t.columns.filter((c) => prev && !prev.columns.some((p) => p.name === c.name)).map((c) => c.name));
    const delta = prev ? t.total - prev.total : 0;
    const header = (
      <>
        <b>{title}</b>
        <span className="meta">{t.total.toLocaleString()} rows · {t.columns.length} columns</span>
        {delta !== 0 && <span className={`tag ${delta < 0 ? "minus" : "plus"}`}>{delta > 0 ? "+" : "−"}{Math.abs(delta).toLocaleString()} rows</span>}
        {added.size > 0 && <span className="tag plus">+{[...added].join(", ")}</span>}
        {currentTrace && t.skipped && <span className="tag mod">left out of preview</span>}
        {s.depth > 0 && <span className="tag">shows the table after step {s.top + 1}</span>}
        <span className="spacer" />
        {traceUpdating && <span className="meta">Updating…</span>}
      </>
    );
    if (t.error) return notReady(title, <span className="err">{t.error}</span>, retryPreview);
    if (!t.columns.length) return notReady(title, "There is no table yet at this step. Start with a Read step.");
    const focusColumn = columnJump?.stepId === s.id && columnJump.trace === currentTrace ? columnJump : undefined;
    return <StepPreview key={traceScope} title={title} trace={t} comparison={currentTrace && compare ? t.comparison : undefined}
      fillColumns={s.fillColumns} added={added} header={header} onAction={currentTrace ? onColumnAction : undefined} focusColumn={focusColumn} />;
  })();

  // ---- render -------------------------------------------------------------------------------------
  const status = busy
    ? { cls: "busy", text: `${busy}…` }
    : problems.some((problem) => problem.level === "error")
      ? { cls: "bad", text: "Has errors" }
      : !desktop ? { cls: "", text: "Browser preview" }
      : !workspace ? { cls: "", text: "No folder open" }
      : !bound ? { cls: "warn", text: "Needs sample files" }
      : traceUpdating ? { cls: "busy", text: "Previewing…" }
      : validation.pending ? { cls: "busy", text: "Validating…" }
      : currentTrace ? { cls: "ok", text: `Valid · ${currentTrace.steps.at(-1)?.total.toLocaleString() ?? 0} rows out` }
      : { cls: "", text: "Ready" };
  const panes = `panes${showNav ? "" : " no-nav"}${showInsp ? "" : " no-insp"}`;
  const editorToolbar = view === "editor" && !panelOpen;
  return (
    <div ref={appRef} className="app" style={{ "--console-h": consoleH ? `${consoleH}px` : undefined } as React.CSSProperties} data-m={panelOpen ? "table" : panel} data-draft-storage-error={recovery.storageUnavailable || undefined}>
      <header className="toolbar">
        <div className="tl">
          {editorToolbar && <button className="tb hide-sm" aria-label="Steps panel" aria-pressed={showNav} title={`Steps panel (${shortcut("1")})`} onClick={() => setShowNav(!showNav)}>{icons.nav}</button>}
          <div className="title">
            {panelOpen ? <b>{workspace.split(/[\\/]/).filter(Boolean).at(-1) || workspace}</b> :
              <button onClick={() => guard(() => setView("home"))} title="All workflows">Workflows</button>}
            {editorToolbar && (
              <>
                <span className="kbd">/</span>
                <b title={workflowPath || "Not saved yet"}>{pipeline.name || "Untitled"}</b>
                <span className="edited">{dirty ? (workflowPath ? "— Edited" : "— Not saved") : ""}</span>
              </>
            )}
          </div>
        </div>
        <div className="center">{editorToolbar && <span className={`pill ${status.cls}`} role="status"><i />{status.text}</span>}</div>
        <div className="tr">
          {editorToolbar && desktop && workspace && workflowPath && <button ref={testsButton} className="tb" onClick={() => setWorkspacePanel("tests")}>Tests</button>}
          {desktop && workspace && !panelOpen && <button ref={runsButton} className="tb" onClick={openRuns}>Runs</button>}
          {!panelOpen && <button className="tb hide-sm" onClick={() => setPalette("all")} title="Command menu">Search <span className="kbd">{shortcut("K")}</span></button>}
          {editorToolbar && (
            <>
              <button className="tb" aria-label="Undo" title={`Undo (${shortcut("Z")})`} disabled={!doc.canUndo} onClick={undo}>{icons.undo}</button>
              <button className="tb hide-sm" aria-label="Redo" title={`Redo (${shortcut("Z", true)})`} disabled={!doc.canRedo} onClick={redo}>{icons.redo}</button>
            </>
          )}
          <button className="tb" aria-label={`Appearance: ${theme}`} title={`Appearance: ${theme}`} onClick={() => setTheme(theme === "system" ? "light" : theme === "light" ? "dark" : "system")}>{icons.theme}</button>
          {editorToolbar && (
            <>
              <button className="tb hide-sm" aria-label="Inspector" aria-pressed={showInsp} title={`Inspector (${shortcut("2")})`} onClick={toggleInspector}>{icons.insp}</button>
              <button className="tb hide-sm" onClick={() => void save()} disabled={!!busy} title={`Save (${shortcut("S")})`}>Save</button>
              {web && <button className="tb hide-sm" onClick={() => void downloadYaml()} title="Download the workflow YAML">↓ YAML</button>}
              {web && artifact && <button className="tb" onClick={() => void downloadOutput()} title={`Download ${artifact.result.path.split("/").pop()} from the last run`}>↓ Output</button>}
              <button className="primary" onClick={() => void runWorkflow()} disabled={!!busy} title={`Run with sample files (${shortcut("Enter")})`}>▶ Run <span className="kbd hide-sm">{shortcut("Enter")}</span></button>
            </>
          )}
        </div>
      </header>
      {recovery.storageUnavailable && (
        <div className="draft-storage-status" role="status">Draft recovery storage is unavailable. You can still save workflows.</div>
      )}

      {workspacePanel === "runs" && <Suspense fallback={<div className="empty" role="status">Loading runs…</div>}><RunsPanel
        key={workspace}
        workspace={workspace}
        refreshToken={artifact?.result.path}
        onRefreshCurrent={view === "editor" ? fileRevisions.refresh : undefined}
        onBack={() => {
          setWorkspacePanel(null);
          requestAnimationFrame(() => runsButton.current?.focus());
        }}
        current={view === "editor" ? {
          workflowPath, source,
          inputs: pipeline.inputs.map(({ id }) => ({ name: id, path: bindings[id], sha256: fileRevisions.revisions?.[bindings[id]] })),
        } : undefined}
        canRunCurrent={canRun && !busy}
        onRunCurrent={view === "editor" ? () => {
          setWorkspacePanel(null);
          void runWorkflow();
        } : undefined}
      /></Suspense>}
      {workspacePanel === "tests" && <Suspense fallback={<div className="empty" role="status">Loading workflow tests…</div>}>
        <WorkflowTests workspace={workspace} workflowPath={workflowPath} dirty={dirty} busy={!!busy}
          onSave={() => save()} onBack={() => {
            setWorkspacePanel(null);
            requestAnimationFrame(() => testsButton.current?.focus());
          }} />
      </Suspense>}
      <div className="workspace-views" hidden={panelOpen}>
      {view === "home" ? (
        <Home
          workspace={workspace}
          desktop={desktop}
          workflows={entries}
          recoveryDrafts={recovery.drafts.map((draft) => ({
            id: draftKey(draft), label: draft.workflowPath || "Untitled workflow", updatedAt: draft.timestamp,
          }))}
          onOpenRecoveryDraft={(id) => guard(() => {
            const record = recovery.refresh().find((draft) => draftKey(draft) === id);
            if (record) void reviewDraft(record);
          })}
          onOpenWorkspace={() => guard(() => void chooseWorkspace())}
          web={web ? { onImport: () => void importFiles(), onClear: () => guard(() => void clearWebWorkspace()) } : undefined}
          onOpen={(p) => guard(() => void openWorkflow(p))}
          onSetup={(path) => guard(() => void openWorkflow(path, true))}
          onTemplate={(t) => guard(() => startTemplate(t))}
          onBack={dirty || workflowPath ? () => { openRequest.current++; setView("editor"); } : undefined}
        />
      ) : (
        <div className={panes} style={paneStyle} data-step={selectedStep ? true : undefined} data-diagram={diagram === "open" || undefined}>
          <Navigator
            hidden={!showNav && !(compactLayout() && panel === "steps")}
            pipeline={pipeline}
            selection={selection}
            onSelect={(s) => { setSelection(s); if (panel === "steps") setPanel("table"); }}
            trace={currentTrace?.steps}
            skipped={skipped}
            onToggleSkip={toggleSkip}
            onAdd={() => setPalette("add")}
            onSetupSamples={desktop && workspace && !parsed.error ? () => setSampleSetup(true) : undefined}
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
            onImport={desktop && workspace ? (id) => void importSamples(id) : undefined}
          />
          {showNav && (
            <PaneSplitter label="Resize workflow navigator" controls="studio-navigator" side="left" {...paneWidths.navigator} onResize={(width) => resizePane("navigator", width)} />
          )}
          <div className="editor-stage">
            <div className="stage-switch" role="group" aria-label="Workflow view">
              <button type="button" aria-pressed={diagram !== "open"} onClick={showTable}>Table</button>
              <button type="button" aria-pressed={diagram === "open"} onClick={showDiagram}>Diagram</button>
            </div>
            <div className="editor-stage-body" hidden={diagram === "open"}>{stage}</div>
            {diagram !== "unopened" && <div className="editor-stage-body" hidden={diagram !== "open"}>
              {parsed.error ? <main className="stage empty" role="alert"><b>Fix the YAML to display the diagram</b><span>{parsed.error}</span></main> :
                <Suspense fallback={<div className="empty" role="status">Loading diagram…</div>}>
                  <WorkflowDiagram key={documentKey} active={diagram === "open" && !panelOpen} source={source}
                    pipeline={pipeline} selection={selection} onSelect={setSelection} trace={currentTrace} />
                </Suspense>}
            </div>}
          </div>
          {showInsp && (
            <>
              <PaneSplitter label="Resize inspector" controls="studio-inspector" side="right" {...paneWidths.inspector} onResize={(width) => resizePane("inspector", width)} />
              <Inspector
                source={source}
                pipeline={pipeline}
                selection={selection}
                onChange={change}
                onError={fail}
                onSelect={setSelection}
                onClose={closeInspector}
                trace={currentTrace?.steps}
                activeMocksPath={currentTrace?.mocks ? mocksPath : undefined}
                previewPending={traceUpdating}
                previewFailed={!!(trace.error || (canPreview && fileRevisions.error))}
                columns={cols}
                jumpColumns={jumpColumns}
                onGoToColumn={goToColumn}
                bindings={bindings}
                onBind={bind}
                onBrowse={desktop && workspace ? (id) => void browseSample(id) : undefined}
                inspection={selection.kind === "source" ? inspection.data : undefined}
                route={route}
                workflowPath={workflowPath}
                canPackage={desktop && !web && !!workspace && !!workflowPath && !dirty}
                onPackage={() => setPackaging(true)}
                onAddRead={(input) => {
                  change(insertStep(source, ["steps"], 0, { from: input }), `Read ${input}`);
                  setSelection({ kind: "step", id: "steps.0" });
                }}
              />
            </>
          )}
        </div>
      )}
      {view === "editor" && (
        <Console
          problems={problems}
          log={log}
          source={draftSource}
          onSourceDraft={(text) => {
            const present = doc.getCurrent().present;
            const next = keepLineEndings(present.source, text);
            updateYamlDraft(next === present.source ? undefined : { revision: present.revision, source: next });
          }}
          onSource={commitYamlDraft}
          onGoTo={(id) => setSelection({ kind: "step", id })}
          open={consoleTab}
          onOpen={setConsoleTab}
          resizer={
            consoleTab && (
              <Splitter
                className="split-console"
                label="Resize the console"
                axis="y"
                invert
                value={consoleH ?? (consoleTab === "yaml" ? Math.round(window.innerHeight * 0.4) : 128)}
                min={60}
                max={Math.max(120, window.innerHeight - 220)}
                fallback={128}
                onChange={setConsoleH}
              />
            )
          }
        />
      )}
      {dropping && view === "editor" && (
        <div className="dropzone" aria-live="polite">
          <div>
            <b>Drop to use as sample files</b>
            <span>.xlsx, .xls or .csv · matched to sources by file name{selection.kind === "source" ? `, else ${selection.id}` : ""}</span>
          </div>
        </div>
      )}
      {view === "editor" && (
        <nav className="mtabs" aria-label="Panels">
          {(["steps", "table", "diagram", "insp"] as const).map((p) => (
            <button key={p} aria-pressed={p === "diagram" ? panel === "table" && diagram === "open" : panel === p && (p !== "table" || diagram !== "open")} onClick={() => {
              if (p === "diagram") { showDiagram(); return; }
              if (p === "table" && panel === "insp") panelFocus.current = "table";
              if (p === "table") { showTable(); return; }
              if (p === "insp") {
                if (panel !== "insp") panelFocus.current = "insp";
                setShowInsp(true);
              }
              setPanel(p);
            }}>
              {p === "steps" ? "Steps" : p === "table" ? "Table" : p === "diagram" ? "Diagram" : "Settings"}
            </button>
          ))}
        </nav>
      )}

      </div>
      {sampleSetup && <SampleSetupDialog workspace={workspace} inputs={pipeline.inputs} bindings={bindings}
        onClose={() => setSampleSetup(false)} onApply={(next) => {
          applyBindings(next);
          setSampleSetup(false);
          setPanel("table");
        }} />}
      {palette && <CommandMenu commands={commands} placeholder={palette === "add" ? "Add a step after the selected one…" : "Search commands, steps and workflows…"} onClose={() => setPalette(null)} />}
      {saveAs !== null && (
        <SaveWorkflowDialog
          path={saveAs}
          route={String(pipeline.service.route ?? "") || base(saveAs)}
          copy={!!recoveryReview}
          saving={saving}
          error={saveError}
          returnFocusTo={saveOpener.current}
          onChange={(path) => { setSaveError(undefined); setSaveAs(path); }}
          onClose={() => { if (!savePending.current) setSaveAs(null); }}
          onSave={() => void save(saveAs)}
        />
      )}
      {recoveryReview && saveAs === null && (
        <Suspense fallback={<div className="scrim"><p role="status">Loading draft comparison…</p></div>}>
          <DraftRecoveryDialog
            path={recoveryReview.path || "Untitled workflow"}
            draftSource={recoveryReview.record?.source ?? draftSource}
            savedSource={recoveryReview.disk?.source ?? null}
            updatedAt={recoveryReview.record?.timestamp}
            conflict={recoveryReview.conflict}
            error={reviewError}
            busy={reviewBusy}
            onRestore={recoveryReview.record && !recoveryReview.conflict ? restoreRecovery : undefined}
            onOpenSaved={recoveryReview.disk && !recoveryReview.conflict ? () => void openDiskVersion("open") : undefined}
            onDiscard={recoveryReview.record ? discardRecoveryCopy : undefined}
            onSaveCopy={recoveryReview.conflict ? saveCopy : undefined}
            onReloadDisk={recoveryReview.disk && recoveryReview.conflict ? () => void openDiskVersion("reload") : undefined}
            onCancel={() => setRecoveryReview(undefined)}
          />
        </Suspense>
      )}
      {testName !== null && (
        <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && setTestName(null)}>
          <form className="dialog" role="dialog" aria-label="Save as test" onSubmit={(e) => { e.preventDefault(); void saveTest(testName.trim()); }}>
            <h2>Save this run as a test</h2>
            <p>Studio runs the saved workflow on the current sample files and keeps the output as the expected result. Later, <code>migo test tests</code> reports any cell that changes.</p>
            <input className="ctl" aria-label="Test name" autoFocus placeholder="What this run shows, e.g. Typical order file" value={testName} onChange={(e) => setTestName(e.target.value)} />
            <div className="hint">Added to <code>tests/{base(workflowPath)}.test.yaml</code>{trace.data?.mocks ? <>, with the answers in <code>{mocksPath}</code></> : null}. Check the expected file before you commit it.</div>
            <div className="actions">
              <button type="button" className="btn" onClick={() => setTestName(null)}>Cancel</button>
              <button className="primary" disabled={!/[A-Za-z0-9]/.test(testName) || !!busy}>Save test</button>
            </div>
          </form>
        </div>
      )}
      {packaging && (
        <Suspense fallback={<div className="scrim"><p role="status">Loading package builder…</p></div>}><PackageBuilder
          workspace={workspace}
          workflows={entries.map((e) => e.path).filter((path) => path.startsWith("workflows/"))}
          blocked={dirty}
          onPending={setPackagePending}
          onClose={() => !packagePending && setPackaging(false)}
        /></Suspense>
      )}
      {pendingDiscard && (
        <DiscardChangesDialog
          onCancel={() => setPendingDiscard(null)}
          onDiscard={() => {
            const a = pendingDiscard;
            if (savePending.current) return;
            discardEditorChanges();
            setPendingDiscard(null);
            openRequest.current++;
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
