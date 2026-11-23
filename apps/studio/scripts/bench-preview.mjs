// Build Studio first, then run: node scripts/bench-preview.mjs
// Compare a frozen build using --dist PATH --runner PATH --label baseline.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { cpSync, existsSync, readFileSync, rmSync, statSync } from "node:fs";
import { arch, availableParallelism, cpus, platform, release, totalmem } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { chromium } from "playwright-core";
import { preview } from "vite";
import { installBridge, prepare, repo } from "./studio-bridge.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const { values } = parseArgs({ options: {
  dist: { type: "string", default: join(root, "dist") },
  runner: { type: "string" },
  iterations: { type: "string", default: "5" },
  rows: { type: "string", default: "100000" },
  label: { type: "string", default: "current" },
  "check-budgets": { type: "boolean" },
} });
const iterations = Number(values.iterations);
const orderRows = Number(values.rows);
assert.ok(Number.isSafeInteger(iterations) && iterations > 0 && iterations <= 100, "--iterations must be between 1 and 100.");
assert.ok(Number.isSafeInteger(orderRows) && orderRows >= 200 && orderRows <= 1_048_575, "--rows must be between 200 and 1,048,575.");
if (values["check-budgets"]) {
  assert.ok(iterations >= 5 && orderRows === 100000, "Budget checks need at least five iterations and the 100,000-row Orders fixture.");
}
const dist = resolve(values.dist);
assert.ok(existsSync(join(dist, "index.html")), `Build Studio or pass a built --dist directory. Missing ${join(dist, "index.html")}.`);

const executablePath = [
  process.env.MAI_CHROME,
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
  "/usr/bin/google-chrome",
  "/opt/google/chrome/chrome",
  process.env.PROGRAMFILES && join(process.env.PROGRAMFILES, "Google", "Chrome", "Application", "chrome.exe"),
  process.env["PROGRAMFILES(X86)"] && join(process.env["PROGRAMFILES(X86)"], "Google", "Chrome", "Application", "chrome.exe"),
].filter(Boolean).find(existsSync);
assert.ok(executablePath, "No system Chrome/Chromium found. Set MAI_CHROME to its executable.");

const fixtures = [
  { name: "BOM", workflow: "workflows/bom.yaml", bindings: { bom: "testdata/bom_basic.xlsx", priority: "testdata/prio_basic.xlsx" }, unseenStep: 15, distantRow: 5 },
  { name: "Orders", workflow: "workflows/orders-totals.yaml", bindings: { orders: "examples/orders-large.xlsx" }, unseenStep: 3, distantRow: Math.floor(orderRows * 0.9) },
];

const summarize = (items) => {
  if (!items.length) return null;
  const sorted = [...items].sort((a, b) => a - b);
  const percentile = (p) => +sorted[Math.ceil(p * sorted.length) - 1].toFixed(2);
  return { p50: percentile(0.5), p95: percentile(0.95), max: +sorted.at(-1).toFixed(2) };
};

function summarizeTrials(trials) {
  const events = trials.flatMap((trial) => trial.events);
  const runner = events.filter(({ command }) => command !== "file_revisions");
  const revisions = events.filter(({ command }) => command === "file_revisions");
  const counts = trials.map(({ events: trialEvents }) => trialEvents.filter(({ command }) => command !== "file_revisions").length);
  const commands = Object.fromEntries([...new Set(runner.map(({ command }) => command))].map((command) => {
    const matches = runner.filter((event) => event.command === command);
    return [command, {
      calls: matches.length,
      durationMs: summarize(matches.map(({ ms }) => ms)),
      responseBytes: summarize(matches.map(({ responseBytes }) => responseBytes)),
    }];
  }));
  return {
    trials: trials.length,
    endToEndMs: summarize(trials.map(({ elapsed }) => elapsed)),
    layoutAndStyleMs: summarize(trials.map((trial) => trial.layoutAndStyleMs).filter(Number.isFinite)),
    runnerCallsPerTrial: summarize(counts),
    runnerCommands: commands,
    runnerPeakRssKiB: summarize(runner.flatMap(({ peakRssKiB }) => peakRssKiB === null ? [] : [peakRssKiB])),
    fileRevisionCalls: revisions.length,
    fileRevisionMs: summarize(revisions.map(({ ms }) => ms)),
  };
}

