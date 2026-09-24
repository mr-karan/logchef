// Wide-schema Explorer benchmark for issue #106. Drives Chrome over CDP, so it
// can force garbage collection, read Performance.getMetrics, and give every run
// a fresh tab. See docs/table-performance.md for the fixture and sources.
//
//   bun scripts/benchmark-wide-schema.ts --cdp <browser-ws-url> \
//     --origin http://localhost:4173 --team 1 --source 18 \
//     --scenario legacy-all-visible --runs 5
//
// Scenarios:
//   fresh               no saved table state
//   legacy-all-visible  every schema field saved as visible, without a
//                       visibilityVersion (state written by v2.0.2)
//   chosen-all-visible  every schema field saved as visible by this version
import { parseArgs } from 'node:util'

const { values: args } = parseArgs({
  options: {
    cdp: { type: 'string' },
    origin: { type: 'string' },
    team: { type: 'string', default: '1' },
    source: { type: 'string' },
    scenario: { type: 'string', default: 'fresh' },
    runs: { type: 'string', default: '5' },
  },
})
const scenarios = ['fresh', 'legacy-all-visible', 'chosen-all-visible'] as const
type Scenario = (typeof scenarios)[number]
const isScenario = (value: string): value is Scenario => scenarios.some(s => s === value)
if (!args.cdp || !args.origin || !args.source || !isScenario(args.scenario)) {
  console.error('Required: --cdp <ws-url> --origin <url> --source <id> [--team 1] [--scenario fresh|legacy-all-visible|chosen-all-visible] [--runs 5]')
  process.exit(2)
}
const cdp = args.cdp
const origin = args.origin
const source = args.source
const scenario: Scenario = args.scenario
const team = args.team
const runs = Number(args.runs)

interface CdpMessage {
  id?: number
  result?: Record<string, unknown>
  error?: { message: string }
}

const ws = new WebSocket(cdp)
await new Promise(resolve => ws.addEventListener('open', resolve, { once: true }))
let nextId = 1
let sessionId: string | undefined
const pending = new Map<number, (message: CdpMessage) => void>()
ws.addEventListener('message', event => {
  const message: CdpMessage = JSON.parse(String(event.data))
  if (message.id !== undefined) pending.get(message.id)?.(message)
})

function send(method: string, params: object = {}, timeoutMs = 120_000): Promise<Record<string, any>> {
  const id = nextId++
  ws.send(JSON.stringify({ id, method, params, sessionId }))
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`CDP timeout: ${method}`)), timeoutMs)
    pending.set(id, message => {
      clearTimeout(timer)
      pending.delete(id)
      if (message.error) reject(new Error(`${method}: ${message.error.message}`))
      else resolve(message.result ?? {})
    })
  })
}

async function evaluate(expression: string, timeoutMs = 120_000) {
  const result = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true }, timeoutMs)
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description ?? 'evaluation failed')
  return result.result.value
}

const sleep = (ms: number) => evaluate(`new Promise(r => setTimeout(r, ${ms}))`, ms + 120_000)

// Milliseconds until `condition` holds in the page, or null after `limitMs`.
async function waitFor(condition: string, limitMs: number): Promise<number | null> {
  const elapsed = await evaluate(`new Promise(resolve => {
    const start = performance.now()
    const tick = () => {
      if (${condition}) return resolve(Math.round(performance.now() - start))
      if (performance.now() - start > ${limitMs}) return resolve(-1)
      setTimeout(tick, 50)
    }
    tick()
  })`, limitMs + 120_000)
  return elapsed < 0 ? null : elapsed
}

// Real pointer input: Reka triggers open on pointerdown, not on click().
async function click(finder: string) {
  const point = await evaluate(`(() => {
    const el = ${finder}
    if (!el) return null
    const r = el.getBoundingClientRect()
    return { x: r.x + r.width / 2, y: r.y + r.height / 2 }
  })()`)
  if (!point) throw new Error(`Element not found: ${finder}`)
  for (const type of ['mousePressed', 'mouseReleased']) {
    await send('Input.dispatchMouseEvent', { type, x: point.x, y: point.y, button: 'left', clickCount: 1 })
  }
}

async function memorySnapshot() {
  await send('HeapProfiler.collectGarbage', {}, 240_000)
  const counts = await evaluate(`({
    visibleColumns: Math.max(0, document.querySelectorAll('thead th').length - 1),
    bodyCells: document.querySelectorAll('tbody td').length,
    sidebarFieldRows: document.querySelectorAll('[data-slot="collapsible"]').length,
  })`, 240_000)
  const { metrics } = await send('Performance.getMetrics', {}, 240_000)
  const metric = (name: string): number => metrics.find((m: { name: string }) => m.name === name)?.value ?? NaN
  return {
    ...counts,
    domNodes: metric('Nodes'),
    listeners: metric('JSEventListeners'),
    heapMB: Math.round(metric('JSHeapUsedSize') / 1048576),
  }
}

