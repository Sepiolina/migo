// Check the built Studio diagram against the Go runner. Set MAI_DIAGRAM_ARTIFACTS
// to keep the BOM screenshots and downloaded SVGs after the smoke run.
import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";
import { preview } from "vite";
import { installBridge, prepare } from "./studio-bridge.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const executablePath = [
  process.env.MAI_CHROME,
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
  "/usr/bin/google-chrome",
  "/opt/google/chrome/chrome",
  process.env.PROGRAMFILES && `${process.env.PROGRAMFILES}\\Google\\Chrome\\Application\\chrome.exe`,
  process.env["PROGRAMFILES(X86)"] && `${process.env["PROGRAMFILES(X86)"]}\\Google\\Chrome\\Application\\chrome.exe`,
].filter(Boolean).find(existsSync);
if (!executablePath) throw new Error("No system Chrome/Chromium found. Set MAI_CHROME to its executable.");

const env = prepare();
const artifacts = process.env.MAI_DIAGRAM_ARTIFACTS;
const escapes = `Parts & <supplier> "quoted" 'label'`;
const longLabel = `${escapes} ${"wide label ".repeat(60)}end`;
const privatePath = "/home/operator/private-diagram-fixture.xlsx";
const privateValue = "private-diagram-cell-value";
const port = 1427;
let server, browser;
try {
  if (artifacts) mkdirSync(artifacts, { recursive: true });
  writeFileSync(join(env.workspace, "workflows", "diagram-blank.yaml"), "name: Blank diagram\nsteps: []\n");
  writeFileSync(join(env.workspace, "workflows", "diagram-branches.yaml"), `name: ${JSON.stringify(escapes)}
input:
  parts: {}
steps:
  - from: parts
  - if: "true"
    then:
      - if: "false"
        then:
          - log: "${privateValue}"
        else:
          - save: nested
    else:
      - from: unknown
output:
  file: ${privatePath}
  sheets: {${JSON.stringify(longLabel)}: nested}
`);

  const csp = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8")).app.security.csp;
  server = await preview({ root, preview: { host: "127.0.0.1", port, strictPort: true, headers: { "Content-Security-Policy": csp } } });
  browser = await chromium.launch({ executablePath, headless: true, args: ["--no-sandbox", "--no-proxy-server"] });
  const page = await browser.newPage({ viewport: { width: 1600, height: 960 } });
  const failures = [];
  page.on("pageerror", (error) => failures.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") failures.push(message.text());
  });
  await installBridge(page, env, () => {
    throw new Error("The diagram must work without choosing sample files.");
  });
  await page.goto(`http://127.0.0.1:${port}/`);
  await page.getByRole("button", { name: "Open folder…" }).click();

  const offline = await browser.newContext({ offline: true });
  const svgPage = await offline.newPage();
  const remoteRequests = [];
  const svgOrigin = `http://127.0.0.1:${port}`;
  let svgBytes;
  let svgUrl;
  // Serve the downloaded bytes in the offline browser without using the network.
  await offline.route(`${svgOrigin}/offline-diagram/*.svg`, (route) =>
    route.fulfill({ contentType: "image/svg+xml", body: svgBytes }));
  offline.on("request", (request) => {
    if (/^https?:/.test(request.url()) && request.url() !== svgUrl) remoteRequests.push(request.url());
  });
  const button = (name) => page.getByRole("button", { name, exact: true });
  const diagram = button("Diagram");
  const table = button("Table");
  const fit = button("Fit to view");
  const yaml = page.getByLabel("Workflow YAML", { exact: true });
  const workflow = (filename) => page.locator(".wf", { hasText: `workflows/${filename}.yaml` });
  const graph = page.getByRole("region", { name: "Workflow diagram" });
  const groups = graph.getByRole("button", { name: /\bgroups\b/i }).and(graph.locator("[aria-expanded]"));
  const fill = graph.getByRole("button", { name: /Fill blanks down.*\bStep 15\b/i });
  const selectedStep = page.locator(".nav .row.sel");

  async function downloadDiagram(name, title) {
    const pending = page.waitForEvent("download");
    await button("Export SVG").click();
    const download = await pending;
    assert.match(download.suggestedFilename(), /\.svg$/);
    const path = join(env.dir, `${name}.svg`);
    await download.saveAs(path);
    const bytes = readFileSync(path);
    const source = bytes.toString("utf8");
    svgBytes = bytes;
    svgUrl = `${svgOrigin}/offline-diagram/${name}.svg`;
    await svgPage.goto(svgUrl);
    const svg = svgPage.locator("svg");
    assert.equal(await svg.count(), 1, `${name} opens as standalone XML`);
    assert.equal(await svg.locator("parsererror, script, foreignObject, image, use, link").count(), 0);
    assert.ok((await svg.locator("title").first().textContent()).includes(title));
    assert.match(await svg.locator("#diagram-legend").textContent(), /Execution order/);
    assert.equal(await svg.locator("style").count(), 1);
    assert.ok(await svg.evaluate((element) => {
      const box = element.viewBox.baseVal;
      const drawn = element.getBBox();
      const tolerance = 1;
      return box.width > 0 && box.height > 0
        && drawn.width > 0 && drawn.height > 0
        && drawn.x >= box.x - tolerance
        && drawn.y >= box.y - tolerance
        && drawn.x + drawn.width <= box.x + box.width + tolerance
        && drawn.y + drawn.height <= box.y + box.height + tolerance;
    }), `${name} viewBox covers the exported drawing`);
    assert.ok(!source.includes(env.workspace), "export excludes the workspace path");
    assert.ok(!source.includes("testdata/bom_basic.xlsx"), "export excludes sample paths");
    if (artifacts && (name === "overview" || name === "expanded")) {
      writeFileSync(join(artifacts, `migo-diagram-${name}.svg`), source);
    }
    return source;
  }

  async function screenshot(name) {
    if (artifacts) await page.screenshot({ path: join(artifacts, `migo-diagram-${name}.png`) });
  }

  await workflow("bom").click();
  await page.getByText("Add sample files", { exact: true }).waitFor();
  const sourceBefore = await yaml.inputValue();
  assert.equal(sourceBefore, readFileSync(join(env.workspace, "workflows", "bom.yaml"), "utf8").replace(/\r\n/g, "\n"));
  assert.ok(await button("Undo").isDisabled());
  assert.ok(await button("Redo").isDisabled());
  await diagram.click();
  await groups.waitFor();
  assert.equal(await groups.getAttribute("aria-expanded"), "false");
  assert.equal(await fill.count(), 0);
  await fit.click();
  const zoom = graph.getByLabel("Current zoom");
  const fittedZoom = Number.parseInt(await zoom.innerText(), 10);
  await button("Zoom in").click();
  assert.ok(Number.parseInt(await zoom.innerText(), 10) > fittedZoom);
  await fit.click();
  assert.equal(Number.parseInt(await zoom.innerText(), 10), fittedZoom);
  const overview = await downloadDiagram("overview", "BOM");
  assert.equal(await downloadDiagram("overview-repeat", "BOM"), overview);
  await screenshot("overview");

  await groups.click();
  assert.equal(await groups.getAttribute("aria-expanded"), "true");
  await groups.press("ArrowDown");
  assert.ok(!(await groups.evaluate((element) => element === document.activeElement)), "arrow moves diagram focus");
  await fill.click();
  assert.match(await selectedStep.innerText(), /Fill blanks down/);
  assert.match(await page.locator(".insp .ihead").innerText(), /Fill blanks down/);
  await fit.click();
  const expanded = await downloadDiagram("expanded", "BOM");
  assert.notEqual(expanded, overview);
  assert.match(expanded, /Fill blanks down/);
  assert.doesNotMatch(overview, /Fill blanks down/);
  await screenshot("expanded");
  if (artifacts) {
    while (Number.parseInt(await zoom.innerText(), 10) < 95) await button("Zoom in").click();
    await fill.evaluate((element) => element.scrollIntoView({ block: "center", inline: "nearest" }));
    await screenshot("detail");
    await fit.click();
  }

  await table.click();
  await page.getByText("Add sample files", { exact: true }).waitFor();
  assert.match(await selectedStep.innerText(), /Fill blanks down/);
  assert.equal(await yaml.inputValue(), sourceBefore);
  assert.ok(await button("Undo").isDisabled());
  assert.ok(await button("Redo").isDisabled());
  assert.equal(await page.locator(".edited").innerText(), "");
  await diagram.click();
  assert.equal(await groups.getAttribute("aria-expanded"), "true");
  assert.equal(await downloadDiagram("expanded-repeat", "BOM"), expanded);

  await page.setViewportSize({ width: 390, height: 800 });
  await fit.click();
  await groups.press("Enter");
  assert.equal(await groups.getAttribute("aria-expanded"), "false");
  await groups.press("Space");
  assert.equal(await groups.getAttribute("aria-expanded"), "true");
  await fill.click();
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  await screenshot("compact");
  await table.click();
  await page.getByText("Add sample files", { exact: true }).waitFor();
  await button("Steps").click();
  assert.match(await selectedStep.innerText(), /Fill blanks down/);
  await page.setViewportSize({ width: 1600, height: 960 });
  await diagram.click();
  const readFromParts = page.locator(".nav .row").filter({ has: page.locator(".num", { hasText: /^13$/ }) });
  await readFromParts.click();
  assert.equal(await graph.getByRole("button", { name: /\bStep 13\b/ }).getAttribute("aria-pressed"), "true");
  assert.equal(await fill.getAttribute("aria-pressed"), "false");

  await button("Workflows").click();
  await workflow("diagram-blank").click();
  await diagram.click();
  await graph.getByText("Add inputs, steps, or outputs to see the workflow here.").waitFor();
  await downloadDiagram("blank", "Blank diagram");

  await button("Workflows").click();
  await workflow("diagram-branches").click();
  await diagram.click();
  await button("Expand all sections").click();
  const branches = await downloadDiagram("branches", escapes);
  assert.match(branches, /&amp;/);
  assert.match(branches, /&lt;supplier&gt;/);
  assert.doesNotMatch(branches, /<supplier>/);
  assert.ok(!branches.includes(privatePath), "the export omits output file paths");
  assert.ok(!branches.includes(privateValue), "the export omits free-text step values");
  const legend = svgPage.locator("#diagram-legend");
  assert.match(await legend.textContent(), /Unresolved reference/);
  const branchLabels = await svgPage.locator("[data-node-kind=branch] > title").allTextContents();
  assert.deepEqual(branchLabels, ["Then", "Then", "Else", "Else"]);
  assert.equal(await svgPage.locator("[data-node-kind=output] > title").textContent(), longLabel);
  assert.match(await svgPage.locator("[data-node-kind=output] > text").allTextContents().then((lines) => lines.join(" ")), /…/);
  assert.deepEqual(remoteRequests, [], "opening the exported diagrams does not contact the network");
  assert.deepEqual(failures, [], "no page errors");
  console.log("Diagram smoke passed: BOM without samples, keyboard traversal, shared selection, compact layout, unchanged source and undo, deterministic offline SVGs, blank and nested workflows, escaped labels and private values.");
} finally {
  await browser?.close();
  await new Promise((resolve) => (server ? server.httpServer.close(resolve) : resolve()));
  rmSync(env.dir, { recursive: true, force: true });
}
