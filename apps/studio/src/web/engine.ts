/**
 * Starts the Go workflow engine (cmd/mai-wasm) in the page and installs the
 * browser host as window.__maiBridge, so the rest of Studio works as it does
 * in the desktop app.
 *
 * Assets come from the page itself when Studio is the single-file build
 * (script elements written by scripts/build-web.mjs), else from files next
 * to the page (vite dev with --mode web):
 *   #mai-engine    mai.wasm, gzip and base64
 *   #mai-examples  examples/ as {path: base64}
 *   wasm_exec.js   Go's loader, a classic script that defines globalThis.Go
 */
import { indexedDbStore, readDropped, ROOT, WebHost } from "./bridge";
import { MemFS } from "./memfs";

type GoRuntime = { importObject: WebAssembly.Imports; env: Record<string, string>; argv: string[]; run(instance: WebAssembly.Instance): Promise<void> };
declare global {
  // eslint-disable-next-line no-var
  var Go: (new () => GoRuntime) | undefined;
}

const fromBase64 = (text: string) => Uint8Array.from(atob(text.replace(/\s+/g, "")), (c) => c.charCodeAt(0));
const inline = (id: string) => document.getElementById(id)?.textContent?.trim() || undefined;

async function gunzip(bytes: Uint8Array) {
  const stream = new Blob([bytes as BlobPart]).stream().pipeThrough(new DecompressionStream("gzip"));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}
async function fetchBytes(url: string) {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`Cannot load ${url}`);
  return new Uint8Array(await response.arrayBuffer());
}
function loadScript(url: string) {
  return new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = url;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`Cannot load ${url}`));
    document.head.append(script);
  });
}

function installGlobals(fs: MemFS) {
  const enosys = () => Object.assign(new Error("not implemented"), { code: "ENOSYS" });
  Object.assign(globalThis, {
    fs,
    process: {
      getuid: () => -1, getgid: () => -1, geteuid: () => -1, getegid: () => -1,
      getgroups: () => { throw enosys(); },
      pid: -1, ppid: -1,
      umask: () => 0o022,
      cwd: () => "/",
      chdir: () => { throw enosys(); },
    },
  });
}

async function startEngine() {
  if (!globalThis.Go) await loadScript("./wasm_exec.js");
  const packed = inline("mai-engine");
  const wasm = packed ? await gunzip(fromBase64(packed)) : await gunzip(await fetchBytes("./mai.wasm.gz"));
  const go = new globalThis.Go!();
  go.env = { TMPDIR: "/tmp", HOME: "/tmp" };
  const ready = new Promise<void>((resolve) => { globalThis.maiRunnerReady = resolve; });
  const { instance } = await WebAssembly.instantiate(wasm as BufferSource, go.importObject);
  void go.run(instance).then(() => console.error("The workflow engine exited"));
  await ready;
}

async function loadExamples(fs: MemFS) {
  let files: Record<string, string> = {};
  const packed = inline("mai-examples");
  try {
    files = packed ? JSON.parse(packed) : await (await fetch("./examples.json")).json();
  } catch {
    return;
  }
  for (const [path, data] of Object.entries(files)) fs.writeFile(`${ROOT}/${path}`, fromBase64(data));
}

/** Files dragged onto the window: workflows open, spreadsheets bind to sources. */
function acceptDrops(host: WebHost) {
  let depth = 0;
  const hasFiles = (e: DragEvent) => [...(e.dataTransfer?.types ?? [])].includes("Files");
  window.addEventListener("dragenter", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    if (depth++ === 0) host.emit("samples-drag", true);
  });
  window.addEventListener("dragover", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
  });
  window.addEventListener("dragleave", (e) => {
    if (!hasFiles(e)) return;
    if (--depth <= 0) {
      depth = 0;
      host.emit("samples-drag", false);
    }
  });
  window.addEventListener("drop", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth = 0;
    host.emit("samples-drag", false);
    void readDropped(e.dataTransfer!.files)
      .then((files) => host.importFiles(files))
      .then((imported) => host.emit("samples-dropped", imported))
      .catch((error) => host.emit("samples-dropped", { files: [], error: String(error?.message ?? error) }));
  });
}

/** Installs the browser host; the engine keeps loading in the background. */
export async function installWebHost() {
  const fs = new MemFS();
  installGlobals(fs);
  const engine = startEngine();
  engine.catch((error) => console.error("Cannot start the workflow engine", error));
  const host = new WebHost(fs, engine, indexedDbStore());
  await Promise.all([loadExamples(fs), host.restore()]);
  (window as unknown as { __maiBridge: unknown }).__maiBridge = {
    web: true,
    invoke: (command: string, args?: Record<string, unknown>) => host.invoke(command, args),
    listen: (event: string, handler: (payload: unknown) => void) => host.listen(event, handler),
  };
  acceptDrops(host);
  return host;
}
