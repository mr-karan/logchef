export type OutcomeTone = 'failure' | 'pending' | 'success'

const columnTokens: Record<'status' | 'blocked', ReadonlySet<string>> = {
  status: new Set([
    'status', 'state', 'result', 'outcome', 'event', 'type', 'level', 'severity',
    'disposition', 'code',
  ]),
  blocked: new Set([
    'service', 'app', 'application', 'namespace', 'host', 'hostname', 'pod',
    'container', 'path', 'url', 'name', 'user', 'id',
  ]),
}

const outcomeTokens: readonly [OutcomeTone, ReadonlySet<string>][] = [
  ['failure', new Set([
    'fail', 'failed', 'failure', 'error', 'blocked', 'rejected', 'denied',
    'unreachable', 'undeliverable', 'expired', 'timeout', 'invalid', 'dropped',
    'bounced', 'fatal', 'aborted', 'cancelled', 'canceled', 'unavailable',
    'forbidden', 'unauthorized', 'refused', 'unsuccessful', 'undelivered',
  ])],
  ['pending', new Set([
    'pending', 'buffered', 'queued', 'retrying', 'processing', 'scheduled', 'waiting',
  ])],
  ['success', new Set([
    'success', 'successful', 'successfully', 'succeed', 'succeeded', 'ok',
    'delivered', 'sent', 'complete', 'completed', 'accepted', 'pass', 'passed', 'done',
  ])],
]

export function getOutcomeTone(value: string, columnName: string, severityField: string): OutcomeTone | undefined {
  const nameTokens = columnName.replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2').replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase().split(/[_.-]+/)
  if (nameTokens.some(token => columnTokens.blocked.has(token))) return
  if (columnName !== severityField && !nameTokens.some(token => columnTokens.status.has(token))) return

  if (value.length > 48 || !/^[a-z_][a-z0-9_.:/-]*$/i.test(value)
    || /^(?:0x)?[a-f0-9]+$/i.test(value) || value.includes('://')) return

  const tokens = value.toLowerCase().split(/[_.:/-]+/).filter(Boolean)
  if (tokens.some(token => /^(no|not|non|never|without|get|post|put|delete|head|patch|options)$/.test(token))) return

  return outcomeTokens.find(([, keywords]) => tokens.some(token => keywords.has(token)))?.[0]
}
