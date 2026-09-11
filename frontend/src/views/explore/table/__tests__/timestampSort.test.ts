import { describe, expect, it } from 'vitest'
import { compareTimestamps } from '../timestampSort'

describe('timestamp sorting', () => {
  it('sorts variable-width fractions chronologically', () => {
    const values = ['8', '9', '760', '770'].map(fraction => `2026-01-01T00:00:00.${fraction}Z`)
    expect(values.sort(compareTimestamps)).toEqual([
      '2026-01-01T00:00:00.760Z', '2026-01-01T00:00:00.770Z',
      '2026-01-01T00:00:00.8Z', '2026-01-01T00:00:00.9Z',
    ])
  })

  it('preserves nanoseconds and normalizes timezone offsets', () => {
    expect(compareTimestamps('2026-01-01T05:30:00.123456788+05:30', '2026-01-01T00:00:00.123456789Z')).toBe(-1)
    expect(compareTimestamps('2026-01-01T05:30:00.8+05:30', '2026-01-01T00:00:00.800000000Z')).toBe(0)
    expect(compareTimestamps('1969-12-31T23:59:59.999999999Z', '1970-01-01T00:00:00Z')).toBe(-1)
  })

  it('orders missing or invalid timestamps before valid ones', () => {
    expect(compareTimestamps(null, '2026-01-01T00:00:00Z')).toBe(-1)
    expect(compareTimestamps('invalid', undefined)).toBe(0)
    expect(compareTimestamps(new Date('2026-01-01T00:00:00Z'), '2026-01-01T00:00:00Z')).toBe(0)
  })
})