async function paintedStep(page, step) {
  await page.waitForFunction((number) => {
    const stage = document.querySelector(".stage");
    return stage?.querySelector(".sbar b")?.textContent.trimStart().startsWith(`${number}. `)
      && !/Updating/.test(stage.querySelector(".sbar")?.textContent ?? "")
      && stage.querySelector(".grid tbody td")
      && document.querySelector(".toolbar .pill")?.textContent.startsWith("Valid");
  }, step);
  return page.evaluate(() => new Promise((done) => requestAnimationFrame(() => done({
    elapsed: performance.now() - window.__benchStarted,
    previewRows: document.querySelectorAll(".grid tbody tr").length,
    previewColumns: document.querySelector(".grid tbody tr").children.length - 1,
  }))));
}

async function layoutAndStyleMs(session) {
  const { metrics } = await session.send("Performance.getMetrics");
  return 1000 * metrics.filter(({ name }) => name === "LayoutDuration" || name === "RecalcStyleDuration")
    .reduce((total, { value }) => total + value, 0);
}

async function stepAction(page, session, target, step, events) {
  const initialLayout = await layoutAndStyleMs(session);
  const before = events.length;
  await target.evaluate((node) => {
    window.__benchStarted = performance.now();
    node.click();
  });
  const paint = await paintedStep(page, step);
  return { ...paint, events: events.slice(before), layoutAndStyleMs: await layoutAndStyleMs(session) - initialLayout };
}

async function inspectWindow(page, fixture, events) {
  const offset = fixture.distantRow - 1;
  const before = events.length;
  const response = await page.evaluate(async (params) => {
    const start = performance.now();
    const reply = await window.__maiBridge.invoke("runner_request", {
      request: { version: 1, id: crypto.randomUUID(), command: "inspectWorkbook", params },
    });
    return { elapsed: performance.now() - start, reply };
  }, { path: Object.values(fixture.bindings)[0], offset, limit: 200 });
  assert.ok(response.reply.ok, response.reply.error?.message);
  assert.equal(response.reply.result.offset, offset);
  assert.ok(response.reply.result.rows.length > 0, "The distant worksheet sample must return rows.");
  return {
    elapsed: response.elapsed,
    events: events.slice(before),
    sourceRowsIncludingHeader: response.reply.result.totalRows,
    sourceWindowColumns: Math.max(...response.reply.result.rows.map((row) => row.length)),
  };
}

function assertBudgets(fixtures) {
  for (const fixture of fixtures) {
    const warmLimit = fixture.name === "BOM" ? 250 : 400;
    for (const name of ["revisitFirst", "revisitUnseen"]) {
      const scenario = fixture.scenarios[name];
      assert.equal(scenario.runnerCallsPerTrial.max, 0, `${fixture.name} ${name} must not invoke the runner.`);
      assert.ok(scenario.endToEndMs.p95 <= warmLimit, `${fixture.name} ${name} p95 must be at most ${warmLimit}ms.`);
    }
  }
  const window = fixtures.find(({ name }) => name === "Orders").scenarios.distantWindowBridge;
  assert.ok(window.endToEndMs.p95 <= 2800, "Orders distant-window p95 must be at most 2800ms.");
  if (process.platform === "linux") {
    assert.ok(window.runnerPeakRssKiB && window.runnerPeakRssKiB.max <= 200 * 1024, "Orders distant-window runner peak RSS must be at most 200 MiB.");
  }
}

