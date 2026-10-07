import { describe, expect, test } from 'bun:test'
import 'vue'
import type { Transport } from '@modelcontextprotocol/core'
import type { JSONRPCMessage, CallToolResult } from '@modelcontextprotocol/core'

// The panel talks to its host with postMessage. This page stands in for the
// iframe: a real AppBridge on the host side exchanges the same JSON-RPC
// messages with the panel's real App and composable. Tool results are fixed
// MCP responses at the host boundary. Vue loads before the page globals exist
// so it does not treat them as a real DOM.
console.debug = () => {}
const host = { postMessage: (message: JSONRPCMessage) => hostTransport.onmessage?.(message) }
const page = Object.assign(new EventTarget(), { parent: host, innerWidth: 800 })
const hostTransport: Transport = {
  async start() {},
  async close() {},
  async send(message) {
    const event = new Event('message')
    Object.defineProperties(event, { data: { value: message }, source: { value: host } })
    page.dispatchEvent(event)
  },
}
const element = { style: {}, dataset: {}, setAttribute() {}, getBoundingClientRect: () => ({ height: 600 }) }
Object.assign(globalThis, {
  window: page,
  document: { documentElement: element, body: element },
  requestAnimationFrame: () => 0,
  ResizeObserver: class { observe() {} disconnect() {} },
})

const { AppBridge } = await import('@modelcontextprotocol/ext-apps/app-bridge')
const { useInvestigation } = await import('./useInvestigation')

const calls: { name: string; arguments: Record<string, unknown> }[] = []
const contexts: unknown[] = []
const sources = { sources: [
  { id: 1, name: 'app', source_type: 'clickhouse', ts_field: 'timestamp', teams: [{ id: 10, name: 'Ops' }] },
  { id: 2, name: 'edge', source_type: 'victorialogs', ts_field: '_time', teams: [{ id: 20, name: 'Edge' }] },
] }

function respond(name: string, args: Record<string, unknown>): CallToolResult {
  const data = (value: Record<string, unknown>): CallToolResult => ({ content: [], structuredContent: value })
  if (name === 'get_sources') return data(sources)
  if (name === 'query_logchefql') return data({ logs: [{ timestamp: args.start_time, message: `matched ${String(args.query)}` }], stats: { execution_time_ms: 3, limit_applied: 100 } })
  if (name === 'translate_logchefql') return data({ valid: true, generated_query_language: 'clickhouse-sql', full_sql: 'SELECT 1' })
  if (name === 'get_log_histogram') return data({ granularity: '1m', data: [{ bucket: args.start_time, log_count: 1 }] })
  throw new Error(`Unexpected tool ${name}.`)
}

async function settled(panel: ReturnType<typeof useInvestigation>, query: string) {
  for (let attempt = 0; attempt < 200; attempt++) {
    if (!panel.pending.value && panel.executed.value?.query === query) return
    await new Promise((resolve) => setTimeout(resolve, 5))
  }
  throw new Error(`Panel did not run ${query}. Error: ${panel.error.value}`)
}

describe('investigation panel tool input', () => {
 test('a second tool call replaces the open panel state and runs its query', async () => {
  const bridge = new AppBridge(null, { name: 'test host', version: '1' }, { serverTools: {}, updateModelContext: {} })
  bridge.oncalltool = async (params) => {
   calls.push({ name: params.name, arguments: params.arguments ?? {} })
   return respond(params.name, params.arguments ?? {})
  }
  bridge.onupdatemodelcontext = async (params) => { contexts.push(params); return {} }
  const initialized = new Promise<void>((resolve) => { bridge.oninitialized = () => resolve() })
  await bridge.connect(hostTransport)
  const panel = useInvestigation()
  const started = panel.initialize()
  await initialized

  await bridge.sendToolInput({ arguments: {} })
  await bridge.sendToolResult({ content: [], structuredContent: { query: '', start_time: '2026-10-07T07:00:00Z', end_time: '2026-10-07T08:00:00Z', timezone: 'UTC' } })
  await started
  await settled(panel, '')
  expect(panel.executed.value?.source_id).toBe(1)
  await panel.selectRow(0)
  expect(panel.selectedRows.value).toEqual([0])
  expect(contexts).toHaveLength(1)
  const firstRuns = calls.filter((call) => call.name === 'query_logchefql').length
  expect(firstRuns).toBe(1)

  const second = { team_id: 20, source_id: 2, query: 'lvl="ERROR"', start_time: '2026-10-07T07:48:12Z', end_time: '2026-10-07T08:48:12Z' }
  await bridge.sendToolInput({ arguments: second })
  await bridge.sendToolResult({ content: [], structuredContent: { ...second, timezone: 'UTC' } })
  await settled(panel, 'lvl="ERROR"')

  expect(panel.selectedSource.value).toBe('2')
  expect(panel.query.value).toBe('lvl="ERROR"')
  expect(panel.start.value).toBe('2026-10-07T07:48:12Z')
  expect(panel.end.value).toBe('2026-10-07T08:48:12Z')
  expect(panel.executed.value).toEqual({ team_id: 20, source_id: 2, query: 'lvl="ERROR"', start_time: '2026-10-07T07:48:12.000Z', end_time: '2026-10-07T08:48:12.000Z', timezone: 'UTC' })
  expect(panel.evidence.value?.logs[0]?.message).toBe('matched lvl="ERROR"')
  expect(panel.selectedRows.value).toEqual([])
  expect(contexts.at(-1)).toEqual({ content: [], structuredContent: {} })
  expect(calls.filter((call) => call.name === 'query_logchefql').length).toBe(firstRuns + 1)
  await bridge.close()
 })
})
