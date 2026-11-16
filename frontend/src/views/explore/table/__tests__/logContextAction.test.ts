import { createApp, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { describe, expect, it } from 'vitest'
import DataTable from '../data-table.vue'
import { rowStreamLabels } from '@/lib/logContext'

const lokiSource = {
  source_type: 'loki',
  capabilities: ['log_context'],
  columns: [
    { name: '_time', type: 'DateTime' },
    { name: '_msg', type: 'String' },
    { name: 'app', type: 'label' },
    { name: 'pod', type: 'label' },
    { name: 'trace', type: 'string' },
  ],
}
const row = { _time: '2026-01-01T00:00:00.000000000Z', _msg: 'hello', app: 'api', pod: 'a', trace: 't1' }

async function contextButton(activeMode: string) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: '_time', type: 'DateTime' }, { name: '_msg', type: 'String' }],
    data: [row],
    sourceId: '1',
    teamId: 1,
    source: lokiSource,
    timestampField: '_time',
    activeMode,
  })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  await nextTick()
  host.querySelector<HTMLTableRowElement>('tbody tr')?.click()
  await nextTick()
  const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent?.includes('Show Context'))
  return { button, cleanup: () => { app.unmount(); host.remove() } }
}

describe('log context on a Loki row', () => {
  it('disables context on a native query row and says why', async () => {
    const { button, cleanup } = await contextButton('native')
    try {
      expect(button?.disabled).toBe(true)
      expect(button?.title).toContain('needs a LogchefQL query')
    } finally {
      cleanup()
    }
  })

  it('offers context on a LogchefQL row', async () => {
    const { button, cleanup } = await contextButton('logchefql')
    try {
      expect(button?.disabled).toBe(false)
    } finally {
      cleanup()
    }
  })

  it('sends the row value of each stream label of the schema', () => {
    expect(rowStreamLabels(lokiSource, row)).toEqual({ app: 'api', pod: 'a' })
    expect(rowStreamLabels({ source_type: 'clickhouse' }, row)).toBeUndefined()
  })
})
