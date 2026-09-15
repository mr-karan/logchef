import { describe, expect, it } from 'vitest'
import { timestampIdentity } from '../timestampIdentity'

describe('timestampIdentity', () => {
  it('distinguishes adjacent nanosecond events', () => {
    const target = '2026-09-15T12:00:00.123456789Z'
    expect(timestampIdentity(target)).not.toBe(timestampIdentity('2026-09-15T12:00:00.123456788Z'))
    expect(timestampIdentity(target)).not.toBe(timestampIdentity('2026-09-15T12:00:00.123456790Z'))
  })

  it('normalizes equivalent timezone offsets', () => {
    expect(timestampIdentity('2026-09-15T12:00:00.123456789Z')).toBe(
      timestampIdentity('2026-09-15T17:30:00.123456789+05:30'),
    )
  })
})
