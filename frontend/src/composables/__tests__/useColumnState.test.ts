import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, reactive, type EffectScope } from 'vue'
import type { Source } from '@/api/sources'
import type { ColumnDef } from '@/views/explore/table/tableFeatures'
import { COLUMN_VISIBILITY_VERSION } from '@/views/explore/table/defaultColumnVisibility'
import { getDefaultColumnOrder } from '@/views/explore/table/defaultColumnOrder'
import { COLUMN_SIZING_VERSION, useColumnState } from '../useColumnState'

const storageKey = 'logchef-tableState-7-source-a'
const scopes: EffectScope[] = []

function setup(source: Pick<Source, 'source_type'> | null = { source_type: 'clickhouse' }, teamId: number | null = 7) {
  const input = reactive<{
    columns: ColumnDef<Record<string, unknown>>[]
    defaultOrder?: string[]
    teamId: number | null
    sourceId: string
    source: Pick<Source, 'source_type'> | null
  }>({
    columns: [{ id: 'message', size: 240 }, { id: 'timestamp', size: 220 }, { id: 'status' }],
    teamId,
    sourceId: 'source-a',
    source,
  })
  const scope = effectScope()
  scopes.push(scope)
  const state = scope.run(() => useColumnState({
    columns: () => input.columns,
    defaultOrder: () => {
      const defaults = getDefaultColumnOrder(input.columns, [{ message: 'hello', timestamp: '2026-01-01', status: 'ok' }])
      return { ...defaults, order: input.defaultOrder ?? defaults.order }
    },
    teamId: () => input.teamId,
    sourceId: () => input.sourceId,
    source: () => input.source,
    timestampField: 'timestamp',
    severityField: 'severity',
  }))
  if (!state) throw new Error('Column state scope did not run')
  return { input, scope, ...state }
}

async function persist() {
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
}

beforeEach(() => {
  localStorage.clear()
  vi.useFakeTimers()
})

