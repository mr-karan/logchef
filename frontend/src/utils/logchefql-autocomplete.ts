/**
 * LogchefQL autocomplete context parsing and helpers.
 *
 * Grammar: field operator value [boolean field operator value]* [| pipe]
 * Operators: = != ~ !~ > < >= <=
 * Boolean: and or (case-insensitive)
 * Values: "quoted strings", 'single quoted', unquoted words, numbers
 * Pipe: fields to select, or an aggregation such as `count() by service`
 */

import { aggregationOutline, pipeTail } from "@/utils/logchefql/aggregation"

// --- Types ---

export type AggregationKeyword = "as" | "by" | ","

type LogchefQLContext =
  | { suggest: "fields"; partial: string }
  | { suggest: "operators"; key: string }
  | { suggest: "values"; key: string; operator: string; partial: string; quote: '"' | "'" | null; contextQuery: string }
  | { suggest: "boolean"; partial: string }
  // After a pipe: functions, and fields too where a select can start.
  | { suggest: "functions"; partial: string; fields: boolean }
  | { suggest: "functionArgument"; func: string; partial: string }
  | { suggest: "aggregationKeywords"; keywords: AggregationKeyword[]; partial: string }
  | { suggest: "groupKeys"; partial: string }
  | { suggest: "none" }

// --- Field type helpers ---

const COMPLEX_TYPE_RE = /^(map|array|tuple|json)\b/i

/** Any scalar field that can have distinct values suggested. Broader than sidebar's isFilterableField. */
export function isValueSuggestableField(type: string): boolean {
  return !COMPLEX_TYPE_RE.test(type.replace(/^(LowCardinality|Nullable)\(/i, ''))
}

const NUMERIC_TYPE_RE = /\b(u?int\d|float\d|decimal)/i

export function isNumericFieldType(type: string): boolean {
  const clean = type
    .replace(/LowCardinality\(([^)]+)\)/gi, '$1')
    .replace(/Nullable\(([^)]+)\)/gi, '$1')
  return NUMERIC_TYPE_RE.test(clean)
}

// --- Formatting ---

export function formatCountShort(count: number): string {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return count.toString()
}

export function formatFieldDetail(type: string, totalDistinct: number | null): string {
  const clean = type
    .replace(/LowCardinality\(([^)]+)\)/gi, '$1')
    .replace(/Nullable\(([^)]+)\)/gi, '$1')
  if (totalDistinct != null) {
    return `${clean} · ${formatCountShort(totalDistinct)} values`
  }
  return clean
}

// --- Value insert text ---

export function buildValueInsertText(
  rawValue: string,
  fieldType: string,
  quote: '"' | "'" | null,
): string {
  const numeric = isNumericFieldType(fieldType)

  if (numeric) {
    // Numeric: insert bare value, close any accidental open quote
    if (quote) return `${rawValue}${quote}`
    return rawValue
  }

  // String-like: escape the delimiter and backslashes, then wrap in quotes
  if (quote) {
    const escaped = escapeForQuote(rawValue, quote)
    return `${escaped}${quote}`
  }
  const escaped = escapeForQuote(rawValue, '"')
  return `"${escaped}"`
}

export function buildValueCompletionEdit(
  rawValue: string,
  fieldType: string,
  quote: '"' | "'" | null,
  textAfterCursor: string,
): { insertText: string; replaceAfter: number; retrigger: boolean } {
  return {
    insertText: buildValueInsertText(rawValue, fieldType, quote),
    replaceAfter: valueRestLength(textAfterCursor, quote),
    // A bare number still parses as a partial value, so reopening would list values again.
    retrigger: !isNumericFieldType(fieldType),
  }
}

// A quote glued to more text opens the next value, unless that text is and/or.
function valueRestLength(textAfterCursor: string, quote: '"' | "'" | null): number {
  if (!quote) return /^[^\s()"'|=!~<>]*/.exec(textAfterCursor)?.[0].length ?? 0
  for (let i = 0; i < textAfterCursor.length; i++) {
    if (textAfterCursor[i] === '\\') i++
    else if (textAfterCursor[i] === quote) return /^(?:$|[\s)|]|(?:and|or)(?![\w.]))/i.test(textAfterCursor.slice(i + 1)) ? i + 1 : 0
  }
  return 0
}

export function buildBooleanInsertText(
  keyword: 'and' | 'or' | 'as' | 'by',
  textBeforeCursor: string,
  partial: string,
): string {
  const beforeKeyword = textBeforeCursor.slice(0, textBeforeCursor.length - partial.length)
  return `${/\s$/.test(beforeKeyword) ? '' : ' '}${keyword} `
}

