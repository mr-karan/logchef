import { describe, expect, it } from 'vitest'
import { addSelectField, appendCondition, buildCondition, containsTerm, fieldPath, filterChoices, isLongValue } from './filters'

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
