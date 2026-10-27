import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { api } from '@/api/config'
import { useFieldValuesStore } from '@/stores/exploreFieldValues'
import { useCompletionFieldValues, type CompletionValuesWindow } from '../useCompletionFieldValues'

const originalAdapter = api.defaults.adapter
let requests: Array<{
  config: InternalAxiosRequestConfig
  complete: (value?: string) => void
  fail: () => void
}>

const lastHour: CompletionValuesWindow = {
  key: 'relative:1h', startTime: '2026-01-01T00:00:00Z', endTime: '2026-01-01T01:00:00Z',
}
const typed = {
  teamId: 1, sourceId: 2, fieldName: 'method', fieldType: 'LowCardinality(String)',
  getWindow: () => lastHour, invoked: false,
}
const referer = { ...typed, fieldName: 'referer', fieldType: 'String' }
const live = { isCancellationRequested: false }

function requestedField(index: number) {
  return new URL(requests[index].config.url ?? '', 'http://localhost').pathname.split('/')[6]
}

beforeEach(() => {
  setActivePinia(createPinia())
  requests = []
  api.defaults.adapter = config => new Promise<AxiosResponse>((resolve, reject) => {
    requests.push({
      config,
      complete(value = 'GET') {
        resolve({ config, status: 200, statusText: 'OK', headers: {}, data: {
          status: 'success', data: {
            field_name: 'method', field_type: 'String', total_distinct: 3,
            is_low_cardinality: false, values: [{ value: 'rare', count: 1 }, { value, count: 42 }],
          },
        } })
      },
      fail() { reject(new Error('network failure')) },
    })
    config.signal?.addEventListener?.('abort', () => reject(new DOMException('aborted', 'AbortError')))
  })
})

afterEach(() => {
  api.defaults.adapter = originalAdapter
  vi.useRealTimers()
})

