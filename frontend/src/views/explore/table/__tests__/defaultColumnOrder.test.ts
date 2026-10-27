import { describe, expect, it } from 'vitest'
import { getDefaultColumnOrder } from '../defaultColumnOrder'

describe('getDefaultColumnOrder', () => {
  it('stably defers opaque values without relying on field names', () => {
    const columns = ['message', 'trace_id', 'payload', 'count', 'empty', 'other'].map(id => ({ id }))
    expect(getDefaultColumnOrder(columns, [
      { message: '0123456789abcdef', trace_id: 'readable context', payload: '{"ok":true}', count: 42, empty: '', other: [] },
      { message: null, trace_id: 'more context', payload: '[1,2]', count: 23, empty: '  ', other: { ok: true } },
    ])).toEqual({ order: ['trace_id', 'count', 'empty', 'message', 'payload', 'other'], columnClasses: { message: 'opaque', trace_id: 'readable', payload: 'opaque', count: 'readable', empty: 'unknown', other: 'opaque' } })
  })

  it('keeps mixed values and fields without nonempty samples in their group', () => {
    const columns = ['opaque', 'mixed', 'missing', 'nulls', 'tail'].map(id => ({ id }))
    expect(getDefaultColumnOrder(columns, [
      { opaque: '0123456789abcdef', mixed: '0123456789abcdef', nulls: null, tail: 'ready' },
      { opaque: 'fedcba9876543210', mixed: 'ready', tail: 'done' },
    ])).toEqual({ order: ['mixed', 'missing', 'nulls', 'tail', 'opaque'], columnClasses: { opaque: 'opaque', mixed: 'readable', missing: 'unknown', nulls: 'unknown', tail: 'readable' } })
    expect(getDefaultColumnOrder(columns, [])).toEqual({ order: columns.map(column => column.id), columnClasses: Object.fromEntries(columns.map(column => [column.id, 'unknown'])) })
  })

  it('retains existing timestamp and severity precedence even with opaque values', () => {
    const columns = [
      { id: 'clock', meta: { columnType: 'timestamp' } },
      { id: 'other_clock', meta: { columnType: 'timestamp' } },
      { id: 'priority', meta: { columnType: 'severity' } },
      { id: 'opaque' },
      { id: 'context' },
    ]
    expect(getDefaultColumnOrder(columns, [{
      clock: '0123456789abcdef', other_clock: '{"time":1}', priority: '0123456789abcdef',
      opaque: '0123456789abcdef', context: 'ready',
    }])).toEqual({ order: ['clock', 'other_clock', 'priority', 'context', 'opaque'], columnClasses: { clock: 'readable', other_clock: 'readable', priority: 'readable', context: 'readable', opaque: 'opaque' } })
  })

  it('bounds the sample to the first 50 rows', () => {
    const rows = Array.from({ length: 50 }, () => ({ a: '0123456789abcdef', b: 'ready' }))
    rows.push({ a: 'readable beyond sample', b: 'ready' })
    expect(getDefaultColumnOrder([{ id: 'a' }, { id: 'b' }], rows)).toEqual({ order: ['b', 'a'], columnClasses: { a: 'opaque', b: 'readable' } })
  })
})
