import { describe, expect, it } from 'vitest'
import {
  applyFilters,
  decodeFilters,
  encodeFilters,
  filterFromAction,
  filterToLogchefql,
  filterToLogsql,
  filteredFields,
  type QueryFilter,
} from '@/utils/queryFilters'

const region: QueryFilter = { field: 'cloud.region', operator: 'is', values: ['us-central1'] }

describe('filter pills', () => {
  it('maps table actions to pills', () => {
    expect(filterFromAction('level', '=', 'error')).toEqual({ field: 'level', operator: 'is', values: ['error'] })
    expect(filterFromAction('level', '!=', 'error')).toEqual({ field: 'level', operator: 'is', values: ['error'], negate: true })
    expect(filterFromAction('msg', '!~', 'boom')).toEqual({ field: 'msg', operator: 'contains', values: ['boom'], negate: true })
    expect(filterFromAction('ms', '>=', 5)).toEqual({ field: 'ms', operator: 'gte', values: [5] })
  })

  it('writes LogchefQL', () => {
    expect(filterToLogchefql(region)).toBe('cloud.region="us-central1"')
    expect(filterToLogchefql({ ...region, negate: true })).toBe('cloud.region!="us-central1"')
    expect(filterToLogchefql({ field: 's', operator: 'is_one_of', values: [500, 502] })).toBe('(s=500 or s=502)')
    expect(filterToLogchefql({ field: 's', operator: 'is_one_of', values: [500, 502], negate: true })).toBe('s!=500 and s!=502')
    expect(filterToLogchefql({ field: 'u', operator: 'exists', values: [] })).toBe('u!=""')
    expect(filterToLogchefql({ field: 'ms', operator: 'gt', values: [100], negate: true })).toBe('ms<=100')
  })

  it('writes LogsQL', () => {
    expect(filterToLogsql(region)).toBe('cloud.region:="us-central1"')
    expect(filterToLogsql({ ...region, negate: true })).toBe('NOT cloud.region:="us-central1"')
    expect(filterToLogsql({ field: 'level', operator: 'is_one_of', values: ['info', 'debug'] })).toBe('level:in("info","debug")')
    expect(filterToLogsql({ field: '"http status"', operator: 'exists', values: [], negate: true })).toBe('NOT "http status":*')
    expect(filterToLogsql({ field: 'msg', operator: 'contains', values: ['a.b'] })).toBe('msg:~"a\\\\.b"')
    expect(filterToLogsql({ field: 'ms', operator: 'lt', values: [3], negate: true })).toBe('NOT ms:<3')
  })

  it('joins the query with and, ahead of pipes, skipping disabled pills', () => {
    const off: QueryFilter = { field: 'x', operator: 'is', values: ['y'], disabled: true }
    expect(applyFilters('level="error" | count() by host', [region, off], 'logchefql')).toBe('level="error" and cloud.region="us-central1" | count() by host')
    expect(applyFilters('a=1 or b=2', [region], 'logchefql')).toBe('(a=1 or b=2) and cloud.region="us-central1"')
    expect(applyFilters('', [region], 'logchefql')).toBe('cloud.region="us-central1"')
    expect(applyFilters('error | stats count()', [region], 'logsql')).toBe('error cloud.region:="us-central1" | stats count()')
    expect(applyFilters('*', [region], 'logsql')).toBe('cloud.region:="us-central1"')
    expect(applyFilters('error', [], 'logsql')).toBe('error')
  })

  it('round-trips through the URL form and drops malformed pills', () => {
    const filters: QueryFilter[] = [region, { field: 'u', operator: 'exists', values: [], negate: true, disabled: true, label: ' users ' }]
    const encoded = encodeFilters(filters)
    expect(decodeFilters(encoded)).toEqual([region, { field: 'u', operator: 'exists', values: [], negate: true, disabled: true, label: 'users' }])
    expect(encodeFilters([])).toBe('')
    expect(decodeFilters('not json')).toEqual([])
    expect(decodeFilters([{ field: 'a', operator: 'like', values: ['b'] }, { field: '', operator: 'is', values: ['b'] }, { field: 'a', operator: 'is', values: [] }, region])).toEqual([region])
  })

  it('reports the columns enabled pills filter', () => {
    expect(filteredFields([region, { field: '"http.status"', operator: 'is', values: [200] }, { field: 'off', operator: 'is', values: [1], disabled: true }]))
      .toEqual(['cloud.region', 'http.status'])
  })
})
