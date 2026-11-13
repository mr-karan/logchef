import { fieldPath } from '@/utils/logchefql/filters'

export interface RowField {
  // LogchefQL field reference, e.g. msg.http.status
  field: string
  depth: number
  value: unknown
  nested: boolean
  // Inside JSON held in a string column: LogchefQL extracts it as text, so
  // filters must compare it as text.
  textual: boolean
  // Objects are listed for context; their leaves carry the filters.
  isObject: boolean
}

const MAX_DEPTH = 6
const MAX_EMBEDDED_JSON_LENGTH = 20000

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function embeddedObject(value: unknown): Record<string, unknown> | null {
  if (isPlainObject(value)) return value
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed.startsWith('{') || trimmed.length > MAX_EMBEDDED_JSON_LENGTH) return null
  try {
    const parsed: unknown = JSON.parse(trimmed)
    return isPlainObject(parsed) ? parsed : null
  } catch {
    return null
  }
}

// Lists a log row's fields in order, descending into objects and into strings
// that hold a JSON object, so nested keys can be filtered by path.
export function flattenRow(row: Record<string, unknown>): RowField[] {
  const fields: RowField[] = []
  const visit = (path: string[], value: unknown, textual: boolean) => {
    const depth = path.length - 1
    const object = depth < MAX_DEPTH ? embeddedObject(value) : null
    fields.push({ field: fieldPath(path), depth, value, nested: depth > 0, textual, isObject: object !== null })
    const childrenTextual = textual || typeof value === 'string'
    if (object) for (const [key, child] of Object.entries(object)) visit([...path, key], child, childrenTextual)
  }
  for (const [key, value] of Object.entries(row)) visit([key], value, false)
  return fields
}