// A comma joins the item before it, so the spaces between them go.
export function buildCommaEdit(textBeforeCursor: string, partial: string): { insertText: string; deleteBefore: number } {
  const beforeComma = textBeforeCursor.slice(0, textBeforeCursor.length - partial.length)
  return { insertText: ', ', deleteBefore: /[ \t]*$/.exec(beforeComma)?.[0].length ?? 0 }
}

// A bare `time` after `by` is the time bucket, so a field named time is quoted.
export function groupKeyInsertText(field: string): string {
  return /^time$/i.test(field) ? `"${field}"` : field
}

type LogchefQLOperator = '=' | '!=' | '~' | '!~' | '>' | '<' | '>=' | '<='

export function getLogchefQLOperators(fieldType: string): LogchefQLOperator[] {
  return isNumericFieldType(fieldType)
    ? ['=', '>=', '>', '<=', '<', '!=']
    : ['=', '!=', '~', '!~', '>', '<', '>=', '<=']
}

function escapeForQuote(value: string, quoteChar: '"' | "'"): string {
  // Escape backslashes first, then the quote delimiter
  return value.replace(/\\/g, '\\\\').replace(new RegExp(escapeRegex(quoteChar), 'g'), `\\${quoteChar}`)
}

// --- Partial filtering ---

export function filterValuesByPartial(
  values: Array<{ value: string; count: number }>,
  partial: string,
  fieldType: string,
): Array<{ value: string; count: number }> {
  if (!partial) return values

  if (isNumericFieldType(fieldType)) {
    // Prefix match on stringified value
    return values.filter(v => String(v.value).startsWith(partial))
  }
  // Case-insensitive substring match
  const lower = partial.toLowerCase()
  return values.filter(v => v.value.toLowerCase().includes(lower))
}

// --- Context parser ---

// Regex building blocks
const FIELD_CHARS = "[\\w][\\w.-]*"
const OP_PATTERN = "!=|!~|>=|<=|[=~><]"
const QUOTED_STR = String.raw`(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')`

// Pre-compiled regexes (avoid recompilation on every keystroke)
const BOOL_TAIL_RE = /\b(?:and|or)\s+$/i
const BOOL_PARTIAL_RE = /\s+(a|an|and|o|or)$/i
const FIELD_OP_RE = new RegExp(`(${FIELD_CHARS})\\s*(${OP_PATTERN})\\s*$`)
const FIELD_OP_VALUE_RE = new RegExp(`(${FIELD_CHARS})\\s*(${OP_PATTERN})\\s*([^"'\\s]*)$`)
const COMPLETE_COND_RE = new RegExp(
  `(${FIELD_CHARS})\\s*(?:${OP_PATTERN})\\s*(?:${QUOTED_STR}|[^"'\\s=~><!()]+)(?:\\s*\\))*\\s*$`
)
const TRAILING_WORD_RE = new RegExp(`(${FIELD_CHARS})$`)

/**
 * Parse the text before the cursor into a LogchefQL autocomplete context.
 *
 * This is a scanner-based approach that splits the input at the last relevant
 * boundary to determine what kind of suggestion is appropriate.
 */
