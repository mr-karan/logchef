import assert from 'node:assert/strict';
import { readFile, readdir, stat } from 'node:fs/promises';
import { resolve } from 'node:path';

const outputDirectory = process.argv[2];
assert.ok(outputDirectory, 'Pass the production build directory.');
const assets = await readdir(resolve(outputDirectory, 'assets'));
assert.ok(
  !assets.some((file) => /^(ts|css|html|json)\.worker-/.test(file)),
  'Custom query languages must not bundle unused language workers.',
);
assert.ok(assets.some((file) => /^editor\.worker-/.test(file)), 'Keep the editor worker.');
const html = await readFile(resolve(outputDirectory, 'index.html'), 'utf8');
const entryFiles = new Set(
  [...html.matchAll(/(?:src|href)="([^"]+\.(?:js|css))"/g)].map((match) => match[1]),
);
const editorFiles = [...entryFiles].filter((file) => /assets\/(monaco|editor)[.-]/.test(file));
assert.deepEqual(editorFiles, [], 'Pages without an editor must not load Monaco JavaScript or CSS.');

const entryScripts = [...entryFiles].filter((file) => file.endsWith('.js'));
const sizes = await Promise.all(
  entryScripts.map(async (file) => (await stat(resolve(outputDirectory, file))).size),
);
const entryBytes = sizes.reduce((total, size) => total + size, 0);
assert.ok(entryBytes < 800 * 1024, `Entry JavaScript is ${Math.round(entryBytes / 1024)} KiB, over the 800 KiB budget.`);
console.log('Editor bundle check passed. Monaco is absent from entry links.');
console.log(`Entry JavaScript is ${Math.round(entryBytes / 1024)} KiB.`);
