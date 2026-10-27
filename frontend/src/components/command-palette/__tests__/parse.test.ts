import { describe, expect, it } from 'vitest'
import { applyCompletion, lexQuery, MAX_INPUT_LENGTH, parsePaletteInput, slugify, type ParsedInput, type TokenKind } from '../parse'

const at = (text: string, caret = text.length) => parsePaletteInput(text, { start: caret, end: caret })

function painted(parsed: ParsedInput): [string, TokenKind][] {
  return parsed.tokens.filter((token) => token.kind !== 'space').map((token) => [parsed.text.slice(token.start, token.end), token.kind])
}

const CORPUS = [
  '',
  '?why last:1h',
  '@a/b ? why last:today',
  '@ ?why',
  '@',
  '@pay',
  '@payments/',
  '@payments/checkout_api status>=500 last:1h',
  '  @a/b   level="x y"  last:7d  ',
  '@timestamp>=1',
  'user=@bot and (status=500 or status=502) | level, msg',
  'msg="unterminated last:1h',
  "msg='a \\' b' last:today",
  'last: last:1x @a/b @c/d',
  'msg="trailing backslash \\',
  'héllo wörld ✓ 日本',
]

describe('parsePaletteInput', () => {
  it.each(CORPUS)('tiles %j with contiguous non-empty tokens', (text) => {
    for (let caret = 0; caret <= text.length; caret++) {
      const { tokens } = at(text, caret)
      expect(tokens.map((token) => text.slice(token.start, token.end)).join('')).toBe(text)
      tokens.forEach((token, index) => {
        expect(token.end).toBeGreaterThan(token.start)
        expect(token.start).toBe(index === 0 ? 0 : tokens[index - 1].end)
      })
    }
  })

  it('reads the leading tag and range, and leaves the rest as the query', () => {
    const parsed = at('@payments/checkout_api status>=500 last:1h')
    expect(parsed.tag).toMatchObject({ team: 'payments', source: 'checkout_api' })
    expect(parsed.range).toMatchObject({ kind: 'valid', window: { kind: 'relative', spec: '1h' } })
    expect(parsed.query).toBe('status>=500')
    expect(parsed.issues).toEqual([])
    expect(painted(parsed)).toEqual([
      ['@', 'tagAt'], ['payments', 'tagTeam'], ['/', 'tagSlash'], ['checkout_api', 'tagSource'],
      ['status', 'field'], ['>=', 'op'], ['500', 'value'],
      ['last:', 'rangeKey'], ['1h', 'rangeValue'],
    ])
  })

  it('keeps the query text between removed words, including quoted whitespace', () => {
    expect(at('@a/b level="x  y" last:7d service=api').query).toBe('level="x  y" service=api')
    expect(at('last:1h  level=error').query).toBe('level=error')
  })

  it.each(['@timestamp>=1', 'user=@bot', 'last:x=1', 'level=error @a'])('leaves %s as LogchefQL', (text) => {
    const parsed = at(text)
    expect(parsed.tag).toBeNull()
    expect(parsed.range).toBeNull()
    expect(parsed.query).toBe(text)
  })

  it('treats a leading @word with no operator as a team tag', () => {
    expect(at('@timestamp').tag).toMatchObject({ team: 'timestamp', source: null })
  })

  it.each(['failed @platform/app', 'failed @platform#1/app#10'])('keeps tag-like text literal without a leading tag, %s', text => {
    const parsed = at(text)
    expect(parsed.tag).toBeNull()
    expect(parsed.query).toBe(text)
    expect(parsed.issues).toEqual([])
  })

  it('ignores tags and ranges inside strings and parentheses', () => {
    expect(at('msg="a last:1h b"').range).toBeNull()
    expect(at('( last:1h )').range).toBeNull()
    expect(at('msg="unterminated last:1h').range).toBeNull()
    expect(painted(at('msg="unterminated last:1h'))).toEqual([['msg', 'field'], ['=', 'op'], ['"unterminated last:1h', 'string']])
  })

  it.each(['1h', '15m', '7d', '2w', 'today', 'yesterday'])('accepts last:%s with the Explore time grammar', (value) => {
    expect(at(`last:${value}`).range).toMatchObject({ kind: 'valid' })
  })

  it.each(['1', '1x', '0m', '1H', '1s', ''])('rejects last:%s without guessing a unit', (value) => {
    const parsed = at(`level=error last:${value}`, 0)
    expect(parsed.range).toMatchObject({ kind: 'invalid', text: value })
    expect(parsed.issues).toContainEqual(expect.objectContaining({ kind: 'invalidRange', text: value }))
  })

  it('reports a second tag and a second range', () => {
    const parsed = at('@a/b level=1 @c/d last:1h last:2h')
    expect(parsed.issues.map((issue) => issue.kind)).toEqual(['extraTag', 'duplicateRange'])
    expect(parsed.range).toMatchObject({ kind: 'valid', text: '1h' })
  })

  describe('completion slot', () => {
    it.each([
      ['@', 1, { kind: 'team', prefix: '' }],
      ['@pay', 4, { kind: 'team', prefix: 'pay' }],
      ['@pay', 2, { kind: 'team', prefix: 'p' }],
      ['@payments/', 10, { kind: 'source', team: 'payments', prefix: '' }],
      ['@payments/che', 13, { kind: 'source', team: 'payments', prefix: 'che' }],
      ['@payments/che', 9, { kind: 'team', prefix: 'payments' }],
      ['@a/b last:', 10, { kind: 'range', prefix: '' }],
      ['@a/b last:1', 11, { kind: 'range', prefix: '1' }],
      ['@a/b last:1h q', 7, { kind: 'range', prefix: '' }],
    ])('%s at %i completes %j', (text, caret, slot) => {
      expect(at(text, caret).slot).toMatchObject(slot)
    })

    it('covers the whole word, so a completion replaces text after the caret too', () => {
      expect(at('@payments/che level=1', 4).slot?.replace).toEqual({ start: 0, end: 13 })
      expect(at('@a/b last:1h level=1', 11).slot?.replace).toEqual({ start: 5, end: 12 })
    })

    it('has no slot before a word or after its trailing space', () => {
      expect(at('@a/b ', 5).slot).toBeNull()
      expect(at('@a/b', 0).slot).toBeNull()
      expect(at('level=1', 3).slot).toBeNull()
    })

    it('clamps the caret and notes a selected range', () => {
      expect(parsePaletteInput('@pay', { start: 99, end: 99 })).toMatchObject({ caret: 4, collapsed: true, slot: { prefix: 'pay' } })
      expect(parsePaletteInput('@pay', { start: -4, end: -1 })).toMatchObject({ caret: 0, collapsed: true })
      expect(parsePaletteInput('@pay', { start: 1, end: 4 }).collapsed).toBe(false)
    })
  })

  it('does not scan input over the length limit', () => {
    const text = `@a/b ${'x'.repeat(MAX_INPUT_LENGTH)}`
    const parsed = at(text)
    expect(parsed.tokens).toEqual([{ kind: 'text', start: 0, end: text.length }])
    expect(parsed.tag).toMatchObject({ team: 'a', source: 'b' })
    expect(parsed.issues).toEqual([{ kind: 'tooLong', limit: MAX_INPUT_LENGTH }])
  })
})