afterEach(() => {
  scopes.splice(0).forEach(scope => scope.stop())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('useColumnState', () => {
  it.each(['failed storage', 'same tick'])('retains current widths and visibility on a result change with %s', async scenario => {
    const state = setup()
    await persist()
    if (scenario === 'failed storage') {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      vi.spyOn(localStorage, 'setItem').mockImplementation(() => { throw new Error('Storage full') })
    }
    if (scenario === 'same tick') state.input.columns = [...state.input.columns]
    state.columnSizing.value.message = 456
    state.columnVisibility.value.message = false
    if (scenario === 'failed storage') await persist()
    state.input.defaultOrder = ['timestamp', 'status', 'message']
    state.input.columns = [...state.input.columns]
    await nextTick()
    expect(state.columnSizing.value.message).toBe(456)
    expect(state.columnVisibility.value.message).toBe(false)
  })

  it('does not write unchanged classes when a result changes', async () => {
    const state = setup()
    await persist()
    const write = vi.spyOn(localStorage, 'setItem')
    state.input.columns = [...state.input.columns]
    await persist()
    expect(write).not.toHaveBeenCalled()
  })

  it('starts with timestamp first, no user widths and source defaults', () => {
    const state = setup()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    expect(state.columnSizing.value).toEqual({})
    expect(state.columnVisibility.value).toEqual({ timestamp: true, message: true, status: true })
  })

  it('keeps the timestamp first for direct and functional reorder updates', () => {
    const state = setup()
    state.setColumnOrder(['message', 'status', 'timestamp'])
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    state.setColumnOrder(order => [...order].reverse())
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'message'])
    state.setColumnOrder(['message', 'status'])
    expect(state.columnOrder.value).toEqual(['message', 'status'])
  })

  it('debounces edits and restores them on the next instance', async () => {
    const first = setup()
    first.setColumnOrder(['timestamp', 'status', 'message'])
    first.columnSizing.value.status = 312
    first.columnVisibility.value.message = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(299)
    expect(localStorage.getItem(storageKey)).toBeNull()
    await vi.advanceTimersByTimeAsync(1)
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}')).toEqual({
      columnOrder: ['timestamp', 'status', 'message'],
      orderSource: 'explicit',
      columnClasses: { message: 'readable', timestamp: 'readable', status: 'readable' },
      columnSizing: { status: 312 },
      columnVisibility: { timestamp: true, message: false, status: true },
      visibilityVersion: COLUMN_VISIBILITY_VERSION,
      sizingVersion: COLUMN_SIZING_VERSION,
    })
    const second = setup()
    expect(second.columnOrder.value).toEqual(first.columnOrder.value)
    expect(second.columnSizing.value).toEqual(first.columnSizing.value)
    expect(second.columnVisibility.value).toEqual(first.columnVisibility.value)
  })

  it('persists a resize when the scope stops before the debounce expires', async () => {
    const first = setup()
    first.columnSizing.value.status = 312
    await nextTick()
    await vi.advanceTimersByTimeAsync(100)
    first.scope.stop()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing?.status).toBe(312)
    expect(setup().columnSizing.value.status).toBe(312)
  })

  it('flushes the old key before restoring state for a new key', async () => {
    const state = setup()
    state.columnSizing.value.status = 312
    await nextTick()
    state.input.sourceId = 'source-b'
    await nextTick()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing?.status).toBe(312)
    expect(state.columnSizing.value.status).toBeUndefined()
    state.columnSizing.value.status = 450
    await persist()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing.status).toBe(312)
    expect(JSON.parse(localStorage.getItem('logchef-tableState-7-source-b') ?? '{}').columnSizing.status).toBe(450)
    state.input.sourceId = 'source-a'
    await nextTick()
    expect(state.columnSizing.value.status).toBe(312)
  })

  it('retains saved entries absent from the current result and appends new fields', () => {
    localStorage.setItem(storageKey, JSON.stringify({
      columnOrder: ['status', 'absent', 'timestamp'],
      columnSizing: { absent: 400, status: 90 },
      sizingVersion: COLUMN_SIZING_VERSION,
      columnVisibility: { absent: false, status: false },
    }))
    const state = setup()
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'absent', 'message'])
    expect(state.columnSizing.value).toEqual({ absent: 400, status: 90 })
    expect(state.columnVisibility.value).toEqual({ absent: false, status: false, timestamp: true, message: true })
  })

  it.each([undefined, 1])('migrates legacy schemaless visibility version %s without dropping order or widths', async (visibilityVersion) => {
    localStorage.setItem(storageKey, JSON.stringify({
      columnOrder: ['message', 'status', 'timestamp', 'absent'],
      columnSizing: { status: 333, absent: 400 },
      sizingVersion: COLUMN_SIZING_VERSION,
      columnVisibility: { timestamp: true, message: false, status: false, absent: true },
      visibilityVersion,
    }))
    const state = setup({ source_type: 'victorialogs' })
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status', 'absent'])
    expect(state.columnSizing.value.status).toBe(333)
    expect(state.columnSizing.value.absent).toBe(400)
    expect(state.columnVisibility.value).toEqual({ message: true, timestamp: true, status: true })
    state.columnSizing.value.status = 334
    await persist()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').visibilityVersion).toBe(COLUMN_VISIBILITY_VERSION)
  })

  it('keeps versioned schemaless visibility choices and caps newly seen columns', async () => {
    localStorage.setItem(storageKey, JSON.stringify({
      columnOrder: ['timestamp', 'message', 'status', 'a', 'b', 'c'],
      columnVisibility: { timestamp: true, message: true, status: true, a: true, b: true, c: true },
      visibilityVersion: COLUMN_VISIBILITY_VERSION,
    }))
    const state = setup({ source_type: 'victorialogs' })
    state.input.columns = [...state.input.columns, { id: 'new-field' }]
    await nextTick()
    expect(state.columnVisibility.value['new-field']).toBe(false)
    expect(state.columnVisibility.value.a).toBe(true)
  })

  it('waits for source metadata before persisting a visibility migration', async () => {
    const saved = JSON.stringify({ columnOrder: ['timestamp', 'message'], columnVisibility: { message: false } })
    localStorage.setItem(storageKey, saved)
    const state = setup(null)
    state.columnSizing.value.timestamp = 500
    await persist()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnVisibility).toEqual({ message: false })
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').visibilityVersion).toBeUndefined()
    state.input.source = { source_type: 'victorialogs' }
    await persist()
    expect(state.columnVisibility.value.message).toBe(true)
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').visibilityVersion).toBe(COLUMN_VISIBILITY_VERSION)
  })

  it('drops legacy widths while retaining order and visibility, then saves only user widths', async () => {
    localStorage.setItem(storageKey, JSON.stringify({
      columnOrder: ['timestamp', 'status', 'message'],
      columnSizing: { timestamp: 220, status: 90, message: 240 },
      columnVisibility: { timestamp: true, status: false, message: true },
      visibilityVersion: COLUMN_VISIBILITY_VERSION,
    }))
    const state = setup()
    expect(state.columnSizing.value).toEqual({})
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'message'])
    expect(state.columnVisibility.value.status).toBe(false)
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing).toEqual({})
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').sizingVersion).toBe(COLUMN_SIZING_VERSION)
    state.columnSizing.value.status = 312
    await persist()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing).toEqual({ status: 312 })
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').sizingVersion).toBe(COLUMN_SIZING_VERSION)
    expect(setup().columnSizing.value).toEqual({ status: 312 })
  })

  it('retains a pending user width when a new result changes the columns', async () => {
    const state = setup()
    state.columnSizing.value.status = 312
    await nextTick()
    state.input.columns = [{ id: 'timestamp', size: 300 }, { id: 'status', size: 80 }, { id: 'new-field' }]
    await nextTick()
    expect(state.columnSizing.value).toEqual({ status: 312 })
    await persist()
    expect(setup().columnSizing.value).toEqual({ status: 312 })
  })

  it('resets saved order, widths and visibility to defaults', async () => {
    const state = setup()
    state.columnSizing.value.status = 500
    state.columnVisibility.value.message = false
    state.setColumnOrder(['timestamp', 'status', 'message'])
    await persist()
    state.resetTableStateToDefaults()
    expect(localStorage.getItem(storageKey)).toBeNull()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    expect(state.columnSizing.value.status).toBeUndefined()
    expect(state.columnVisibility.value.message).toBe(true)
    await persist()
    expect(setup().columnSizing.value.status).toBeUndefined()
  })

  it('uses separate keys for team and source, including team zero', async () => {
    const state = setup()
    state.columnSizing.value.status = 450
    await persist()
    state.input.teamId = 0
    state.input.sourceId = 'source-b'
    await nextTick()
    expect(state.columnSizing.value.status).toBeUndefined()
    state.columnSizing.value.status = 250
    await persist()
    expect(JSON.parse(localStorage.getItem('logchef-tableState-0-source-b') ?? '{}').columnSizing.status).toBe(250)
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing.status).toBe(450)
    state.input.teamId = 7
    state.input.sourceId = 'source-a'
    await nextTick()
    expect(state.columnSizing.value.status).toBe(450)
  })

  it('restores automatic order after persistence without changing widths or visibility', async () => {
    const first = setup()
    first.columnSizing.value.status = 312
    first.columnVisibility.value.message = false
    await persist()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').orderSource).toBe('auto')

    const second = setup()
    second.input.defaultOrder = ['timestamp', 'status', 'message']
    await nextTick()
    expect(second.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    expect(second.columnSizing.value.status).toBe(312)
    expect(second.columnVisibility.value.message).toBe(false)
    await persist()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').orderSource).toBe('auto')
  })

  it('keeps explicit order when defaults change before and after reload', async () => {
    const first = setup()
    first.setColumnOrder(['timestamp', 'status', 'message'])
    first.input.defaultOrder = ['timestamp', 'message', 'status']
    await persist()
    expect(first.columnOrder.value).toEqual(['timestamp', 'status', 'message'])

    const second = setup()
    second.input.defaultOrder = ['timestamp', 'message', 'status']
    await nextTick()
    expect(second.columnOrder.value).toEqual(['timestamp', 'status', 'message'])
  })

  it('keeps a legacy order when automatic defaults change', async () => {
    localStorage.setItem(storageKey, JSON.stringify({ columnOrder: ['timestamp', 'status', 'message'] }))
    const state = setup()
    state.input.defaultOrder = ['timestamp', 'message', 'status']
    await nextTick()
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'message'])
  })

  it('keeps manual order while source metadata and persistence are unavailable', async () => {
    const state = setup(null)
    state.setColumnOrder(['timestamp', 'status', 'message'])
    state.input.defaultOrder = ['timestamp', 'message', 'status']
    await nextTick()
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'message'])
    expect(localStorage.getItem(storageKey)).toBeNull()
  })

  it('tracks explicit order separately for each source and returns to automatic order on reset', async () => {
    const state = setup()
    state.setColumnOrder(['timestamp', 'status', 'message'])
    await persist()
    state.input.sourceId = 'source-b'
    state.input.defaultOrder = ['timestamp', 'message', 'status']
    await nextTick()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    state.input.defaultOrder = ['timestamp', 'status', 'message']
    await nextTick()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])

    state.input.sourceId = 'source-a'
    state.input.defaultOrder = ['timestamp', 'message', 'status']
    await nextTick()
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'message'])
    state.resetTableStateToDefaults()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    state.input.defaultOrder = ['timestamp', 'status', 'message']
    await persist()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').orderSource).toBe('auto')
  })

  it('persists reset defaults even when the order does not change', async () => {
    const state = setup()
    await persist()
    state.resetTableStateToDefaults()
    await persist()
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnOrder).toEqual(['timestamp', 'message', 'status'])
    const reloaded = setup()
    reloaded.input.defaultOrder = ['timestamp', 'status', 'message']
    await nextTick()
    expect(reloaded.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
  })

  it('does not resurrect a pending explicit order when reset is followed by immediate disposal', async () => {
    const state = setup()
    state.setColumnOrder(['timestamp', 'status', 'message'])
    await nextTick()
    state.resetTableStateToDefaults()
    state.scope.stop()
    expect(setup().columnOrder.value).toEqual(['timestamp', 'message', 'status'])
  })

  it('does not write without a team, source or columns', async () => {
    const state = setup(undefined, null)
    await nextTick()
    state.columnSizing.value.status = 500
    await persist()
    expect(localStorage.length).toBe(0)
    state.input.teamId = 7
    state.input.sourceId = ''
    await nextTick()
    state.columnSizing.value.status = 501
    await persist()
    expect(localStorage.length).toBe(0)
    state.input.sourceId = 'source-a'
    state.input.columns = []
    await persist()
    expect(state.columnOrder.value).toEqual([])
    expect(state.columnSizing.value).toEqual({})
    expect(state.columnVisibility.value).toEqual({})
    expect(localStorage.length).toBe(0)
  })

  it('accepts an old order-only shape with default widths and visibility', () => {
    localStorage.setItem(storageKey, JSON.stringify({ columnOrder: ['status', 'timestamp', 'message'] }))
    const state = setup()
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'message'])
    expect(state.columnSizing.value.status).toBeUndefined()
    expect(state.columnVisibility.value.message).toBe(true)
  })

  it.each(['{broken', 'null', '{"columnOrder":"message"}', '{"columnOrder":["status","timestamp","message"],"columnSizing":{"status":"wide"}}'])('falls back to defaults for invalid saved state %s', (saved) => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    localStorage.setItem(storageKey, saved)
    expect(setup().columnOrder.value).toEqual(['timestamp', 'message', 'status'])
  })

  it('keeps usable in-memory state when storage is unavailable', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(localStorage, 'getItem').mockImplementation(() => { throw new Error('Storage unavailable') })
    vi.spyOn(localStorage, 'setItem').mockImplementation(() => { throw new Error('Storage full') })
    const state = setup()
    state.columnSizing.value.status = 500
    await persist()
    expect(state.columnSizing.value.status).toBe(500)
  })
})

it('restores widths and visibility when the saved order is empty', () => {
  localStorage.setItem(storageKey, JSON.stringify({
    columnOrder: [], columnSizing: { status: 312 },
    columnVisibility: { message: false },
    sizingVersion: COLUMN_SIZING_VERSION, visibilityVersion: COLUMN_VISIBILITY_VERSION,
  }))
  const state = setup()
  expect(state.columnSizing.value.status).toBe(312)
  expect(state.columnVisibility.value.message).toBe(false)
  expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
})

it('retains the complete automatic order through subset results and reloads', async () => {
  const state = setup()
  const full = [...state.input.columns]
  const original = [...state.columnOrder.value]
  await persist()
  state.input.columns = full.filter(column => column.id !== 'message')
  await persist()
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnOrder).toEqual(original)
  state.input.columns = full
  await persist()
  expect(state.columnOrder.value).toEqual(original)
  expect(setup().columnOrder.value).toEqual(original)
})
