import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import type { Source } from '@/api/sources'
import { controlledApi } from '@/test/controlledApi'
import { useSourcesStore } from '../sources'
import { useContextStore } from '../context'

const source: Source = {
  id: 1, name: 'Logs', source_type: 'clickhouse',
  connection: { host: 'localhost:9000', database: 'default', table_name: 'logs' },
  _meta_ts_field: 'timestamp', _meta_is_auto_created: false, ttl_days: 7,
  created_at: '', updated_at: '', is_connected: true,
  columns: [{ name: 'timestamp', type: 'DateTime' }],
}

describe('source update caches', () => {
  let pinia: ReturnType<typeof createPinia>
  let network: ReturnType<typeof controlledApi>
  beforeEach(() => {
    localStorage.clear()
    pinia = createPinia()
    setActivePinia(pinia)
    network = controlledApi()
  })
  afterEach(() => { disposePinia(pinia); network.restore() })

  it('retains updated source records and evicts only derived inspection data', async () => {
    const store = useSourcesStore()
    const other = { ...source, id: 2, name: 'Other logs' }
    const original = { ...source, description: 'Old description' }
    store.sources.push(original, other)
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(1)
    store.holdResolved(1, [original, other], original, store.getMetadataGeneration())
    const inspection = store.getSourceInspection(1)
    const activity = store.getSourceActivity(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[0].succeed({ status: 'ready' })
    network.requests[1].succeed({ status: 'ready' })
    await Promise.all([inspection, activity])
    expect(store.getSourceInspectionById(1)).toBeDefined()
    expect(store.getSourceActivityById(1)).toBeDefined()

    const updated = { ...source, name: 'Renamed logs', _meta_ts_field: 'event_time' }
    const pending = store.updateSource(1, { name: updated.name, meta_ts_field: updated._meta_ts_field })
    await vi.waitFor(() => expect(network.requests).toHaveLength(3))
    network.requests[2].succeed(updated)
    expect((await pending).success).toBe(true)

    expect(store.getSourceById(1)).toEqual(updated)
    expect(store.getTeamSourceById(1)).toEqual(updated)
    expect(store.currentSourceDetails).toEqual(updated)
    expect(store.getSourceById(2)).toEqual(other)
    expect(store.getTeamSourceById(2)).toEqual(other)
    expect(store.getSourceInspectionById(1)).toBeUndefined()
    expect(store.getSourceActivityById(1)).toBeUndefined()
  })
})
