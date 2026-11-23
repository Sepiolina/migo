// Exercise the built Studio in Chromium using the shared native-command bridge,
// real workflow files, and a browser profile that survives process restarts.
import assert from "node:assert/strict";
import { cpSync, existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
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
const workflowPath = "examples/orders-totals.yaml";
const productionPath = join(env.workspace, workflowPath);
const copyPath = "workflows/order-totals-recovered.yaml";
const initial = readFileSync(productionPath, "utf8").replaceAll("\r\n", "\n");
const label = (name) => initial.replace("name: Customer order totals", `name: ${name}`);
const profile = join(env.dir, "browser");
const unrelated = join(env.dir, "unrelated");
const failures = [];
let server;
let context;
let url;

async function newStudio(workspace = env.workspace, holdSave) {
  const page = await context.newPage();
  page.setDefaultTimeout(8000);
  page.on("pageerror", error => failures.push(error.message));
  await installBridge(page, { ...env, workspace });
  if (holdSave) {
    await page.exposeFunction("__holdDraftSmokeSave", holdSave);
    await page.addInitScript(() => {
      window.addEventListener("DOMContentLoaded", () => {
        const invoke = window.__maiNative;
        window.__maiNative = async (command, args) => {
          if (command === "save_workflow") {
            window.__draftSmokeSaveStarted = true;
            await window.__holdDraftSmokeSave();
          }
          return invoke(command, args);
        };
      }, { once: true });
    });
  }
  await page.goto(url);
  const openFolder = page.getByRole("button", { name: "Open folder…" });
  if (await openFolder.isVisible()) await openFolder.click();
  await workflowCard(page).waitFor();
  return page;
}

function workflowCard(page) {
  return page.locator(".project-workflow .wf", { hasText: workflowPath });
}

function yaml(page) {
  return page.getByRole("textbox", { name: "Workflow YAML" });
}

async function showYaml(page) {
  const tab = page.getByRole("tab", { name: "YAML", exact: true });
  if (await tab.getAttribute("aria-selected") !== "true") await tab.click();
}

async function waitForCheckpoint(page, source) {
  await page.waitForFunction((expected) =>
    Object.keys(localStorage).some(key =>
      key.startsWith("mai.workflow-draft:v1:") && JSON.parse(localStorage.getItem(key)).source === expected
    ), source, { timeout: 8000 });
}

const recovery = (page) => page.getByRole("dialog", { name: "Recover workflow draft" });
const conflict = (page) => page.getByRole("dialog", { name: "Workflow save conflict" });
const save = (page) => page.getByRole("button", { name: "Save", exact: true });
const workflows = (page) => page.getByRole("button", { name: "Workflows", exact: true });
const disk = () => readFileSync(productionPath, "utf8").replaceAll("\r\n", "\n");

async function launch() {
  context = await chromium.launchPersistentContext(profile, {
    executablePath,
    headless: true,
    args: ["--no-sandbox", "--no-proxy-server"],
    viewport: { width: 1440, height: 900 },
  });
}

try {
  cpSync(join(env.workspace, "examples"), join(unrelated, "examples"), { recursive: true });
  const csp = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8")).app.security.csp;
  server = await preview({ root, preview: { host: "127.0.0.1", port: 1431, strictPort: true, headers: { "Content-Security-Policy": csp } } });
  url = "http://127.0.0.1:1431/";
  await launch();

  // Close with the YAML textarea focused, after autosave and without blur or manual save.
  let page = await newStudio();
  await workflowCard(page).click();
  await showYaml(page);
  const interrupted = label("Order totals — interrupted session");
  await yaml(page).fill(interrupted);
  await waitForCheckpoint(page, interrupted);
  assert.equal(await yaml(page).evaluate(element => document.activeElement === element), true);
  assert.equal(disk(), initial);
  await page.close({ runBeforeUnload: false });
  await context.close();
  await launch();

  // Temporarily opening the saved version leaves the recovery copy available.
  page = await newStudio();
  await workflowCard(page).click();
  await recovery(page).getByRole("button", { name: "Open saved version", exact: true }).click();
  await showYaml(page);
  assert.equal(await yaml(page).inputValue(), initial);
  await workflows(page).click();
  await workflowCard(page).click();
  await recovery(page).getByRole("button", { name: "Restore draft", exact: true }).click();
  await showYaml(page);
  assert.equal(await yaml(page).inputValue(), interrupted);
  assert.equal(disk(), initial);
  await save(page).click();
  await page.locator(".toast", { hasText: `Saved ${workflowPath}` }).waitFor();
  assert.equal(disk(), interrupted);
  await page.close();

  // The second tab can save its own editor while the first retains its checkpoint.
  const first = await newStudio();
  const second = await newStudio();
  await workflowCard(first).click();
  await workflowCard(second).click();
  await showYaml(first);
  await showYaml(second);
  const unsaved = label("Order totals — reshaped in Studio");
  const external = label("Order totals — updated by another editor");
  await yaml(first).fill(unsaved);
  await waitForCheckpoint(first, unsaved);
  await yaml(second).fill(external);
  await waitForCheckpoint(second, external);
  await waitForCheckpoint(first, unsaved);
  await save(second).click();
  await second.locator(".toast", { hasText: `Saved ${workflowPath}` }).waitFor();
  assert.equal(disk(), external);
  await second.close();

  // A stale save opens the real comparison and Save copy preserves both versions.
  await save(first).click();
  await conflict(first).waitFor();
  assert.match(await conflict(first).getByRole("region", { name: "Draft version YAML" }).textContent(), /name: Order totals — reshaped in Studio/);
  assert.match(await conflict(first).getByRole("region", { name: "Saved version YAML" }).textContent(), /name: Order totals — updated by another editor/);
  if (process.env.MAI_DRAFT_SCREENSHOT) {
    await first.locator(".toast").waitFor({ state: "hidden" });
    await first.screenshot({ path: process.env.MAI_DRAFT_SCREENSHOT, animations: "disabled" });
  }
  await conflict(first).getByRole("button", { name: "Save copy", exact: true }).click();
  const saveDialog = first.getByRole("dialog", { name: "Save a workflow copy" });
  await saveDialog.getByRole("textbox", { name: "File path" }).fill(workflowPath);
  await saveDialog.getByRole("button", { name: "Save copy", exact: true }).click();
  await saveDialog.getByRole("alert").filter({ hasText: "file already exists" }).waitFor();
  assert.equal(disk(), external);
  await saveDialog.getByRole("textbox", { name: "File path" }).fill(copyPath);
  await saveDialog.getByRole("button", { name: "Save copy", exact: true }).click();
  await first.locator(".toast", { hasText: `Saved ${copyPath}` }).waitFor();
  assert.equal(readFileSync(join(env.workspace, copyPath), "utf8"), unsaved);
  assert.equal(disk(), external);
  await first.close();

  // Delay the save while typing newer text. The older version reaches disk,
  // while the post-submit text remains a matching recovery copy on restart.
  let releaseSave;
  const saveHeld = new Promise(resolve => { releaseSave = resolve; });
  page = await newStudio(env.workspace, () => saveHeld);
  await workflowCard(page).click();
  await showYaml(page);
  const submitted = label("Order totals — submitted");
  const newer = label("Order totals — typed during save");
  await yaml(page).fill(submitted);
  await waitForCheckpoint(page, submitted);
  await save(page).click();
  try {
    await page.waitForFunction(() => window.__draftSmokeSaveStarted === true, undefined, { timeout: 8000 });
    await yaml(page).fill(newer);
    await waitForCheckpoint(page, newer);
    assert.equal(disk(), external);
  } finally {
    releaseSave();
  }
  await page.locator(".toast", { hasText: `Saved ${workflowPath}` }).waitFor();
  assert.equal(disk(), submitted);
  assert.equal(await yaml(page).inputValue(), newer);
  await page.close({ runBeforeUnload: false });
  await context.close();
  await launch();

  // Identical relative paths in another workspace have independent recovery.
  const other = await newStudio(unrelated);
  assert.equal(await other.getByRole("heading", { name: "Recovery drafts" }).count(), 0);
  await workflowCard(other).click();
  await showYaml(other);
  assert.equal(await yaml(other).inputValue(), initial);
  await other.close();

  page = await newStudio();
  await workflowCard(page).click();
  await recovery(page).waitFor();
  await recovery(page).getByRole("button", { name: "Restore draft", exact: true }).click();
  await showYaml(page);
  assert.equal(await yaml(page).inputValue(), newer);
  await workflows(page).click();
  await page.getByRole("button", { name: /Discard changes/ }).click();
  await workflowCard(page).click();
  await showYaml(page);
  assert.equal(await yaml(page).inputValue(), submitted);
  assert.equal(await recovery(page).count(), 0);
  await page.close({ runBeforeUnload: false });

  // Verify deliberate discard survives another browser restart.
  await context.close();
  await launch();
  page = await newStudio();
  await workflowCard(page).click();
  await showYaml(page);
  assert.equal(await yaml(page).inputValue(), submitted);

  // Reopening a draft based on a superseded disk revision also opens the
  // comparison. Reloading drops that draft and loads the current disk contents.
  const staleDraft = label("Order totals — unsaved before disk change");
  const rewritten = label("Order totals — saved outside Studio");
  await yaml(page).fill(staleDraft);
  await waitForCheckpoint(page, staleDraft);
  await page.close({ runBeforeUnload: false });
  writeFileSync(productionPath, rewritten);
  page = await newStudio();
  await workflowCard(page).click();
  assert.equal(await conflict(page).getByRole("region", { name: "Draft version YAML" }).textContent(), staleDraft);
  assert.equal(await conflict(page).getByRole("region", { name: "Saved version YAML" }).textContent(), rewritten);
  await conflict(page).getByRole("button", { name: "Reload disk version", exact: true }).click();
  await showYaml(page);
  assert.equal(await yaml(page).inputValue(), rewritten);
  await workflows(page).click();
  assert.equal(await page.getByRole("heading", { name: "Recovery drafts" }).count(), 0);

  await page.getByRole("button", { name: /Look up and enrich/ }).click();
  await showYaml(page);
  const untitled = (await yaml(page).inputValue()).replace(/^name:.*$/m, "name: Unsaved regional report");
  await yaml(page).fill(untitled);
  await waitForCheckpoint(page, untitled);
  await page.close({ runBeforeUnload: false });

  page = await newStudio();
  const untitledEntry = page.getByRole("region", { name: "Recovery drafts" }).getByRole("button", { name: /Untitled workflow/ });
  await untitledEntry.click();
  assert.equal(await recovery(page).getByRole("region", { name: "Draft version YAML" }).textContent(), untitled);
  await recovery(page).getByRole("button", { name: "Discard recovery copy", exact: true }).click();
  await untitledEntry.waitFor({ state: "hidden" });
  assert.deepEqual(failures, [], "Studio page errors");
  console.log("Draft recovery smoke passed: abrupt restart, save conflict and Save copy, reopened disk conflict and reload, concurrent tabs, newer text during save, workspace isolation, untitled drafts, and deliberate discard.");
} finally {
  await context?.close();
  await new Promise(resolve => server ? server.httpServer.close(resolve) : resolve());
  rmSync(env.dir, { recursive: true, force: true });
}
