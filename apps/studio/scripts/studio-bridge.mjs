// A stand-in for the Tauri host, for browser smoke tests and screenshots.
// It answers the same commands (choose_workspace, choose_sample_file, list_workflows,
// read_workflow, save_workflow, runner_request) against a real workspace
// folder and the real Go runner, through window.__maiBridge.
import { execFile, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cpSync, lstatSync, mkdirSync, mkdtempSync, realpathSync, readdirSync, readFileSync, rmSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

export const repo = fileURLToPath(new URL("../../../", import.meta.url));

/** Build the runner and copy the example files into a throwaway workspace. */
export function prepare({ runner: existingRunner } = {}) {
  const dir = mkdtempSync(join(tmpdir(), "mai-studio-"));
  const runner = existingRunner ?? join(dir, process.platform === "win32" ? "runner.exe" : "runner");
  if (!existingRunner) execFileSync(process.env.MAI_GO || "go", ["build", "-mod=readonly", "-o", runner, "./cmd/mai-runner"], { cwd: repo, stdio: "inherit" });
  const workspace = join(dir, "project");
  for (const sub of ["workflows", "examples", "testdata"]) cpSync(join(repo, sub), join(workspace, sub), { recursive: true, filter: (p) => !p.endsWith(".go") });
  // MAI_CRLF=1 checks the files as Git on Windows checks them out.
  if (process.env.MAI_CRLF)
    for (const f of readdirSync(join(workspace, "workflows"))) {
      const file = join(workspace, "workflows", f);
      writeFileSync(file, readFileSync(file, "utf8").replace(/\r?\n/g, "\r\n"));
    }
  return { dir, runner, workspace };
}

const revision = (text) => createHash("sha256").update(text).digest("hex");

const runnerOptions = { encoding: "utf8", maxBuffer: 4 << 20, timeout: 60000 };
const isWorkflowTest = (command) => command === "discoverTests" || command === "runTests";

function asyncRunner(runner, request, record, signal) {
  const start = performance.now();
  return new Promise((resolve, reject) => {
    let peakRssKiB = null;
    const options = { ...runnerOptions, killSignal: "SIGKILL" };
    if (isWorkflowTest(request.command)) {
      options.timeout = 35000;
      options.maxBuffer = 1 << 20;
    }
    const scratch = request.command === "runTests" ? mkdtempSync(join(tmpdir(), "mai-studio-tests-")) : undefined;
    if (scratch) options.env = { ...process.env, TMPDIR: scratch, TMP: scratch, TEMP: scratch };
    const child = execFile(runner, [], options, (error, stdout) => {
      clearInterval(sampler);
      signal?.removeEventListener("abort", abort);
      try {
        if (scratch) rmSync(scratch, { recursive: true, force: true });
      } catch (cleanupError) {
        reject(cleanupError);
        return;
      }
      record?.({ command: request.command, step: request.params.step, ms: performance.now() - start, peakRssKiB, responseBytes: Buffer.byteLength(stdout) });
      if (error) {
        reject(error);
      } else {
        try { resolve(JSON.parse(stdout)); }
        catch (parseError) { reject(parseError); }
      }
    });
    const sampler = record && process.platform === "linux" ? setInterval(() => {
      try {
        const status = readFileSync(`/proc/${child.pid}/status`, "utf8");
        const rss = Number(status.match(/^VmHWM:\s+(\d+)/m)?.[1]);
        if (rss) peakRssKiB = Math.max(peakRssKiB ?? 0, rss);
      } catch {
        // A short-lived process can exit before the next sample.
      }
    }, 5) : undefined;
    const abort = () => child.kill("SIGKILL");
    signal?.addEventListener("abort", abort, { once: true });
    child.stdin.on("error", () => child.kill("SIGKILL"));
    child.stdin.end(JSON.stringify(request) + "\n");
  });
}

function listWorkflows(workspace) {
  const paths = [];
  let remaining = 10000;
  function visit(dir, depth, examples) {
    for (const entry of readdirSync(join(workspace, dir), { withFileTypes: true })) {
      if (remaining-- === 0) throw new Error("Workflow discovery exceeds the entry limit");
      const path = `${dir}/${entry.name}`;
      if (entry.isDirectory() && !examples) {
        if (depth >= 32) throw new Error("Workflow discovery exceeds the directory depth limit");
        visit(path, depth + 1, examples);
      } else if (entry.isFile() && /\.ya?ml$/.test(entry.name) && (!examples || !/\.test\.ya?ml$/.test(entry.name))) {
        paths.push(path);
      }
    }
  }
  for (const dir of ["workflows", "examples"]) {
    let metadata;
    try {
      metadata = lstatSync(join(workspace, dir));
    } catch (error) {
      if (error.code === "ENOENT") continue;
      throw error;
    }
    const examples = dir === "examples";
    if (metadata.isDirectory()) visit(dir, 0, examples);
    else if (!examples) throw new Error("Workflows directory must be a directory inside the workspace");
  }
  return { paths: paths.sort() };
}

/** Expose the bridge to a Playwright page; call before page.goto. */
export async function installBridge(page, { workspace, runner }, chooseSample = () => ({ path: "testdata/prio_basic.xlsx", copied: false }), record) {
  const testRuns = new Map();
  await page.exposeFunction("__maiNative", (command, args = {}) => {
    switch (command) {
      case "choose_workspace":
        return { path: workspace };
      case "choose_sample_file":
        // The native picker cannot be driven here; answer with a workspace file.
        return chooseSample();
      case "file_revisions": {
        const start = performance.now();
        const revisions = Object.fromEntries(args.paths.map((path) => {
          try {
            const file = realpathSync(join(workspace, path));
            const rel = relative(realpathSync(workspace), file);
            if (isAbsolute(path) || isAbsolute(rel) || rel === ".." || rel.startsWith(`..${sep}`)) return [path, null];
            return [path, revision(readFileSync(file))];
          } catch {
            return [path, null];
          }
        }));
        record?.({ command, ms: performance.now() - start, fileCount: args.paths.length });
        return revisions;
      }
      case "list_workflows":
        return listWorkflows(workspace);
      case "read_workflow": {
        const source = readFileSync(join(workspace, args.path), "utf8");
        return { path: args.path, source, revision: revision(source) };
      }
      case "save_workflow": {
        const file = join(workspace, args.path);
        if (relative(workspace, file).startsWith("..")) throw new Error("Outside the workspace");
        if (!existsSync(dirname(file))) mkdirSync(dirname(file), { recursive: true });
        if (existsSync(file) && args.expectedRevision !== revision(readFileSync(file, "utf8"))) {
          if (args.expectedRevision === null) throw new Error("A file already exists at this path.");
          throw new Error("The file changed on disk.");
        }
        writeFileSync(file, args.source);
        return { path: args.path, source: args.source, revision: revision(args.source) };
      }
      case "runner_prepare_tests": {
        const { id } = args;
        if (typeof id !== "string" || !id.length || id.length > 64) throw new Error("Invalid test run ID");
        const now = Date.now();
        for (const [key, run] of testRuns) {
          if (!run.started && run.expires <= now) testRuns.delete(key);
        }
        if (testRuns.size >= 64 || testRuns.has(id)) throw new Error("Cannot reserve this test run");
        testRuns.set(id, { expires: now + 30000, started: false, controller: new AbortController() });
        return;
      }
      case "runner_cancel_tests": {
        const run = testRuns.get(args.id);
        if (run?.started) run.controller.abort();
        else testRuns.delete(args.id);
        return;
      }
      case "runner_request": {
        const request = { ...args.request, params: { ...args.request.params, workspace } };
        if (args.cancellable) {
          if (request.command !== "runTests") throw new Error("Cancellation is only available for test runs");
          const run = testRuns.get(request.id);
          if (!run || run.started || run.expires <= Date.now()) throw new Error("Test run cancelled");
          run.started = true;
          return asyncRunner(runner, request, record, run.controller.signal)
            .finally(() => testRuns.delete(request.id));
        }
        if (record || isWorkflowTest(request.command)) return asyncRunner(runner, request, record);
        const out = execFileSync(runner, [], { ...runnerOptions, input: JSON.stringify(request) + "\n" });
        return JSON.parse(out);
      }
    }
    throw new Error(`Unsupported command ${command}`);
  });
  await page.addInitScript(() => {
    window.__maiBridge = { invoke: (command, args) => window.__maiNative(command, args) };
  });
}
