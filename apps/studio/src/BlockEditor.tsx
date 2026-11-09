import { useEffect, useRef, useState } from "react";
import * as Blockly from "blockly/core";
import * as English from "blockly/msg/en";
import {
  loadWorkflow,
  registerWorkflowBlocks,
  serializeWorkflow,
  workflowToolbox,
} from "./blocklyWorkflow";
import { BlockInspector } from "./BlockInspector";
import "./block-editor.css";

const light = {
  workspaceBackgroundColour: "#f8fafc",
  toolboxBackgroundColour: "#ffffff",
  toolboxForegroundColour: "#1e293b",
  flyoutBackgroundColour: "#f1f5f9",
  flyoutForegroundColour: "#1e293b",
  flyoutOpacity: 1,
  scrollbarColour: "#94a3b8",
  scrollbarOpacity: 0.5,
  insertionMarkerColour: "#4f46e5",
  insertionMarkerOpacity: 0.35,
  cursorColour: "#4f46e5",
  gridColour: "#e2e8f0",
};
const dark = {
  ...light,
  workspaceBackgroundColour: "#0f172a",
  toolboxBackgroundColour: "#111827",
  toolboxForegroundColour: "#e2e8f0",
  flyoutBackgroundColour: "#1e293b",
  flyoutForegroundColour: "#e2e8f0",
  scrollbarColour: "#475569",
  insertionMarkerColour: "#a5b4fc",
  cursorColour: "#a5b4fc",
  gridColour: "#1e293b",
};
function theme(mode: "light" | "dark") {
  const { gridColour: _grid, ...componentStyles } = mode === "dark" ? dark : light;
  return Blockly.Theme.defineTheme(`mai-${mode}`, {
    name: `mai-${mode}`,
    base: Blockly.Themes.Classic,
    componentStyles,
    fontStyle: {
      family: "Inter, \"Segoe UI\", system-ui, sans-serif",
      size: 12,
      weight: "500",
    },
  });
}
const prefersDark = () =>
  typeof window !== "undefined" &&
  !!window.matchMedia?.("(prefers-color-scheme: dark)").matches &&
  document.documentElement.dataset.theme !== "light";

interface BlockEditorProps {
  source: string;
  onChange: (source: string) => void;
  disabled?: boolean;
  onDraftChange: (pending: boolean) => void;
  onAdvanced: () => void;
}

