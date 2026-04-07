import { describe, it, expect } from 'vitest'
import {
  parseLogchefQLContext,
  isValueSuggestableField,
  isNumericFieldType,
  formatCountShort,
  buildValueInsertText,
  filterValuesByPartial,
  formatFieldDetail,
} from '../logchefql-autocomplete'

const FIELDS = ['host', 'method', 'status', 'path', 'user-identifier', 'host.name']

// --- parseLogchefQLContext ---

describe('parseLogchefQLContext', () => {
  describe('empty / whitespace input', () => {
    it('returns fields suggestion for empty string', () => {
      expect(parseLogchefQLContext('', FIELDS)).toEqual({ suggest: 'fields', partial: '' })
    })

    it('returns fields suggestion for whitespace-only string', () => {
      expect(parseLogchefQLContext('   ', FIELDS)).toEqual({ suggest: 'fields', partial: '' })
    })
  })

  describe('bare field name', () => {
    it('suggests operators for a known field', () => {
      expect(parseLogchefQLContext('host', FIELDS)).toEqual({ suggest: 'operators', key: 'host' })
    })

    it('suggests operators for a hyphenated field', () => {
      expect(parseLogchefQLContext('user-identifier', FIELDS)).toEqual({
        suggest: 'operators',
        key: 'user-identifier',
      })
    })

    it('suggests operators for a dotted field', () => {
      expect(parseLogchefQLContext('host.name', FIELDS)).toEqual({
        suggest: 'operators',
        key: 'host.name',
      })
    })

    it('suggests fields with partial for unknown partial word', () => {
      expect(parseLogchefQLContext('ho', FIELDS)).toEqual({ suggest: 'fields', partial: 'ho' })
    })

    it('suggests fields with partial for single character', () => {
      expect(parseLogchefQLContext('m', FIELDS)).toEqual({ suggest: 'fields', partial: 'm' })
    })
  })

  describe('field + operator', () => {
    it('suggests values after field=', () => {
      const result = parseLogchefQLContext('host=', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        operator: '=',
        partial: '',
        quote: null,
      })
    })

    it.each(['=', '!=', '~', '!~', '>', '<', '>=', '<='])('suggests values after field%s', (op) => {
      const result = parseLogchefQLContext(`status${op}`, FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'status',
        operator: op,
        partial: '',
        quote: null,
      })
    })

    it('suggests values with space between field and operator', () => {
      const result = parseLogchefQLContext('host =', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        operator: '=',
        partial: '',
      })
    })
  })

  describe('field + operator + partial unquoted value', () => {
    it('includes the partial text', () => {
      const result = parseLogchefQLContext('host=cdn', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        operator: '=',
        partial: 'cdn',
        quote: null,
      })
    })

    it('works with numeric partial', () => {
      const result = parseLogchefQLContext('status=40', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'status',
        operator: '=',
        partial: '40',
      })
    })
  })

  describe('open quote (value in progress)', () => {
    it('detects double quote context', () => {
      const result = parseLogchefQLContext('host="cdn', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        operator: '=',
        partial: 'cdn',
        quote: '"',
      })
    })

    it('detects single quote context', () => {
      const result = parseLogchefQLContext("host='cdn", FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        operator: '=',
        partial: 'cdn',
        quote: "'",
      })
    })

    it('handles empty partial inside open quote', () => {
      const result = parseLogchefQLContext('host="', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        operator: '=',
        partial: '',
        quote: '"',
      })
    })

    it('handles dot in partial inside open quote', () => {
      const result = parseLogchefQLContext('host="cdn.logchef', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        partial: 'cdn.logchef',
        quote: '"',
      })
    })

    it('handles escaped quote inside open double quote', () => {
      const result = parseLogchefQLContext('host="val\\"ue', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        quote: '"',
      })
    })
  })

  describe('complete condition (closed quote) → boolean', () => {
    it('suggests boolean after closed double quote', () => {
      expect(parseLogchefQLContext('host="cdn.logchef.dev"', FIELDS)).toEqual({
        suggest: 'boolean',
        partial: '',
      })
    })

    it('suggests boolean after closed single quote', () => {
      expect(parseLogchefQLContext("host='cdn.logchef.dev'", FIELDS)).toEqual({
        suggest: 'boolean',
        partial: '',
      })
    })

    it('suggests values (not boolean) for unquoted value with trailing space because trimEnd collapses it', () => {
      // The parser trims trailing space, so "method=GET " becomes "method=GET"
      // which matches Step 3 (field+op+partial) since method is a known field.
      const result = parseLogchefQLContext('method=GET ', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'method',
        operator: '=',
        partial: 'GET',
      })
    })
  })

  describe('after boolean keyword', () => {
    it('suggests fields after "and "', () => {
      expect(parseLogchefQLContext('method=GET and ', FIELDS)).toEqual({
        suggest: 'fields',
        partial: '',
      })
    })

    it('suggests fields after "or "', () => {
      expect(parseLogchefQLContext('method=GET or ', FIELDS)).toEqual({
        suggest: 'fields',
        partial: '',
      })
    })

    it('is case-insensitive for AND', () => {
      expect(parseLogchefQLContext('method=GET AND ', FIELDS)).toEqual({
        suggest: 'fields',
        partial: '',
      })
    })

    it('is case-insensitive for Or', () => {
      expect(parseLogchefQLContext('method=GET Or ', FIELDS)).toEqual({
        suggest: 'fields',
        partial: '',
      })
    })

    it('treats partial "an" as partial field', () => {
      const result = parseLogchefQLContext('method=GET an', FIELDS)
      expect(result).toEqual({ suggest: 'fields', partial: 'an' })
    })
  })

  describe('complex multi-condition queries', () => {
    it('suggests values with contextQuery for second condition', () => {
      const result = parseLogchefQLContext('method=GET and status=', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'status',
        operator: '=',
        partial: '',
      })
      if (result.suggest === 'values') {
        expect(result.contextQuery).toBe('method=GET')
      }
    })

    it('builds contextQuery across multiple conditions', () => {
      const result = parseLogchefQLContext('method=GET and host="cdn" and status=', FIELDS)
      expect(result).toMatchObject({ suggest: 'values', key: 'status' })
      if (result.suggest === 'values') {
        expect(result.contextQuery).toBe('method=GET and host="cdn"')
      }
    })

    it('handles quoted values in contextQuery', () => {
      const result = parseLogchefQLContext('host="example.com" and method=', FIELDS)
      expect(result).toMatchObject({ suggest: 'values', key: 'method' })
      if (result.suggest === 'values') {
        expect(result.contextQuery).toBe('host="example.com"')
      }
    })

    it('contextQuery excludes partial unquoted value in current clause', () => {
      // P1 fix: "status=40" should NOT leak "status=40" into contextQuery
      const result = parseLogchefQLContext('method=GET and status=40', FIELDS)
      expect(result).toMatchObject({ suggest: 'values', key: 'status', partial: '40' })
      if (result.suggest === 'values') {
        expect(result.contextQuery).toBe('method=GET')
      }
    })

    it('contextQuery is empty for first clause with partial value', () => {
      const result = parseLogchefQLContext('status=40', FIELDS)
      expect(result).toMatchObject({ suggest: 'values', key: 'status', partial: '40' })
      if (result.suggest === 'values') {
        expect(result.contextQuery).toBe('')
      }
    })

    it('suggests operators for field after boolean + known field', () => {
      const result = parseLogchefQLContext('method=GET and host', FIELDS)
      expect(result).toEqual({ suggest: 'operators', key: 'host' })
    })

    it('suggests fields with partial after boolean + partial', () => {
      const result = parseLogchefQLContext('method=GET and ho', FIELDS)
      expect(result).toEqual({ suggest: 'fields', partial: 'ho' })
    })
  })

  describe('field + operator + open quote in multi-condition', () => {
    it('detects open quote in second condition', () => {
      const result = parseLogchefQLContext('method=GET and host="cdn', FIELDS)
      expect(result).toMatchObject({
        suggest: 'values',
        key: 'host',
        partial: 'cdn',
        quote: '"',
      })
    })
  })

  describe('edge cases', () => {
    it('returns fields suggestion when field is not in field list after operator', () => {
      // "unknown=" — unknown is not in FIELDS so step 3 skips it
      const result = parseLogchefQLContext('unknown=', FIELDS)
      // The parser should not suggest values for an unknown field
      expect(result.suggest).not.toBe('values')
    })

    it('handles field with no trailing content as operators', () => {
      expect(parseLogchefQLContext('status', FIELDS)).toEqual({
        suggest: 'operators',
        key: 'status',
      })
    })
  })
})

