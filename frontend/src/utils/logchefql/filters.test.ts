import { describe, expect, it } from 'vitest'
import { addSelectField, appendCondition, buildCondition, containsTerm, fieldPath, filterChoices, filteredColumns, isLongValue, removeColumnConditions } from './filters'

describe('buildCondition', () => {
  it('escapes backslashes before quotes so an escaped JSON line survives', () => {
    const line = '{"msg":"say \\"hi\\""}'
    expect(buildCondition('msg', '=', line)).toBe('msg="{\\"msg\\":\\"say \\\\\\"hi\\\\\\"\\"}"')
  })

  it('keeps numbers, booleans and null bare', () => {
    expect(buildCondition('status', '=', 500)).toBe('status=500')
    expect(buildCondition('ok', '!=', false)).toBe('ok!=false')
    expect(buildCondition('user', '=', null)).toBe('user=null')
  })
})

describe('fieldPath', () => {
  it('quotes nested keys that are not identifiers', () => {
    expect(fieldPath(['msg', 'http', 'status'])).toBe('msg.http.status')
    expect(fieldPath(['msg', 'user agent', 'a.b'])).toBe('msg."user agent"."a.b"')
  })
})

describe('contains filters', () => {
  it('treats long and multi-line strings as long', () => {
    expect(isLongValue('x'.repeat(81))).toBe(true)
    expect(isLongValue('a\nb')).toBe(true)
    expect(isLongValue('short')).toBe(false)
    expect(isLongValue(12345)).toBe(false)
  })

  it('uses the selection, else the first line cut at a word boundary', () => {
    const line = 'processed batch 42 for account placeholder after retrying upstream call to inventory'
    expect(containsTerm(line, '  batch 42 ')).toBe('batch 42')
    expect(containsTerm(line)).toBe('processed batch 42 for account placeholder after retrying')
    expect(containsTerm('first line\nsecond line')).toBe('first line')
  })
})

describe('appendCondition', () => {
  it('adds the condition before a pipe projection', () => {
    expect(appendCondition('level="error" | timestamp msg', 'job="api"')).toBe('level="error" and job="api" | timestamp msg')
    expect(appendCondition('| timestamp msg', 'job="api"')).toBe('job="api" | timestamp msg')
  })

  it('groups an existing or-expression', () => {
    expect(appendCondition('a=1 or b=2', 'c=3')).toBe('(a=1 or b=2) and c=3')
    expect(appendCondition('(a=1) or (b=2)', 'c=3')).toBe('((a=1) or (b=2)) and c=3')
  })

  it('ignores pipes inside quoted values', () => {
    expect(appendCondition('msg~"a|b"', 'c=3')).toBe('msg~"a|b" and c=3')
  })

  it('keeps every stage of an aggregation', () => {
    expect(appendCondition('level="error" | count() by service | sort count desc | limit 5', 'job="api"'))
      .toBe('level="error" and job="api" | count() by service | sort count desc | limit 5')
    expect(removeColumnConditions('level="error" and job="api" | count() by job | limit 5', 'job'))
      .toBe('level="error" | count() by job | limit 5')
  })
})

describe('addSelectField', () => {
  it('projects the shown columns plus the new field when there is no projection', () => {
    expect(addSelectField('level="error"', 'msg.http.status', ['timestamp', 'level', 'msg']))
      .toBe('level="error" | timestamp level msg msg.http.status')
  })

  it('extends an existing projection once', () => {
    expect(addSelectField('| timestamp msg', 'msg.http.status', [])).toBe('| timestamp msg msg.http.status')
    expect(addSelectField('| timestamp msg', 'msg', [])).toBe('| timestamp msg')
  })

  it('leaves an aggregation unchanged', () => {
    expect(addSelectField('level="error" | count() by service', 'msg', ['timestamp', 'msg']))
      .toBe('level="error" | count() by service')
  })
})

describe('filterChoices', () => {
  it('offers exact filters for short values', () => {
    expect(filterChoices('checkout-api')).toEqual({
      include: { operator: '=', value: 'checkout-api' },
      exclude: { operator: '!=', value: 'checkout-api' },
    })
  })

  it('offers contains filters for long values and for a selection', () => {
    const line = 'processed batch 42 for account placeholder after retrying upstream call to inventory'
    expect(filterChoices(line).include).toEqual({ operator: '~', value: 'processed batch 42 for account placeholder after retrying' })
    expect(filterChoices('checkout-api', 'checkout').exclude).toEqual({ operator: '!~', value: 'checkout' })
  })
})

describe('filteredColumns', () => {
  it('lists the columns compared by top-level and-terms', () => {
    expect(filteredColumns('level="error" and status>=500 and status<600 | timestamp msg')).toEqual(['level', 'status'])
    expect(filteredColumns('"user agent"~"curl" AND host!="a"')).toEqual(['user agent', 'host'])
  })

  it('skips groups, nested paths and keywords inside values or names', () => {
    expect(filteredColumns('(a=1 or b=2) and c=3')).toEqual(['c'])
    expect(filteredColumns('msg.http.status=500 and brand="x and y"')).toEqual(['brand'])
    expect(filteredColumns('color="red" and operator=1')).toEqual(['color', 'operator'])
  })

  it('reports nothing for a top-level or, an empty query or a projection only', () => {
    expect(filteredColumns('a=1 or b=2')).toEqual([])
    expect(filteredColumns('a=1 and b=2 or c=3')).toEqual([])
    expect(filteredColumns('')).toEqual([])
    expect(filteredColumns('| timestamp msg')).toEqual([])
  })
})

describe('removeColumnConditions', () => {
  it('drops every top-level condition on the column and keeps the rest', () => {
    expect(removeColumnConditions('status>=500 and level="error" and status<600', 'status')).toBe('level="error"')
    expect(removeColumnConditions('(a=1 or b=2) and c=3 | timestamp msg', 'c')).toBe('(a=1 or b=2) | timestamp msg')
    expect(removeColumnConditions('"user agent"~"curl" and host="a"', 'user agent')).toBe('host="a"')
  })

  it('keeps the projection when the last condition goes', () => {
    expect(removeColumnConditions('level="error" | timestamp msg', 'level')).toBe('| timestamp msg')
    expect(removeColumnConditions('level="error"', 'level')).toBe('')
  })

  it('leaves the query untouched when nothing matches or the query has a top-level or', () => {
    expect(removeColumnConditions('level = "error"  and host="a"', 'status')).toBe('level = "error"  and host="a"')
    expect(removeColumnConditions('level="error" or level="warn"', 'level')).toBe('level="error" or level="warn"')
    expect(removeColumnConditions('msg.level="error"', 'msg')).toBe('msg.level="error"')
  })

  it('removes what appendCondition added', () => {
    const query = appendCondition('a=1 or b=2', buildCondition('status', '>', 499))
    expect(removeColumnConditions(query, 'status')).toBe('(a=1 or b=2)')
  })
})
