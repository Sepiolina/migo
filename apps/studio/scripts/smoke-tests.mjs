// Exercise Studio workflow tests with the browser bridge and the real Go runner.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";
import { preview } from "vite";
import { installBridge, prepare, repo } from "./studio-bridge.mjs";

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

const workflow = "examples/orders-totals.yaml";
const suite = "examples/tests/orders-totals.test.yaml";
const fixturePaths = ["examples/orders.xlsx", "examples/parts.xlsx", suite, "examples/tests/orders-totals.xlsx"];
const golden = fixturePaths.at(-1);
const totalsCase = "totals per customer";
const missingSheetCase = "a workbook without an Orders sheet is refused";
const env = prepare();
const digest = (path) => createHash("sha256").update(readFileSync(join(env.workspace, path))).digest("hex");
let server, browser;
try {
  const source = readFileSync(join(env.workspace, workflow), "utf8");
  const draft = source.replace('num(col("Unit price"))', 'num(col("Unit price")) + 1');
  assert.notEqual(draft, source);
  const fixturesBefore = fixturePaths.map(digest);
  const cli = join(env.dir, process.platform === "win32" ? "migo.exe" : "migo");
  execFileSync(process.env.MAI_GO || "go", ["build", "-mod=readonly", "-o", cli, "./cmd/migo"], { cwd: repo, stdio: "inherit" });
  const runCli = (expectedCode) => {
    const result = spawnSync(cli, ["test", suite], { cwd: env.workspace, encoding: "utf8", timeout: 30000 });
    assert.ifError(result.error);
    assert.equal(result.status, expectedCode, result.stdout + result.stderr);
    return result.stdout;
  };
  const passing = runCli(0);
  assert.ok(passing.includes(`ok    ${totalsCase}`));
  assert.ok(passing.includes(`ok    ${missingSheetCase}`));
  assert.match(passing, /2 passed, 0 failed/);

  const csp = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8")).app.security.csp;
  server = await preview({ root, preview: { host: "127.0.0.1", port: 1428, strictPort: true, headers: { "Content-Security-Policy": csp } } });
  browser = await chromium.launch({ executablePath, headless: true, args: ["--no-sandbox", "--no-proxy-server"] });
  const page = await browser.newPage({ viewport: { width: 1200, height: 620 }, colorScheme: "dark" });
  page.setDefaultTimeout(10000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  const runnerCalls = [];
  await installBridge(page, env, () => {
    throw new Error("Workflow tests must use the suite fixtures without asking for Studio sample bindings.");
  }, (call) => runnerCalls.push(call));
  await page.goto("http://127.0.0.1:1428/");

  const button = (name) => page.getByRole("button", { name, exact: true });
  const label = (name) => page.getByText(name, { exact: true });
  const backToWorkflow = () => page.getByRole("button", { name: /Back to workflow$/ }).click();
  const yaml = page.getByRole("textbox", { name: "Workflow YAML" });
  const field = (name, value) => label(name).locator("xpath=following-sibling::dd[1]").filter({ has: label(value) });
  const assertPass = async () => {
    await label("Passed").nth(1).waitFor();
    assert.equal(await label("Passed").count(), 2);
    await label(totalsCase).waitFor();
    await label(missingSheetCase).waitFor();
    await field("Expected error fragment", "Orders").waitFor();
    await label("Actual error").locator("xpath=following-sibling::dd[1]").filter({ hasText: /Orders/ }).waitFor();
    await field("Tested revision", digest(workflow)).waitFor();
  };

  await button("Open folder…").click();
  await page.getByRole("button", { name: /^Customer order totals examples\/orders-totals\.yaml/ }).click();
  await button("Tests").click();
  await page.getByRole("heading", { name: "Workflow tests", exact: true }).waitFor();
  await button("Run all tests").click();
  await assertPass();
  await backToWorkflow();

  await page.getByRole("tab", { name: "YAML", exact: true }).click();
  await yaml.fill(draft);
  await yaml.press("Tab");
  await label("— Edited").waitFor();
  await button("Tests").click();
  await button("Run all tests").click();
  await button("Test saved version").click();
  await assertPass();
  assert.equal(readFileSync(join(env.workspace, workflow), "utf8"), source);
  await backToWorkflow();
  assert.equal(await yaml.inputValue(), draft);
  await label("— Edited").waitFor();
  await button("Tests").click();
  await button("Run all tests").click();
  await button("Save and run tests").click();
  await label("Failed").waitFor();
  await label("Passed").waitFor();
  assert.equal(readFileSync(join(env.workspace, workflow), "utf8"), draft);

  const failing = runCli(1);
  assert.match(failing, /1 passed, 1 failed/);
  const mismatch = failing.match(/sheet ("[^"]+") cell ([A-Z]+\d+): got ("[^"]+"), expected ("[^"]+")/);
  assert.ok(mismatch, failing);
  const [message, sheet, cell, actual, expected] = mismatch;
  for (const [name, value] of [
    ["Sheet", JSON.parse(sheet)],
    ["Cell", cell],
    ["Actual value", actual],
    ["Expected value", expected],
  ]) {
    await field(name, value).waitFor();
  }
  await label(message).waitFor();
  await field("Tested revision", digest(workflow)).waitFor();
  if (process.env.MAI_TESTS_SCREENSHOT) {
    await page.locator(".toast").waitFor({ state: "hidden" });
    await page.screenshot({ path: process.env.MAI_TESTS_SCREENSHOT });
  }

  const moved = join(env.dir, "temporarily-missing.xlsx");
  renameSync(join(env.workspace, golden), moved);
  try {
    await button("Run all tests").click();
    await page.getByText(/orders-totals\.xlsx.*(?:no such file|cannot find|does not exist|missing|not found)/i).waitFor();
    await label("Failed").waitFor();
    await label("Passed").waitFor();
  } finally {
    renameSync(moved, join(env.workspace, golden));
  }

  // Give the real runner enough cases that the UI can cancel while it is working.
  const cancellationCases = Array.from({ length: 600 }, (_, index) => `  - name: Cancellation case ${index}
    in: {orders: ../orders.xlsx}
    expect: orders-totals.xlsx
`).join("");
  writeFileSync(join(env.workspace, "examples/tests/cancellation.test.yaml"), `workflow: ../orders-totals.yaml
tests:
${cancellationCases}`);
  await button("Refresh suites").click();
  await button("Run suite cancellation").click();
  await button("Cancel tests").click();
  await page.getByRole("status").filter({ hasText: "Workflow tests cancelled." }).waitFor();
  await button("Run suite orders-totals").click();
  await label(message).waitFor();

  await page.setViewportSize({ width: 390, height: 740 });
  for (const name of ["Run all tests", "Run suite orders-totals"]) {
    await button(name).waitFor();
    const bounds = await button(name).boundingBox();
    assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= 390, `${name} fits a compact viewport`);
  }
  assert.ok(await page.locator(".workflow-tests-body").evaluate((body) => body.scrollWidth <= body.clientWidth),
    "workflow test content fits a compact viewport");

  const testCalls = runnerCalls.filter(({ command }) => command === "runTests");
  assert.equal(testCalls.filter(({ responseBytes }) => responseBytes === 0).length, 1, "cancellation stops the runner before it returns a report");
  assert.deepEqual(fixturePaths.map(digest), fixturesBefore, "workflow tests leave fixture bytes unchanged");
  assert.deepEqual(errors, [], "no page errors");
  console.log("Studio workflow tests smoke passed: CLI parity, expected error, workbook cell mismatch, saved revisions, missing fixture, cancellation, and unchanged fixture bytes.");
} finally {
  await browser?.close();
  await new Promise((resolve) => (server ? server.httpServer.close(resolve) : resolve()));
  rmSync(env.dir, { recursive: true, force: true });
}
