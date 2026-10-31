import { spawn, spawnSync } from 'node:child_process';
import { closeSync, existsSync, mkdirSync, openSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const root = resolve(web, '..');
const args = process.argv.slice(2);
const setup = args[0] === '--setup';
const withAPI = args[0] === '--api';
const lock = resolve(web, '.cache/dev.lock');
const max = resolve(web, 'node_modules/@umijs/max/bin/max.js');
const port = Number(process.env.PORT || 5173);
const groups = [];
let locked = false;
let requestStop;
const stopped = new Promise((done) => { requestStop = done; });

function acquireLock() {
  mkdirSync(dirname(lock), { recursive: true });
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const fd = openSync(lock, 'wx');
      writeFileSync(fd, String(process.pid));
      closeSync(fd);
      locked = true;
      return;
    } catch (error) {
      if (error.code !== 'EEXIST') throw error;
      const owner = Number(readFileSync(lock, 'utf8'));
      try {
        if (!Number.isInteger(owner) || owner <= 0) throw new Error('开发服务正在启动，请稍后重试。');
        process.kill(owner, 0);
      } catch (error) {
        if (error.code === 'ESRCH') {
          unlinkSync(lock);
          continue;
        }
        throw error;
      }
      throw new Error('已有开发服务正在运行，请先在原终端按 Ctrl+C 停止，避免同时重建 Umi 临时目录。');
    }
  }
  throw new Error('未能取得开发服务锁，请重试。');
}

// A legacy/orphaned Umi process may predate the lock. Check before generating files.
async function checkPort() {
  await new Promise((done, reject) => {
    const server = createServer();
    server.once('error', (error) => reject(new Error(
      error.code === 'EADDRINUSE'
        ? `端口 ${port} 已被占用，请先停止原前端服务。`
        : `无法检查开发端口：${error.message}`,
    )));
    server.listen(port, '127.0.0.1', () => server.close(done));
  });
}

function start(command, commandArgs, cwd, env = process.env) {
  // Isolate the whole tree, including Umi's forked dev server and esbuild.
  const child = spawn(command, commandArgs, { cwd, env, stdio: 'inherit', detached: true });
  const closed = new Promise((done) => {
    child.once('error', (error) => {
      console.error(`启动失败：${error.message}`);
      requestStop(1);
    });
    child.once('exit', (code, signal) => requestStop(code ?? (signal ? 1 : 0)));
    child.once('close', done);
  });
  groups.push({ child, closed });
}

function signalGroups(signal) {
  for (const { child } of groups) {
    if (!child.pid) continue;
    try { process.kill(-child.pid, signal); } catch (error) {
      if (error.code !== 'ESRCH') console.error(`停止开发服务失败：${error.message}`);
    }
  }
}

for (const [signal, code] of [['SIGINT', 130], ['SIGTERM', 143]]) {
  process.on(signal, () => requestStop(code));
}

try {
  if (!existsSync(max)) throw new Error('缺少前端依赖，请先运行 make install。');
  acquireLock();
  await checkPort();
  const generated = spawnSync(process.execPath, ['scripts/generate-pages.mjs'], { cwd: web, stdio: 'inherit' });
  if (generated.error) throw generated.error;
  if (generated.status !== 0) throw new Error('生成页面注册表失败。');
  if (withAPI) start(resolve(root, 'bin/intent-admin'), ['serve', '--config', args[1] || 'configs/config.yaml'], root);
  start(process.execPath, [max, setup ? 'setup' : 'dev'], web, { ...process.env, PORT: String(port) });
  process.exitCode = await stopped;
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
} finally {
  signalGroups('SIGTERM');
  let timeout;
  await Promise.race([
    Promise.all(groups.map(({ closed }) => closed)),
    new Promise((done) => { timeout = setTimeout(done, 5000); }),
  ]);
  clearTimeout(timeout);
  // A wrapper can exit before its descendants; never leave watchers behind.
  signalGroups('SIGKILL');
  if (locked) unlinkSync(lock);
}