const env = prepare({ runner: values.runner && resolve(values.runner) });
let server, browser;
try {
  cpSync(join(repo, "examples/orders-totals.yaml"), join(env.workspace, fixtures[1].workflow));
  execFileSync(process.env.MAI_PYTHON || "python3", [
    join(repo, "scripts/generate-orders-fixture.py"), "--rows", String(orderRows), join(env.workspace, fixtures[1].bindings.orders),
  ], { stdio: "inherit" });
  const csp = JSON.parse(readFileSync(join(root, "src-tauri/tauri.conf.json"), "utf8")).app.security.csp;
  server = await preview({ root, build: { outDir: dist }, preview: { host: "127.0.0.1", port: 0, headers: { "Content-Security-Policy": csp } } });
  const url = `http://127.0.0.1:${server.httpServer.address().port}/`;
  browser = await chromium.launch({ executablePath, headless: true, args: ["--no-sandbox", "--no-proxy-server"] });
  const fixtureResults = [];
  for (const fixture of fixtures) {
    const results = { coldOpen: [], unseenStep: [], revisitFirst: [], revisitUnseen: [], distantWindowBridge: [] };
    for (let i = 0; i < iterations; i++) {
      const page = await browser.newPage({ viewport: { width: 1360, height: 860 } });
      const events = [];
      try {
        const session = await page.context().newCDPSession(page);
        await session.send("Performance.enable");
        await installBridge(page, env, undefined, (event) => events.push(event));
        await page.addInitScript(({ key, bindings }) => localStorage.setItem(key, JSON.stringify(bindings)), {
          key: `mai.bindings:${env.workspace}:${fixture.workflow}`,
          bindings: fixture.bindings,
        });
        await page.goto(url);
        await page.getByRole("button", { name: "Open folder…" }).click();
        const workflow = page.locator(".wf", { hasText: fixture.workflow });
        results.coldOpen.push(await stepAction(page, session, workflow, 1, events));
        const step = (number) => page.locator(".nav .row", { has: page.locator(".num", { hasText: new RegExp(`^${number}$`) }) });
        results.unseenStep.push(await stepAction(page, session, step(fixture.unseenStep), fixture.unseenStep, events));
        results.revisitFirst.push(await stepAction(page, session, step(1), 1, events));
        results.revisitUnseen.push(await stepAction(page, session, step(fixture.unseenStep), fixture.unseenStep, events));
        // The baseline only exposed the first sheet page in the UI. Issue the
        // same bridge request in both builds to isolate distant-window reading.
        results.distantWindowBridge.push(await inspectWindow(page, fixture, events));
      } finally {
        await page.context().close();
      }
    }
    fixtureResults.push({
      name: fixture.name,
      files: Object.values(fixture.bindings).map((path) => ({ path, bytes: statSync(join(env.workspace, path)).size })),
      firstPreview: { rows: results.coldOpen[0].previewRows, columns: results.coldOpen[0].previewColumns },
      unseenStep: fixture.unseenStep,
      distantRow: fixture.distantRow,
      sourceRowsIncludingHeader: results.distantWindowBridge[0].sourceRowsIncludingHeader,
      sourceWindowColumns: results.distantWindowBridge[0].sourceWindowColumns,
      scenarios: Object.fromEntries(Object.entries(results).map(([name, trials]) => [name, summarizeTrials(trials)])),
    });
  }
  console.log(JSON.stringify({
    label: values.label,
    machine: { platform: platform(), release: release(), arch: arch(), cpu: cpus()[0]?.model, availableParallelism: availableParallelism(), memoryMiB: Math.round(totalmem() / (1024 ** 2)), chromium: browser.version() },
    methodology: "Fresh browser context per iteration; cold timing starts at opening an already-bound workflow from Home. Step timings end after a current grid is rendered. Layout and style timings use Chromium Performance counters for the page during each UI action. Distant window timing is a browser bridge round trip without grid rendering. Runner duration includes process launch; Linux per-runner peak RSS is sampled every 5 ms. No OS page-cache flush.",
    fixtures: fixtureResults,
  }, null, 2));
  if (values["check-budgets"]) assertBudgets(fixtureResults);
} finally {
  await browser?.close();
  await server?.close();
  rmSync(env.dir, { recursive: true, force: true });
}