// --- isValueSuggestableField ---

describe('isValueSuggestableField', () => {
  it.each([
    ['String', true],
    ['LowCardinality(String)', true],
    ['Nullable(String)', true],
    ['UInt16', true],
    ['UInt32', true],
    ['Float64', true],
    ['Int64', true],
    ['DateTime64(3)', true],
    ['Map(String, String)', false],
    ['Array(String)', false],
    ['Tuple(String, UInt32)', false],
    ['JSON', false],
    ['json', false],
  ])('isValueSuggestableField(%s) → %s', (type, expected) => {
    expect(isValueSuggestableField(type)).toBe(expected)
  })
})

// --- isNumericFieldType ---

describe('isNumericFieldType', () => {
  it.each([
    ['UInt16', true],
    ['UInt32', true],
    ['Int64', true],
    ['Float64', true],
    ['Float32', true],
    ['Decimal(10,2)', true],
    ['LowCardinality(UInt32)', true],
    ['Nullable(Int64)', true],
    ['String', false],
    ['DateTime64(3)', false],
    ['LowCardinality(String)', false],
    // Note: Map(String, UInt32) returns true because the inner "UInt32" contains "int"
    ['Map(String, UInt32)', true],
  ])('isNumericFieldType(%s) → %s', (type, expected) => {
    expect(isNumericFieldType(type)).toBe(expected)
  })
})

