import { describe, expect, it } from 'vitest'
import { fieldsWithValues, logsqlFilterFields } from '../fieldRelevance'

describe('fieldsWithValues', () => {
  it('keeps fields with a value in any row and drops null and empty ones', () => {
    const rows = [
      { level: 'error', host: '', trace: null },
      { level: 'info', host: 'a', status: 0 },
    ]
    expect([...fieldsWithValues(rows)].sort()).toEqual(['host', 'level', 'status'])
  })
})

describe('logsqlFilterFields', () => {
  it('reads bare, quoted and negated filter fields, ahead of and inside pipes', () => {
    const query = 'service:="api" NOT level:="debug" -env:dev "http.status":>=500 | filter user.id:~"x:y" | stats by (host) count()'
    expect(logsqlFilterFields(query).sort()).toEqual(['env', 'http.status', 'level', 'service', 'user.id'])
  })

  it('ignores colons inside quoted values and plain words', () => {
    expect(logsqlFilterFields('error "a: b" msg:"k:v"')).toEqual(['msg'])
  })
})
