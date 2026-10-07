import { describe, expect, test } from 'bun:test'
import { attachmentPayload, boundedRange, parseEvidence, parseHistogram, parseSources, selectedInterval, parseComparison } from './data'

describe('investigation evidence', () => {
 test('does not report percentages for incomplete or zero-baseline comparisons', () => {
  const window = { start: '2026-10-01T00:00:00Z', end: '2026-10-01T01:00:00Z', row_count: 100, truncated: true }
  const result = { window1: window, window2: { ...window, row_count: 50, truncated: false }, delta: { row_count_percent: -50 } }
  expect(parseComparison(result).percent).toBeNull()
  expect(parseComparison({ ...result, window1: { ...window, row_count: 0, truncated: false } }).percent).toBeNull()
  expect(parseComparison({ ...result, window1: { ...window, truncated: false } }).percent).toBe(-50)
  expect(() => parseComparison({ ...result, window2: { ...window, row_count: -1 } })).toThrow()
 })
 test('uses advertised source capabilities instead of backend guesses', () => {
  const sources = parseSources({ sources: [
   { id: 1, source_type: 'clickhouse', capabilities: ['log_context'] },
   { id: 2, source_type: 'victorialogs', capabilities: ['histogram'] },
   { id: 3, source_type: 'clickhouse' },
  ] })
  expect(sources[0]?.capabilities.includes('log_context')).toBe(true)
  expect(sources[1]?.capabilities.includes('log_context')).toBe(false)
  expect(sources[2]?.capabilities.includes('log_context')).toBe(false)
 })
 test('rejects inverted or excessive ranges', () => {
  expect(() => boundedRange('2026-10-01T00:00:00Z', '2026-09-30T00:00:00Z')).toThrow()
  expect(() => boundedRange('2026-09-01T00:00:00Z', '2026-10-01T00:00:00Z')).toThrow()
 })
 test('sparse buckets select their actual granularity', () => {
  const histogram = parseHistogram({ granularity: '1m', data: [{ bucket: '2026-10-01T00:00:00Z', log_count: 2 }, { bucket: '2026-10-01T00:10:00Z', log_count: 3 }] })
  expect(selectedInterval(histogram, 0, '2026-10-01T00:00:00Z', '2026-10-01T01:00:00Z').end_time).toBe('2026-10-01T00:01:00.000Z')
 })
 test('preserves fractional durations, warnings, and truncation', () => {
  const evidence = parseEvidence({ logs: [{ message: '<script>unsafe()</script>' }], columns: [], stats: { execution_time_ms: 1.25, truncated: true, limit_applied: 100 }, query_id: 'q1', warnings: [{ message: 'sample' }] })
  expect(evidence.duration).toBe(1.25)
  expect(evidence.truncated).toBe(true)
  expect(evidence.warnings).toEqual(['sample'])
  expect(evidence.logs[0]?.message).toBe('<script>unsafe()</script>')
 })
 test('keeps the executed range separate from a selected interval', () => {
  const input = { team_id: 1, source_id: 2, query: 'level=error', start_time: '2026-10-01T00:00:00Z', end_time: '2026-10-01T01:00:00Z', timezone: 'UTC' }
  const evidence = parseEvidence({ logs: [{ message: 'a' }, { message: 'b' }], columns: [], stats: { truncated: false, limit_applied: 100 }, query_id: 'q-full', warnings: [{ message: 'partial shard' }] })
  const histogram = parseHistogram({ granularity: '1m', data: [{ bucket: '2026-10-01T00:10:00Z', log_count: 2 }] })
  const payload = attachmentPayload(input, evidence, histogram, 0, [1])
  expect(payload.query_range).toEqual({ start_time: '2026-10-01T00:00:00Z', end_time: '2026-10-01T01:00:00Z', query_id: 'q-full' })
  expect(payload.selected_interval).toEqual({ start_time: '2026-10-01T00:10:00.000Z', end_time: '2026-10-01T00:11:00.000Z' })
  expect(payload.rows).toEqual([{ message: 'b' }])
  expect(payload).not.toHaveProperty('start_time')
  expect(payload).not.toHaveProperty('query_id')
  expect(attachmentPayload(input, evidence, histogram, null, []).selected_interval).toBeUndefined()
 })
 test('attaches backend warnings with the evidence', () => {
  const input = { query: '', start_time: '2026-10-01T00:00:00Z', end_time: '2026-10-01T01:00:00Z', timezone: 'UTC' }
  const evidence = parseEvidence({ logs: [], columns: [], stats: {}, query_id: 'q1', warnings: [{ code: 'PARTIAL', message: 'partial shard' }] })
  expect(attachmentPayload(input, evidence, { buckets: [], notice: '', granularity: '' }, null, []).warnings).toEqual(['partial shard'])
 })
})
