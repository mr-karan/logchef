import type { CancellationToken } from 'monaco-editor/editor/editor.api.js'
import { sourcesApi, type FieldValueInfo } from '@/api/sources'
import { isPriorityField } from '@/lib/sourceFields'
import { useFieldValuesStore } from '@/stores/exploreFieldValues'

/** A time window resolved at lookup time. */
export interface CompletionValuesWindow {
  /** The window as the user chose it (for example "last 15m"), so relative windows keep one cache entry. */
  key: string
  startTime: string
  endTime: string
}

export type CompletionValuesWindowGetter = () => CompletionValuesWindow | null

interface CompletionValuesRequest {
  teamId: number
  sourceId: number
  fieldName: string
  /** The field's schema type. An empty type never looks up values. */
  fieldType: string
  getWindow: CompletionValuesWindowGetter | undefined
  /** The user asked for suggestions (Ctrl+Space) instead of getting them while typing. */
  invoked: boolean
}

const LOOKUP_TIMEOUT_MS = 1500
const VALUES_TTL_MS = 60_000
const FAILURE_COOLDOWN_MS = 30_000
const MAX_AUTOMATIC_LOOKUPS = 2
const MAX_SETTLED_LOOKUPS = 100

/**
 * Field values for editor completion. Reads, in order: this editor's own cache for the
 * field in the current window, then the values the sidebar loaded for the same source and
 * field, then the backend when the lookup policy allows it. Failures are cached as empty,
 * and an empty cache entry still defers to values the sidebar loads later.
 */
export function useCompletionFieldValues() {
  const sidebarValues = useFieldValuesStore()
  const settled = new Map<string, { values: FieldValueInfo[]; expiresAt: number }>()
  const pending = new Map<string, {
    controller: AbortController
    automatic: boolean
    result: Promise<FieldValueInfo[]>
  }>()

  function remember(key: string, values: FieldValueInfo[], ttl: number) {
    settled.delete(key)
    settled.set(key, { values, expiresAt: Date.now() + ttl })
    const oldest = settled.keys().next().value
    if (settled.size > MAX_SETTLED_LOOKUPS && oldest !== undefined) settled.delete(oldest)
  }

  function lookUp(key: string, request: CompletionValuesRequest, window: CompletionValuesWindow) {
    const controller = new AbortController()
    let timedOut = false
    const timeout = setTimeout(() => {
      timedOut = true
      controller.abort()
    }, LOOKUP_TIMEOUT_MS)
    const result = sourcesApi.getFieldValues(
      request.teamId, request.sourceId, request.fieldName, request.fieldType,
      window.startTime, window.endTime, undefined, 10, 'logchefql',
      undefined, undefined, controller.signal,
    ).then(response => {
      if (!response.data) throw new Error('missing field values')
      const values = [...response.data.values].sort((a, b) => b.count - a.count)
      remember(key, values, VALUES_TTL_MS)
      return values
    }).catch(() => {
      if (timedOut || !controller.signal.aborted) remember(key, [], FAILURE_COOLDOWN_MS)
      return []
    }).finally(() => {
      clearTimeout(timeout)
      if (pending.get(key)?.controller === controller) pending.delete(key)
    })
    const entry = { controller, automatic: !request.invoked, result }
    pending.set(key, entry)
    return entry
  }

  async function getValues(
    request: CompletionValuesRequest,
    token: Pick<CancellationToken, 'isCancellationRequested'>,
  ): Promise<FieldValueInfo[]> {
    if (token.isCancellationRequested) return []
    const window = request.getWindow?.() ?? null
    const key = window && `${request.teamId}|${request.sourceId}|${request.fieldName}|${window.key}`
    const cached = key ? settled.get(key) : undefined
    const fresh = cached && cached.expiresAt > Date.now() ? cached.values : null
    if (fresh?.length) return fresh
    const shared = sidebarValues.findCachedValues(request.sourceId, request.fieldName)
    if (shared) return shared.values
    if (fresh || !window || !key || !request.fieldType) return []

    let entry = pending.get(key)
    if (!entry) {
      const automaticLookups = [...pending.values()].filter(lookup => lookup.automatic).length
      // Typing looks up only the fields the sidebar loads by itself; the rest are often high-cardinality or personal data.
      if (!request.invoked && (!isPriorityField(request.fieldType) || automaticLookups >= MAX_AUTOMATIC_LOOKUPS)) return []
      entry = lookUp(key, request, window)
    }
    const values = await entry.result
    return token.isCancellationRequested ? [] : values
  }

  function cancelAll() {
    pending.forEach(entry => entry.controller.abort())
    pending.clear()
  }

  return { getValues, cancelAll }
}
