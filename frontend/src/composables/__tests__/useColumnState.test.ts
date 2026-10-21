import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, reactive, type EffectScope } from 'vue'
import type { Source } from '@/api/sources'
import type { ColumnDef } from '@/views/explore/table/tableFeatures'
import { COLUMN_VISIBILITY_VERSION } from '@/views/explore/table/defaultColumnVisibility'
import { useColumnState } from '../useColumnState'

const storageKey = 'logchef-tableState-7-source-a'
const scopes: EffectScope[] = []

function setup(source: Pick<Source, 'source_type'> | null = { source_type: 'clickhouse' }) {
  const input = reactive<{
    columns: ColumnDef<Record<string, unknown>>[]
    teamId: number | null
    sourceId: string
    source: Pick<Source, 'source_type'> | null
  }>({
    columns: [{ id: 'message', size: 240 }, { id: 'timestamp', size: 220 }, { id: 'status' }],
    teamId: 7,
    sourceId: 'source-a',
    source,
  })
  const scope = effectScope()
  scopes.push(scope)
  const state = scope.run(() => useColumnState({
    columns: () => input.columns,
    teamId: () => input.teamId,
    sourceId: () => input.sourceId,
    source: () => input.source,
    timestampField: 'timestamp',
    severityField: 'severity',
    defaultSize: 150,
  }))
  if (!state) throw new Error('Column state scope did not run')
  return { input, ...state }
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
  it('starts with timestamp first, defined widths and source defaults', () => {
    const state = setup()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    expect(state.columnSizing.value).toEqual({ timestamp: 220, message: 240, status: 150 })
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
    first.columnOrder.value = ['timestamp', 'status', 'message']
    first.columnSizing.value.status = 312
    first.columnVisibility.value.message = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(299)
    expect(localStorage.getItem(storageKey)).toBeNull()
    await vi.advanceTimersByTimeAsync(1)
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}')).toEqual({
      columnOrder: ['timestamp', 'status', 'message'],
      columnSizing: { timestamp: 220, message: 240, status: 312 },
      columnVisibility: { timestamp: true, message: false, status: true },
      visibilityVersion: COLUMN_VISIBILITY_VERSION,
    })
    const second = setup()
    expect(second.columnOrder.value).toEqual(first.columnOrder.value)
    expect(second.columnSizing.value).toEqual(first.columnSizing.value)
    expect(second.columnVisibility.value).toEqual(first.columnVisibility.value)
  })

  it('retains saved entries absent from the current result and appends new fields', () => {
    localStorage.setItem(storageKey, JSON.stringify({
      columnOrder: ['status', 'absent', 'timestamp'],
      columnSizing: { absent: 400, status: 90 },
      columnVisibility: { absent: false, status: false },
    }))
    const state = setup()
    expect(state.columnOrder.value).toEqual(['timestamp', 'status', 'absent', 'message'])
    expect(state.columnSizing.value).toEqual({ absent: 400, status: 90, message: 240, timestamp: 220 })
    expect(state.columnVisibility.value).toEqual({ absent: false, status: false, timestamp: true, message: true })
  })

  it.each([undefined, 1])('migrates legacy schemaless visibility version %s without dropping order or widths', async (visibilityVersion) => {
    localStorage.setItem(storageKey, JSON.stringify({
      columnOrder: ['message', 'status', 'timestamp', 'absent'],
      columnSizing: { status: 333, absent: 400 },
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
    expect(localStorage.getItem(storageKey)).toBe(saved)
    state.input.source = { source_type: 'victorialogs' }
    await persist()
    expect(state.columnVisibility.value.message).toBe(true)
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').visibilityVersion).toBe(COLUMN_VISIBILITY_VERSION)
  })

  it('resets saved order, widths and visibility to defaults', async () => {
    const state = setup()
    state.columnSizing.value.status = 500
    state.columnVisibility.value.message = false
    state.columnOrder.value = ['timestamp', 'status', 'message']
    await persist()
    state.resetTableStateToDefaults()
    expect(localStorage.getItem(storageKey)).toBeNull()
    expect(state.columnOrder.value).toEqual(['timestamp', 'message', 'status'])
    expect(state.columnSizing.value.status).toBe(150)
    expect(state.columnVisibility.value.message).toBe(true)
    await persist()
    expect(setup().columnSizing.value.status).toBe(150)
  })

  it('uses separate keys for team and source, including team zero', async () => {
    const state = setup()
    state.columnSizing.value.status = 450
    await persist()
    state.input.teamId = 0
    state.input.sourceId = 'source-b'
    await nextTick()
    expect(state.columnSizing.value.status).toBe(150)
    state.columnSizing.value.status = 250
    await persist()
    expect(JSON.parse(localStorage.getItem('logchef-tableState-0-source-b') ?? '{}').columnSizing.status).toBe(250)
    expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing.status).toBe(450)
    state.input.teamId = 7
    state.input.sourceId = 'source-a'
    await nextTick()
    expect(state.columnSizing.value.status).toBe(450)
  })

  it('does not write without a team, source or columns', async () => {
    const state = setup()
    state.input.teamId = null
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
    expect(state.columnSizing.value.status).toBe(150)
    expect(state.columnVisibility.value.message).toBe(true)
  })

  it.each(['{broken', 'null', '{"columnOrder":"message"}', '{"columnSizing":{"status":"wide"}}'])('falls back to defaults for invalid saved state %s', (saved) => {
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
