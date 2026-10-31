import { readdir, readFile, writeFile } from 'node:fs/promises';
import { brotliCompressSync, constants, gzipSync } from 'node:zlib';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../dist/', import.meta.url));

// Brotli runs about 19% smaller than gzip -9 on this bundle, so it is served
// first where the client accepts it. Gzip stays for the rest.
const encodings = [
  {
    ext: 'br',
    compress: (data) =>
      brotliCompressSync(data, {
        params: {
          [constants.BROTLI_PARAM_QUALITY]: 11,
          [constants.BROTLI_PARAM_SIZE_HINT]: data.length,
        },
      }),
  },
  { ext: 'gz', compress: (data) => gzipSync(data, { level: 9 }) },
];

async function walk(dir) {
  for (const item of await readdir(dir, { withFileTypes: true })) {
    const path = resolve(dir, item.name);
    if (item.isDirectory()) await walk(path);
    else if (/\.(js|css|html|svg|json)$/.test(item.name)) {
      const data = await readFile(path);
      if (data.length < 1024) continue;
      for (const { ext, compress } of encodings) {
        const compressed = compress(data);
        if (compressed.length < data.length)
          await writeFile(`${path}.${ext}`, compressed);
      }
    }
  }
}
await walk(root);
