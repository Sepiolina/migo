import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const studio = fileURLToPath(new URL('../', import.meta.url));
const root = path.resolve(studio, '../..');
const rustVersion = execFileSync('rustc', ['-vV'], { encoding: 'utf8' });
const target = rustVersion.match(/^host: (.+)$/m)?.[1];
if (!target) throw new Error('Cannot identify the Rust host target');
const binaryDir = path.join(studio, 'src-tauri/binaries');
mkdirSync(binaryDir, { recursive: true });
const output = path.join(binaryDir, `mai-runner-${target}${process.platform === 'win32' ? '.exe' : ''}`);
execFileSync(process.env.MAI_GO || 'go', ['build', '-mod=readonly', '-trimpath', '-o', output, './cmd/mai-runner'], {
  cwd: root,
  stdio: 'inherit',
});
console.log(`Built desktop runner: ${output}`);
