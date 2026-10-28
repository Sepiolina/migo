import { useEffect, useRef, useState } from "react";
import * as Blockly from "blockly/core";
import * as English from "blockly/msg/en";
import {
  loadWorkflow,
  registerWorkflowBlocks,
  serializeWorkflow,
  workflowToolbox,
} from "./blocklyWorkflow";
import "./block-editor.css";

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
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [loadError, setLoadError] = useState("");
  const [preservedSteps, setPreservedSteps] = useState(0);
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
    const theme = Blockly.Theme.defineTheme("migo-workflow", {
      name: "migo-workflow",
      base: Blockly.Themes.Classic,
      componentStyles: {
        workspaceBackgroundColour: "#f7f9fd",
        toolboxBackgroundColour: "#ffffff",
        toolboxForegroundColour: "#243854",
        flyoutBackgroundColour: "#f0f4fa",
        flyoutForegroundColour: "#243854",
        flyoutOpacity: 1,
        scrollbarColour: "#aebed3",
        scrollbarOpacity: 0.6,
        insertionMarkerColour: "#315fba",
        insertionMarkerOpacity: 0.35,
        cursorColour: "#275ad5",
      },
      fontStyle: {
        family: "Segoe UI, Inter, system-ui, sans-serif",
        size: 17,
        weight: "500",
      },
    });
    const workspace = Blockly.inject(hostRef.current, {
      toolbox: workflowToolbox,
      theme,
      renderer: "geras",
      media: "/blockly/",
      grid: { spacing: 24, length: 2, colour: "#d5dfed", snap: true },
      zoom: {
        controls: false,
        wheel: true,
        startScale: 1,
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
    Blockly.svgResize(workspace);
    return () => {
      observer?.disconnect();
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
        <div>
          <h2>Build with blocks</h2>
          <p>
            Drag a block from the library. Snap it into the workflow. Fill in
            the blanks.
          </p>
        </div>
        {preservedSteps > 0 && (
          <button
            type="button"
            onClick={onAdvanced}
            disabled={disabled || pending}
          >
            Advanced YAML
          </button>
        )}
      </div>
      <div className="block-editor-guide" aria-label="How to build a workflow">
        <span>
          <b>1</b> Choose a block
        </span>
        <span>
          <b>2</b> Connect it
        </span>
        <span>
          <b>3</b> Fill in the details
        </span>
      </div>
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
      <div className="block-editor-canvas-wrap">
        <div
          className="block-editor-canvas"
          ref={hostRef}
          aria-label="Blocks workspace"
          inert={disabled || !!loadError}
        />
        {!loadError && (
          <div className="block-editor-zoom" aria-label="Workspace zoom">
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
      </div>
    </section>
  );
}

export default BlockEditor;
