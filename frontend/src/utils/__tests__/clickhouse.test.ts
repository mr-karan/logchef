import { describe, expect, it } from 'vitest'
import { quoteClickHouseIdentifier } from '../clickhouse'

describe('ClickHouse identifier quoting', () => {
  it.each([
    ['timestamp', '`timestamp`'],
    ['event`time\\path', '`event\\x60time\\\\path`'],
    ['a\\`b', '`a\\\\\\x60b`'],
  ])('quotes %s as one identifier token', (raw, quoted) => {
    expect(quoteClickHouseIdentifier(raw)).toBe(quoted)
  })
})