export function parseLogchefQLContext(text: string, fields: string[]): LogchefQLContext {
  if (!text.trim()) return { suggest: "fields", partial: "" }

  const pipeContext = parsePipeContext(text)
  if (pipeContext) return pipeContext

  // Step 1: Detect open quote at cursor position
  const quoteState = getQuoteState(text)

  if (quoteState.inQuote) {
    const beforeQuote = text.slice(0, quoteState.quoteStart)
    const fieldOpMatch = beforeQuote.match(FIELD_OP_RE)
    if (fieldOpMatch) {
      const partial = text.slice(quoteState.quoteStart + 1) // text after the opening quote
      return {
        suggest: "values",
        key: fieldOpMatch[1],
        operator: fieldOpMatch[2],
        partial,
        quote: quoteState.quoteChar,
        contextQuery: extractContextQuery(beforeQuote, fieldOpMatch[1]),
      }
    }
    // Inside a quote but can't determine field context — no suggestions
    return { suggest: "none" }
  }

  const trimmed = text.trimEnd()

  if (BOOL_TAIL_RE.test(text)) {
    return { suggest: "fields", partial: "" }
  }

  if (trimmed.endsWith('(')) return { suggest: "fields", partial: "" }

  const booleanPartial = text.match(BOOL_PARTIAL_RE)
  if (booleanPartial && COMPLETE_COND_RE.test(text.slice(0, booleanPartial.index).trimEnd())) {
    return { suggest: "boolean", partial: booleanPartial[1] }
  }

  const completeCondition = COMPLETE_COND_RE.test(trimmed)
  if (completeCondition && (/\s$/.test(text) || /["')]$/.test(trimmed))) {
    return { suggest: "boolean", partial: "" }
  }

  const fieldOpValueMatch = trimmed.match(FIELD_OP_VALUE_RE)
  if (fieldOpValueMatch) {
    const [, fieldName, operator, partial] = fieldOpValueMatch
    if (fields.includes(fieldName)) {
      return {
        suggest: "values",
        key: fieldName,
        operator,
        partial,
        quote: null,
        contextQuery: extractContextQuery(trimmed, fieldName),
      }
    }
  }

  if (completeCondition) return { suggest: "none" }

  const trailingMatch = trimmed.match(TRAILING_WORD_RE)
  if (trailingMatch) {
    const word = trailingMatch[1]

    // Is it a known field? → suggest operators
    if (fields.includes(word)) {
      return { suggest: "operators", key: word }
    }

    return { suggest: "fields", partial: word }
  }

  return { suggest: "fields", partial: "" }
}

// --- Internal helpers ---

// The context after the first pipe, or null for text without one.
function parsePipeContext(text: string): LogchefQLContext | null {
  const partial = /\s$/.test(text) ? "" : TRAILING_WORD_RE.exec(text)?.[1] ?? ""
  const prefix = text.slice(0, text.length - partial.length)
  const tail = pipeTail(prefix)
  if (tail === null) return null
  if (!tail.trim()) return { suggest: "functions", partial, fields: true }

  const outline = aggregationOutline(prefix)
  if (!outline) return getQuoteState(text).inQuote ? { suggest: "none" } : { suggest: "fields", partial }
  const next = outline.next
  switch (next.expect) {
    case "item":
      return { suggest: "functions", partial, fields: false }
    case "argument":
      return { suggest: "functionArgument", func: next.func, partial }
    case "afterItem":
      return { suggest: "aggregationKeywords", keywords: next.aliased ? ["by", ","] : ["as", "by", ","], partial }
    case "key":
      return { suggest: "groupKeys", partial }
    case "afterKey":
      return { suggest: "aggregationKeywords", keywords: next.aliased ? [","] : ["as", ","], partial }
    default:
      return { suggest: "none" }
  }
}

interface QuoteState {
  inQuote: boolean
  quoteChar: '"' | "'"
  quoteStart: number
}

function getQuoteState(text: string): QuoteState {
  let inDouble = false
  let inSingle = false
  let doubleStart = -1
  let singleStart = -1

  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    // Skip escaped characters
    if (i > 0 && text[i - 1] === '\\' && !(i >= 2 && text[i - 2] === '\\')) continue

    if (ch === '"' && !inSingle) {
      if (inDouble) {
        inDouble = false
      } else {
        inDouble = true
        doubleStart = i
      }
    }
    if (ch === "'" && !inDouble) {
      if (inSingle) {
        inSingle = false
      } else {
        inSingle = true
        singleStart = i
      }
    }
  }

  if (inDouble) return { inQuote: true, quoteChar: '"', quoteStart: doubleStart }
  if (inSingle) return { inQuote: true, quoteChar: "'", quoteStart: singleStart }
  return { inQuote: false, quoteChar: '"', quoteStart: -1 }
}

/**
 * Extract the completed query clauses before the current field being edited.
 * This is used as the logchefql context filter for value fetching.
 *
 * Example: "method=GET and host=" → contextQuery = "method=GET"
 */
function extractContextQuery(textBeforeField: string, currentField: string): string {
  // Strip the current clause: field + operator + any partial value (quoted or unquoted)
  const clauseRe = new RegExp(
    `\\b${escapeRegex(currentField)}\\s*(?:${OP_PATTERN})\\s*(?:"[^"]*|'[^']*|[^"'\\s]*)$`
  )
  const cleaned = textBeforeField.replace(clauseRe, '').trimEnd()

  // Remove trailing boolean operator
  const withoutTrailingBool = cleaned.replace(/\s+(?:and|or)\s*$/i, '').trim()
  return withoutTrailingBool
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
