import { useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import type { Trace } from "./api";
import { saveDiagramSvg, serializeDiagramSvg } from "./diagramExport";
import { buildDiagram, diagramSections, type DiagramNode } from "./diagramModel";
import type { Selection } from "./Navigator";
import type { Pipeline } from "./pipeline";
import "./diagram.css";

interface Props {
  source: string;
  pipeline: Pipeline;
  selection: Selection;
  onSelect: (selection: Selection) => void;
  trace?: Trace;
  active?: boolean;
}

function selectionKey(selection: Selection | undefined) {
  if (selection?.kind === "step" || selection?.kind === "source") return `${selection.kind}:${selection.id}`;
  if (selection?.kind === "output") return `${selection.kind}:${selection.index}`;
  return selection?.kind;
}

function nodeName(node: DiagramNode) {
  return [node.label, node.detail, node.rowCount === undefined ? undefined : `${node.rowCount.toLocaleString()} rows`, ...(node.unresolved ?? [])].filter(Boolean).join(". ");
}

function panToNode(view: HTMLElement, node: HTMLElement) {
  const inset = 8;
  const viewport = view.getBoundingClientRect();
  const target = node.getBoundingClientRect();
  if (target.left < viewport.left + inset) view.scrollLeft -= viewport.left + inset - target.left;
  else if (target.right > viewport.left + view.clientWidth - inset) view.scrollLeft += target.right - viewport.left - view.clientWidth + inset;
  if (target.top < viewport.top + inset) view.scrollTop -= viewport.top + inset - target.top;
  else if (target.bottom > viewport.top + view.clientHeight - inset) view.scrollTop += target.bottom - viewport.top - view.clientHeight + inset;
}

export function WorkflowDiagram({ source, pipeline, selection, onSelect, trace, active = true }: Props) {
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const [zoom, setZoom] = useState<number | "width" | "fit">("width");
  const [viewport, setViewport] = useState({ width: 0, height: 0 });
  const [focusedId, setFocusedId] = useState<string>();
  const [exporting, setExporting] = useState(false);
  const [exportStatus, setExportStatus] = useState("");
  const scrollport = useRef<HTMLDivElement>(null);
  const focusable = useRef(new Map<string, HTMLElement>());
  const graphId = useId();
  const selectedKey = selectionKey(selection);
  const previousSelection = useRef(selectedKey);
  const revealSelection = useRef(false);
  const sections = useMemo(() => diagramSections(pipeline, source), [pipeline, source]);
  const selectedSection = selection.kind === "step" ? sections.find((section) => section.steps.some((step) => step.id === selection.id)) : undefined;
  const layout = useMemo(() => buildDiagram(source, pipeline, expanded, trace?.steps), [source, pipeline, expanded, trace]);
  const selectedId = layout.nodes.find((node) => selectionKey(node.selection) === selectedKey)?.id
    ?? layout.nodes.find((node) => node.kind === "section" && node.sectionId === selectedSection?.id)?.id;
  const tabId = layout.nodes.some((node) => node.id === focusedId) ? focusedId : selectedId ?? layout.nodes[0]?.id;
  const scale = typeof zoom === "number" ? zoom : Math.min(1,
    viewport.width ? Math.max(zoom === "width" ? 0.85 : 0, viewport.width / layout.width) : 1,
    zoom === "fit" && viewport.height ? viewport.height / layout.height : 1,
  );

  useLayoutEffect(() => {
    const element = scrollport.current;
    if (!active || !element) return;
    const measure = () => {
      const width = element.clientWidth;
      const height = element.clientHeight;
      if (width && height) setViewport((previous) => previous.width === width && previous.height === height ? previous : { width, height });
    };
    measure();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", measure);
      return () => window.removeEventListener("resize", measure);
    }
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [active]);

  useLayoutEffect(() => {
    const element = scrollport.current;
    if (!active || !element) return;
    if (previousSelection.current !== selectedKey) {
      previousSelection.current = selectedKey;
      revealSelection.current = true;
      if (!element.contains(document.activeElement)) setFocusedId(undefined);
      if (selectedSection && !expanded.has(selectedSection.id)) {
        setExpanded(new Set([...expanded, selectedSection.id]));
        return;
      }
    }
    if (!revealSelection.current) return;
    revealSelection.current = false;
    const selected = selectedId && focusable.current.get(selectedId);
    if (selected) panToNode(element, selected);
  }, [active, expanded, layout, selectedId, selectedKey, selectedSection]);

  function bindNode(id: string, element: HTMLElement | null) {
    if (element) focusable.current.set(id, element);
    else focusable.current.delete(id);
  }

  function traverse(event: KeyboardEvent<HTMLElement>, index: number) {
    let next: number;
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown": next = Math.min(index + 1, layout.nodes.length - 1); break;
      case "ArrowLeft":
      case "ArrowUp": next = Math.max(index - 1, 0); break;
      case "Home": next = 0; break;
      case "End": next = layout.nodes.length - 1; break;
      default: return;
    }
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    event.preventDefault();
    event.stopPropagation();
    const element = focusable.current.get(layout.nodes[next].id);
    element?.focus({ preventScroll: true });
    if (element && scrollport.current) panToNode(scrollport.current, element);
  }

  function activate(node: DiagramNode) {
    if (node.selection) {
      onSelect(node.selection);
    } else if (node.sectionId) {
      const id = node.sectionId;
      setExpanded((previous) => {
        const next = new Set(previous);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      });
    }
  }

  async function exportSvg() {
    setExporting(true);
    setExportStatus("");
    try {
      const svg = serializeDiagramSvg(buildDiagram(source, pipeline, expanded));
      if (await saveDiagramSvg(svg)) setExportStatus("SVG exported.");
    } catch {
      setExportStatus("Unable to export SVG. Try again.");
    } finally {
      setExporting(false);
    }
  }

  return <section className="workflow-diagram" aria-label="Workflow diagram">
    <div className="diagram-toolbar">
      <div className="diagram-controls" role="group" aria-label="Sections">
        <button type="button" className="tb" aria-label="Expand all sections" disabled={!sections.some((section) => !expanded.has(section.id))} onClick={() => setExpanded(new Set(sections.map((section) => section.id)))}>Expand all</button>
        <button type="button" className="tb" aria-label="Collapse all sections" disabled={!sections.some((section) => expanded.has(section.id))} onClick={() => setExpanded(new Set())}>Collapse all</button>
      </div>
      <div className="diagram-controls" role="group" aria-label="Zoom">
        <button type="button" className="tb" title="Zoom out" aria-label="Zoom out" disabled={scale <= 0.1} onClick={() => setZoom(Math.max(0.1, scale / 1.25))}>−</button>
        <span className="diagram-zoom" aria-label="Current zoom">{Math.round(scale * 100)}%</span>
        <button type="button" className="tb" title="Zoom in" aria-label="Zoom in" disabled={scale >= 2} onClick={() => setZoom(Math.min(2, Math.max(0.1, scale * 1.25)))}>+</button>
        <button type="button" className="tb" onClick={() => {
          setZoom("fit");
          if (scrollport.current) scrollport.current.scrollTop = scrollport.current.scrollLeft = 0;
        }}>Fit to view</button>
      </div>
      <button type="button" className="btn diagram-export" disabled={exporting} onClick={() => void exportSvg()}>Export SVG</button>
    </div>
    <div className="diagram-scroll" ref={scrollport} role="group" aria-label="Workflow nodes" aria-describedby={`${graphId}-keyboard`}>
      <span className="sr-only" id={`${graphId}-keyboard`}>Use arrow keys, Home or End to move through the diagram. Press Enter or Space to select a node or toggle a section.</span>
      {layout.nodes.length === 0 ? <p className="diagram-empty">Add inputs, steps, or outputs to see the workflow here.</p> :
        <div className="diagram-scaled" style={{ width: layout.width * scale, height: layout.height * scale }}>
          <div className="diagram-canvas" style={{ width: layout.width, height: layout.height, transform: `scale(${scale})` }}>
            <svg className="diagram-edges" width={layout.width} height={layout.height} aria-hidden="true">
              <defs>{(["order", "read", "write"] as const).map((kind) =>
                <marker key={kind} id={`${graphId}-${kind}`} className={`diagram-edge-${kind}`} viewBox="0 0 8 8" markerWidth="7" markerHeight="7" refX="7" refY="4" orient="auto">
                  <path d="M0 0 L8 4 L0 8 L2 4 Z" />
                </marker>)}</defs>
              {layout.edges.map((edge) => <path key={edge.id} className={`diagram-edge diagram-edge-${edge.kind}`} d={edge.path} markerEnd={`url(#${graphId}-${edge.kind})`} />)}
              {layout.edges.map((edge) => edge.label && <text key={edge.id} className={`diagram-edge-label diagram-edge-${edge.kind}`} x={edge.labelX} y={edge.labelY}>{edge.label}</text>)}
            </svg>
            {layout.nodes.map((node, index) => {
              const interactive = node.selection !== undefined || node.kind === "section";
              const attributes = {
                className: `diagram-node diagram-node-${node.kind}`,
                style: { left: node.x, top: node.y, width: node.width, height: node.height },
                tabIndex: node.id === tabId ? 0 : -1,
                "aria-label": nodeName(node),
                title: nodeName(node),
                "data-selected": node.id === selectedId || undefined,
                onKeyDown: (event: KeyboardEvent<HTMLElement>) => traverse(event, index),
                onFocus: () => setFocusedId(node.id),
              };
              const content = <span className="diagram-node-content" aria-hidden="true">
                {node.kind === "section" && <span className="diagram-chevron">{node.expanded ? "▾" : "▸"}</span>}
                {node.lines.map((line, i) => <span className="diagram-node-title" key={i}>{line}</span>)}
                {(node.detail || node.rowCount !== undefined) && <span className="diagram-node-detail">{node.detail}{node.detail && node.rowCount !== undefined ? " · " : ""}{node.rowCount === undefined ? "" : `${node.rowCount.toLocaleString()} rows`}</span>}
                {node.unresolved?.map((line, i) => <span className="diagram-node-warning" key={i}>{line}</span>)}
              </span>;
              return interactive ? <button {...attributes} key={node.id} type="button" ref={(element) => bindNode(node.id, element)}
                aria-expanded={node.kind === "section" ? Boolean(node.expanded) : undefined}
                aria-pressed={node.selection ? node.id === selectedId : undefined}
                onClick={() => activate(node)}>{content}</button> :
                <div {...attributes} key={node.id} ref={(element) => bindNode(node.id, element)} role="img">{content}</div>;
            })}
          </div>
        </div>}
    </div>
    <footer className="diagram-footer">
      <ul className="diagram-legend" aria-label="Diagram legend">
        <li><i className="diagram-order-key" />Execution order</li>
        <li><i className="diagram-read-key" />Named table read</li>
        <li><i className="diagram-write-key" />Named table write</li>
        {layout.nodes.some((node) => node.unresolved?.length) && <li className="diagram-warning-key">Unresolved reference</li>}
      </ul>
      {exportStatus && <span role="status">{exportStatus}</span>}
    </footer>
  </section>;
}
