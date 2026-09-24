import { describe, expect, it } from 'vitest'
import { COLUMN_VISIBILITY_VERSION, getTrustedSavedVisibility, resolveColumnVisibility } from '../defaultColumnVisibility'

const victoriaLogs = { source_type: 'victorialogs' }
const clickhouse = { source_type: 'clickhouse' }

function columnsNamed(...names: string[]) {
  return names.map(name => ({ id: name }))
}

function visibleIds(visibility: Record<string, boolean>): string[] {
  return Object.keys(visibility).filter(id => visibility[id])
}

describe('resolveColumnVisibility', () => {
  it('keeps saved choices and hides new schemaless columns once the cap is reached', () => {
    const saved = { _time: true, _msg: true, level: true, service: true, host: true, pod: true }
    const columns = columnsNamed('_time', '_msg', 'level', 'service', 'host', 'pod', 'trace_id', 'span_id', 'user_id')

    const visibility = resolveColumnVisibility(columns, victoriaLogs, saved, '_time', 'level')

    expect(visibleIds(visibility)).toEqual(['_time', '_msg', 'level', 'service', 'host', 'pod'])
    expect(visibility.trace_id).toBe(false)
    expect(visibility.span_id).toBe(false)
    expect(visibility.user_id).toBe(false)
  })

  it('lets new schemaless columns take their default while under the cap', () => {
    const saved = { _time: true, _msg: true }
    const columns = columnsNamed('_time', '_msg', '_stream', 'level', 'service', 'trace_id', 'span_id', 'a', 'b', 'c')

    const visibility = resolveColumnVisibility(columns, victoriaLogs, saved, '_time', 'level')

    expect(visibleIds(visibility)).toHaveLength(6)
    expect(visibility.level).toBe(true)
    expect(visibility.service).toBe(true)
    expect(visibility._stream).toBe(false)
  })

  it('does not exceed the cap across repeated queries with disjoint field sets', () => {
    let saved: Record<string, boolean> = {}
    for (let query = 0; query < 40; query++) {
      const fields = Array.from({ length: 5 }, (_, i) => `field_${query}_${i}`)
      saved = resolveColumnVisibility(columnsNamed('_time', '_msg', ...fields), victoriaLogs, saved, '_time')
    }

    expect(Object.keys(saved).length).toBeGreaterThan(200)
    expect(visibleIds(saved)).toHaveLength(6)
  })

  it('never overrides an explicit saved hide, even for default columns', () => {
    const saved = { _time: true, _msg: false }
    const visibility = resolveColumnVisibility(columnsNamed('_time', '_msg', 'level'), victoriaLogs, saved, '_time')

    expect(visibility._msg).toBe(false)
    expect(visibility.level).toBe(true)
  })

  it('retains saved entries for columns absent from the current result', () => {
    const saved = { timestamp: true, hidden_elsewhere: false }
    const visibility = resolveColumnVisibility(columnsNamed('timestamp', 'body'), clickhouse, saved, 'timestamp')

    expect(visibility).toEqual({ timestamp: true, hidden_elsewhere: false, body: true })
  })

  it('shows new columns for fixed-schema sources regardless of count', () => {
    const saved = { timestamp: true }
    const columns = columnsNamed('timestamp', ...Array.from({ length: 30 }, (_, i) => `col_${i}`))

    const visibility = resolveColumnVisibility(columns, clickhouse, saved, 'timestamp')

    expect(visibleIds(visibility)).toHaveLength(31)
  })
})

describe('getTrustedSavedVisibility', () => {
  const saved = { a: true, b: true, c: false }

  it('discards unversioned schemaless visibility accumulated by older versions', () => {
    expect(getTrustedSavedVisibility(victoriaLogs, saved, undefined)).toEqual({})
  })

  it('keeps schemaless visibility saved by the current version', () => {
    expect(getTrustedSavedVisibility(victoriaLogs, saved, COLUMN_VISIBILITY_VERSION)).toEqual(saved)
  })

  it('keeps fixed-schema visibility regardless of version', () => {
    expect(getTrustedSavedVisibility(clickhouse, saved, undefined)).toEqual(saved)
  })
})
