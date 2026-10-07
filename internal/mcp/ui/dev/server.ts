import { resolve } from 'node:path'
import { loadConnection } from './config'

// Local MCP Apps test host. It serves the built panel in an iframe, bridges
// its tool calls to Logchef's /mcp endpoint, and shows the context the panel
// attaches. It is a test harness, not ChatGPT.
const connection = await loadConnection(process.env)
const directory = resolve(import.meta.dir, '..')
const build = await Bun.build({ entrypoints: [resolve(import.meta.dir, 'host.ts')], target: 'browser' })
if (!build.success) throw new Error('Host build failed.')
const bundle = build.outputs[0]
if (!bundle) throw new Error('Host bundle missing.')
const readTools = new Set(['get_sources', 'query_logchefql', 'translate_logchefql', 'get_log_histogram', 'get_log_context', 'compare_windows', 'open_investigation'])
const port = Number(process.env.LOGCHEF_DEV_HOST_PORT ?? 5180)
const server = Bun.serve({ hostname: '127.0.0.1', port, async fetch(request) {
  const url = new URL(request.url)
  if (url.pathname === '/rpc' && request.method === 'POST') {
    const value: unknown = await request.json()
    if (typeof value !== 'object' || value === null || !('method' in value) || value.method !== 'tools/call' || !('params' in value) || typeof value.params !== 'object' || value.params === null || !('name' in value.params) || typeof value.params.name !== 'string' || !readTools.has(value.params.name)) return new Response('Tool denied', { status: 403 })
    const body = { ...value, params: { ...value.params, _meta: { 'io.modelcontextprotocol/protocolVersion': '2026-07-28', 'io.modelcontextprotocol/clientInfo': { name: 'local-test', version: '1' }, 'io.modelcontextprotocol/clientCapabilities': {} } } }
    const upstream = await fetch(connection.mcpUrl, { method: 'POST', redirect: 'error', headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', 'Mcp-Protocol-Version': '2026-07-28', 'Mcp-Method': 'tools/call', 'Mcp-Name': value.params.name, Authorization: `Bearer ${connection.token}` }, body: JSON.stringify(body) })
    // fetch has already decoded the body, so forwarding Logchef's
    // Content-Encoding header would make the browser decode it twice.
    return new Response(await upstream.text(), { status: upstream.status, headers: { 'Content-Type': upstream.headers.get('Content-Type') ?? 'application/json' } })
  }
  if (url.pathname === '/host.js') return new Response(bundle, { headers: { 'Content-Type': 'application/javascript' } })
  if (url.pathname === '/panel') return new Response(Bun.file(resolve(directory, 'investigation.html')), { headers: { 'Content-Type': 'text/html' } })
  return new Response('<!doctype html><title>Logchef Apps test host</title><style>body{font:14px system-ui;margin:24px}iframe{width:100%;height:1000px;border:1px solid #ddd}pre{white-space:pre-wrap}</style><h1>Logchef local Apps test host</h1><p id="status">Connecting</p><button id="theme">Toggle panel theme</button> <button id="width">Toggle mobile width</button><p><label>open_investigation input <textarea id="tool-input" rows="2" cols="80">{}</textarea></label> <button id="deliver">Deliver tool call</button></p><p>Use Expand in the panel to test fullscreen. This is a local test host, not ChatGPT.</p><iframe title="Logchef investigation" sandbox="allow-scripts allow-same-origin"></iframe><h2>Attached context</h2><pre id="context"></pre><script type="module" src="/host.js"></script>', { headers: { 'Content-Type': 'text/html' } })
} })
process.on('SIGINT', () => { server.stop(); process.exit(0) })
process.on('SIGTERM', () => { server.stop(); process.exit(0) })
console.log(`Read-only test host for ${connection.mcpUrl}: http://127.0.0.1:${port}`)
