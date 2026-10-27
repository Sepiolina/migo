// Exercise the same serialized request/response contract used by the native host.
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, copyFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const root = fileURLToPath(new URL('../../../', import.meta.url));
const workspace = mkdtempSync(join(tmpdir(), 'mai-smoke-'));
try {
  const runner = join(workspace, process.platform === 'win32' ? 'runner.exe' : 'runner');
  execFileSync(process.env.MAI_GO || 'go', ['build', '-mod=readonly', '-o', runner, './cmd/mai-runner'], { cwd: root, stdio: 'inherit' });
  copyFileSync(join(root, 'examples/parts.xlsx'), join(workspace, 'parts.xlsx'));
  const yaml = readFileSync(join(root, 'examples/parts-summary.yaml'), 'utf8');
  function request(command, params) {
    const id = `smoke-${command}`;
    const output = execFileSync(runner, [], { input: JSON.stringify({ version: 1, id, command, params: { workspace, ...params } }) + '\n', encoding: 'utf8', timeout: 30000, maxBuffer: 1024 * 1024 });
    const response = JSON.parse(output);
    assert.equal(response.id, id);
    assert.equal(response.version, 1);
    assert.equal(response.ok, true, JSON.stringify(response.error));
    return response.result;
  }
  const validation = request('validate', { yaml });
  assert.equal(validation.inputs[0].name, 'parts');
  const run = request('run', { yaml, inputs: { parts: 'parts.xlsx' } });
  const artifact = readFileSync(resolve(workspace, run.path));
  assert.equal(createHash('sha256').update(artifact).digest('hex'), run.sha256);
  const preview = request('inspectWorkbook', { path: run.path });
  assert.deepEqual(preview.rows, run.preview.rows);
  assert.ok(preview.totalRows > 1);
  console.log(`Runner integration passed: ${preview.totalRows} output rows and verified artifact hash.`);
} finally {
  rmSync(workspace, { recursive: true, force: true });
}
