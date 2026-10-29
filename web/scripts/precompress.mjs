import { readdir, readFile, writeFile } from 'node:fs/promises';
import { gzipSync } from 'node:zlib';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../dist/', import.meta.url));
async function walk(dir) {
  for (const item of await readdir(dir, { withFileTypes: true })) {
    const path = resolve(dir, item.name);
    if (item.isDirectory()) await walk(path);
    else if (/\.(js|css|html|svg|json)$/.test(item.name)) {
      const data = await readFile(path);
      if (data.length < 1024) continue;
      const compressed = gzipSync(data, { level: 9 });
      if (compressed.length < data.length) await writeFile(`${path}.gz`, compressed);
    }
  }
}
await walk(root);