describe('completion field values', () => {
  it('loads a field the sidebar loads by itself while typing, then serves it from its own cache', async () => {
    const loader = useCompletionFieldValues()
    const values = loader.getValues(typed, live)
    expect(requests).toHaveLength(1)
    const url = new URL(requests[0].config.url ?? '', 'http://localhost')
    expect(url.pathname).toBe('/teams/1/sources/2/fields/method/values')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      type: 'LowCardinality(String)', limit: '10', query_language: 'logchefql',
      start_time: lastHour.startTime, end_time: lastHour.endTime,
    })
    requests[0].complete()
    expect(await values).toEqual([{ value: 'GET', count: 42 }, { value: 'rare', count: 1 }])
    expect(await loader.getValues(typed, live)).toHaveLength(2)
    expect(requests).toHaveLength(1)
    expect(useFieldValuesStore().entries.size).toBe(0)
  })

  it('looks up a click-to-load field only when the user invokes suggestions', async () => {
    const loader = useCompletionFieldValues()
    expect(await loader.getValues(referer, live)).toEqual([])
    expect(requests).toHaveLength(0)
    const invoked = loader.getValues({ ...referer, invoked: true }, live)
    expect(requests).toHaveLength(1)
    const whileTyping = loader.getValues(referer, live)
    expect(requests).toHaveLength(1)
    requests[0].complete('-')
    expect((await invoked)[0].value).toBe('-')
    expect((await whileTyping)[0].value).toBe('-')
    expect((await loader.getValues(referer, live))[0].value).toBe('-')
    expect(requests).toHaveLength(1)
  })

  it('does not look up anything without a window', async () => {
    const loader = useCompletionFieldValues()
    expect(await loader.getValues({ ...typed, getWindow: undefined, invoked: true }, live)).toEqual([])
    expect(await loader.getValues({ ...typed, getWindow: () => null, invoked: true }, live)).toEqual([])
    expect(requests).toHaveLength(0)
  })

  it('resolves the window at lookup time and keys the cache by the chosen window', async () => {
    const loader = useCompletionFieldValues()
    let window = lastHour
    const request = { ...typed, getWindow: () => window }
    const first = loader.getValues(request, live)
    requests[0].complete('first')
    await first
    window = { ...lastHour, startTime: '2026-01-01T00:05:00Z', endTime: '2026-01-01T01:05:00Z' }
    expect((await loader.getValues(request, live))[0].value).toBe('first')
    expect(requests).toHaveLength(1)
    window = { key: 'relative:15m', startTime: '2026-01-01T00:50:00Z', endTime: '2026-01-01T01:05:00Z' }
    const narrower = loader.getValues(request, live)
    expect(requests).toHaveLength(2)
    expect(new URL(requests[1].config.url ?? '', 'http://localhost').searchParams.get('start_time')).toBe(window.startTime)
    requests[1].complete('second')
    expect((await narrower)[0].value).toBe('second')
  })

  it('refreshes cached values after a minute', async () => {
    vi.useFakeTimers()
    const loader = useCompletionFieldValues()
    const first = loader.getValues(typed, live)
    requests[0].complete()
    await first
    await vi.advanceTimersByTimeAsync(59_999)
    await loader.getValues(typed, live)
    expect(requests).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    void loader.getValues(typed, live)
    expect(requests).toHaveLength(2)
  })

  it('shares one request per field while ignoring a canceled completion', async () => {
    const loader = useCompletionFieldValues()
    const stale = { isCancellationRequested: false }
    const first = loader.getValues(typed, stale)
    stale.isCancellationRequested = true
    const latest = loader.getValues(typed, live)
    expect(requests).toHaveLength(1)
    requests[0].complete()
    expect(await first).toEqual([])
    expect((await latest)[0].value).toBe('GET')
  })

  it('does not fetch for an already canceled completion', async () => {
    const loader = useCompletionFieldValues()
    expect(await loader.getValues(typed, { isCancellationRequested: true })).toEqual([])
    expect(requests).toHaveLength(0)
  })

  it('uses values that the sidebar loaded for the same source without a request', async () => {
    useFieldValuesStore().populateFromFetch(2, 'referer', {
      field_name: 'referer', field_type: 'String', total_distinct: 1,
      is_low_cardinality: false, values: [{ value: 'from sidebar', count: 3 }],
    })
    const loader = useCompletionFieldValues()
    expect((await loader.getValues(referer, live))[0].value).toBe('from sidebar')
    expect(await loader.getValues({ ...referer, sourceId: 3 }, live)).toEqual([])
    expect(requests).toHaveLength(0)
  })

  it('uses values the sidebar loads after a failed lookup', async () => {
    const loader = useCompletionFieldValues()
    const failed = loader.getValues(typed, live)
    requests[0].fail()
    expect(await failed).toEqual([])
    useFieldValuesStore().populateFromFetch(2, 'method', {
      field_name: 'method', field_type: 'String', total_distinct: 1,
      is_low_cardinality: true, values: [{ value: 'from sidebar', count: 3 }],
    })
    expect((await loader.getValues(typed, live))[0].value).toBe('from sidebar')
    expect(requests).toHaveLength(1)
  })

  it('never looks up a field without a type, even when invoked', async () => {
    const loader = useCompletionFieldValues()
    expect(await loader.getValues({ ...typed, fieldType: '', invoked: true }, live)).toEqual([])
    expect(requests).toHaveLength(0)
  })

  it('caps automatic lookups at two while an invoked lookup still runs', async () => {
    const loader = useCompletionFieldValues()
    const status = { ...typed, fieldName: 'status', fieldType: 'UInt16' }
    const method = loader.getValues(typed, live)
    void loader.getValues({ ...typed, fieldName: 'host' }, live)
    expect(await loader.getValues(status, live)).toEqual([])
    void loader.getValues({ ...referer, invoked: true }, live)
    expect(requests.map((_, index) => requestedField(index))).toEqual(['method', 'host', 'referer'])
    requests[0].complete()
    await method
    void loader.getValues(status, live)
    expect(requestedField(3)).toBe('status')
  })

  it.each([
    { name: 'times out at 1.5 seconds', settle: async () => { await vi.advanceTimersByTimeAsync(1500) } },
    { name: 'fails', settle: async () => { requests[0].fail(); await vi.advanceTimersByTimeAsync(0) } },
  ])('returns no values when a lookup $name and waits 30 seconds before retrying', async ({ settle }) => {
    vi.useFakeTimers()
    const loader = useCompletionFieldValues()
    const values = loader.getValues(typed, live)
    await settle()
    expect(await values).toEqual([])
    await vi.advanceTimersByTimeAsync(29_000)
    expect(await loader.getValues({ ...typed, invoked: true }, live)).toEqual([])
    expect(requests).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1_000)
    const retry = loader.getValues(typed, live)
    expect(requests).toHaveLength(2)
    requests[1].complete()
    expect((await retry)[0].value).toBe('GET')
  })

  it('cancels lookups for an old context without a cooldown', async () => {
    const loader = useCompletionFieldValues()
    const old = loader.getValues(typed, live)
    loader.cancelAll()
    expect(await old).toEqual([])
    expect(requests[0].config.signal?.aborted).toBe(true)
    const latest = loader.getValues(typed, live)
    expect(requests).toHaveLength(2)
    requests[1].complete('new context')
    expect((await latest)[0].value).toBe('new context')
  })
})
