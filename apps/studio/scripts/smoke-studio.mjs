// Drive the built Studio in a system Chrome against the real Go runner:
// open the BOM, preview it step by step, inspect the written workbook, add
// a step from a column, and run and save workflows from templates.
import assert from "node:assert/strict";
import { existsSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
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
  const browserOnly = await browser.newPage();
  for (const studio of [page, browserOnly]) {
    studio.on("pageerror", (e) => failures.push(e.message));
    studio.on("console", (m) => m.type() === "error" && failures.push(m.text()));
  }
  await browserOnly.goto("http://127.0.0.1:1423/");
  await browserOnly.getByText(/This is the browser preview/).waitFor();
  await browserOnly.getByRole("button", { name: /Look up and enrich/ }).click();
  await browserOnly.locator(".nav").getByRole("button", { name: /sheet By region/ }).click();
  await browserOnly.getByText("Open the desktop app to read files.", { exact: true }).waitFor();
  await browserOnly.close();

  const samplePicks = ["testdata/bom_basic.xlsx", "testdata/prio_basic.xlsx", "testdata/priority-moved.xlsx"];
  await installBridge(page, env, () => ({ path: samplePicks.shift(), copied: false }));
  await page.goto("http://127.0.0.1:1423/");

  // Home: open the folder and the BOM.
  await page.getByRole("button", { name: "Open folder…" }).click();
  await page.locator(".wf", { hasText: "workflows/bom.yaml" }).click();
  const rows = page.locator(".nav .row");
  await rows.filter({ hasText: "Fill blanks down" }).waitFor();
  assert.ok((await rows.count()) > 40, "every BOM step is listed");

  // Desktop pane widths survive reopening Studio.
  const separators = page.locator(".panes").getByRole("separator");
  const width = (selector) => page.locator(selector).evaluate((el) => el.getBoundingClientRect().width);
  const navWidth = await width(".nav");
  await separators.first().press("ArrowRight");
  assert.ok(await width(".nav") > navWidth, "right arrow expands the workflow pane");
  const inspectorWidth = await width(".insp");
  const handle = await separators.last().boundingBox();
  const handleX = handle.x + handle.width / 2;
  const handleY = handle.y + handle.height / 2;
  await page.mouse.move(handleX, handleY);
  await page.mouse.down();
  await page.mouse.move(handleX - 40, handleY);
  await page.mouse.up();
  assert.ok(await width(".insp") > inspectorWidth, "dragging the divider expands the inspector");
  const resized = [await width(".nav"), await width(".insp")];
  await page.reload();
  await page.getByRole("button", { name: "Open folder…" }).click();
  await page.getByRole("button", { name: "Set up samples for workflows/bom.yaml" }).click();
  const setup = page.getByRole("dialog", { name: "Set up samples" });
  await setup.waitFor();
  const bomInput = setup.getByRole("region", { name: "Customer BOM", exact: true });
  const bomPath = bomInput.getByRole("textbox");
  await bomPath.fill("examples/regions.csv");
  await bomInput.getByText("Sheet1", { exact: true }).waitFor();
  await bomPath.fill("testdata/golden_random7_nosap.xlsx");
  await bomInput.getByText("BOM, debug", { exact: true }).waitFor();
  for (const label of ["Customer BOM", "Priority"]) {
    await setup.getByRole("region", { name: label, exact: true }).getByRole("button", { name: /Browse/ }).click();
  }
  await setup.getByRole("button", { name: "Finish" }).click();
  assert.deepEqual([await width(".nav"), await width(".insp")], resized);
  await page.locator(".nav .row", { hasText: "Fill blanks down" }).click();
  await page.locator(".grid tbody tr").first().waitFor();
  assert.match(await page.locator(".sbar").innerText(), /6 rows/);
  await page.locator(".pill", { hasText: /^Valid ·/ }).waitFor();

  assert.equal(await page.getByRole("tab", { name: "Problems", exact: true }).count(), 1);
  await page.getByText("Preview details", { exact: true }).click();
  await page.getByText(/SQL and REST steps run on the server only/).waitFor();
  await page.getByText("Preview details", { exact: true }).click();

  // Compare the same effective part number before and after forward fill.
  const comparison = page.getByRole("group", { name: "Step comparison" });
  await comparison.getByRole("button", { name: "Affected columns" }).click();
  await comparison.getByRole("button", { name: "Before", exact: true }).click();
  const changedPart = page.getByRole("gridcell", { name: /Row 2, column EffPartNumber, .*changed by fill/ });
  assert.equal(await changedPart.innerText(), "");
  await comparison.getByRole("button", { name: "After", exact: true }).click();
  assert.equal(await changedPart.innerText(), "TOPASSY-001");
  await comparison.getByRole("button", { name: "Affected columns" }).click();

  // Jump to an off-screen fill column and copy three effective part numbers.
  await page.getByRole("button", { name: "Go to column EffPartNumber", exact: true }).click();
  const grid = page.getByRole("region", { name: "Rows" });
  assert.ok(await grid.evaluate((el) => el.scrollLeft > 0), "the jump scrolls the table horizontally");
  await page.keyboard.press("Shift+ArrowDown");
  await page.keyboard.press("Shift+ArrowDown");
  assert.equal(await grid.getByRole("gridcell", { selected: true }).count(), 3);
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"], { origin: "http://127.0.0.1:1423" });
  await page.evaluate(() => navigator.clipboard.writeText(""));
  await page.keyboard.press("Control+c");
  await page.waitForFunction(async () =>
    (await navigator.clipboard.readText()).replaceAll("\r\n", "\n") === "TOPASSY-001\nTOPASSY-001\nTOPASSY-001",
  );

  // At the CSS viewport equivalent to 200% zoom, inspect the same cell
  // after closing both search and the compact Settings panel.
  const cells = page.getByRole("grid", { name: "Preview cells" });
  const selectedCell = await cells.getAttribute("aria-activedescendant");
  assert.match(await cells.ariaSnapshot(), /Row 3, column EffPartNumber, TOPASSY-001/);
  for (const colorScheme of ["dark", "light"]) {
    await page.emulateMedia({ colorScheme });
    const fills = await cells.evaluate((table) =>
      [true, false].map((selected) =>
        getComputedStyle(table.querySelector(`td[aria-selected="${selected}"]`)).backgroundColor,
      ),
    );
    assert.notEqual(fills[0], fills[1], `selected range is distinguished in ${colorScheme} mode`);
  }
  await page.setViewportSize({ width: 680, height: 430 });
  await page.keyboard.press("Control+k");
  await page.getByRole("combobox", { name: "Search commands" }).press("Escape");
  assert.ok(await cells.evaluate((el) => document.activeElement === el));
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByRole("button", { name: "Close inspector" }).press("Enter");
  await page.waitForFunction(() => document.activeElement?.getAttribute("role") === "grid");
  assert.equal(await cells.getAttribute("aria-activedescendant"), selectedCell);
  assert.equal(await separators.count(), 0);
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  await page.setViewportSize({ width: 1360, height: 860 });

  // A sample replaced in another app keeps its path. Refocusing Studio must
  // invalidate the successful trace and recover when the workbook is restored.
  const priorityPath = `${env.workspace}/testdata/prio_basic.xlsx`;
  const originalPriority = readFileSync(priorityPath);
  writeFileSync(priorityPath, readFileSync(`${env.workspace}/testdata/bom_basic.xlsx`));
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.locator(".pill", { hasText: "Has errors" }).waitFor();
  writeFileSync(priorityPath, originalPriority);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.locator(".pill", { hasText: /^Valid ·/ }).waitFor();
  assert.match(await page.locator(".sbar").innerText(), /6 rows/);

  // Repair a moved workbook while keeping the selected step and other binding.
  renameSync(priorityPath, `${env.workspace}/testdata/priority-moved.xlsx`);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.locator(".pill", { hasText: "Has errors" }).waitFor();
  await page.locator(".nav").getByRole("button", { name: "Set up samples" }).click();
  await setup.getByRole("button", { name: /Locate file/i }).click();
  await setup.getByRole("button", { name: "Finish" }).click();
  await page.locator(".pill", { hasText: /^Valid ·/ }).waitFor();
  assert.match(await page.locator(".nav .row.sel").innerText(), /Fill blanks down/);
  assert.match(await page.locator(".nav").innerText(), /testdata\/bom_basic.xlsx/);
  assert.match(await page.locator(".nav").innerText(), /testdata\/priority-moved.xlsx/);

  // Run the BOM and inspect the written XLSX, including styled blank cells.
  const workbook = page.getByRole("grid", { name: "Worksheet cells" });
  const sheetTabs = page.getByRole("tablist", { name: "Workbook sheets" });
  const sheet = (name) => sheetTabs.getByRole("tab", { name, exact: true, selected: true });
  const cell = (name) => workbook.getByRole("gridcell", { name });
  // The grid's ARIA indices include its worksheet coordinate headers.
  const cellAt = (row, column) => workbook.locator(`tr[aria-rowindex="${row + 1}"] td[aria-colindex="${column + 1}"]`);
  const bomOutput = page.locator(".nav button.row").filter({ has: page.locator(".name", { hasText: /^BOM/ }) });
  const debugOutput = page.locator(".nav button.row").filter({ has: page.locator(".name", { hasText: /^debug/ }) });
  async function goToCell(address, target) {
    const input = page.getByRole("textbox", { name: "Cell address" });
    await input.fill(address);
    await input.press("Enter");
    await target.waitFor();
    assert.equal(await workbook.getAttribute("aria-activedescendant"), await target.getAttribute("id"));
    assert.ok(await workbook.evaluate((table) => document.activeElement === table));
  }
  const cellStyle = (locator) => locator.evaluate((element) => {
    const { backgroundColor, color, fontWeight } = getComputedStyle(element);
    return { backgroundColor, color, fontWeight };
  });
  await page.keyboard.press("Control+Enter");
  await sheet("BOM").waitFor();
  assert.match(await bomOutput.getAttribute("class"), /\bsel\b/);
  const heading = cellAt(1, 1);
  assert.equal(await heading.innerText(), "Level");
  const headerHeight = (await heading.boundingBox()).height;
  assert.ok(Math.abs(headerHeight - 28 * 96 / 72) <= 1, `28-point BOM header measured ${headerHeight}px`);
  assert.equal(await cellAt(2, 2).innerText(), "TOPASSY-001");
  const limitations = page.getByText("Preview limitations", { exact: true });
  await limitations.click();
  await page.getByText("Frozen panes", { exact: true }).waitFor();
  await limitations.click();
  const main = cell(/^Row 3, column O, MAIN/);
  await goToCell("O3", main);
  assert.deepEqual(await cellStyle(main), { backgroundColor: "rgb(0, 112, 192)", color: "rgb(255, 255, 255)", fontWeight: "700" });
  const separator = cellAt(3, 12);
  assert.equal(await separator.innerText(), "");
  assert.equal((await cellStyle(separator)).backgroundColor, "rgb(0, 0, 0)");
  const separatorWidth = (await separator.boundingBox()).width;
  assert.ok(Math.abs(separatorWidth - 28) <= 1, `4-unit BOM column measured ${separatorWidth}px`);
  for (const [row, column, value, fill] of [
    [6, 10, "YAGEO CORPORATION", "rgb(255, 0, 0)"],
    [6, 11, "RC0402FR-0710KL_REV_B2", "rgb(255, 0, 0)"],
    [5, 15, "MAIN", "rgb(0, 112, 192)"],
    [3, 14, "CREATE P/N", "rgb(255, 255, 0)"],
  ]) {
    const target = cellAt(row, column);
    assert.equal(await target.innerText(), value);
    assert.equal((await cellStyle(target)).backgroundColor, fill);
  }
  if (process.env.MAI_WORKBOOK_SCREENSHOT) {
    await page.setViewportSize({ width: 2140, height: 570 });
    await page.getByRole("button", { name: "Close console" }).click();
    await bomOutput.scrollIntoViewIfNeeded();
    await goToCell("O3", main);
    await page.locator(".toast").waitFor({ state: "hidden" });
    await page.screenshot({ path: process.env.MAI_WORKBOOK_SCREENSHOT });
    await page.setViewportSize({ width: 1360, height: 860 });
  }

  await page.setViewportSize({ width: 390, height: 800 });
  const compactMain = cell(/^Row 5, column O, MAIN/);
  await goToCell("O5", compactMain);
  assert.ok(await compactMain.evaluate((entry) => {
    const cell = entry.getBoundingClientRect();
    const viewport = entry.closest(".worksheet-grid");
    return cell.left >= viewport.getBoundingClientRect().left
      && cell.right <= viewport.getBoundingClientRect().left + viewport.clientWidth + 1;
  }));
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  assert.ok(await sheet("BOM").isVisible());
  await page.setViewportSize({ width: 1360, height: 860 });

  await sheetTabs.getByRole("tab", { name: "debug", exact: true }).click();
  await sheet("debug").waitFor();
  assert.match(await debugOutput.getAttribute("class"), /\bsel\b/);
  await goToCell("AA7", cell(/^Row 7, column AA, -/));
  const debugHeader = cellAt(1, 20);
  assert.equal(await debugHeader.innerText(), "Debug: Priority (P1)");
  assert.deepEqual(await cellStyle(debugHeader), { backgroundColor: "rgb(44, 62, 80)", color: "rgb(255, 255, 255)", fontWeight: "700" });
  if (process.env.MAI_WORKBOOK_DEBUG_SCREENSHOT) await page.screenshot({ path: process.env.MAI_WORKBOOK_DEBUG_SCREENSHOT });
  await goToCell("AB9", cell("Row 9, column AB, blank"));
  await bomOutput.click();
  await sheet("BOM").waitFor();
  await debugOutput.click();
  await sheet("debug").waitFor();
  await page.locator(".nav .row", { hasText: "Fill blanks down" }).click();
  await page.locator(".grid tbody tr").first().waitFor();

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

  // An edit leaves the last workbook readable until the new run completes.
  await bomOutput.click();
  await sheet("BOM").waitFor();
  const previousRun = page.getByText("Previous run", { exact: true });
  await previousRun.waitFor();
  assert.equal(await cellAt(2, 2).innerText(), "TOPASSY-001");
  await page.getByRole("button", { name: "Run to refresh", exact: true }).click();
  await previousRun.waitFor({ state: "hidden" });
  await sheet("BOM").waitFor();
  assert.equal(await cellAt(2, 2).innerText(), "TOPASSY-001");

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
  await sheet("By region").waitFor();
  const north = cellAt(2, 1);
  const south = cellAt(3, 1);
  assert.match(await north.locator("..").innerText(), /North\s+2\s+50/);
  assert.match(await south.locator("..").innerText(), /South\s+1\s+5/);
  const textAlignment = (entry) => entry.evaluate((element) => getComputedStyle(element).textAlign);
  assert.equal(await textAlignment(north), "left");
  assert.equal(await textAlignment(cellAt(2, 3)), "right");
  await page.keyboard.press("Control+s");
  await page.getByRole("dialog", { name: "Save workflow" }).getByRole("button", { name: "Save" }).click();
  await page.locator(".toast", { hasText: "Saved workflows/sales-by-region.yaml" }).waitFor();
  const saved = parse(readFileSync(`${env.workspace}/workflows/sales-by-region.yaml`, "utf8"));
  assert.equal(saved.name, "Sales by region");

  // Returning to an unchanged document preserves its completed run.
  await page.getByRole("button", { name: "Workflows", exact: true }).click();
  await page.getByRole("button", { name: "Back to the open workflow" }).click();
  await sheet("By region").waitFor();
  assert.equal(await north.innerText(), "North");

  // CSV output keeps the table preview and its column headers.
  await page.getByRole("button", { name: "Workflows", exact: true }).click();
  await page.getByRole("button", { name: /Excel to CSV/ }).click();
  await page.locator(".pill", { hasText: /^Valid ·/ }).waitFor();
  await page.keyboard.press("Control+Enter");
  await page.locator(".sbar", { hasText: "Output Rows" }).waitFor();
  await page.locator(".grid tbody tr").first().waitFor();
  const headers = await page.locator(".grid thead .colh .ch1").allTextContents();
  assert.equal(headers.length, 3);
  for (const [index, heading] of ["Part Number", "Description", "Quantity"].entries()) {
    assert.ok(headers[index].includes(heading));
  }
  assert.match(await page.locator(".grid tbody").innerText(), /00123\s+Resistor\s+2/);
  assert.equal(await sheetTabs.count(), 0);

  // A hidden desktop navigator can still be opened from the compact tabs.
  await page.getByRole("button", { name: "Steps panel", exact: true }).press("Enter");
  assert.ok(!(await page.locator(".nav").isVisible()));
  await page.setViewportSize({ width: 390, height: 800 });
  await page.getByRole("button", { name: "Steps", exact: true }).press("Enter");
  assert.ok(await page.locator(".nav").isVisible());
  assert.ok(!(await page.locator(".stage").isVisible()));

  assert.deepEqual(failures, [], "no page errors");
  console.log("Studio smoke passed: browser guard, sample setup and repair, BOM step preview and styled workbook output, sheet selection and cell jumps, previous run refresh, CSV output, keyboard selection and copy, editing and undo, endpoint page, template run and save, and desktop and compact layouts.");
} finally {
  await browser?.close();
  await new Promise((resolve) => (server ? server.httpServer.close(resolve) : resolve()));
  rmSync(env.dir, { recursive: true, force: true });
}