/** A Blockly canvas whose incomplete edits stay on the canvas until they can be saved. */
export function BlockEditor({
  source,
  onChange,
  disabled = false,
  onDraftChange,
  onAdvanced,
}: BlockEditorProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const workspaceRef = useRef<Blockly.WorkspaceSvg | null>(null);
  const sourceRef = useRef(source);
  const emittedRef = useRef<string | null>(null);
  const callbacksRef = useRef({ onChange, onDraftChange });
  const pendingRef = useRef(false);
  const loadingRef = useRef(false);
  const [selectedHelp, setSelectedHelp] = useState("");
  const [showHelp, setShowHelp] = useState(false);
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [loadError, setLoadError] = useState("");
  const [preservedSteps, setPreservedSteps] = useState(0);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const [inspectorOpen, setInspectorOpen] = useState(true);
  callbacksRef.current = { onChange, onDraftChange };

  function updatePending(value: boolean) {
    if (pendingRef.current !== value) {
      pendingRef.current = value;
      setPending(value);
      callbacksRef.current.onDraftChange(value);
    }
  }

  function load(sourceToLoad: string) {
    const workspace = workspaceRef.current;
    if (!workspace) return;
    loadingRef.current = true;
    setSelectedHelp("");
    sourceRef.current = sourceToLoad;
    Blockly.Events.disable();
    try {
      const result = loadWorkflow(workspace, sourceToLoad);
      sourceRef.current = sourceToLoad;
      emittedRef.current = null;
      setPreservedSteps(result.preservedSteps);
      setLoadError("");
      setMessage("");
      updatePending(false);
    } catch (error) {
      setLoadError(
        error instanceof Error
          ? error.message
          : "This workflow could not be opened in blocks.",
      );
      setMessage("");
      updatePending(false);
    } finally {
      Blockly.Events.enable();
      loadingRef.current = false;
    }
  }

  useEffect(() => {
    if (!hostRef.current) return;
    // Blockly's locale declarations include a CommonJS default export while
    // Vite uses named ESM messages. Pass only the actual string messages.
    const messages: Record<string, string> = {};
    for (const [key, value] of Object.entries(English)) {
      if (typeof value === "string") messages[key] = value;
    }
    Blockly.setLocale(messages);
    registerWorkflowBlocks();
    const workspace = Blockly.inject(hostRef.current, {
      toolbox: workflowToolbox,
      theme: theme(prefersDark() ? "dark" : "light"),
      renderer: "thrasos",
      media: "/blockly/",
      grid: {
        spacing: 24,
        length: 2,
        colour: (prefersDark() ? dark : light).gridColour,
        snap: true,
      },
      zoom: {
        controls: false,
        wheel: true,
        startScale: 0.9,
        maxScale: 2,
        minScale: 0.5,
        scaleSpeed: 1.1,
      },
      move: { scrollbars: true, drag: true, wheel: true },
      trashcan: true,
      sounds: false,
    });
    // The document owns history, so Blockly must not replay a second, local undo stack.
    workspace.MAX_UNDO = 0;
    workspaceRef.current = workspace;
    load(sourceRef.current);
    const listener = (event: Blockly.Events.Abstract) => {
      if (event.type === Blockly.Events.SELECTED) {
        const id = (event as Blockly.Events.Selected).newElementId;
        const block = id ? workspace.getBlockById(id) : null;
        setSelectedHelp(block?.getTooltip() || "");
        setSelectedId(block && block.type !== "mai_workflow" ? block.id : null);
        if (block) setInspectorOpen(true);
      }
      if (!event.isUiEvent) setRevision((value) => value + 1);
      if (loadingRef.current || event.isUiEvent || !workspaceRef.current)
        return;
      // Only model changes should serialize. Viewport, selection and toolbox events do not.
      if (!["create", "delete", "change", "move"].includes(event.type)) return;
      try {
        const nextSource = serializeWorkflow(workspace, sourceRef.current);
        setMessage("");
        updatePending(false);
        if (nextSource !== sourceRef.current) {
          sourceRef.current = nextSource;
          emittedRef.current = nextSource;
          callbacksRef.current.onChange(nextSource);
        }
      } catch (error) {
        setMessage(
          error instanceof Error
            ? error.message
            : "Finish connecting and filling in your blocks.",
        );
        updatePending(true);
      }
    };
    workspace.addChangeListener(listener);
    const observer =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => Blockly.svgResize(workspace));
    observer?.observe(hostRef.current);
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    const onScheme = () => workspace.setTheme(theme(prefersDark() ? "dark" : "light"));
    media?.addEventListener?.("change", onScheme);
    Blockly.svgResize(workspace);
    return () => {
      observer?.disconnect();
      media?.removeEventListener?.("change", onScheme);
      workspace.removeChangeListener(listener);
      workspace.dispose();
      workspaceRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (source === emittedRef.current) {
      emittedRef.current = null;
      return;
    }
    if (source !== sourceRef.current) load(source);
  }, [source]);

  return (
    <section className="block-editor" aria-label="Workflow block editor">
      <div className="block-editor-heading">
        <div><h2>Workflow canvas</h2><p>Select a block for guidance · Drag the background to pan</p></div>
        <button aria-expanded={showHelp} onClick={() => setShowHelp(!showHelp)}>Block guide</button>
      </div>
      {showHelp && <div className="block-editor-guide" aria-label="How to build a workflow">
        <span><b>1</b> Choose a category in the left library.</span>
        <span><b>2</b> Drag a block and snap it below a step.</span>
        <span><b>3</b> Fill its fields. Steps run from top to bottom.</span>
        <span>Value blocks fit into sockets. Use Step outline for form-based editing.</span>
      </div>}
      {preservedSteps > 0 && !loadError && (
        <p className="block-editor-note">
          {preservedSteps} {preservedSteps === 1 ? "step is" : "steps are"}{" "}
          preserved in Advanced YAML.
        </p>
      )}
      {pending && (
        <div className="block-editor-draft" role="status">
          <div>
            <strong>Finish your block edits</strong>
            <p>
              {message ||
                "Connect and complete your blocks to apply these edits."}
            </p>
          </div>
          <button
            type="button"
            onClick={() => load(sourceRef.current)}
            disabled={disabled}
          >
            Discard block edits
          </button>
        </div>
      )}
      {selectedHelp && !(inspectorOpen && selectedId) && (
        <div className="block-context" role="status">
          <strong>Selected block</strong>
          <span>{selectedHelp}</span>
        </div>
      )}
      <div className="block-editor-canvas-wrap">
        <div
          className="block-editor-canvas"
          ref={hostRef}
          aria-label="Blocks workspace"
          inert={disabled || !!loadError}
        />
        {!loadError && (
          <div className="block-editor-zoom" aria-label="Workspace zoom">
            <button type="button" aria-label="Zoom out" onClick={() => workspaceRef.current?.zoomCenter(-1)} disabled={disabled}>−</button>
            <button type="button" aria-label="Zoom in" onClick={() => workspaceRef.current?.zoomCenter(1)} disabled={disabled}>+</button>
            <button
              type="button"
              onClick={() => workspaceRef.current?.setScale(1)}
              disabled={disabled}
              aria-label="Reset zoom to 100 percent"
            >
              100%
            </button>
            <button
              type="button"
              onClick={() => workspaceRef.current?.zoomToFit()}
              disabled={disabled}
              aria-label="Fit blocks in workspace"
            >
              Fit
            </button>
          </div>
        )}
        {loadError && (
          <div className="block-editor-recovery" role="alert">
            <strong>We couldn’t open this workflow as blocks.</strong>
            <p>{loadError}</p>
            <button type="button" onClick={onAdvanced} disabled={disabled}>
              Open Advanced YAML
            </button>
          </div>
        )}
        {disabled && (
          <div className="block-editor-disabled" aria-hidden="true" />
        )}
        {inspectorOpen && selectedId && !loadError && (
          <BlockInspector
            block={workspaceRef.current?.getBlockById(selectedId) ?? null}
            revision={revision}
            disabled={disabled}
            onClose={() => setInspectorOpen(false)}
          />
        )}
      </div>
    </section>
  );
}

export default BlockEditor;
