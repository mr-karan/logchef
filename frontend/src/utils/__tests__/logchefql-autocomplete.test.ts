import { describe, it, expect } from 'vitest'
import {
  parseLogchefQLContext,
  isValueSuggestableField,
  isNumericFieldType,
  formatCountShort,
  buildValueInsertText,
  buildValueCompletionEdit,
  buildBooleanInsertText,
  getLogchefQLOperators,
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

    it.each([
      'method=GET ', 'status>=400 ', '(method="GET")', '(status>=400) ',
      '((method="GET" ) )', 'method="escaped \\"quote\\""', "method='escaped \\'quote\\''",
    ])(
      'suggests boolean after a completed value or group in %s', (text) => {
        expect(parseLogchefQLContext(text, FIELDS)).toEqual({ suggest: 'boolean', partial: '' })
      },
    )

    it.each(['a', 'an', 'and', 'o', 'or', 'OR'])(
      'completes the boolean prefix %s after a condition', (partial) => {
        expect(parseLogchefQLContext(`method="GET" ${partial}`, FIELDS)).toEqual({
          suggest: 'boolean', partial,
        })
      },
    )

    it('keeps a boolean prefix in a value as a value', () => {
      expect(parseLogchefQLContext('method=or', FIELDS)).toMatchObject({
        suggest: 'values', key: 'method', partial: 'or',
      })
    })

    it.each(['(', '((', 'method="GET" and (', '(method="GET" or ('])(
      'offers fields at the start of a group in %s', (text) => {
        expect(parseLogchefQLContext(text, FIELDS)).toEqual({ suggest: 'fields', partial: '' })
      },
    )

    it('offers operators for a field inside a group', () => {
      expect(parseLogchefQLContext('(method', FIELDS)).toEqual({ suggest: 'operators', key: 'method' })
    })

    it('does not finish an empty numeric value on whitespace', () => {
      expect(parseLogchefQLContext('status>= ', FIELDS)).toMatchObject({
        suggest: 'values', key: 'status', partial: '',
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

    it('treats partial "an" after a condition as a boolean', () => {
      const result = parseLogchefQLContext('method=GET an', FIELDS)
      expect(result).toEqual({ suggest: 'boolean', partial: 'an' })
    })

    it('keeps a partial field after a boolean in field context', () => {
      expect(parseLogchefQLContext('method=GET and an', FIELDS)).toEqual({
        suggest: 'fields', partial: 'an',
      })
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

describe('buildValueCompletionEdit', () => {
  it.each([
    { quote: '"', suffix: '") and host="api"' },
    { quote: "'", suffix: "')" },
  ] satisfies Array<{ quote: '"' | "'"; suffix: string }>)(
    'replaces the existing $quote without changing the suffix', ({ quote, suffix }) => {
      const prefix = `method=${quote}`
      const edit = buildValueCompletionEdit('GET', 'String', quote, suffix)
      const result = prefix + edit.insertText + suffix.slice(edit.replaceAfter)
      expect(result).toBe(`${prefix}GET${suffix}`)
      expect(prefix.length + edit.insertText.length).toBe('method="GET"'.length)
    },
  )

  it('closes an open quote when there is no existing quote', () => {
    expect(buildValueCompletionEdit('GET', 'String', '"', ')')).toEqual({
      insertText: 'GET"', replaceAfter: 0, retrigger: true,
    })
  })

  it('preserves parentheses when no quote was typed', () => {
    expect(buildValueCompletionEdit('GET', 'String', null, ')')).toEqual({
      insertText: '"GET"', replaceAfter: 0, retrigger: true,
    })
  })

  it('preserves the following clause for numeric values', () => {
    expect(buildValueCompletionEdit('400', 'UInt16', null, ' and host="api"')).toEqual({
      insertText: '400', replaceAfter: 0, retrigger: false,
    })
  })

  it.each([
    { before: 'method="G', after: 'ET" and status=200', quote: '"', expected: 'method="GET" and status=200' },
    { before: 'method="G', after: 'ET"and host="api"', quote: '"', expected: 'method="GET"and host="api"' },
    { before: 'method="G', after: 'ET"OR host="api"', quote: '"', expected: 'method="GET"OR host="api"' },
    { before: "method='G", after: "ET')", quote: "'", expected: "method='GET')" },
    { before: 'method="G', after: 'E\\"T" and status=200', quote: '"', expected: 'method="GET" and status=200' },
    { before: 'method="G', after: 'ET" and host="api"', quote: '"', expected: 'method="GET" and host="api"' },
    { before: 'method="', after: ' and host="api"', quote: '"', expected: 'method="GET" and host="api"' },
    { before: 'method="', after: ' and host="api" and path="/x"', quote: '"', expected: 'method="GET" and host="api" and path="/x"' },
    { before: 'method=G', after: 'ET and status=200', quote: null, expected: 'method="GET" and status=200' },
    { before: 'method="G', after: 'ET" and host="', quote: '"', expected: 'method="GET" and host="' },
    { before: 'method="G', after: 'ET" and msg="say \\"hi', quote: '"', expected: 'method="GET" and msg="say \\"hi' },
    { before: 'method=G', after: 'ET|method', quote: null, expected: 'method="GET"|method' },
    { before: 'method=G', after: 'ET!=x', quote: null, expected: 'method="GET"!=x' },
  ] satisfies Array<{ before: string; after: string; quote: '"' | "'" | null; expected: string }>)(
    'replaces the rest of the value in $before|$after', ({ before, after, quote, expected }) => {
      const partial = quote ? before.slice(before.indexOf(quote) + 1) : before.slice(before.indexOf('=') + 1)
      const edit = buildValueCompletionEdit('GET', 'String', quote, after)
      expect(before.slice(0, before.length - partial.length) + edit.insertText + after.slice(edit.replaceAfter)).toBe(expected)
    },
  )

  it('replaces the rest of a bare number', () => {
    const edit = buildValueCompletionEdit('200', 'UInt16', null, '00 and host="api"')
    expect('status=' + edit.insertText + '00 and host="api"'.slice(edit.replaceAfter)).toBe('status=200 and host="api"')
  })
})

describe('buildBooleanInsertText', () => {
  it.each(['and', 'or'] satisfies Array<'and' | 'or'>)('inserts only the selected %s keyword', (keyword) => {
    expect(buildBooleanInsertText(keyword, 'method="GET"', '')).toBe(` ${keyword} `)
    expect(buildBooleanInsertText(keyword, 'method="GET" ', '')).toBe(`${keyword} `)
    expect(buildBooleanInsertText(keyword, '(method="GET")', '')).toBe(` ${keyword} `)
    expect(buildBooleanInsertText(keyword, 'status>=400 ', '')).toBe(`${keyword} `)
    expect(buildBooleanInsertText(keyword, 'method="GET" a', 'a')).toBe(`${keyword} `)
  })
})

describe('getLogchefQLOperators', () => {
  it.each(['UInt16', 'Nullable(Float64)', 'LowCardinality(Int32)'])(
    'prioritizes numeric comparisons and excludes regex for %s', (type) => {
      expect(getLogchefQLOperators(type)).toEqual(['=', '>=', '>', '<=', '<', '!='])
    },
  )

  it('keeps equality and regex ahead of string comparisons', () => {
    expect(getLogchefQLOperators('String')).toEqual(['=', '!=', '~', '!~', '>', '<', '>=', '<='])
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
