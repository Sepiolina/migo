// Drive the built Studio in a system Chrome against the real Go runner:
// open the BOM, preview it step by step, add a step from a column, use the
// command menu and the endpoint page, run a template and save it.
import assert from "node:assert/strict";
import { existsSync, readFileSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { preview } from "vite";
import { chromium } from "playwright-core";
import { parse } from "yaml";
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
]
  .filter(Boolean)
  .find(existsSync);
if (!executablePath) throw new Error("No system Chrome/Chromium found. Set MAI_CHROME to its executable.");

const env = prepare();
let server, browser;
const failures = [];
try {
  const csp = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8")).app.security.csp;
  server = await preview({ root, preview: { host: "127.0.0.1", port: 1423, strictPort: true, headers: { "Content-Security-Policy": csp } } });
  browser = await chromium.launch({ executablePath, headless: true, args: ["--no-sandbox", "--no-proxy-server"] });
  const page = await browser.newPage({ viewport: { width: 1360, height: 860 } });
  page.on("pageerror", (e) => failures.push(e.message));
  page.on("console", (m) => m.type() === "error" && failures.push(m.text()));
  await installBridge(page, env);
  await page.goto("http://127.0.0.1:1423/");

  // Home: open the folder and the BOM.
  await page.getByRole("button", { name: "Open folder…" }).click();
  await page.locator(".wf", { hasText: "workflows/bom.yaml" }).click();
  const rows = page.locator(".nav .row");
  assert.ok((await rows.count()) > 40, "every BOM step is listed");

  // Bind the sample files, then the table shows the data after a step.
  for (const [id, file] of [["bom", "testdata/bom_basic.xlsx"], ["priority", "testdata/prio_basic.xlsx"]]) {
    await page.locator(".nav .row", { hasText: new RegExp(`^\\w+${id}`) }).click();
    await page.locator("#in-file").fill(file);
    await page.locator("#in-file").press("Enter");
  }
  await page.locator(".nav .row", { hasText: "Fill blanks down" }).click();
  await page.locator(".grid tbody tr").first().waitFor();
  assert.match(await page.locator(".sbar").innerText(), /6 rows/);
  assert.match(await page.locator(".pill").innerText(), /Valid/);

  // A column action adds a step after the selected one, and Undo removes it.
  await page.locator(".colh", { hasText: "EffPartNumber" }).click();
  await page.getByRole("menuitem", { name: "Remove duplicate values" }).click();
  await page.locator(".nav .row.sel", { hasText: "Remove duplicate EffPartNumber" }).waitFor();
  await page.getByRole("button", { name: "Undo", exact: true }).first().click();
  await page.locator(".nav .row.sel", { hasText: "Fill blanks down" }).waitFor();

  // Inspector edits are one-line YAML diffs.
  await page.locator(".nav .row", { hasText: "Remove duplicate Key, keep last" }).first().click();
  await page.locator(".insp").getByLabel("Keep").selectOption("first");
  await page.getByRole("tab", { name: "YAML" }).click();
  const yaml = await page.getByLabel("Workflow YAML").inputValue();
  // A text box reports LF; Git on Windows checks the file out with CRLF.
  const original = readFileSync(`${env.workspace}/workflows/bom.yaml`, "utf8").replace(/\r\n/g, "\n");
  const changed = yaml.split("\n").filter((l, i) => l !== original.split("\n")[i]);
  assert.deepEqual(changed, ["  - dedupe: {by: [Key], keep: first}"]);

  // The endpoint page documents the API.
  await page.locator(".nav .row", { hasText: "/api/bom" }).click();
  await page.locator(".endpoint", { hasText: "/api/bom/run" }).waitFor();
  assert.match(await page.locator(".curl pre").innerText(), /-F "bam=1234"/);

  // A template: run it, see the output, save it as an endpoint.
  await page.keyboard.press("Control+k");
  await page.keyboard.type("look up and enrich");
  await page.keyboard.press("Enter");
  await page.getByRole("button", { name: "Discard changes" }).click();
  await page.locator(".nav .row", { hasText: "Group by Region" }).click();
  await page.locator(".grid tbody tr").first().waitFor();
  await page.keyboard.press("Control+Enter");
  await page.locator(".sbar", { hasText: "Output By region" }).waitFor();
  const output = await page.locator(".grid tbody").innerText();
  assert.match(output, /North\s+2\s+50/);
  assert.match(output, /South\s+1\s+5/);
  await page.keyboard.press("Control+s");
  await page.getByRole("dialog", { name: "Save workflow" }).getByRole("button", { name: "Save" }).click();
  await page.locator(".toast", { hasText: "Saved workflows/sales-by-region.yaml" }).waitFor();
  const saved = parse(readFileSync(`${env.workspace}/workflows/sales-by-region.yaml`, "utf8"));
  assert.equal(saved.name, "Sales by region");

  // Phone width: one panel at a time.
  await page.setViewportSize({ width: 390, height: 800 });
  await page.getByRole("button", { name: "Steps", exact: true }).click();
  assert.ok(await page.locator(".nav").isVisible());
  assert.ok(!(await page.locator(".stage").isVisible()));

  assert.deepEqual(failures, [], "no page errors");
  console.log("Studio smoke passed: BOM preview, column action, undo, one-line edit, endpoint page, template run and save, phone layout.");
} finally {
  await browser?.close();
  await new Promise((resolve) => (server ? server.httpServer.close(resolve) : resolve()));
  rmSync(env.dir, { recursive: true, force: true });
}
