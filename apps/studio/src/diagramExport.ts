import { native } from "./api";
import type { DiagramNode, WorkflowDiagram } from "./diagramModel";

const filename = "workflow-diagram.svg";

function xml(value: string): string {
  return Array.from(value, (character) => {
    const code = character.codePointAt(0)!;
    if (
      (code < 0x20 && code !== 9 && code !== 10 && code !== 13)
      || (code >= 0xd800 && code <= 0xdfff)
      || code === 0xfffe || code === 0xffff
    ) return "\ufffd";
    switch (character) {
      case "&": return "&amp;";
      case "<": return "&lt;";
      case ">": return "&gt;";
      case "\"": return "&quot;";
      case "'": return "&apos;";
      default: return character;
    }
  }).join("");
}

function svgNode(node: DiagramNode): string {
  const textX = node.x + 14;
  let baseline = node.y + 22;
  const labels = node.lines.map((line) => {
    const label = `<text x="${textX}" y="${baseline}">${xml(line)}</text>`;
    baseline += 17;
    return label;
  });
  if (node.detail) {
    labels.push(`<text class="detail" x="${textX}" y="${baseline + 1}">${xml(node.detail)}</text>`);
    baseline += 18;
  }
  for (const warning of node.unresolved ?? []) {
    labels.push(`<text class="warning" x="${textX}" y="${baseline}">${xml(warning)}</text>`);
    baseline += 17;
  }
  return `<g class="node ${node.kind}" data-node-kind="${node.kind}">
  <title>${xml(node.label)}</title>
  <rect x="${node.x}" y="${node.y}" width="${node.width}" height="${node.height}" rx="7"/>
  ${labels.join("\n  ")}
</g>`;
}

const edgeStyles = [
  { kind: "order", color: "#526276", legend: "Execution order" },
  { kind: "read", color: "#12685d", legend: "Reads input or table" },
  { kind: "write", color: "#604396", legend: "Writes named table" },
] as const;

const styles = `
  text { font-family: system-ui, sans-serif; font-size: 13px; fill: #172233; }
  .heading { font-size: 18px; font-weight: 700; }
  .node text { font-weight: 600; }
  .node rect { fill: #fff; stroke: #8795a6; }
  .node.section rect { fill: #eaf1fc; stroke: #567297; }
  .node.input rect, .node.output rect { fill: #e8f3fa; stroke: #417291; }
  .node.table rect { fill: #e6f5ef; stroke: #3e7a69; }
  .node.branch rect, .node.merge rect { fill: #f3edfb; stroke: #7963a0; }
  .node .detail { fill: #4b596c; }
  .warning { fill: #874414; }
  .node .detail, .node .warning, .edge-label { font-size: 12px; font-weight: 400; }
  .edge { fill: none; stroke-width: 1.7; }
  .read.edge { stroke-dasharray: 5 3; }
  .write.edge { stroke-dasharray: 2 2; }
  .edge-label { text-anchor: middle; paint-order: stroke; stroke: #fff; stroke-width: 4px; stroke-linejoin: round; }
  ${edgeStyles.map(({ kind, color }) => `.${kind}.edge { stroke: ${color}; }`).join("\n  ")}
`;

export function serializeDiagramSvg(diagram: WorkflowDiagram): string {
  const width = Math.max(diagram.width, 676);
  const graphX = (width - diagram.width) / 2;
  const graphY = 48;
  const legendY = graphY + diagram.height + 18;
  const height = legendY + 77;
  const title = diagram.title === "Workflow" ? "Workflow diagram" : `${diagram.title} — workflow diagram`;
  const name = Array.from(diagram.title);
  const shortName = name.length > 32 ? `${name.slice(0, 31).join("")}…` : diagram.title;
  const heading = shortName === "Workflow" ? title : `${shortName} — workflow diagram`;
  const edges = diagram.edges.map((edge) =>
    `<path class="edge ${edge.kind}" d="${xml(edge.path)}" marker-end="url(#arrow-${edge.kind})"/>`);
  const labels = diagram.edges.flatMap((edge) => edge.label ? [
    `<text class="edge-label" x="${edge.labelX}" y="${edge.labelY}">${xml(edge.label)}</text>`,
  ] : []);
  const markers = edgeStyles.map(({ kind, color }) => `
    <marker id="arrow-${kind}" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
      <path d="M0 0 L8 4 L0 8 Z" fill="${color}"/>
    </marker>`);
  const legend = edgeStyles.map(({ kind, legend }, index) => {
    const x = [16, 149, 310][index];
    return `
    <path class="edge ${kind}" d="M${x} 47 h16" marker-end="url(#arrow-${kind})"/>
    <text x="${x + 25}" y="51">${legend}</text>`;
  });
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"
  role="img" aria-labelledby="diagram-title diagram-description">
  <title id="diagram-title">${xml(title)}</title>
  <desc id="diagram-description">Workflow structure, execution order, and named table references. ${diagram.nodes.length} visible nodes.</desc>
  <style>${styles}</style>
  <defs>${markers.join("")}
  </defs>
  <rect width="${width}" height="${height}" fill="#fff"/>
  <text class="heading" x="24" y="32">${xml(heading)}</text>
  <g transform="translate(${graphX} ${graphY})">
    ${edges.join("\n    ")}
    ${diagram.nodes.map(svgNode).join("\n    ")}
    ${labels.join("\n    ")}
    ${diagram.nodes.length ? "" : '<text x="24" y="40">No workflow steps, inputs, or outputs.</text>'}
  </g>
  <g id="diagram-legend" transform="translate(24 ${legendY})">
    <rect width="628" height="58" rx="7" fill="#f5f7fa" stroke="#b3becb"/>
    <text x="16" y="22" font-weight="700">Legend</text>${legend.join("")}
    <text class="warning" x="470" y="51">Unresolved reference</text>
  </g>
</svg>
`;
}

/** Returns false when the desktop save picker is cancelled. */
export async function saveDiagramSvg(svg: string): Promise<boolean> {
  if ("__TAURI_INTERNALS__" in window) {
    return native<boolean>("save_diagram_svg", { svg });
  }
  const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.append(link);
  try {
    link.click();
  } finally {
    link.remove();
    // Give the browser time to start consuming the URL before releasing it.
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  return true;
}
