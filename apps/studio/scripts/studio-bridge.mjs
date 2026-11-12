// A stand-in for the Tauri host, for browser smoke tests and screenshots.
// It answers the same commands (choose_workspace, choose_sample_file, list_workflows,
// read_workflow, save_workflow, runner_request) against a real workspace
// folder and the real Go runner, through window.__maiBridge.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cpSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

export const repo = fileURLToPath(new URL("../../../", import.meta.url));

/** Build the runner and copy the example files into a throwaway workspace. */
export function prepare() {
  const dir = mkdtempSync(join(tmpdir(), "mai-studio-"));
  const runner = join(dir, process.platform === "win32" ? "runner.exe" : "runner");
  execFileSync(process.env.MAI_GO || "go", ["build", "-mod=readonly", "-o", runner, "./cmd/mai-runner"], { cwd: repo, stdio: "inherit" });
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

const revision = (text) => createHash("sha256").update(text).digest("hex").slice(0, 16);

/** Expose the bridge to a Playwright page; call before page.goto. */
export async function installBridge(page, { workspace, runner }) {
  await page.exposeFunction("__maiNative", (command, args = {}) => {
    switch (command) {
      case "choose_workspace":
        return { path: workspace };
      case "choose_sample_file":
        // The native picker cannot be driven here; answer with a workspace file.
        return { path: "testdata/prio_basic.xlsx", copied: false };
      case "list_workflows": {
        const dir = join(workspace, "workflows");
        return { paths: readdirSync(dir).filter((f) => /\.ya?ml$/.test(f)).map((f) => `workflows/${f}`).sort() };
      }
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
      case "runner_request": {
        const request = { ...args.request, params: { ...args.request.params, workspace } };
        const out = execFileSync(runner, [], { input: JSON.stringify(request) + "\n", encoding: "utf8", maxBuffer: 4 << 20, timeout: 60000 });
        return JSON.parse(out);
      }
    }
    throw new Error(`Unsupported command ${command}`);
  });
  await page.addInitScript(() => {
    window.__maiBridge = { invoke: (command, args) => window.__maiNative(command, args) };
  });
}
