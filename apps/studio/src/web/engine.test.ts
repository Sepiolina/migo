/**
 * The browser host end to end: the real Go engine (cmd/mai-wasm, built by
 * `node scripts/build-web.mjs --engine`) on the in-memory workspace.
 * Skipped when the engine has not been built.
 */
import { existsSync, readFileSync } from "node:fs";
import { gunzipSync } from "node:zlib";
import { runInThisContext } from "node:vm";
import { beforeAll, describe, expect, it } from "vitest";
import { ROOT, WebHost } from "./bridge";
import { MemFS } from "./memfs";

const engineDir = new URL("../../.web-engine/", import.meta.url);
const built = existsSync(new URL("mai.wasm.gz", engineDir));
const repo = new URL("../../../../", import.meta.url);

type Reply<T = any> = { ok: boolean; result?: T; error?: { message: string } };

describe.skipIf(!built)("browser engine", () => {
  let host: WebHost;
  let fs: MemFS;
  const ask = async <T = any>(command: string, params: Record<string, unknown> = {}) =>
    (await host.invoke("runner_request", { request: { version: 1, id: command, command, params } })) as Reply<T>;

  beforeAll(async () => {
    fs = new MemFS();
    const g = globalThis as Record<string, unknown>;
    const saved = { fs: g.fs, process: g.process };
    g.fs = fs;
    g.process = { ...process, cwd: () => "/" };
    runInThisContext(readFileSync(new URL("wasm_exec.js", engineDir), "utf8"));
    const Go = g.Go as new () => { importObject: WebAssembly.Imports; env: Record<string, string>; run(i: WebAssembly.Instance): Promise<void> };
    const go = new Go();
    go.env = { TMPDIR: "/tmp" };
    const ready = new Promise<void>((resolve) => { globalThis.maiRunnerReady = resolve; });
    const { instance } = await WebAssembly.instantiate(gunzipSync(readFileSync(new URL("mai.wasm.gz", engineDir))), go.importObject);
    void go.run(instance);
    await ready;
    // Node's own process stays in place for vitest; Go read what it needs at start.
    g.process = saved.process;
    host = new WebHost(fs, Promise.resolve());
    for (const name of ["orders.xlsx", "orders-totals.yaml", "stock-lookup.yaml", "parts.xlsx", "regions.csv", "tests/stock-lookup.mocks.yaml", "tests/stock-lookup.test.yaml", "tests/stock-lookup.xlsx"])
      fs.writeFile(`${ROOT}/examples/${name}`, readFileSync(new URL(`examples/${name}`, repo)));
  }, 60_000);

  it("lists, validates, previews and runs a workflow with no folder", async () => {
    const yaml = readFileSync(new URL("examples/orders-totals.yaml", repo), "utf8");
    const imported = await host.importFiles([
      { name: "orders-totals.yaml", data: new TextEncoder().encode(yaml) },
      { name: "My Orders.xlsx", data: readFileSync(new URL("examples/orders.xlsx", repo)) },
    ]);
    expect(imported.workflows).toEqual(["workflows/orders-totals.yaml"]);
    expect(imported.files).toEqual([{ path: "samples/My Orders.xlsx", name: "My Orders.xlsx", copied: true }]);
    expect(await host.invoke("list_workflows")).toEqual({
      paths: ["examples/orders-totals.yaml", "examples/stock-lookup.yaml", "workflows/orders-totals.yaml"],
    });

    const validation = await ask("validate", { yaml });
    expect(validation.ok).toBe(true);
    expect(validation.result.inputs.map((i: { name: string }) => i.name)).toEqual(["orders"]);

    const inputs = { orders: "samples/My Orders.xlsx" };
    const trace = await ask("trace", { yaml, inputs, step: 0 });
    expect(trace.error).toBeUndefined();
    expect(trace.result.steps.length).toBeGreaterThan(1);
    expect(trace.result.steps[0].total).toBeGreaterThan(0);

    const run = await ask("run", { workflowPath: "workflows/orders-totals.yaml", yaml, inputs, omitPreview: true });
    expect(run.error).toBeUndefined();
    expect(run.result.path).toMatch(/^\.studio\/runs\/run-[^/]+\/output\.xlsx$/);
    expect(fs.readFile(`${ROOT}/${run.result.path}`)!.length).toBeGreaterThan(1000);

    const page = await ask("inspectWorkbookPage", { path: run.result.path, sheet: "Totals", limit: 10, columnLimit: 10 });
    expect(page.error).toBeUndefined();
    const cells = page.result.rows.flat();
    expect(cells).toContain("Acme");
    expect(cells).toContain("50");

    const runs = await ask("listRuns", { limit: 10 });
    expect(runs.result.runs).toHaveLength(1);
    expect(runs.result.runs[0].available).toBe(true);
  });

  it("answers sql and rest steps from a mocks file, and runs workflow tests", async () => {
    const yaml = readFileSync(new URL("examples/stock-lookup.yaml", repo), "utf8");
    const trace = await ask("trace", { yaml, inputs: { parts: "examples/parts.xlsx" }, mocks: "examples/tests/stock-lookup.mocks.yaml" });
    expect(trace.error).toBeUndefined();
    expect(trace.result.mocks).toBe(true);

    const tests = await ask("runTests", { workflowPath: "examples/stock-lookup.yaml", path: "examples/tests/stock-lookup.test.yaml" });
    expect(tests.error).toBeUndefined();
    const cases = tests.result.suites.flatMap((suite: { cases: { outcome: string }[] }) => suite.cases);
    expect(cases.map((c: { outcome: string }) => c.outcome)).toEqual(["passed", "passed"]);
  });

  it("saves workflows with revision checks and refuses paths outside the workspace", async () => {
    const saved = (await host.invoke("save_workflow", { path: "workflows/new.yaml", source: "name: x\n" })) as { revision: string };
    await expect(host.invoke("save_workflow", { path: "workflows/new.yaml", source: "name: y\n" })).rejects.toThrow("already exists");
    await expect(host.invoke("save_workflow", { path: "workflows/new.yaml", source: "name: y\n", expectedRevision: "0" })).rejects.toThrow("changed externally");
    await host.invoke("save_workflow", { path: "workflows/new.yaml", source: "name: y\n", expectedRevision: saved.revision });
    expect(((await host.invoke("read_workflow", { path: "workflows/new.yaml" })) as { source: string }).source).toBe("name: y\n");
    await expect(host.invoke("read_workflow", { path: "../etc/passwd.yaml" })).rejects.toThrow("relative");
    await expect(ask("trace", { yaml: "name: x", inputs: { a: "/etc/passwd" } })).rejects.toThrow("relative");
  });

  it("keeps distinct samples with the same name and reuses identical ones", async () => {
    const a = await host.importFile("data.csv", new TextEncoder().encode("a\n1\n"));
    const same = await host.importFile("data.csv", new TextEncoder().encode("a\n1\n"));
    const other = await host.importFile("data.csv", new TextEncoder().encode("a\n2\n"));
    expect([a.path, same.path, other.path]).toEqual(["samples/data.csv", "samples/data.csv", "samples/data-2.csv"]);
    expect((await host.importFile("notes.txt", new Uint8Array())).error).toMatch(/workflow/);
  });
});
