import { normalizeExploreMode, type ExploreMode } from '@/lib/queryMetadata'
import type { TimeWindow } from '@/utils/timeWindow'
import { decodeWindowFromUrl } from '@/utils/timeWindowCodec'

export const EXPLORE_PATH = '/logs/explore'

/** What an Explorer URL names besides team and source. */
export type ExploreTarget =
  | { kind: 'draft' }
  | { kind: 'query'; mode: ExploreMode | null; text: string; window: TimeWindow | null; limit: number | null }
  | { kind: 'saved'; queryId: number }
  | { kind: 'share'; token: string }

export interface ExploreLocation {
  teamId: number | null
  sourceId: number | null
  target: ExploreTarget
}

type QueryParams = Record<string, unknown>

function normalizeQueryParams(query: QueryParams): Record<string, string | undefined> {
  const normalized: Record<string, string | undefined> = {}

  const getValue = (key: string) => {
    const value = query[key]
    const first = Array.isArray(value) ? value[0] : value
    return first === undefined || first === null || first === '' ? undefined : String(first)
  }

  const team = getValue('team')
  if (team) normalized.team = team
  const source = getValue('source')
  if (source) normalized.source = source

  const relativeTime = getValue('t') ?? getValue('time')
  if (relativeTime) normalized.t = relativeTime

  const start = getValue('start') ?? getValue('start_time')
  if (start) normalized.start = start
  const end = getValue('end') ?? getValue('end_time')
  if (end) normalized.end = end

  const limit = getValue('limit')
  if (limit) normalized.limit = limit

  const mode = getValue('mode')
  if (mode) normalized.mode = mode

  // An empty q or sql is kept: the link names an empty query, so the draft
  // must not replace it.
  const getText = (key: string) => {
    const value = query[key]
    const first = Array.isArray(value) ? value[0] : value
    return typeof first === 'string' ? first : undefined
  }
  const q = getText('q')
  if (q !== undefined) normalized.q = q
  const sql = getText('sql')
  if (sql !== undefined) normalized.sql = sql

  const id = getValue('id')
  if (id) normalized.id = id

  const share = getValue('share')
  if (share) normalized.share = share

  return normalized
}

function parseId(value: string | undefined): number | null {
  const parsed = value === undefined ? NaN : parseInt(value, 10)
  return Number.isNaN(parsed) ? null : parsed
}

export function parseExploreLocation(query: QueryParams): ExploreLocation {
  const params = normalizeQueryParams(query)
  const teamId = parseId(params.team)
  const sourceId = parseId(params.source)

  if (params.share) return { teamId, sourceId, target: { kind: 'share', token: params.share } }
  const queryId = parseId(params.id)
  if (queryId) return { teamId, sourceId, target: { kind: 'saved', queryId } }
  if (Object.keys(params).every((key) => key === 'team' || key === 'source')) {
    return { teamId, sourceId, target: { kind: 'draft' } }
  }

  const mode = params.mode
    ? normalizeExploreMode(params.mode)
    : params.q !== undefined ? 'logchefql' : params.sql !== undefined ? 'native' : null
  return {
    teamId,
    sourceId,
    target: {
      kind: 'query',
      mode,
      text: (mode === 'native' ? params.sql : params.q) ?? '',
      window: decodeWindowFromUrl(params),
      limit: parseId(params.limit),
    },
  }
}

/** Two URLs show the same Explorer state. */
export function sameExploreLocation(a: QueryParams, b: QueryParams): boolean {
  const [left, right] = [normalizeQueryParams(a), normalizeQueryParams(b)]
  const keys = Object.keys(left)
  return keys.length === Object.keys(right).length && keys.every((key) => left[key] === right[key])
}
