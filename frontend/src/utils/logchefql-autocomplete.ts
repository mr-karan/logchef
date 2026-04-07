/**
 * LogchefQL autocomplete context parsing and helpers.
 *
 * Grammar: field operator value [boolean field operator value]*
 * Operators: = != ~ !~ > < >= <=
 * Boolean: and or (case-insensitive)
 * Values: "quoted strings", 'single quoted', unquoted words, numbers
 */

// --- Types ---

export type LogchefQLContext =
  | { suggest: "fields"; partial: string }
  | { suggest: "operators"; key: string }
  | { suggest: "values"; key: string; operator: string; partial: string; quote: '"' | "'" | null; contextQuery: string }
  | { suggest: "boolean"; partial: string }
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
const QUOTED_STR = '(?:"[^"]*"|' + "'[^']*'" + ')'

// Pre-compiled regexes (avoid recompilation on every keystroke)
const BOOL_TAIL_RE = /\b(?:and|or)\s+$/i
const FIELD_OP_RE = new RegExp(`(${FIELD_CHARS})\\s*(${OP_PATTERN})\\s*$`)
const FIELD_OP_VALUE_RE = new RegExp(`(${FIELD_CHARS})\\s*(${OP_PATTERN})\\s*([^"'\\s]*)$`)
const COMPLETE_COND_RE = new RegExp(
  `(${FIELD_CHARS})\\s*(?:${OP_PATTERN})\\s*(?:${QUOTED_STR}|\\S+)\\s*$`
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

  if (COMPLETE_COND_RE.test(trimmed)) {
    const lastChar = trimmed[trimmed.length - 1]
    if (lastChar === '"' || lastChar === "'" || /\s$/.test(text)) {
      return { suggest: "boolean", partial: "" }
    }
    // Still typing an unquoted value — this is a value context
    // (already handled in Step 3)
    return { suggest: "none" }
  }

  const trailingMatch = trimmed.match(TRAILING_WORD_RE)
  if (trailingMatch) {
    const word = trailingMatch[1]

    // Is it a known field? → suggest operators
    if (fields.includes(word)) {
      return { suggest: "operators", key: word }
    }

    // Is it a partial "and"/"or"?
    if (/^(a|an|and|o|or)$/i.test(word)) {
      // Could be typing a boolean or a field name — suggest both contexts
      // but prefer fields since they're more actionable
      return { suggest: "fields", partial: word }
    }

    return { suggest: "fields", partial: word }
  }

  return { suggest: "fields", partial: "" }
}

// --- Internal helpers ---

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
