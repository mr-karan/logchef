import { describe, expect, it } from 'vitest'
import { appendLogsqlFilter, buildLogsqlFilter, escapeRegex, logsqlColumn, logsqlField } from './filters'

describe('logsqlField', () => {
  it('keeps simple and dotted names bare and quotes the rest', () => {
    expect(logsqlField('level')).toBe('level')
    expect(logsqlField('_msg')).toBe('_msg')
    expect(logsqlField('http.status')).toBe('http.status')
    expect(logsqlField('user agent')).toBe('"user agent"')
    expect(logsqlField('k8s-pod')).toBe('"k8s-pod"')
  })

  it('reads a quoted LogchefQL segment as the column name', () => {
    expect(logsqlColumn('"http.status"')).toBe('http.status')
    expect(logsqlField('"http.status"')).toBe('http.status')
    expect(logsqlField('"say \\"hi\\""')).toBe('"say \\"hi\\""')
  })
})

describe('buildLogsqlFilter', () => {
  it('builds exact include and exclude filters', () => {
    expect(buildLogsqlFilter('level', '=', 'error')).toBe('level:="error"')
    expect(buildLogsqlFilter('level', '!=', 'error')).toBe('NOT level:="error"')
    expect(buildLogsqlFilter('status', '=', 500)).toBe('status:=500')
    expect(buildLogsqlFilter('user', '=', null)).toBe('user:=""')
  })

  it('escapes quotes and backslashes in values', () => {
    expect(buildLogsqlFilter('_msg', '=', 'say "hi" \\o/')).toBe('_msg:="say \\"hi\\" \\\\o/"')
  })

  it('matches contains filters as literal regex text', () => {
    expect(buildLogsqlFilter('_msg', '~', 'GET /api?id=1')).toBe('_msg:~"GET /api\\\\?id=1"')
    expect(buildLogsqlFilter('_msg', '!~', 'a.b')).toBe('NOT _msg:~"a\\\\.b"')
  })

  it('builds range comparisons', () => {
    expect(buildLogsqlFilter('duration', '>=', 100)).toBe('duration:>=100')
    expect(buildLogsqlFilter('duration', '<', 5)).toBe('duration:<5')
  })

  it('quotes field names that need it', () => {
    expect(buildLogsqlFilter('"user agent"', '=', 'curl')).toBe('"user agent":="curl"')
  })
})

describe('escapeRegex', () => {
  it('escapes RE2 metacharacters', () => {
    expect(escapeRegex('a.b*(c)|[d]{2}^$+?\\')).toBe('a\\.b\\*\\(c\\)\\|\\[d\\]\\{2\\}\\^\\$\\+\\?\\\\')
  })
})

describe('appendLogsqlFilter', () => {
  it('replaces an empty or match-all query', () => {
    expect(appendLogsqlFilter('', 'level:="error"')).toBe('level:="error"')
    expect(appendLogsqlFilter('  *  ', 'level:="error"')).toBe('level:="error"')
    expect(appendLogsqlFilter('* | limit 10', 'level:="error"')).toBe('level:="error" | limit 10')
  })

  it('joins filters with whitespace ahead of the pipes', () => {
    expect(appendLogsqlFilter('error', 'NOT app:="api"')).toBe('error NOT app:="api"')
    expect(appendLogsqlFilter('level:="error" | stats count() by (app)', 'app:="api"'))
      .toBe('level:="error" app:="api" | stats count() by (app)')
  })

  it('groups a top-level or', () => {
    expect(appendLogsqlFilter('a:=1 or b:=2', 'c:=3')).toBe('(a:=1 or b:=2) c:=3')
    expect(appendLogsqlFilter('a:=1 OR b:=2 | limit 5', 'c:=3')).toBe('(a:=1 OR b:=2) c:=3 | limit 5')
    expect(appendLogsqlFilter('(a:=1 or b:=2) d:=4', 'c:=3')).toBe('(a:=1 or b:=2) d:=4 c:=3')
    expect(appendLogsqlFilter('error:"or this"', 'c:=3')).toBe('error:"or this" c:=3')
  })

  it('ignores pipes inside quoted values', () => {
    expect(appendLogsqlFilter('_msg:~"a|b"', 'c:=3')).toBe('_msg:~"a|b" c:=3')
  })
})
