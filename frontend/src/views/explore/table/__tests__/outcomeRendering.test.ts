import { createApp, h, nextTick } from 'vue'
import { createPinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import { getStandardSeverityClasses } from '@/lib/utils'
import DataTable from '../data-table.vue'

it.each([{ detail: 'retained JSON' }, 42])('renders non-string severity %j without a fallback badge', async value => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: 'severity', type: 'String' }],
    data: [{ severity: value }], severityField: 'severity',
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 20 },
    sourceId: 'non-string-severity', teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const cell = host.querySelector('td[data-cell-id] .flex-render-content')
    expect(cell?.textContent).toBe(typeof value === 'object' ? JSON.stringify(value) : String(value))
    expect(cell?.classList.contains('bg-gray-300')).toBe(false)
    expect(cell?.classList.contains('json-content')).toBe(typeof value === 'object')
  } finally {
    app.unmount()
    host.remove()
  }
})

it.each(['severity_text', 'level'])('renders outcome tones without replacing highlights, configured %s or HTTP badges', async severityField => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const data = [{
    timestamp: '2026-10-27T10:00:00Z', [severityField]: 'error',
    event_type: '_JOB.FAILURE', result: 'PENDING', record_status: 'SUCCESSFUL',
    status: 503, body: 'request failed',
  }]
  const columns = Object.keys(data[0]).map(name => ({ name, type: 'String' }))
  const app = createApp({ render: () => h(DataTable, {
    columns, data, severityField,
    regexHighlights: { event_type: { pattern: 'FAILURE', isNegated: false } },
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 64 },
    sourceId: 'outcome-test', teamId: null,
  }) })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const failure = host.querySelector('.outcome-failure')
    expect(failure?.textContent).toBe('_JOB.FAILURE')
    expect(failure?.getAttribute('title')).toBe('_JOB.FAILURE')
    expect(failure?.querySelector('.search-highlight')?.textContent).toBe('FAILURE')
    expect(host.querySelector('.outcome-pending')?.textContent).toBe('PENDING')
    expect(host.querySelector('.outcome-success')?.textContent).toBe('SUCCESSFUL')
    expect(host.querySelector('.outcome-success')?.getAttribute('title')).toBe('SUCCESSFUL')
    expect(host.querySelector('.outcome-success')?.classList.contains('outcome-badge')).toBe(false)
    expect(host.querySelectorAll('.outcome-badge')).toHaveLength(2)
    expect(host.querySelector('span[title="request failed"]')?.classList.contains('font-mono')).toBe(false)
    expect(host.querySelector('.status-code.status-server')?.textContent).toBe('503')
    const severity = [...host.querySelectorAll('tbody span')].find(node => node.textContent === 'error')
    for (const token of getStandardSeverityClasses('error')?.split(' ') ?? []) {
      expect(severity?.classList.contains(token)).toBe(true)
    }
  } finally {
    app.unmount()
    host.remove()
  }
})

it.each([
  'error-reporter', 'payment-timeout-worker', 'dlq.failed-events',
  'invalid-cache-sweeper', 'cancelled-orders-svc',
])('keeps service %s neutral and preserves raw cell actions', async value => {
  const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue(undefined)
  const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  const onDrillDown = vi.fn()
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [
      { name: 'timestamp', type: 'DateTime' },
      { name: 'service', type: 'String' },
      { name: 'status', type: 'String' },
    ],
    data: [{ timestamp: '2026-01-01T00:00:00Z', service: value, status: value }],
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 64 },
    sourceId: `outcome-service-${value}`, teamId: null, onDrillDown,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    for (const columnName of ['service', 'status']) {
      const cell = host.querySelector<HTMLTableCellElement>(`td[data-cell-id="0_${columnName}"]`)
      if (!cell) throw new Error(`${columnName} cell was not rendered`)
      const content = cell.querySelector('.flex-render-content')
      expect(content?.textContent).toBe(value)
      expect(content?.getAttribute('title')).toBe(value)
      expect(content?.classList.contains('outcome-failure')).toBe(columnName === 'status')
      expect(content?.classList.contains('outcome-badge')).toBe(columnName === 'status')

      cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      await nextTick()
      const buttons = cell.querySelectorAll<HTMLButtonElement>('.cell-actions button')
      expect(buttons).toHaveLength(3)
      buttons[0]?.click()
      await nextTick()
      expect(writeText).toHaveBeenLastCalledWith(value)
      buttons[1]?.click()
      expect(onDrillDown).toHaveBeenLastCalledWith({ column: columnName, value, operator: '=' })
      buttons[2]?.click()
      expect(onDrillDown).toHaveBeenLastCalledWith({ column: columnName, value, operator: '!=' })
    }
  } finally {
    app.unmount()
    host.remove()
    if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
    else Reflect.deleteProperty(navigator, 'clipboard')
  }
})

it.each([
  ['FAILED', 'outcome-failure'], ['PENDING', 'outcome-pending'],
  ['error', 'bg-red-500'], ['warn', 'bg-yellow-400'], ['info', 'bg-blue-500'],
  ['debug', 'bg-gray-300'], ['UNKNOWN', 'bg-gray-300'],
  ['notice', 'bg-blue-500'], ['err', 'bg-red-500'], ['crit', 'bg-red-700'],
  ['alert', 'bg-red-700'], ['emerg', 'bg-red-700'], ['verbose', 'bg-gray-300'],
  ['WARNING', 'bg-yellow-400'], [' Warn ', 'bg-yellow-400'],
])('renders configured record_status value %s with its own tone', async (value, className) => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: 'record_status', type: 'String' }],
    data: [{ record_status: value }], severityField: 'record_status',
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 20 },
    sourceId: 'custom-severity', teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const cell = host.querySelector('td[data-cell-id] .flex-render-content')
    expect(cell?.textContent).toBe(value)
    if (className) expect(cell?.classList.contains(className)).toBe(true)
    else expect(cell?.classList.contains('outcome-badge')).toBe(false)
    if (['error', 'warn', 'info', 'debug'].includes(value)) {
      expect(cell?.classList.contains('outcome-badge')).toBe(false)
    }
  } finally {
    app.unmount()
    host.remove()
  }
})
