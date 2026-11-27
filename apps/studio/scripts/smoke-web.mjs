// Drive the single-file browser Studio (dist-web/mai-studio.html, from
// `npm run build:web`) opened straight from disk, with every network request
// refused: start from a template, run it, open a workflow and its sample
// files, drop files on the window, and check that the files survive a reload.
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright-core";

const studio = fileURLToPath(new URL("..", import.meta.url));
const repo = fileURLToPath(new URL("../../..", import.meta.url));
const page_ = `${studio}dist-web/mai-studio.html`;
if (!existsSync(page_)) throw new Error("Build the page first: npm run build:web");
const executablePath = [
  process.env.MAI_CHROME,
  "/opt/pw-browsers/chromium",
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
  "/usr/bin/google-chrome",
  "/opt/google/chrome/chrome",
  process.env.PROGRAMFILES && `${process.env.PROGRAMFILES}\\Google\\Chrome\\Application\\chrome.exe`,
].filter(Boolean).find(existsSync);

const failures = [];
const network = [];
const browser = await chromium.launch({ ...(executablePath ? { executablePath } : {}), headless: true, args: ["--no-sandbox", "--allow-file-access-from-files"] });
try {
  const context = await browser.newContext({ viewport: { width: 1360, height: 860 }, acceptDownloads: true });
  await context.route(/^(?!file:|data:|blob:)/, (route) => {
    network.push(route.request().url());
    return route.abort();
  });
  const page = await context.newPage();
  page.on("pageerror", (e) => failures.push(e.message));
  page.on("console", (m) => m.type() === "error" && failures.push(m.text()));
  await page.goto(pathToFileURL(page_).href);

  // Home: no folder to open, the examples are there.
  await page.getByText("Check a workflow in your browser").waitFor();
  assert.equal(await page.getByRole("button", { name: "Open folder…" }).count(), 0);
  await page.locator(".wf", { hasText: "examples/orders-totals.yaml" }).waitFor();

  // A template with bundled sample data previews and runs at once.
  await page.locator(".tpl", { hasText: "Customer order totals" }).click();
  await page.getByRole("status").filter({ hasText: /Valid · \d+ rows out/ }).waitFor({ timeout: 60_000 });
  await page.getByRole("button", { name: /▶ Run/ }).click();
  await page.locator(".workbook-preview, [aria-label*='orkbook']").first().waitFor({ timeout: 60_000 });
  await page.getByText("Acme", { exact: true }).first().waitFor();
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "↓ Output" }).click();
  const output = await download;
  assert.match(output.suggestedFilename(), /\.xlsx$/);
  assert.ok(readFileSync(await output.path()).length > 1000, "the run output downloads");

  // Open files…: a workflow and its sample file, picked together.
  await page.getByRole("button", { name: "Workflows" }).click();
  const discard = page.getByRole("button", { name: /Discard/ });
  if (await discard.count()) await discard.first().click();
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Open files…" }).click();
  await (await chooser).setFiles([`${repo}examples/parts-summary.yaml`, `${repo}examples/parts.xlsx`]);
  await page.getByRole("status").filter({ hasText: /Valid · \d+ rows out/ }).waitFor({ timeout: 60_000 });
  await page.screenshot({ path: process.env.SHOT || "/dev/null" });
  await page.locator(".nav").getByText("samples/parts.xlsx").first().waitFor();

  // Save keeps it in the browser; YAML downloads.
  const yaml = page.waitForEvent("download");
  await page.getByRole("button", { name: "↓ YAML" }).click();
  assert.equal((await yaml).suggestedFilename(), "parts-summary.yaml");

  // Dropping files on the window imports them the same way.
  const regions = readFileSync(`${repo}examples/regions.csv`, "utf8");
  await page.evaluate((text) => {
    const data = new DataTransfer();
    data.items.add(new File([text], "regions.csv", { type: "text/csv" }));
    for (const type of ["dragenter", "dragover", "drop"])
      window.dispatchEvent(new DragEvent(type, { dataTransfer: data, bubbles: true, cancelable: true }));
  }, regions);
  await page.getByText(/regions\.csv/).first().waitFor();

  // Files are kept in the browser between visits.
  await page.waitForTimeout(800);
  await page.reload();
  await page.locator(".wf", { hasText: "workflows/parts-summary.yaml" }).waitFor();

  assert.deepEqual(network, [], "the page makes no network requests");
  assert.deepEqual(failures, [], "no page errors");
  console.log("Browser Studio smoke test passed");
} finally {
  await browser.close();
}
