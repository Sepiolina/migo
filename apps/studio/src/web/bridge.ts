/**
 * The browser host for Mai Studio: it answers the commands the Tauri desktop
 * host answers (see api.ts, window.__maiBridge), with no folder on the
 * computer and no network.
 *
 * - The workspace is an in-memory file system (memfs.ts) with the usual
 *   layout: workflows/, samples/, tests/, examples/ and .studio/runs/.
 *   People never see that layout: they open or drop a workflow and its
 *   sample files, and the bridge files each one in the right place.
 * - The production Go engine runs as WebAssembly (cmd/mai-wasm) on that file
 *   system, so previews, runs, tests and validation are the real thing. SQL
 *   and REST steps are offline, answered by a mocks file when there is one.
 * - Workflows, samples and tests are kept in the browser (IndexedDB) between
 *   visits; outputs and workflows can be downloaded.
 */
import { MemFS } from "./memfs";

export const ROOT = "/workspace";
const SPREADSHEETS = /\.(xlsx|xls|csv)$/i;
const WORKFLOWS = /\.(ya?ml)$/i;
const TESTS = /\.(test|mocks)\.ya?ml$/i;
const KEPT = ["workflows", "samples", "tests"];
const MAX_IMPORT_BYTES = 25 << 20;
const RUNNER_COMMANDS = new Set(["capabilities", "validate", "inspectWorkbook", "inspectWorkbookPage", "run", "trace", "listRuns", "discoverTests", "runTests", "saveTest"]);

export type ImportedFile = { path?: string; name: string; copied?: boolean; error?: string };
export type Imported = { files: ImportedFile[]; workflows: string[]; tests: string[] };
type Listener = (payload: unknown) => void;

declare global {
  // eslint-disable-next-line no-var
  var maiRunner: ((request: string) => Promise<string>) | undefined;
  // eslint-disable-next-line no-var
  var maiRunnerReady: (() => void) | undefined;
}

const hex = (bytes: ArrayBuffer) => Array.from(new Uint8Array(bytes), (b) => b.toString(16).padStart(2, "0")).join("");
async function sha256(data: Uint8Array | string) {
  const bytes = typeof data === "string" ? new TextEncoder().encode(data) : data;
  return hex(await crypto.subtle.digest("SHA-256", bytes as BufferSource));
}
const join = (path: string) => `${ROOT}/${path}`;

/** A workspace-relative path without traversal, or an error message. */
function checkRelative(path: unknown, what = "File paths"): string {
  if (typeof path !== "string" || !path) throw new Error(`${what} must be strings`);
  const names = path.replace(/\\/g, "/").split("/");
  if (path.startsWith("/") || /^[A-Za-z]:/.test(path) || names.some((n) => n === ".." || n === "." || n === ""))
    throw new Error("Use paths relative to the workspace");
  return names.join("/");
}
function workflowPath(path: unknown) {
  const relative = checkRelative(path, "Workflow paths");
  if (!WORKFLOWS.test(relative)) throw new Error("Workflow path must end in .yaml or .yml");
  return relative;
}

