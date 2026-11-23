// Drive the built Studio through the desktop bridge and the real Go runner.
import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, utimesSync, writeFileSync } from "node:fs";
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

const name = "Customer order totals";
const path = "workflows/history-smoke.yaml";
const source = `name: ${name}
input:
  orders: {label: Orders}
steps:
  - from: orders
  - group: {by: [Customer], sum: [Amount]}
  - save: totals
output:
  sheets:
    - name: Totals
      from: totals
      header: {fill: "#1F4E78", font: {bold: true, color: "#FFFFFF"}}
      columns:
        - {header: Customer, width: 22}
        - {header: Amount, width: 14, number_format: "0.00"}
`;
const firstSample = "examples/orders-january.csv";
const secondSample = "examples/orders-february.csv";
const origin = "http://127.0.0.1:1427";
const env = prepare({ runner: process.env.MAI_HISTORY_RUNNER });
let server, browser;
try {
  writeFileSync(join(env.workspace, path), source);
  writeFileSync(join(env.workspace, firstSample), "Customer,Amount\nNorthwind,25\nNorthwind,15\nContoso,20\n");
  writeFileSync(join(env.workspace, secondSample), "Customer,Amount\nFabrikam,30\nFabrikam,35\nContoso,45\n");
  const runsPath = join(env.workspace, ".studio", "runs");
  const csp = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8")).app.security.csp;
  server = await preview({ root, preview: { host: "127.0.0.1", port: 1427, strictPort: true, headers: { "Content-Security-Policy": csp } } });
  browser = await chromium.launch({ executablePath, headless: true, args: ["--no-sandbox", "--no-proxy-server"] });
  const page = await browser.newPage({ viewport: { width: 1360, height: 860 }, colorScheme: "dark" });
  page.setDefaultTimeout(10000);
  page.setDefaultNavigationTimeout(10000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => message.type() === "error" && errors.push(message.text()));
  await installBridge(page, env);
  await page.goto(origin);

  const button = (name) => page.getByRole("button", { name, exact: true });
  const openWorkspace = () => button("Open folder…").click();
  const openWorkflow = () => page.getByRole("button", { name: new RegExp(`^${name} workflows/history-smoke`) }).click();
  const selectSample = async (path) => {
    const setup = page.getByRole("dialog", { name: "Set up samples" });
    await setup.getByRole("textbox", { name: "Sample path for Orders" }).fill(path);
    await setup.getByText("All samples are ready.").waitFor();
    await setup.getByRole("button", { name: "Finish" }).click();
    await page.locator(".nav").getByText(path, { exact: true }).waitFor();
    await page.locator(".pill", { hasText: /^Valid ·/ }).waitFor();
  };
  const workbook = page.getByRole("grid", { name: "Worksheet cells" });
  const value = (text) => workbook.getByRole("gridcell", { name: new RegExp(`(?:^|, )${text}$`) });
  const run = async (customer, amount) => {
    await page.keyboard.press("Control+Enter");
    await value(customer).waitFor();
    await value(amount).waitFor();
  };
  await openWorkspace();
  await button(`Set up samples for ${path}`).click();
  await selectSample(firstSample);
  await run("Northwind", "40\\.00");
  const [earlierRunId] = readdirSync(runsPath);
  assert.ok(earlierRunId);

  await page.locator(".nav").getByRole("button", { name: "Set up samples" }).click();
  await selectSample(secondSample);
  await run("Fabrikam", "65\\.00");
  const runIds = readdirSync(runsPath);
  assert.equal(runIds.length, 2);
  const laterRunId = runIds.find((id) => id !== earlierRunId);
  const artifact = (id) => join(runsPath, id, "output.xlsx");
  const legacyDir = join(runsPath, "run-legacy");
  mkdirSync(legacyDir);
  copyFileSync(artifact(laterRunId), artifact("run-legacy"));
  utimesSync(legacyDir, 1_700_000_000, 1_700_000_000);

  // Reload to discard the editor's in-memory result and open history from Home.
  await page.reload();
  await openWorkspace();
  await button("Runs").click();
  await page.getByRole("heading", { name: "Runs", exact: true }).waitFor();
  const list = page.getByRole("list", { name: "Recent runs" });
  const records = list.getByRole("button", { name: new RegExp(name) });
  await records.nth(1).waitFor();
  assert.equal(await records.count(), 2);
  const openOutput = () => button("Open output").click();
  await records.first().click();
  await page.getByText(secondSample, { exact: true }).waitFor();
  await openOutput();
  await value("Fabrikam").waitFor();
  await records.nth(1).click();
  await page.getByText(firstSample, { exact: true }).waitFor();
  await openOutput();
  await value("Northwind").waitFor();
  await value("40\\.00").waitFor();
  assert.equal(await value("Fabrikam").count(), 0);
  await page.getByRole("button", { name: /Back to workspace$/ }).click();
  await openWorkflow();
  const details = page.getByRole("region", { name: "Run details" });
  const field = (label) => details.getByText(label, { exact: true }).locator("xpath=following-sibling::dd[1]");
  const waitForField = (label, text) => field(label).and(details.getByText(text, { exact: true })).waitFor();
  await button("Runs").click();
  await records.first().click();
  await waitForField("Current YAML", "Matches");
  await waitForField("Current binding", "Matches");
  await waitForField("Current bytes", "Matches");
  const backToWorkflow = () => page.getByRole("button", { name: /Back to workflow$/ }).click();
  await backToWorkflow();

  // Keep a selected transform and an unsaved draft while inspecting old output.
  const step = page.locator(".nav .row", { hasText: /Group by Customer/ });
  await step.click();
  await page.getByRole("tab", { name: "YAML", exact: true }).click();
  const yaml = page.getByRole("textbox", { name: "Workflow YAML" });
  const draft = source.replace(name, `${name} draft`);
  await yaml.fill(draft);
  await yaml.press("Tab");
  await page.getByText("— Edited", { exact: true }).waitFor();
  await button("Runs").click();
  await records.nth(1).click();
  await page.getByText(firstSample, { exact: true }).waitFor();
  await waitForField("Current YAML", "Changed");
  await field("Current binding").getByText(secondSample, { exact: true }).waitFor();
  await waitForField("Current bytes", "Changed");
  await openOutput();
  await value("Northwind").waitFor();
  if (process.env.MAI_HISTORY_SCREENSHOT) {
    await page.setViewportSize({ width: 1360, height: 1120 });
    await page.locator(".toast").waitFor({ state: "hidden" });
    await page.screenshot({ path: process.env.MAI_HISTORY_SCREENSHOT });
  }
  await page.setViewportSize({ width: 520, height: 760 });
  await value("Northwind").waitFor();
  const compact = await details.boundingBox();
  assert.ok(compact && compact.x >= 0 && compact.x + compact.width <= 520, "run details fit the compact viewport");
  await page.setViewportSize({ width: 1360, height: 860 });
  await backToWorkflow();
  assert.match(await step.getAttribute("class"), /\bsel\b/);
  assert.equal(await yaml.inputValue(), draft);
  assert.equal(readFileSync(join(env.workspace, path), "utf8"), source);

  // A legacy workbook remains readable; a missing output has an explicit state.
  await button("Runs").click();
  await list.getByRole("button", { name: /Earlier run/ }).click();
  await details.getByText(/run metadata is unavailable/i).waitFor();
  await openOutput();
  await value("Fabrikam").waitFor();
  await records.nth(1).click();
  await page.getByText(firstSample, { exact: true }).waitFor();
  rmSync(artifact(earlierRunId));
  await button("Refresh runs").click();
  await page.getByText("Output file is unavailable.", { exact: true }).waitFor();
  assert.equal(await workbook.count(), 0);
  assert.deepEqual(errors, [], "no page errors");
  console.log("Studio history smoke passed: persisted runs, reopened workbooks, matching and changed provenance, preserved draft and selection, compact history, legacy workbook, and unavailable output.");
} finally {
  await browser?.close();
  await new Promise((resolve) => (server ? server.httpServer.close(resolve) : resolve()));
  rmSync(env.dir, { recursive: true, force: true });
}
