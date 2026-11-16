// Reads the aggregation pipe of a LogchefQL query for the editor. It reads
// tokens and does not validate: the server owns validation, through
// /logchefql/validate. internal/logchefql/testdata/aggregation_cases.json keeps
// this reader and the Go parser in step.

export const AGGREGATE_FUNCTIONS = [
  'count', 'count_distinct', 'sum', 'avg', 'min', 'max', 'p50', 'p75', 'p90', 'p95', 'p99',
] as const

// What a function takes between its parentheses. An unknown function gets any
// field: the server names the error.
export function functionArgument(func: string): 'none' | 'any' | 'number' {
  const name = func.toLowerCase()
  if (name === 'count') return 'none'
  if (name === 'count_distinct' || !(AGGREGATE_FUNCTIONS as readonly string[]).includes(name)) return 'any'
  return 'number'
}

// How a number function reads a column, from its outer type: a number type is
// read as is, text is parsed as a number, and the other types are rejected by
// the server. A container of numbers, such as Array(UInt8), is not a number.
export function numberColumnKind(type: string): 'number' | 'parsed' | 'rejected' {
  let outer = type.trim()
  while (/^(Nullable|LowCardinality)\(/i.test(outer)) outer = outer.replace(/^\w+\(/, '').replace(/\)$/, '')
  if (/^(u?int\d|float\d|decimal)/i.test(outer)) return 'number'
  if (/^(bool|date|array|map|tuple|json|object)/i.test(outer)) return 'rejected'
  return 'parsed'
}

type TokenKind = 'string' | 'operator' | 'pipe' | 'lparen' | 'rparen' | 'dot' | 'comma' | 'number' | 'ident' | 'other'

interface Token {
  kind: TokenKind
  text: string
  start: number
  end: number
}

// The rules and their order follow the Go lexer (internal/logchefql/grammar.go).
// An unterminated string runs to the end, so text being typed still lexes.
const TOKEN_RULES: Array<[TokenKind | null, string]> = [
  [null, String.raw`\s+`],
  ['string', String.raw`"(?:[^"\\]|\\.)*(?:"|$)|'(?:[^'\\]|\\.)*(?:'|$)`],
  ['operator', '!=|!~|>=|<=|[=~><]'],
  ['pipe', String.raw`\|`],
  ['lparen', String.raw`\(`],
  ['rparen', String.raw`\)`],
  ['dot', String.raw`\.`],
  ['comma', ','],
  ['number', String.raw`[-+]?[0-9]*\.?[0-9]+`],
  ['ident', '@?[a-zA-Z_][a-zA-Z0-9_:@-]*'],
  ['other', '[^]'],
]
const TOKEN_RE = new RegExp(TOKEN_RULES.map(([, pattern]) => `(${pattern})`).join('|'), 'g')

function tokenize(query: string): Token[] {
  const tokens: Token[] = []
  for (const match of query.matchAll(TOKEN_RE)) {
    const rule = match.slice(1).findIndex(group => group !== undefined)
    const kind = TOKEN_RULES[rule][0]
    if (kind) tokens.push({ kind, text: match[0], start: match.index, end: match.index + match[0].length })
  }
  return tokens
}

export interface FieldRef {
  name: string
  baseName: string
  start: number
  end: number
}

export interface AggregateItem {
  func: string
  field: FieldRef | null
  alias: string | null
}

export interface GroupKey {
  time: boolean
  window: string | null
  field: FieldRef | null
  alias: string | null
}

// What the text expects at its end, for autocomplete. "item" follows a comma
// between items, "argument" follows `func(`, "alias" follows `as`, "key"
// follows `by` or a comma between keys. "other" is anything else: inside a
// path or a window, a later stage, or text the reader cannot follow.
export type AggregationNext =
  | { expect: 'item' }
  | { expect: 'argument'; func: string }
  | { expect: 'afterItem'; aliased: boolean }
  | { expect: 'alias' }
  | { expect: 'key' }
  | { expect: 'afterKey'; aliased: boolean }
  | { expect: 'other' }

export interface AggregationOutline {
  items: AggregateItem[]
  keys: GroupKey[]
  next: AggregationNext
}

function firstPipe(tokens: Token[]): number {
  return tokens.findIndex(token => token.kind === 'pipe')
}

// Text after the first pipe outside a string, or null when there is no pipe.
export function pipeTail(query: string): string | null {
  const tokens = tokenize(query)
  const pipe = firstPipe(tokens)
  return pipe < 0 ? null : query.slice(tokens[pipe].end)
}

// A pipe whose first item is `name(` is an aggregation.
export function isAggregation(query: string): boolean {
  const tokens = tokenize(query)
  const pipe = firstPipe(tokens)
  return pipe >= 0 && tokens[pipe + 1]?.kind === 'ident' && tokens[pipe + 2]?.kind === 'lparen'
}

function unquote(segment: string): string {
  return /^['"]/.test(segment) ? segment.slice(1, -1).replace(/\\(.)/g, '$1') : segment
}

function isKeyword(token: Token | undefined, keyword: string): boolean {
  return token?.kind === 'ident' && token.text.toLowerCase() === keyword
}

class Reader {
  constructor(private readonly tokens: Token[], public i: number) {}

  get done(): boolean { return this.i >= this.tokens.length }
  peek(offset = 0): Token | undefined { return this.tokens[this.i + offset] }
  take(kind: TokenKind): Token | undefined {
    const token = this.peek()
    if (token?.kind !== kind) return undefined
    this.i++
    return token
  }
  keyword(keyword: string): boolean {
    if (!isKeyword(this.peek(), keyword)) return false
    this.i++
    return true
  }

  // A field path. A trailing dot leaves the path open, so it reads as not done.
  path(): FieldRef | null | 'open' {
    const first = this.peek()
    if (first?.kind !== 'ident' && first?.kind !== 'string') return null
    this.i++
    const segments = [unquote(first.text)]
    let end = first.end
    while (this.peek()?.kind === 'dot') {
      this.i++
      const segment = this.peek()
      if (segment?.kind !== 'ident' && segment?.kind !== 'string') return 'open'
      this.i++
      segments.push(unquote(segment.text))
      end = segment.end
    }
    return { name: segments.join('.'), baseName: segments[0], start: first.start, end }
  }

  // An alias is an identifier, or a number then an identifier so that `as 2xx`
  // reaches the server and gets its message there.
  alias(): string | null {
    const number = this.take('number')
    const ident = this.take('ident')
    if (number && ident && ident.start === number.end) return number.text + ident.text
    if (number && ident) return null
    return (number ?? ident)?.text ?? null
  }
}

// The functions, keys and aliases of the aggregation pipe, or null when the
// query has none. A window such as `5m` is the number and unit joined; a space
// between them stays, as in the Go parser.
export function aggregationOutline(query: string): AggregationOutline | null {
  const tokens = tokenize(query)
  const pipe = firstPipe(tokens)
  if (pipe < 0 || tokens[pipe + 1]?.kind !== 'ident' || tokens[pipe + 2]?.kind !== 'lparen') return null
  const reader = new Reader(tokens, pipe + 1)
  const outline: AggregationOutline = { items: [], keys: [], next: { expect: 'other' } }
  const stop = (next: AggregationNext) => {
    outline.next = reader.done ? next : { expect: 'other' }
    return outline
  }

  for (;;) {
    if (reader.done) return stop({ expect: 'item' })
    const func = reader.take('ident')
    if (!func || !reader.take('lparen')) return stop({ expect: 'other' })
    if (reader.done) return stop({ expect: 'argument', func: func.text.toLowerCase() })
    const field = reader.path()
    if (field === 'open' || !reader.take('rparen')) return stop({ expect: 'other' })
    const item: AggregateItem = { func: func.text.toLowerCase(), field, alias: null }
    outline.items.push(item)
    if (reader.keyword('as')) {
      if (reader.done) return stop({ expect: 'alias' })
      item.alias = reader.alias()
      if (item.alias === null) return stop({ expect: 'other' })
    }
    if (reader.done) return stop({ expect: 'afterItem', aliased: item.alias !== null })
    if (!reader.take('comma')) break
  }

  if (!reader.keyword('by')) return stop({ expect: 'other' })
  for (;;) {
    if (reader.done) return stop({ expect: 'key' })
    const key: GroupKey = { time: false, window: null, field: null, alias: null }
    if (isKeyword(reader.peek(), 'time') && reader.peek(1)?.kind !== 'dot') {
      reader.i++
      key.time = true
      if (reader.take('lparen')) {
        const number = reader.take('number')
        const unit = reader.take('ident')
        if (!number || !unit || !reader.take('rparen')) return stop({ expect: 'other' })
        key.window = number.text + (unit.start === number.end ? '' : ' ') + unit.text
      }
    } else {
      const field = reader.path()
      if (field === null || field === 'open') return stop({ expect: 'other' })
      key.field = field
    }
    outline.keys.push(key)
    if (reader.keyword('as')) {
      if (reader.done) return stop({ expect: 'alias' })
      key.alias = reader.alias()
      if (key.alias === null) return stop({ expect: 'other' })
    }
    if (reader.done) return stop({ expect: 'afterKey', aliased: key.alias !== null })
    if (!reader.take('comma')) break
  }
  return stop({ expect: 'other' })
}
