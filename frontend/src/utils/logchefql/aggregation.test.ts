import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { aggregationOutline, functionArgument, isAggregation, numberColumnKind, pipeTail, type AggregationNext } from './aggregation'

interface FixtureCase {
  query: string
  valid: boolean
  is_aggregation: boolean
  functions?: string[]
  keys?: Array<{ field?: string; time?: boolean; window?: string }>
}

// Shared with the Go parser test, so a syntax change that reaches one side fails the other.
const fixture: { cases: FixtureCase[] } = JSON.parse(readFileSync(
  resolve(import.meta.dirname, '../../../../internal/logchefql/testdata/aggregation_cases.json'), 'utf8',
))

describe('shared aggregation fixture', () => {
  it('has cases', () => {
    expect(fixture.cases.length).toBeGreaterThan(0)
  })

  it.each(fixture.cases)('reads $query', (tc) => {
    expect(isAggregation(tc.query)).toBe(tc.is_aggregation)
    const outline = aggregationOutline(tc.query)
    expect(outline !== null).toBe(tc.is_aggregation)
    if (!outline || !tc.valid) return
    expect(outline.items.map(item => item.func)).toEqual(tc.functions)
    expect(outline.keys.map(key => key.time
      ? { time: true, ...(key.window ? { window: key.window } : {}) }
      : { field: key.field?.name })).toEqual(tc.keys)
  })
})

describe('aggregationOutline', () => {
  it('reads aliases, windows and field positions', () => {
    const query = 'level="a|b" | count() as errors, P95(log."dur ms") by time(5m) as t, service as svc | sort errors'
    const outline = aggregationOutline(query)
    expect(outline?.items.map(({ func, alias }) => ({ func, alias }))).toEqual([
      { func: 'count', alias: 'errors' },
      { func: 'p95', alias: null },
    ])
    expect(outline?.keys.map(({ time, window, alias }) => ({ time, window, alias }))).toEqual([
      { time: true, window: '5m', alias: 't' },
      { time: false, window: null, alias: 'svc' },
    ])
    const field = outline?.items[1].field
    expect(field).toMatchObject({ name: 'log.dur ms', baseName: 'log' })
    expect(query.slice(field?.start, field?.end)).toBe('log."dur ms"')
    expect(outline?.keys[1].field?.name).toBe('service')
  })

  it('keeps a space inside a window, as the server does', () => {
    expect(aggregationOutline('| count() by time(5 m)')?.keys[0].window).toBe('5 m')
  })

  it('joins a number alias so that the server names the error', () => {
    expect(aggregationOutline('| count() as 2xx')?.items[0].alias).toBe('2xx')
  })

  it.each<[string, AggregationNext]>([
    ['| count(), ', { expect: 'item' }],
    ['| SUM(', { expect: 'argument', func: 'sum' }],
    ['| sum( ', { expect: 'argument', func: 'sum' }],
    ['| count()', { expect: 'afterItem', aliased: false }],
    ['| count() as n ', { expect: 'afterItem', aliased: true }],
    ['| count() as ', { expect: 'alias' }],
    ['| count() by ', { expect: 'key' }],
    ['| count() by service, ', { expect: 'key' }],
    ['| count() by service ', { expect: 'afterKey', aliased: false }],
    ['| count() by time(5m) as t ', { expect: 'afterKey', aliased: true }],
    ['| count() by time(', { expect: 'other' }],
    ['| sum(log.', { expect: 'other' }],
    ['| count() by service | ', { expect: 'other' }],
    ['| count() foo ', { expect: 'other' }],
  ])('expects %j next', (query, next) => {
    expect(aggregationOutline(query)?.next).toEqual(next)
  })
})

describe('pipe detection', () => {
  it('ignores a pipe inside a string', () => {
    expect(isAggregation('msg="x | count()"')).toBe(false)
    expect(pipeTail('msg="x | count()"')).toBeNull()
    expect(pipeTail('msg="x|y" | count()')).toBe(' count()')
  })

  it('allows space between the function name and its parenthesis', () => {
    expect(isAggregation('| count ()')).toBe(true)
  })
})

describe('functionArgument', () => {
  it.each([
    ['count', 'none'],
    ['COUNT', 'none'],
    ['count_distinct', 'any'],
    ['median', 'any'],
    ['sum', 'number'],
    ['p99', 'number'],
  ])('%s takes %s', (func, argument) => {
    expect(functionArgument(func)).toBe(argument)
  })
})

describe('numberColumnKind', () => {
  it.each([
    ['UInt64', 'number'],
    ['Nullable(Float64)', 'number'],
    ['LowCardinality(Nullable(Int32))', 'number'],
    ['Decimal(18, 2)', 'number'],
    ['String', 'parsed'],
    ['LowCardinality(String)', 'parsed'],
    ['Dynamic', 'parsed'],
    ['Tuple(String, UInt8)', 'rejected'],
    ['Array(UInt64)', 'rejected'],
    ['Map(String, Float64)', 'rejected'],
    ['Nullable(DateTime64(3))', 'rejected'],
    ['Bool', 'rejected'],
  ])('%s is %s', (type, kind) => {
    expect(numberColumnKind(type)).toBe(kind)
  })
})
