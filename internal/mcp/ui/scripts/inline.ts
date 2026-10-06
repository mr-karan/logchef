import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const directory = resolve(import.meta.dir, '../dist')
let html = await readFile(resolve(directory, 'index.html'), 'utf8')
for (const match of html.matchAll(/<script[^>]*src="([^"]+)"[^>]*><\/script>/g)) {
  const path = match[1]
  if (!path) continue
  const script = await readFile(resolve(directory, path.replace(/^\//, '')), 'utf8')
  html = html.replace(match[0], () => `<script type="module">${script.replace(/<\/script/gi, '<\\/script')}</script>`)
}
for (const match of html.matchAll(/<link[^>]*href="([^"]+\.css)"[^>]*>/g)) {
  const path = match[1]
  if (!path) continue
  const css = await readFile(resolve(directory, path.replace(/^\//, '')), 'utf8')
  html = html.replace(match[0], () => `<style>${css}</style>`)
}
await writeFile(resolve(directory, '../investigation.html'), html)
