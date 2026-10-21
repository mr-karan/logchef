import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ref } from 'vue'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { api } from '@/api/config'
import { useFieldValuesStore } from '@/stores/exploreFieldValues'
import { useFieldValuesLoader, type LoaderOptions } from '../useFieldValuesLoader'

const originalAdapter = api.defaults.adapter
let requests: Array<{ config: InternalAxiosRequestConfig; complete: (value?: string) => void; fail: () => void }>
let active: number
let maxActive: number
let ignoreAbort: boolean

beforeEach(() => {
  setActivePinia(createPinia())
  requests = []
  active = 0
  maxActive = 0
  ignoreAbort = false
  api.defaults.adapter = (config) => {
    active++
    maxActive = Math.max(maxActive, active)
    return new Promise<AxiosResponse>((resolve, reject) => {
      requests.push({
        config,
        complete(value = 'ok') {
          resolve({ config, status: 200, statusText: 'OK', headers: {}, data: {
            status: 'success', data: {
              field_name: 'level', field_type: 'String', total_distinct: 1,
              is_low_cardinality: true, values: [{ value, count: 1 }],
            },
          } })
        },
        fail() { reject(new Error('network failure')) },
      })
      config.signal?.addEventListener?.('abort', () => {
        if (!ignoreAbort) reject(new DOMException('aborted', 'AbortError'))
      })
    }).finally(() => { active-- })
  }
})

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

function loaderOptions() {
  return ref<LoaderOptions>({
    teamId: 1, sourceId: 2,
    getTimeRange: () => ({ startTime: '2026-01-01T00:00:00Z', endTime: '2026-01-01T01:00:00Z' }),
    getFilterQuery: () => 'level="error"',
    getFilterQueryLanguage: () => 'logchefql',
    getFilterVariables: () => [],
  })
}

describe('field values loader', () => {
  it.each(['clearCache', 'cancelAll'] as const)('%s settles running and queued loads', async (cancel) => {
    const loader = useFieldValuesLoader(loaderOptions())
    const loads = Array.from({ length: 12 }, (_, i) => loader.loadField(`field${i}`, 'Int64'))
    await vi.waitFor(() => expect(requests).toHaveLength(4))
    loader[cancel]()
    let settled = false
    void Promise.all(loads).then(() => { settled = true })
    await vi.waitFor(() => expect(settled).toBe(true), { timeout: 1000 })
    expect(requests).toHaveLength(4)
    expect(loader.isAnyLoading.value).toBe(false)
    if (cancel === 'clearCache') expect(loader.fieldStates.value.size).toBe(0)
    expect(useFieldValuesStore().entries.size).toBe(0)
  })

  it('shares four slots across progressive batches and individual loads', async () => {
    const loader = useFieldValuesLoader(loaderOptions())
    loader.loadPriorityFields(Array.from({ length: 8 }, (_, i) => ({ name: `field${i}`, type: 'Int64' })))
    await vi.waitFor(() => expect(requests).toHaveLength(4))
    const clicked = loader.loadField('message', 'String')
    loader.loadPriorityFields([{ name: 'additional', type: 'Int64' }])
    expect(requests).toHaveLength(4)
    for (let i = 0; i < 10; i++) {
      await vi.waitFor(() => expect(requests.length).toBeGreaterThan(i))
      requests[i].complete()
    }
    await clicked
    await vi.waitFor(() => expect(Object.keys(loader.fieldValues.value)).toHaveLength(10))
    expect(maxActive).toBe(4)
    expect(loader.isAnyLoading.value).toBe(false)
    loader.loadPriorityFields([{ name: 'field0', type: 'Int64' }])
    expect(requests).toHaveLength(10)
  })

  it('ignores obsolete responses and failures after a clear and reload', async () => {
    ignoreAbort = true
    const loader = useFieldValuesLoader(loaderOptions())
    const previous = loader.loadField('level', 'String')
    const failed = loader.loadField('old', 'String')
    await vi.waitFor(() => expect(requests).toHaveLength(2))
    loader.clearCache()
    const current = loader.loadField('level', 'String')
    await vi.waitFor(() => expect(requests).toHaveLength(3))
    requests[2].complete('current')
    await current
    requests[0].complete('obsolete')
    requests[1].fail()
    await Promise.all([previous, failed])
    expect(loader.getFieldState('old')).toEqual({ status: 'idle' })
    expect(loader.fieldValues.value.level.values[0].value).toBe('current')
    expect(useFieldValuesStore().findCachedValues(2, 'level')?.values[0].value).toBe('current')
  })

  it('replaces a queued load for the same field without sending the obsolete request', async () => {
    const loader = useFieldValuesLoader(loaderOptions())
    const busy = Array.from({ length: 4 }, (_, i) => loader.loadField(`busy${i}`, 'Int64'))
    const obsolete = loader.loadField('level', 'String')
    const latest = loader.loadField('level', 'String')
    await vi.waitFor(() => expect(requests).toHaveLength(4))
    requests.slice(0, 4).forEach(request => request.complete())
    await vi.waitFor(() => expect(requests).toHaveLength(5))
    requests[4].complete()
    await Promise.all([...busy, obsolete, latest])
    expect(requests).toHaveLength(5)
    expect(loader.getFieldState('level').status).toBe('loaded')
  })
})