async function runOnce() {
  sessionId = undefined
  const { targetId } = await send('Target.createTarget', { url: 'about:blank' })
  sessionId = (await send('Target.attachToTarget', { targetId, flatten: true })).sessionId
  await send('Performance.enable')
  await send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 1000, deviceScaleFactor: 1, mobile: false })
  try {
    await send('Page.navigate', { url: `${origin}/` })
    await sleep(1500)
    await evaluate(`(async () => {
      for (const key of Object.keys(localStorage)) if (key.startsWith('logchef-tableState')) localStorage.removeItem(key)
      if (${JSON.stringify(scenario)} === 'fresh') return
      const response = await fetch('/api/v1/teams/${team}/sources/${source}/schema', { credentials: 'include' })
      const names = (await response.json()).data.map(column => column.name)
      localStorage.setItem('logchef-tableState-${team}-${source}', JSON.stringify({
        columnOrder: names,
        columnSizing: {},
        columnVisibility: Object.fromEntries(names.map(name => [name, true])),
        ...(${JSON.stringify(scenario)} === 'chosen-all-visible' ? { visibilityVersion: 2 } : {}),
      }))
    })()`)

    const start = Date.now()
    await send('Page.navigate', { url: `${origin}/logs/explore?team=${team}&source=${source}&t=6h&limit=1000&mode=logchefql` })
    const rendered = await waitFor(`document.querySelectorAll('tbody tr td').length > 20`, 120_000)
    const firstRowsMs = rendered === null ? null : Date.now() - start
    await sleep(3000)
    const initial = await memorySnapshot()

    await click(`[...document.querySelectorAll('button[role="combobox"]')].find(b => b.textContent.trim() === '50')`)
    await waitFor(`[...document.querySelectorAll('[role="option"]')].some(o => o.textContent.trim() === '1000')`, 10_000)
    const pageSizeStart = Date.now()
    await click(`[...document.querySelectorAll('[role="option"]')].find(o => o.textContent.trim() === '1000')`)
    const grown = await waitFor(`document.querySelectorAll('tbody tr').length >= 300`, 180_000)
    const pageSize1000Ms = grown === null ? null : Date.now() - pageSizeStart
    await sleep(2000)
    const at1000Rows = await memorySnapshot()

    const groupByStart = Date.now()
    await click(`(() => {
      const label = [...document.querySelectorAll('label')].find(l => l.textContent.trim().toLowerCase().startsWith('group by'))
      return label?.parentElement?.querySelector('button[role="combobox"]')
    })()`)
    const opened = await waitFor(`document.querySelectorAll('[role="option"], [data-item]').length > 0`, 120_000)
    const groupByOpenMs = opened === null ? null : Date.now() - groupByStart

    return { firstRowsMs, pageSize1000Ms, groupByOpenMs, initial, at1000Rows }
  } finally {
    sessionId = undefined
    await send('Target.closeTarget', { targetId }).catch(() => {})
  }
}

const median = (values: (number | null)[]): number | null => {
  const numbers = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v)).sort((a, b) => a - b)
  if (numbers.length === 0) return null
  const mid = Math.floor(numbers.length / 2)
  return numbers.length % 2 ? numbers[mid] : Math.round((numbers[mid - 1] + numbers[mid]) / 2)
}

const results: Awaited<ReturnType<typeof runOnce>>[] = []
for (let i = 0; i < runs; i++) {
  const result = await runOnce()
  console.error(JSON.stringify({ run: i + 1, ...result }))
  results.push(result)
}

const summarize = (pick: (r: Awaited<ReturnType<typeof runOnce>>) => number | null) => median(results.map(pick))
console.log(JSON.stringify({
  origin, source, scenario, runs,
  median: {
    firstRowsMs: summarize(r => r.firstRowsMs),
    pageSize1000Ms: summarize(r => r.pageSize1000Ms),
    groupByOpenMs: summarize(r => r.groupByOpenMs),
    initial: {
      visibleColumns: summarize(r => r.initial.visibleColumns),
      sidebarFieldRows: summarize(r => r.initial.sidebarFieldRows),
      domNodes: summarize(r => r.initial.domNodes),
      listeners: summarize(r => r.initial.listeners),
      heapMB: summarize(r => r.initial.heapMB),
    },
    at1000Rows: {
      domNodes: summarize(r => r.at1000Rows.domNodes),
      listeners: summarize(r => r.at1000Rows.listeners),
      heapMB: summarize(r => r.at1000Rows.heapMB),
    },
  },
}))
ws.close()
