// Verify the real Vite watcher ignores native output but still watches frontend edits.
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, rm, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
import { createServer, loadConfigFromFile } from 'vite';

const root = await mkdtemp(join(tmpdir(), 'mai-watcher-'));
let server;
try {
  // Resolve the actual React plugin's dependencies without installing into the fixture.
  await symlink(fileURLToPath(new URL('../node_modules', import.meta.url)), join(root, 'node_modules'), process.platform === 'win32' ? 'junction' : 'dir');
  const frontend = join(root, 'src', 'main.js');
  const native = join(root, 'src-tauri', 'target', 'debug', 'build', 'test.pdb');
  await mkdir(join(root, 'src'), { recursive: true });
  await mkdir(join(root, 'src-tauri', 'target', 'debug', 'build'), { recursive: true });
  await writeFile(frontend, 'export const value = 1;');
  await writeFile(native, 'native output');
  await writeFile(join(root, 'index.html'), '<script type="module" src="/src/main.js"></script>');
  const loaded = await loadConfigFromFile(
    { command: 'serve', mode: 'development' },
    fileURLToPath(new URL('../vite.config.ts', import.meta.url)),
  );
  assert.ok(loaded, 'Studio Vite config must load');
  server = await createServer({
    ...loaded.config,
    configFile: false,
    root,
    server: { ...loaded.config.server, middlewareMode: true, hmr: false },
    optimizeDeps: { noDiscovery: true },
  });
  const events = [];
  server.watcher.on('all', (_, path) => events.push(path));
  // Wait for initial directory discovery before modifying either fixture.
  const deadline = Date.now() + 5000;
  while (!Object.values(server.watcher.getWatched()).some(files => files.includes('main.js'))) {
    assert.ok(Date.now() < deadline, 'Frontend directory was not watched');
    await delay(25);
  }
  events.length = 0;
  await writeFile(native, 'new native output');
  await writeFile(frontend, 'export const value = 2;');
  const changed = Date.now() + 5000;
  while (!events.some(path => normalize(path) === normalize(frontend))) {
    assert.ok(Date.now() < changed, 'Frontend edits did not reach the watcher');
    await delay(25);
  }
  await delay(250);
  assert.ok(!events.some(path => path.includes('src-tauri')), 'Native output reached the frontend watcher');
  assert.ok(!Object.keys(server.watcher.getWatched()).some(path => path.includes('src-tauri')), 'Native directories were registered with the watcher');
  console.log('Watcher regression passed: native files ignored; frontend edits detected.');
} finally {
  await server?.close();
  await rm(root, { recursive: true, force: true });
}
