// Builds Mai Studio for the browser as one self-contained HTML file:
// dist-web/mai-studio.html. It holds the editor, the Go workflow engine
// compiled to WebAssembly and the example workflows, so it opens from a
// file, an e-mail attachment or any static web server, needs no install and
// makes no network requests.
//
//   node scripts/build-web.mjs            build everything
//   node scripts/build-web.mjs --engine   only the engine files in .web-engine/
//                                          (for `vite --mode web` development)
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync, copyFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import path from "node:path";
import { fileURLToPath } from "node:url";

const studio = fileURLToPath(new URL("../", import.meta.url));
const repo = path.resolve(studio, "../..");
const engineDir = path.join(studio, ".web-engine");
const outDir = path.join(studio, "dist-web");
const go = process.env.MAI_GO || "go";

function buildEngine() {
  mkdirSync(engineDir, { recursive: true });
  const wasm = path.join(engineDir, "mai.wasm");
  execFileSync(go, ["build", "-mod=readonly", "-trimpath", "-ldflags=-s -w", "-o", wasm, "./cmd/mai-wasm"], {
    cwd: repo,
    stdio: "inherit",
    env: { ...process.env, GOOS: "js", GOARCH: "wasm" },
  });
  writeFileSync(path.join(engineDir, "mai.wasm.gz"), gzipSync(readFileSync(wasm), { level: 9 }));
  rmSync(wasm);
  // wasm_exec.js must come from the Go that built the module.
  const goroot = execFileSync(go, ["env", "GOROOT"], { encoding: "utf8" }).trim();
  const loader = [path.join(goroot, "lib/wasm/wasm_exec.js"), path.join(goroot, "misc/wasm/wasm_exec.js")].find(existsSync);
  if (!loader) throw new Error(`Cannot find wasm_exec.js in ${goroot}`);
  copyFileSync(loader, path.join(engineDir, "wasm_exec.js"));
  // The examples, so templates open with sample data at once.
  const examples = {};
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const file = path.join(dir, name);
      if (statSync(file).isDirectory()) walk(file);
      else examples[path.relative(repo, file).split(path.sep).join("/")] = readFileSync(file).toString("base64");
    }
  };
  walk(path.join(repo, "examples"));
  writeFileSync(path.join(engineDir, "examples.json"), JSON.stringify(examples));
  console.log(`Built the browser engine in ${engineDir}`);
}

function buildPage() {
  const vite = path.join(studio, "node_modules/vite/bin/vite.js");
  execFileSync(process.execPath, [vite, "build", "--mode", "web"], { cwd: studio, stdio: "inherit" });
  const assets = path.join(outDir, "assets");
  let html = readFileSync(path.join(assets, "index.html"), "utf8");
  const asset = (ref) => readFileSync(path.join(assets, ref.replace(/^\.\//, "")));
  const script = (text) => text.replace(/<\/(script)/gi, "<\\/$1");
  html = html.replace(/<script type="module"[^>]*src="([^"]+)"[^>]*><\/script>/, (_, src) => `<script type="module">${script(asset(src).toString("utf8"))}</script>`);
  html = html.replace(/<link rel="stylesheet"[^>]*href="([^"]+)"[^>]*>/g, (_, href) => `<style>${asset(href).toString("utf8")}</style>`);
  html = html.replace(/<link rel="icon"([^>]*)href="([^"]+)"([^>]*)>/, (_, a, href, b) => {
    const file = path.join(studio, "public", path.basename(href));
    return existsSync(file) ? `<link rel="icon"${a}href="data:image/svg+xml;base64,${readFileSync(file).toString("base64")}"${b}>` : "";
  });
  if (/src="\.\/assets|href="\.\/assets/.test(html)) throw new Error("An asset was not inlined");
  const engine = [
    `<script>${script(readFileSync(path.join(engineDir, "wasm_exec.js"), "utf8"))}</script>`,
    `<script type="text/plain" id="mai-engine">${readFileSync(path.join(engineDir, "mai.wasm.gz")).toString("base64")}</script>`,
    `<script type="application/json" id="mai-examples">${script(readFileSync(path.join(engineDir, "examples.json"), "utf8"))}</script>`,
  ].join("\n");
  html = html.replace("</head>", `${engine}\n</head>`);
  const out = path.join(outDir, "mai-studio.html");
  writeFileSync(out, html);
  rmSync(assets, { recursive: true, force: true });
  console.log(`Built ${out} (${(statSync(out).size / 1048576).toFixed(1)} MiB)`);
}

buildEngine();
if (!process.argv.includes("--engine")) buildPage();
