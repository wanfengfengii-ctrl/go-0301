// Build script for the seed-vault frontend. It copies the static source files
// into the Go embed directory (internal/httpapi/dist) so the Go server can
// serve a single self-contained binary. No third-party dependencies required.
import { cp, mkdir, readdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(here, '..', 'src');
const outDir = path.join(here, '..', '..', 'internal', 'httpapi', 'dist');

await mkdir(outDir, { recursive: true });
const entries = await readdir(srcDir);
for (const name of entries) {
  await cp(path.join(srcDir, name), path.join(outDir, name));
}
console.log(`built frontend: ${entries.length} file(s) -> ${path.relative(process.cwd(), outDir)}`);
