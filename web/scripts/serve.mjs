// Minimal static dev server so the frontend can be previewed independently
// of the Go backend.
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(here, '..', 'src');

const types = { '.html': 'text/html; charset=utf-8', '.css': 'text/css; charset=utf-8', '.js': 'text/javascript; charset=utf-8' };

createServer(async (req, res) => {
  const url = req.url === '/' ? '/index.html' : req.url;
  try {
    const data = await readFile(path.join(srcDir, url));
    res.writeHead(200, { 'Content-Type': types[path.extname(url)] || 'application/octet-stream' });
    res.end(data);
  } catch {
    res.writeHead(404, { 'Content-Type': 'text/plain' });
    res.end('not found');
  }
}).listen(3000, () => console.log('frontend preview on http://localhost:3000'));
