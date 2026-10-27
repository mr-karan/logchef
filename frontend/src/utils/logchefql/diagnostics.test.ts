import { describe, expect, it } from 'vitest';
import { escapeHoverText, getLogchefQLErrorRange, getLogchefQLFields, getSchemaFieldType, getUnknownLogchefQLFields } from './diagnostics';
import { prepareLogchefQLTemplate } from './template';

describe('LogchefQL field references', () => {
  it('locates fields without treating values or boolean operators as fields', () => {
    const query = 'method="and unknown=1" and missing=GET or (status>=400) | host method';
    const fields = getLogchefQLFields(query);
    expect(fields.map(field => field.name)).toEqual(['method', 'missing', 'status', 'host', 'method']);
    expect(fields.every(field => query.slice(field.start, field.end) === field.name)).toBe(true);
    expect(fields.every(field => field.complete)).toBe(true);
  });

  it('recognizes quoted paths, template values, and optional conditions', () => {
    const query = 'p."app.name"={{app}} [[and "host-name"="{{host}}"]] | p.id';
    expect(getLogchefQLFields(query).map(({ name, baseName }) => ({ name, baseName }))).toEqual([
      { name: 'p.app.name', baseName: 'p' },
      { name: 'host-name', baseName: 'host-name' },
      { name: 'p.id', baseName: 'p' },
    ]);
  });

  it('allows hover on an incomplete field without warning while typing', () => {
    expect(getLogchefQLFields('method')).toEqual([
      { name: 'method', baseName: 'method', start: 0, end: 6, complete: false },
    ]);
    expect(getLogchefQLFields('method="unfinished unknown=1').map(field => field.name)).toEqual(['method']);
  });
});

describe('LogchefQL syntax ranges', () => {
  it('marks the token at a server position on the correct line', () => {
    const query = 'method="GET"\nand and';
    const range = getLogchefQLErrorRange(query, query, { line: 2, column: 5 });
    expect(query.slice(range.start, range.end)).toBe('and');
    expect(range.start).toBe(17);
  });

  it('marks the original line when templates change columns', () => {
    const query = 'method={{method}} and and';
    expect(getLogchefQLErrorRange(query, prepareLogchefQLTemplate(query), { line: 1, column: 31 })).toEqual({ start: 0, end: query.length });
  });

  it('uses the first whole line without a position', () => {
    expect(getLogchefQLErrorRange('method and and\nstatus=200', 'method and and\nstatus=200')).toEqual({ start: 0, end: 14 });
  });

  it('clamps end-of-input and accounts for Unicode columns', () => {
    const query = 'method="😀" and and';
    const range = getLogchefQLErrorRange(query, query, { line: 1, column: 16 });
    expect(query.slice(range.start, range.end)).toBe('and');
    expect(getLogchefQLErrorRange('method', 'method', { line: 1, column: 7 })).toEqual({ start: 5, end: 6 });
  });
});

it('escapes cached values before displaying hover Markdown', () => {
  expect(escapeHoverText('[open](command:run) <img> `value`')).toBe('\\[open\\]\\(command:run\\) \\<img\\> \\`value\\`');
});

describe('LogchefQL schema lookups', () => {
  const schema = { method: { type: 'LowCardinality(String)' }, p: { type: 'JSON' } };

  it('flags inherited object keys as unknown fields', () => {
    expect(getUnknownLogchefQLFields('constructor=1 and toString="x" and method="GET" and p.id=2', schema).map(field => field.name))
      .toEqual(['constructor', 'toString']);
  });

  it('finds no type for inherited object keys and falls back to the base name', () => {
    expect(getSchemaFieldType(schema, { name: 'constructor', baseName: 'constructor' })).toBeUndefined();
    expect(getSchemaFieldType(schema, { name: 'p.id', baseName: 'p' })).toBe('JSON');
    expect(getSchemaFieldType(schema, { name: 'method', baseName: 'method' })).toBe('LowCardinality(String)');
  });

  it('does not warn before the schema loads', () => {
    expect(getUnknownLogchefQLFields('anything=1', {})).toEqual([]);
  });
});
