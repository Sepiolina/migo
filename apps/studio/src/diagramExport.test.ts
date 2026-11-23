// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import { saveDiagramSvg, serializeDiagramSvg } from "./diagramExport";
import { buildDiagram, diagramSections } from "./diagramModel";
import { readPipeline } from "./pipeline";

function parseSvg(svg: string) {
  const document = new DOMParser().parseFromString(svg, "image/svg+xml");
  expect(document.querySelector("parsererror")).toBeNull();
  expect(document.documentElement.namespaceURI).toBe("http://www.w3.org/2000/svg");
  return document;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("diagram SVG export", () => {
  it("exports the BOM overview and selected expansion with stable layout", () => {
    const source = readFileSync("../../workflows/bom.yaml", "utf8");
    const pipeline = readPipeline(source);
    const section = diagramSections(pipeline, source).find(({ steps }) => steps.some(({ top }) => top === 14))!;
    const expanded = new Set([section.id]);
    const overview = parseSvg(serializeDiagramSvg(buildDiagram(source, pipeline)));
    const diagram = buildDiagram(source, pipeline, expanded);
    const svg = serializeDiagramSvg(diagram);
    const document = parseSvg(svg);

    expect(svg).toEqual(serializeDiagramSvg(buildDiagram(source, pipeline, expanded)));
    expect(overview.querySelectorAll('[data-node-kind="step"]')).toHaveLength(0);
    expect([...document.querySelectorAll('[data-node-kind="step"] .detail')]
      .some((element) => element.textContent === "Step 15")).toBe(true);
    expect(document.querySelector("#diagram-title")?.textContent).toBe("BOM — workflow diagram");
    expect(document.querySelector("#diagram-legend")?.textContent).toContain("Execution order");
    expect(document.querySelector("style")?.textContent).toContain(".node");
    expect(document.querySelector("script, image, foreignObject, a")).toBeNull();
    const [, , width, height] = document.documentElement.getAttribute("viewBox")!.split(" ").map(Number);
    expect(width).toBeGreaterThanOrEqual(diagram.width);
    expect(height).toBeGreaterThan(diagram.height);
  });

  it("renders a blank diagram and escapes markup in long labels", () => {
    const blank = buildDiagram("steps: []", readPipeline("steps: []"));
    expect(parseSvg(serializeDiagramSvg(blank)).documentElement.textContent).toContain("No workflow steps");

    const name = `A > B & C "test" 'sample' <script> ${"long title ".repeat(10)}`.trim();
    const source = `name: ${JSON.stringify(name)}\ninput: {input: {file: '/private/sample.xlsx'}}\nsteps: [{log: 'secret sample text'}]\n`;
    const pipeline = readPipeline(source);
    const expanded = new Set(diagramSections(pipeline, source).map((section) => section.id));
    const diagram = buildDiagram(source, pipeline, expanded);
    const svg = serializeDiagramSvg(diagram);
    const document = parseSvg(svg);

    expect(svg).toContain("&lt;script&gt;");
    expect(document.querySelector("#diagram-title")?.textContent).toBe(`${name} — workflow diagram`);
    expect(document.querySelector("script")).toBeNull();
    expect(svg).not.toContain("/private/sample.xlsx");
    expect(svg).not.toContain("secret sample text");
  });

  it("delegates desktop saving to the native dialog and reports cancellation", async () => {
    const invoke = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    vi.stubGlobal("__TAURI_INTERNALS__", { invoke });

    await expect(saveDiagramSvg("<svg/>")).resolves.toBe(false);
    await expect(saveDiagramSvg("<svg/>")).resolves.toBe(true);
    expect(invoke).toHaveBeenLastCalledWith("save_diagram_svg", { svg: "<svg/>" }, undefined);
  });
});
