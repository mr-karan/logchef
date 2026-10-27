import { describe, expect, it } from 'vitest'
import { getOutcomeTone } from '../outcomeTone'

describe('enum outcome tones', () => {
  it.each([
    '_JOB.FAILURE', 'BLOCKED', 'UNREACHABLE', 'TTL_EXPIRED', 'fail', 'failed',
    'error', 'rejected', 'denied', 'undeliverable', 'timeout', 'invalid',
    'dropped', 'bounced', 'fatal', 'task/failed', 'rpc:ERROR', 'job-failed',
    'unsuccessful', 'undelivered', 'CANCELLED',
  ])('recognizes failure in %s', value => {
    expect(getOutcomeTone(value, 'status', 'severity_text')).toBe('failure')
  })

  it.each([
    'PENDING', '_JOB.BUFFERED', 'queued', 'RETRYING', 'processing',
    'scheduled', 'job.waiting',
  ])('recognizes pending work in %s', value => {
    expect(getOutcomeTone(value, 'status', 'severity_text')).toBe('pending')
  })

  it.each([
    '_JOB.SUCCESS', 'succeeded', 'ok', 'DELIVERED', 'sent', 'completed',
    'accepted', 'job:done', 'SUCCESSFUL', 'successfully', 'succeed', 'complete', 'pass',
  ])('recognizes success in %s', value => {
    expect(getOutcomeTone(value, 'record_status', 'severity_text')).toBe('success')
  })

  it.each([
    '', 'UNKNOWN', 'callback', 'errorless', 'oklahoma', 'failedCount',
    'b6c4c816373baf3e734687bed27f8587', 'deadbeef', '0xdeadbeef',
    '550e8400-e29b-41d4-a716-446655440000', '+15555900629', '15555900629',
    '200', '-500', '3.14', '{"status":"error"}', '["failed"]',
    'the delivery failed', 'request failed', 'GET /api', 'GET/api/error',
    '/api/error', 'https://example.com/error', 'NOT_OK', 'no_error',
    'never_failed', `namespace_${'x'.repeat(48)}_failed`,
  ])('leaves %s neutral', value => {
    expect(getOutcomeTone(value, 'status', 'severity_text')).toBeUndefined()
  })

  it('prioritizes failure over success and pending over success', () => {
    expect(getOutcomeTone('SUCCESS_FAILED', 'status', 'severity_text')).toBe('failure')
    expect(getOutcomeTone('ACCEPTED_PENDING', 'status', 'severity_text')).toBe('pending')
  })

  it.each([
    'error-reporter', 'payment-timeout-worker', 'dlq.failed-events',
    'invalid-cache-sweeper', 'cancelled-orders-svc',
  ])('uses the column to distinguish %s from an outcome', value => {
    expect(getOutcomeTone(value, 'service', 'severity_text')).toBeUndefined()
    expect(getOutcomeTone(value, 'status', 'severity_text')).toBe('failure')
  })

  it.each([
    'status', 'state', 'result', 'outcome', 'event', 'type', 'level', 'severity',
    'disposition', 'code', 'record_status', 'record.state', 'record-result',
    'RECORD_STATUS', 'event_type', 'deliveryStatus', 'recordOutcome', 'HTTPStatus', 'SMSState',
  ])('classifies status-like column %s', columnName => {
    expect(getOutcomeTone('_JOB.FAILURE', columnName, 'severity_text')).toBe('failure')
  })

  it.each([
    'service', 'app', 'application', 'namespace', 'host', 'hostname', 'pod',
    'container', 'path', 'url', 'name', 'user', 'id',
    'service_status', 'status.app', 'result-application', 'event_namespace',
    'host_status', 'hostname.state', 'pod-result', 'container_outcome',
    'path_event', 'url_type', 'level_name', 'user_severity', 'disposition_id',
    'STATUS_SERVICE', 'serviceStatus', 'deliveryStatusName',
  ])('blocks identity or location column %s even when configured as severity', columnName => {
    expect(getOutcomeTone('FAILED', columnName, 'severity_text')).toBeUndefined()
    expect(getOutcomeTone('FAILED', columnName, columnName)).toBeUndefined()
    expect(getOutcomeTone('PENDING', columnName, columnName)).toBeUndefined()
    expect(getOutcomeTone('SUCCESSFUL', columnName, columnName)).toBeUndefined()
  })

  it.each(['body', 'message', 'value', 'resultant', 'statuscode', 'eventual'])('leaves unconfigured column %s neutral', columnName => {
    expect(getOutcomeTone('FAILED', columnName, 'severity_text')).toBeUndefined()
    expect(getOutcomeTone('PENDING', columnName, 'severity_text')).toBeUndefined()
    expect(getOutcomeTone('SUCCESSFUL', columnName, 'severity_text')).toBeUndefined()
  })

  it('recognizes a configured severity field without a status token', () => {
    expect(getOutcomeTone('FAILED', 'importance', 'importance')).toBe('failure')
    expect(getOutcomeTone('PENDING', 'importance', 'importance')).toBe('pending')
    expect(getOutcomeTone('SUCCESSFUL', 'importance', 'importance')).toBe('success')
    expect(getOutcomeTone('FAILED', 'importance', 'other')).toBeUndefined()
  })
})
