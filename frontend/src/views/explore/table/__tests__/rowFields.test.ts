import { expect, it } from 'vitest'
import { flattenRow } from '../rowFields'

it('lists nested keys of objects and of JSON held in a string, by LogchefQL path', () => {
  const msg = '{"event":"request_done","http":{"status":500,"user agent":"probe"},"tags":["a"]}'
  const fields = flattenRow({ level: 'error', msg, attrs: { 'k8s.pod': 'pod-1' } })
  expect(fields.map(field => [field.field, field.depth, field.isObject])).toEqual([
    ['level', 0, false],
    ['msg', 0, true],
    ['msg.event', 1, false],
    ['msg.http', 1, true],
    ['msg.http.status', 2, false],
    ['msg.http."user agent"', 2, false],
    ['msg.tags', 1, false],
    ['attrs', 0, true],
    ['attrs."k8s.pod"', 1, false],
  ])
  expect(fields.find(field => field.field === 'msg.http.status')).toMatchObject({ value: 500, textual: true })
  expect(fields.find(field => field.field === 'attrs."k8s.pod"')).toMatchObject({ value: 'pod-1', textual: false })
})

it('keeps strings that are not a JSON object as plain values', () => {
  const fields = flattenRow({ msg: '{not json', list: '[1,2]' })
  expect(fields.every(field => !field.isObject && !field.nested)).toBe(true)
})