// --- formatCountShort ---

describe('formatCountShort', () => {
  it.each([
    [0, '0'],
    [500, '500'],
    [999, '999'],
    [1000, '1.0K'],
    [1700, '1.7K'],
    [15000, '15.0K'],
    [999999, '1000.0K'],
    [1000000, '1.0M'],
    [1500000, '1.5M'],
    [10000000, '10.0M'],
  ])('formatCountShort(%d) → %s', (count, expected) => {
    expect(formatCountShort(count)).toBe(expected)
  })
})

// --- buildValueInsertText ---

describe('buildValueInsertText', () => {
  describe('string field', () => {
    it('wraps in double quotes when no quote started', () => {
      expect(buildValueInsertText('example.com', 'String', null)).toBe('"example.com"')
    })

    it('appends closing double quote when double quote started', () => {
      expect(buildValueInsertText('example.com', 'String', '"')).toBe('example.com"')
    })

    it('appends closing single quote when single quote started', () => {
      expect(buildValueInsertText('example.com', 'String', "'")).toBe("example.com'")
    })

    it('handles LowCardinality(String) as string', () => {
      expect(buildValueInsertText('val', 'LowCardinality(String)', null)).toBe('"val"')
    })

    it('escapes embedded double quotes when wrapping in double quotes', () => {
      expect(buildValueInsertText('he said "no"', 'String', null)).toBe('"he said \\"no\\""')
    })

    it('escapes embedded single quotes when inside single quote context', () => {
      expect(buildValueInsertText("it's", 'String', "'")).toBe("it\\'s'")
    })

    it('escapes backslashes in values', () => {
      expect(buildValueInsertText('path\\to\\file', 'String', null)).toBe('"path\\\\to\\\\file"')
    })
  })

  describe('numeric field', () => {
    it('returns bare value when no quote', () => {
      expect(buildValueInsertText('200', 'UInt16', null)).toBe('200')
    })

    it('closes accidental double quote', () => {
      expect(buildValueInsertText('200', 'UInt16', '"')).toBe('200"')
    })

    it('closes accidental single quote', () => {
      expect(buildValueInsertText('200', 'UInt16', "'")).toBe("200'")
    })

    it('handles Float64 as numeric', () => {
      expect(buildValueInsertText('3.14', 'Float64', null)).toBe('3.14')
    })
  })
})

// --- filterValuesByPartial ---

describe('filterValuesByPartial', () => {
  const values = [
    { value: 'cdn.logchef.dev', count: 10 },
    { value: 'api.logchef.dev', count: 20 },
    { value: 'CDN-backup.logchef.dev', count: 5 },
    { value: '404', count: 100 },
    { value: '200', count: 500 },
    { value: '4xx', count: 50 },
  ]

  it('returns all values when partial is empty', () => {
    expect(filterValuesByPartial(values, '', 'String')).toEqual(values)
  })

  it('does case-insensitive substring match for string fields', () => {
    const result = filterValuesByPartial(values, 'cdn', 'String')
    expect(result).toEqual([
      { value: 'cdn.logchef.dev', count: 10 },
      { value: 'CDN-backup.logchef.dev', count: 5 },
    ])
  })

  it('does prefix match for numeric fields', () => {
    const result = filterValuesByPartial(values, '40', 'UInt16')
    expect(result).toEqual([{ value: '404', count: 100 }])
  })

  it('returns empty array when nothing matches', () => {
    expect(filterValuesByPartial(values, 'zzz', 'String')).toEqual([])
  })

  it('handles LowCardinality wrapper for string matching', () => {
    const result = filterValuesByPartial(values, 'api', 'LowCardinality(String)')
    expect(result).toEqual([{ value: 'api.logchef.dev', count: 20 }])
  })
})

// --- formatFieldDetail ---

describe('formatFieldDetail', () => {
  it('strips LowCardinality wrapper and shows count', () => {
    expect(formatFieldDetail('LowCardinality(String)', 3)).toBe('String · 3 values')
  })

  it('returns bare type when totalDistinct is null', () => {
    expect(formatFieldDetail('UInt32', null)).toBe('UInt32')
  })

  it('strips Nullable wrapper and formats large count', () => {
    expect(formatFieldDetail('Nullable(String)', 1500)).toBe('String · 1.5K values')
  })

  it('strips nested wrappers (both LowCardinality and Nullable are removed)', () => {
    // The regex with /gi flag strips both wrappers
    expect(formatFieldDetail('LowCardinality(Nullable(String))', 10)).toBe(
      'String · 10 values',
    )
  })

  it('handles zero distinct values', () => {
    expect(formatFieldDetail('String', 0)).toBe('String · 0 values')
  })

  it('formats million-scale counts', () => {
    expect(formatFieldDetail('String', 2500000)).toBe('String · 2.5M values')
  })
})
