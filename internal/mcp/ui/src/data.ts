export interface Source {
  id: number
  name: string
  sourceType: string
  timestampField: string
  capabilities: string[]
  teams: { id: number; name: string }[]
}

export interface InvestigationInput {
  team_id?: number
  source_id?: number
  query: string
  start_time: string
  end_time: string
  timezone: string
}

export interface QueryEvidence {
  logs: Record<string, unknown>[]
  columns: { name: string; type: string }[]
  queryID: string
  duration: number
  truncated: boolean
  limit: number
  warnings: string[]
}

export interface Bucket { bucket: string; log_count: number }
export interface Histogram { buckets: Bucket[]; notice: string; granularity: string }

export function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function text(value: unknown): string { return typeof value === 'string' ? value : '' }
export function numeric(value: unknown): number { return typeof value === 'number' && Number.isFinite(value) ? value : 0 }
export function items(value: unknown): unknown[] { return Array.isArray(value) ? value : [] }

export function parseSources(value: unknown): Source[] {
  if (!record(value) || !Array.isArray(value.sources)) throw new Error('Logchef returned an invalid source list.')
  return value.sources.map((item) => {
    if (!record(item) || !Number.isInteger(item.id) || typeof item.id !== 'number') throw new Error('Source is missing its ID.')
    return {
      id: item.id, name: text(item.name), sourceType: text(item.source_type), timestampField: text(item.ts_field),
      capabilities: items(item.capabilities).flatMap((capability) => typeof capability === 'string' ? [capability] : []),
      teams: items(item.teams).flatMap((team) => record(team) && typeof team.id === 'number' ? [{ id: team.id, name: text(team.name) }] : []),
    }
  })
}

export function parseEvidence(value: unknown): QueryEvidence {
  if (!record(value) || !Array.isArray(value.logs) || !record(value.stats)) throw new Error('Logchef returned an invalid query result.')
  return {
    logs: value.logs.filter(record),
    columns: items(value.columns).flatMap((column) => record(column) && typeof column.name === 'string' ? [{ name: column.name, type: text(column.type) }] : []),
    queryID: text(value.query_id), duration: numeric(value.stats.execution_time_ms),
    truncated: value.stats.truncated === true, limit: numeric(value.stats.limit_applied),
    warnings: items(value.warnings).flatMap((warning) => record(warning) && typeof warning.message === 'string' ? [warning.message] : []),
  }
}

export function parseHistogram(value: unknown): Histogram {
  if (!record(value) || !Array.isArray(value.data)) throw new Error('Logchef returned an invalid histogram.')
  // Multiple backend groups may share a bucket. Sum them before selection.
  const buckets = new Map<string, number>()
  for (const item of value.data) {
    if (!record(item) || typeof item.bucket !== 'string' || typeof item.log_count !== 'number') throw new Error('Invalid histogram bucket.')
    if (!Number.isFinite(Date.parse(item.bucket)) || !Number.isFinite(item.log_count) || item.log_count < 0) throw new Error('Invalid histogram value.')
    buckets.set(item.bucket, (buckets.get(item.bucket) ?? 0) + item.log_count)
  }
  return { buckets: [...buckets].map(([bucket, log_count]) => ({ bucket, log_count })).sort((a, b) => Date.parse(a.bucket) - Date.parse(b.bucket)), notice: text(value.notice), granularity: text(value.granularity) }
}

export function display(value: unknown): string {
  if (typeof value === 'string') return value
  return JSON.stringify(value) ?? ''
}

export function boundedRange(start: string, end: string): { start_time: string; end_time: string } {
  const from = Date.parse(start)
  const to = Date.parse(end)
  if (!Number.isFinite(from) || !Number.isFinite(to) || from >= to) throw new Error('Enter a valid start and end time. End must be after start.')
  if (to - from > 7 * 86400000) throw new Error('Use a time range of seven days or less.')
  return { start_time: new Date(from).toISOString(), end_time: new Date(to).toISOString() }
}

export function selectedInterval(histogram: Histogram, index: number, start: string, end: string): { start_time: string; end_time: string } {
  const bucket = histogram.buckets[index]
  if (!bucket) throw new Error('Select an available histogram bucket.')
  const match = /^(\d+)(s|m|h|d)$/.exec(histogram.granularity)
  const units: Record<string, number> = { s: 1000, m: 60000, h: 3600000, d: 86400000 }
  const width = match ? Number(match[1]) * (units[match[2] ?? ''] ?? 0) : 0
  if (width <= 0) throw new Error('The backend did not return a supported histogram granularity.')
  return boundedRange(new Date(Math.max(Date.parse(start), Date.parse(bucket.bucket))).toISOString(), new Date(Math.min(Date.parse(end), Date.parse(bucket.bucket) + width)).toISOString())
}

export interface EvidenceAttachment {
  team_id?: number
  source_id?: number
  query: string
  timezone: string
  // The range and query that produced every attached row. Never rewritten.
  query_range: { start_time: string; end_time: string; query_id: string }
  // A histogram interval the user selected inside query_range. It was not queried on its own.
  selected_interval?: { start_time: string; end_time: string }
  truncated: boolean
  returned_rows: number
  limit_applied: number
  sample_limited: boolean
  warnings: string[]
  rows: Record<string, unknown>[]
}

export function attachmentPayload(input: InvestigationInput, evidence: QueryEvidence, histogram: Histogram, bucket: number | null, rowIndexes: number[]): EvidenceAttachment {
  const payload: EvidenceAttachment = {
    team_id: input.team_id, source_id: input.source_id, query: input.query, timezone: input.timezone,
    query_range: { start_time: input.start_time, end_time: input.end_time, query_id: evidence.queryID },
    truncated: evidence.truncated, returned_rows: evidence.logs.length, limit_applied: evidence.limit,
    sample_limited: evidence.truncated || evidence.logs.length >= (evidence.limit || 100),
    warnings: evidence.warnings,
    rows: rowIndexes.flatMap((index) => evidence.logs[index] ? [evidence.logs[index]] : []).slice(0, 20),
  }
  if (bucket !== null) payload.selected_interval = selectedInterval(histogram, bucket, input.start_time, input.end_time)
  return payload
}

export interface ComparisonWindow { start: string; end: string; rows: number; truncated: boolean }
export interface WindowComparison { previous: ComparisonWindow; current: ComparisonWindow; difference: number; percent: number | null }

export function parseComparison(value: unknown): WindowComparison {
  if (!record(value) || !record(value.delta)) throw new Error('Invalid window comparison.')
  function window(value: unknown): ComparisonWindow {
    if (!record(value) || typeof value.row_count !== 'number' || !Number.isInteger(value.row_count) || value.row_count < 0 || !text(value.start) || !text(value.end)) throw new Error('Invalid comparison window.')
    return { start: text(value.start), end: text(value.end), rows: value.row_count, truncated: value.truncated === true }
  }
  const previous = window(value.window1)
  const current = window(value.window2)
  const percent = value.delta.row_count_percent
  if (percent !== null && (typeof percent !== 'number' || !Number.isFinite(percent))) throw new Error('Invalid comparison percentage.')
  return { previous, current, difference: current.rows - previous.rows,
    percent: previous.truncated || current.truncated || previous.rows === 0 ? null : percent }
}

export function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date.toLocaleTimeString(undefined, { timeZone: 'UTC', hour: '2-digit', minute: '2-digit', hour12: false }) : value
}
