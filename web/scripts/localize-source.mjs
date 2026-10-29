// Converts authored UI copy to catalogue lookups. Runtime data, comparisons,
// protocol strings and TypeScript literal types remain unchanged.
import ts from 'typescript';
import { readdir, readFile, writeFile } from 'node:fs/promises';
import { resolve, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

process.chdir(fileURLToPath(new URL('..', import.meta.url)));
const mode = process.argv[2] ?? '--check';
const han = /[\u3400-\u9fff]/;
const keys = new Set();

async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    if (entry.name.startsWith('.')) continue;
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) files.push(...await walk(path));
    else if (/\.tsx?$/.test(entry.name) && !entry.name.includes('.test.')) files.push(path);
  }
  return files;
}
function jsxText(value) {
  const lines = value.replace(/\r/g, '').split('\n');
  let last = 0;
  for (let i = 0; i < lines.length; i++) if (/\S/.test(lines[i])) last = i;
  return lines.map((line, i) => {
    let text = line.replace(/\t/g, ' ');
    if (i !== 0) text = text.trimStart();
    if (i !== lines.length - 1) text = text.trimEnd();
    return text ? text + (i !== last ? ' ' : '') : '';
  }).join('');
}
const compareKinds = new Set([
  ts.SyntaxKind.EqualsEqualsToken, ts.SyntaxKind.EqualsEqualsEqualsToken,
  ts.SyntaxKind.ExclamationEqualsToken, ts.SyntaxKind.ExclamationEqualsEqualsToken,
]);
const protocolMethods = new Set(['startsWith', 'endsWith', 'includes', 'indexOf', 'replace', 'replaceAll', 'split', 'match']);
function excluded(node) {
  const p = node.parent;
  if (!p) return false;
  if (ts.isTypeNode(p) || ts.isImportDeclaration(p) || ts.isExportDeclaration(p)) return true;
  if ((ts.isPropertyAssignment(p) || ts.isPropertySignature(p) || ts.isMethodDeclaration(p)) && p.name === node) return true;
  if (ts.isPropertyAssignment(p) && ['value', 'defaultValue', 'userName', 'nickName', 'remark'].includes(p.name.getText())) return true;
  if (ts.isBinaryExpression(p) && compareKinds.has(p.operatorToken.kind)) return true;
  if (ts.isCallExpression(p) && ts.isPropertyAccessExpression(p.expression) && protocolMethods.has(p.expression.name.text)) return true;
  return false;
}

function adapt(source, filename, importRuntime = true) {
  const sf = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true, filename.endsWith('x') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const edits = [];
  let used = false;
  function add(node, replacement) { edits.push([node.getStart(sf), node.end, replacement]); used = true; }
  function visit(node) {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && ['t', 'localizeMessage', 'translate'].includes(node.expression.text)) {
      const arg = node.arguments[0];
      if (arg && ts.isStringLiteral(arg)) keys.add(arg.text);
      return;
    }
    if (ts.isTemplateExpression(node) && han.test(node.head.text + node.templateSpans.map((s) => s.literal.text).join('')) && !excluded(node)) {
      let key = node.head.text;
      const args = node.templateSpans.map((span, i) => {
        key += `{value${i}}${span.literal.text}`;
        return `value${i}: (${adapt(span.expression.getText(sf), filename.endsWith('x') ? 'expression.tsx' : 'expression.ts', false)})`;
      });
      keys.add(key);
      add(node, `t(${JSON.stringify(key)}, { ${args.join(', ')} })`);
      return;
    }
    if (ts.isJsxText(node) && han.test(node.text)) {
      const key = jsxText(node.text);
      keys.add(key); add(node, `{t(${JSON.stringify(key)})}`); return;
    }
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && han.test(node.text) && !excluded(node)) {
      keys.add(node.text);
      const expression = `t(${JSON.stringify(node.text)})`;
      add(node, ts.isJsxAttribute(node.parent) ? `{${expression}}` : expression);
      return;
    }
    ts.forEachChild(node, visit);
  }
  visit(sf);
  let next = source;
  for (const [start, end, replacement] of edits.sort((a, b) => b[0] - a[0])) next = next.slice(0, start) + replacement + next.slice(end);
  if (used && importRuntime && !/import\s*\{[^}]*\bt\b[^}]*\}\s*from\s*['"]@\/i18n['"]/.test(source)) next = `import { t } from '@/i18n';\n${next}`;
  return next;
}

let changed = 0;
for (const filename of await walk(resolve('src'))) {
  const path = relative(resolve('src'), filename);
  if (/^(i18n|locales|generated)\//.test(path)) continue;
  const source = await readFile(filename, 'utf8');
  const next = adapt(source, filename);
  if (source !== next) {
    changed++;
    if (mode === '--write') await writeFile(filename, next);
  }
}
const messages = {};
for (const filename of await readdir('src/i18n/messages')) {
  if (filename.endsWith('.json')) Object.assign(messages, JSON.parse(await readFile(`src/i18n/messages/${filename}`, 'utf8')));
}
const missing = [...keys].filter((key) => !messages[key] && !messages[key.trim()]).sort();
console.log(JSON.stringify({ changedFiles: changed, messageKeys: keys.size, missing }, null, 2));
if (mode === '--check' && (changed || missing.length)) process.exitCode = 1;
