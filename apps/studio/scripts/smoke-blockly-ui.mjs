// Exercise the actual Blockly canvas in a system-installed browser; no browser download is needed.
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { preview } from "vite";
import { chromium } from "playwright-core";

const root = fileURLToPath(new URL("..", import.meta.url));
const candidates = [
  process.env.MAI_CHROME,
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
  "/usr/bin/google-chrome",
  "/opt/google/chrome/chrome",
  process.env.PROGRAMFILES &&
    `${process.env.PROGRAMFILES}\\Google\\Chrome\\Application\\chrome.exe`,
  process.env["PROGRAMFILES(X86)"] &&
    `${process.env["PROGRAMFILES(X86)"]}\\Google\\Chrome\\Application\\chrome.exe`,
].filter(Boolean);
const executablePath = candidates.find(existsSync);
if (!executablePath)
  throw new Error(
    "No system Chrome/Chromium found. Set MAI_CHROME to its executable.",
  );
let server;
let browser;
const failures = [];
try {
  const nativeConfig = JSON.parse(
    readFileSync(
      new URL("../src-tauri/tauri.conf.json", import.meta.url),
      "utf8",
    ),
  );
  server = await preview({
    root,
    preview: {
      host: "127.0.0.1",
      port: 1422,
      strictPort: true,
      headers: { "Content-Security-Policy": nativeConfig.app.security.csp },
    },
  });
  browser = await chromium.launch({
    executablePath,
    headless: true,
    args: ["--no-sandbox", "--no-proxy-server"],
  });
  const page = await browser.newPage({
    viewport: { width: 1280, height: 900 },
  });
  page.setDefaultTimeout(20000);
  page.on("dialog", (dialog) => dialog.accept());
  page.on("pageerror", (error) =>
    failures.push(`Page error: ${error.message}`),
  );
  page.on("console", (message) => {
    if (message.type() === "error") failures.push(message.text());
  });
  page.on("response", (response) => {
    if (response.status() >= 400 && !response.url().endsWith("/favicon.ico"))
      failures.push(`HTTP ${response.status()}: ${response.url()}`);
  });
  await page.goto("http://127.0.0.1:1422/", { waitUntil: "networkidle" });
  await page.getByRole("heading", { name: "Build with blocks" }).waitFor();
  const numberField = page.locator(".mai_number .blocklyNumberField");
  await numberField.getByText("0", { exact: true }).waitFor();
  await numberField.dblclick();
  const input = page.locator(".blocklyHtmlInput");
  await input.waitFor({ state: "visible" });
  await input.fill("5");
  await input.press("Enter");
  await numberField.getByText("5", { exact: true }).waitFor();
  await page.getByRole("button", { name: "YAML source", exact: true }).click();
  const yaml = page.getByRole("textbox", { name: "Workflow YAML source" });
  await yaml.waitFor();
  if (!/Quantity[^\n]*5/.test(await yaml.inputValue()))
    throw new Error(
      `Blockly number edit did not reach YAML: ${await yaml.inputValue()}`,
    );
  await page.getByRole("button", { name: "Blocks", exact: true }).click();
  await page
    .locator(".mai_number .blocklyNumberField")
    .getByText("5", { exact: true })
    .waitFor();
  // Exercise the application's document history with focus on the workspace.
  const background = await page.locator(".blocklyMainBackground").boundingBox();
  if (!background) throw new Error("Blockly canvas is missing.");
  await page.mouse.click(
    background.x + background.width - 100,
    background.y + 50,
  );
  await page.keyboard.press("Control+z");
  await page
    .locator(".mai_number .blocklyNumberField")
    .getByText("0", { exact: true })
    .waitFor();
  // Detach the last step using a real pointer drag, then verify draft protection.
  const save = page.locator(".mai_save .blocklyPath").first();
  const box = await save.boundingBox();
  const canvas = await page.locator(".blocklyMainBackground").boundingBox();
  if (!box || !canvas)
    throw new Error("Could not locate the save block and canvas for dragging.");
  await page.mouse.move(box.x + 12, box.y + 20);
  await page.mouse.down();
  await page.mouse.move(canvas.x + canvas.width - 100, canvas.y + 100, {
    steps: 15,
  });
  await page.mouse.up();
  await page.getByText("Finish your block edits", { exact: true }).waitFor();
  if (
    await page
      .getByRole("button", { name: "YAML source", exact: true })
      .isEnabled()
  )
    throw new Error("YAML navigation remained enabled with detached blocks.");
  if (
    await page
      .getByRole("button", { name: "Workflow canvas", exact: true })
      .isEnabled()
  )
    throw new Error("Canvas navigation remained enabled with detached blocks.");
  await page.getByRole("button", { name: "Discard block edits" }).click();
  await page
    .getByText("Finish your block edits", { exact: true })
    .waitFor({ state: "hidden" });
  if (
    !(await page
      .getByRole("button", { name: "YAML source", exact: true })
      .isEnabled())
  )
    throw new Error("Discard did not clear the draft.");
  await page.getByRole("button", { name: "+ New workflow" }).click();
  await page.getByRole("button", { name: /Copy a worksheet/ }).click();
  await page.getByRole("heading", { name: "Build with blocks" }).waitFor();
  await page
    .locator(".mai_save")
    .getByText("result", { exact: true })
    .waitFor();
  await page.getByLabel("Workflow display name").fill("Smoke test workbook");
  await page.getByRole("button", { name: "Apply workbook settings" }).click();
  await page.getByRole("button", { name: "YAML source", exact: true }).click();
  if (
    !(
      await page
        .getByRole("textbox", { name: "Workflow YAML source" })
        .inputValue()
    ).includes("Smoke test workbook")
  )
    throw new Error("Workbook setup did not update YAML.");
  await page.getByRole("button", { name: "Blocks", exact: true }).click();
  await page.setViewportSize({ width: 980, height: 680 });
  await page.getByRole("button", { name: "Workbook setup" }).click();
  await page.getByRole("heading", { name: "Build with blocks" }).waitFor();
  if (
    await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth,
    )
  )
    throw new Error("Studio overflows horizontally at 980px width.");
  if (failures.length) throw new Error(failures.join("\n"));
  console.log("Blockly browser smoke passed.");
} finally {
  await browser?.close();
  if (server)
    await new Promise((resolve, reject) =>
      server.httpServer.close((error) => (error ? reject(error) : resolve())),
    );
}