describe('applyCompletion', () => {
  it('adds one space after a finished word at the end', () => {
    expect(applyCompletion('@pay', { start: 0, end: 4 }, '@payments/api', true)).toEqual({ text: '@payments/api ', caret: 14 })
  })

  it('keeps a team completion open for the source', () => {
    expect(applyCompletion('@pay', { start: 0, end: 4 }, '@payments/', false)).toEqual({ text: '@payments/', caret: 10 })
  })

  it('replaces a word mid-text and keeps the rest of the input', () => {
    expect(applyCompletion('@a/b last:1 level=1', { start: 5, end: 11 }, 'last:1h', true)).toEqual({ text: '@a/b last:1h level=1', caret: 13 })
  })
})

describe('lexQuery', () => {
  it('paints fields, operators, values, keywords and pipes', () => {
    const text = 'status>=500 and msg~"time out" | level'
    expect(lexQuery(text).filter((token) => token.kind !== 'space').map((token) => [text.slice(token.start, token.end), token.kind])).toEqual([
      ['status', 'field'], ['>=', 'op'], ['500', 'value'], ['and', 'keyword'],
      ['msg', 'field'], ['~', 'op'], ['"time out"', 'string'], ['|', 'pipe'], ['level', 'text'],
    ])
  })
})

describe('slugify', () => {
  it.each([
    ['App Logs', 'app-logs'],
    ['checkout_api', 'checkout_api'],
    ['  Query regression checks ', 'query-regression-checks'],
    ['VictoriaLogs Demo', 'victorialogs-demo'],
    ['ops/infra', 'ops-infra'],
  ])('%j types as %j', (name, slug) => {
    expect(slugify(name)).toBe(slug)
  })
})

describe('qualified palette tags', () => {
  it('parses positive safe IDs once and tiles their entire tokens', () => {
    const text = '@platform#4/app-logs#25 level=error last:1h'
    const parsed = parsePaletteInput(text, { start: text.length, end: text.length })
    expect(parsed.tag).toMatchObject({ team: 'platform', teamId: 4, source: 'app-logs', sourceId: 25 })
    expect(parsed.query).toBe('level=error')
    expect(parsed.tokens.map(token => text.slice(token.start, token.end)).join('')).toBe(text)
    expect(parsed.tokens.every((token, i) => token.start === (parsed.tokens[i - 1]?.end ?? 0))).toBe(true)
  })
  it.each(['0', '-1', '9007199254740992', '1x', ''])('rejects invalid ID %j', id => {
    const text = `@platform#${id}/app-logs#25`
    expect(parsePaletteInput(text, { start: text.length, end: text.length }).tag).toBeNull()
  })
})


describe('Ask marker parsing', () => {
  it.each(['?why last:1h', '  ? why last:1h', '@a/b ?why last:1h'])('removes the leading question marker in %j', text => {
    const parsed = at(text);
    expect(parsed.ask).not.toBeNull();
    expect(parsed.query).toBe('why');
    expect(parsed.range).toMatchObject({ kind: 'valid', text: '1h' });
  });
  it.each(['why ? now', '@a/b last:1h ? why', 'https://example.com/?q=x', 'message="?why"'])('keeps nonleading question characters in %j', text => {
    expect(at(text).ask).toBeNull();
  });
});