/** A file name safe to keep: no folders, no control characters. */
export function safeName(name: string) {
  const base = name.replace(/\\/g, "/").split("/").pop() ?? "";
  // eslint-disable-next-line no-control-regex
  const clean = base.replace(/[\x00-\x1f<>:"|?*]/g, "_").replace(/^\.+/, "").trim();
  return clean || "file";
}

/** Where an imported file lives in the workspace, by its name. */
export function placeFor(name: string): string | undefined {
  if (TESTS.test(name)) return "tests";
  if (WORKFLOWS.test(name)) return "workflows";
  if (SPREADSHEETS.test(name)) return "samples";
  return undefined;
}

export class WebHost {
  readonly fs: MemFS;
  private listeners = new Map<string, Set<Listener>>();
  private engine: Promise<void>;
  private store?: Store;
  private saveTimer?: ReturnType<typeof setTimeout>;

  constructor(fs: MemFS, engine: Promise<void>, store?: Store) {
    this.fs = fs;
    this.engine = engine;
    this.store = store;
    fs.mkdirp(join("workflows"));
    fs.onChange = (path) => {
      if (KEPT.some((dir) => path.startsWith(join(dir) + "/"))) this.scheduleSave();
    };
  }

  // ---- events -----------------------------------------------------------------------------
  listen(event: string, handler: Listener) {
    const set = this.listeners.get(event) ?? new Set();
    set.add(handler);
    this.listeners.set(event, set);
    return () => set.delete(handler);
  }
  emit(event: string, payload: unknown) {
    for (const handler of this.listeners.get(event) ?? []) handler(payload);
  }

  // ---- files --------------------------------------------------------------------------------
  /** Files a picked or dropped file in the workspace; same content keeps one copy. */
  async importFile(name: string, data: Uint8Array): Promise<ImportedFile & { place?: string }> {
    const clean = safeName(name);
    const place = placeFor(clean);
    if (!place) return { name, error: "Use a workflow (.yaml), a spreadsheet (.xlsx, .xls, .csv) or a test file" };
    if (data.length > MAX_IMPORT_BYTES) return { name, error: "Files can be at most 25 MiB" };
    const dot = clean.search(/(\.(test|mocks))?\.[^.]+$/);
    const stem = dot > 0 ? clean.slice(0, dot) : clean;
    const ext = dot > 0 ? clean.slice(dot) : "";
    const digest = await sha256(data);
    for (let n = 1; ; n++) {
      const candidate = `${place}/${n === 1 ? clean : `${stem}-${n}${ext}`}`;
      const existing = this.fs.readFile(join(candidate));
      // A workflow or test of the same name is replaced: people re-open a newer copy.
      if (!existing || place !== "samples" || (await sha256(existing)) === digest) {
        this.fs.writeFile(join(candidate), data);
        return { path: candidate, name, copied: true, place };
      }
    }
  }
  async importFiles(files: { name: string; data: Uint8Array }[]): Promise<Imported> {
    const out: Imported = { files: [], workflows: [], tests: [] };
    for (const file of files) {
      const result = await this.importFile(file.name, file.data);
      const { place, ...entry } = result;
      if (place === "workflows") out.workflows.push(entry.path!);
      else if (place === "tests") out.tests.push(entry.path!);
      else out.files.push(entry);
    }
    return out;
  }

  // ---- the host commands --------------------------------------------------------------------
  async invoke(command: string, args: Record<string, unknown> = {}): Promise<unknown> {
    switch (command) {
      case "choose_workspace":
        return { path: ROOT };
      case "list_workflows":
        return { paths: this.listWorkflows() };
      case "read_workflow":
        return this.readWorkflow(workflowPath(args.path));
      case "save_workflow":
        return this.saveWorkflow(workflowPath(args.path), String(args.source ?? ""), (args.expectedRevision as string | null | undefined) ?? null);
      case "file_revisions": {
        const out: Record<string, string | null> = {};
        for (const path of (args.paths as string[]) ?? []) {
          let data: Uint8Array | undefined;
          try {
            data = this.fs.readFile(join(checkRelative(path)));
          } catch {
            data = undefined;
          }
          out[path] = data ? await sha256(data) : null;
        }
        return out;
      }
      case "choose_sample_file": {
        const picked = await pickFiles(".xlsx,.xls,.csv", false);
        if (!picked.length) return null;
        const [file] = (await this.importFiles(picked)).files;
        if (file.error) throw new Error(file.error);
        return { path: file.path, copied: true };
      }
      case "choose_sample_files": {
        const picked = await pickFiles(".xlsx,.xls,.csv", !!args.multiple);
        return { files: (await this.importFiles(picked)).files };
      }
      case "import_files":
        return this.importFiles(await pickFiles(".yaml,.yml,.xlsx,.xls,.csv", true));
      case "download_file": {
        const path = checkRelative(args.path);
        const data = this.fs.readFile(join(path));
        if (!data) throw new Error(`${path} is no longer in the workspace`);
        download(String(args.name || path.split("/").pop()), data);
        return true;
      }
      case "download_text":
        download(String(args.name || "workflow.yaml"), new TextEncoder().encode(String(args.text ?? "")));
        return true;
      case "save_diagram_svg":
        download("workflow-diagram.svg", new TextEncoder().encode(String(args.svg ?? "")));
        return true;
      case "clear_workspace":
        for (const dir of [...KEPT, ".studio"]) this.fs.remove(join(dir));
        this.fs.mkdirp(join("workflows"));
        await this.store?.clear();
        return true;
      case "runner_prepare_tests":
      case "runner_cancel_tests":
        return null;
      case "runner_request":
        return this.runner(args.request as Record<string, unknown>);
      case "get_signing_identity":
      case "create_signing_identity":
      case "build_linux_package":
        throw new Error("Building a Linux package needs the Mai Studio desktop app. This browser version checks and runs workflows only.");
      default:
        throw new Error(`The browser version of Studio does not support ${command}`);
    }
  }

  private listWorkflows() {
    const own = this.fs.walk(join("workflows")).filter((p) => WORKFLOWS.test(p)).map((p) => `workflows/${p}`);
    const examples = this.fs.list(join("examples"))
      .filter((p) => WORKFLOWS.test(p) && !TESTS.test(p) && !this.fs.isDir(join(`examples/${p}`)))
      .map((p) => `examples/${p}`);
    return [...own, ...examples].sort();
  }
  private async readWorkflow(path: string) {
    const data = this.fs.readFile(join(path));
    if (!data) throw new Error("Cannot access workflow path");
    if (data.length > 1 << 20) throw new Error("Workflow exceeds 1 MiB");
    let source: string;
    try {
      source = new TextDecoder("utf-8", { fatal: true }).decode(data);
    } catch {
      throw new Error("Workflow must be UTF-8");
    }
    return { path, source, revision: await sha256(data) };
  }
  private async saveWorkflow(path: string, source: string, expectedRevision: string | null) {
    const current = this.fs.readFile(join(path));
    if (expectedRevision != null) {
      if (!current) throw new Error("Workflow conflict: file changed or was removed");
      if ((await sha256(current)) !== expectedRevision) throw new Error("Workflow conflict: file changed externally");
    } else if (current) {
      throw new Error("Workflow conflict: file already exists");
    }
    const bytes = new TextEncoder().encode(source);
    if (bytes.length > 1 << 20) throw new Error("Workflow exceeds 1 MiB");
    this.fs.writeFile(join(path), bytes);
    return { path, source, revision: await sha256(bytes) };
  }
  private async runner(request: Record<string, unknown>) {
    if (!request || request.version !== 1) throw new Error("Unsupported runner protocol version");
    const command = String(request.command ?? "");
    if (!RUNNER_COMMANDS.has(command)) throw new Error("Unsupported runner command");
    const params = { ...((request.params as Record<string, unknown>) ?? {}) };
    if (command !== "capabilities") {
      params.workspace = ROOT;
      for (const key of ["path", "workflowPath", "mocks"])
        if (typeof params[key] === "string" && params[key]) params[key] = checkRelative(params[key]);
      const inputs = (params.inputs ?? {}) as Record<string, string>;
      for (const value of Object.values(inputs)) checkRelative(value);
    }
    await this.engine;
    const reply = await globalThis.maiRunner!(JSON.stringify({ ...request, params }));
    return JSON.parse(reply);
  }

  // ---- persistence --------------------------------------------------------------------------
  private scheduleSave() {
    if (!this.store) return;
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => void this.persist(), 400);
  }
  private async persist() {
    const files: Record<string, Uint8Array> = {};
    for (const dir of KEPT) for (const p of this.fs.walk(join(dir))) files[`${dir}/${p}`] = this.fs.readFile(join(`${dir}/${p}`))!;
    try {
      await this.store?.save(files);
    } catch (error) {
      console.warn("Could not keep the workspace in this browser", error);
    }
  }
  /** Restores the files kept from an earlier visit. */
  async restore() {
    try {
      const files = await this.store?.load();
      for (const [path, data] of Object.entries(files ?? {})) {
        if (KEPT.includes(path.split("/")[0])) this.fs.writeFile(join(checkRelative(path)), data);
      }
    } catch (error) {
      console.warn("Could not restore the workspace kept in this browser", error);
    }
  }
}

// ---- browser helpers ---------------------------------------------------------------------------

export async function readDropped(list: FileList | File[]) {
  return Promise.all([...list].map(async (file) => ({ name: file.name, data: new Uint8Array(await file.arrayBuffer()) })));
}

/** Opens the browser's file picker; resolves to [] when cancelled. */
function pickFiles(accept: string, multiple: boolean): Promise<{ name: string; data: Uint8Array }[]> {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = accept;
    input.multiple = multiple;
    input.style.display = "none";
    let settled = false;
    const done = (files: { name: string; data: Uint8Array }[]) => {
      if (settled) return;
      settled = true;
      input.remove();
      resolve(files);
    };
    input.addEventListener("change", () => void readDropped(input.files ?? []).then(done, () => done([])));
    input.addEventListener("cancel", () => done([]));
    document.body.append(input);
    input.click();
  });
}

const MIME: Record<string, string> = {
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  csv: "text/csv",
  yaml: "application/yaml",
  yml: "application/yaml",
  svg: "image/svg+xml",
};
function download(name: string, data: Uint8Array) {
  const type = MIME[name.split(".").pop()?.toLowerCase() ?? ""] ?? "application/octet-stream";
  const url = URL.createObjectURL(new Blob([data as BlobPart], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

// ---- IndexedDB ------------------------------------------------------------------------------------

export type Store = {
  load(): Promise<Record<string, Uint8Array>>;
  save(files: Record<string, Uint8Array>): Promise<void>;
  clear(): Promise<void>;
};

/** Keeps the workspace files in one IndexedDB record; undefined when unavailable. */
export function indexedDbStore(name = "mai-studio-web"): Store | undefined {
  if (typeof indexedDB === "undefined") return undefined;
  const open = () => new Promise<IDBDatabase>((resolve, reject) => {
    const req = indexedDB.open(name, 1);
    req.onupgradeneeded = () => req.result.createObjectStore("workspace");
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
  const run = async <T>(mode: IDBTransactionMode, work: (s: IDBObjectStore) => IDBRequest<T>) => {
    const db = await open();
    try {
      return await new Promise<T>((resolve, reject) => {
        const req = work(db.transaction("workspace", mode).objectStore("workspace"));
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });
    } finally {
      db.close();
    }
  };
  return {
    load: async () => ((await run("readonly", (s) => s.get("files"))) as Record<string, Uint8Array> | undefined) ?? {},
    save: async (files) => void (await run("readwrite", (s) => s.put(files, "files"))),
    clear: async () => void (await run("readwrite", (s) => s.delete("files"))),
  };
}
