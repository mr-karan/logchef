import { describe, expect, it } from 'vitest';
import { recognizeLogchefQL } from '../implicitQuery';
import { MAX_INPUT_LENGTH } from '../parse';

const fields = new Set(['status', 'method', 'message', 'attributes', '@timestamp', 'literal.path']);

describe('strict implicit LogchefQL recognition', () => {
  it.each([
    'status>=500', 'method="GET" and status>=400', '(status=500 or status=502) AND method=GET',
    '((status!=200))', 'message~"timeout"', "message!~'timed out'", 'status=-1.5', 'status=+.5',
    'attributes.service=api', '"attributes"."service"=api', 'attributes."service.name"=api',
    '"literal.path"=value', '@timestamp>=123', 'message="https://example.com/?q=x"',
    'message="error: count > 5"', 'message="a\\"b"', 'status=and',
  ])('accepts the complete known-field filter %j', query => {
    expect(recognizeLogchefQL(query, fields)).toEqual({ kind: 'query' });
  });

  it.each([
    '', 'please show errors', '5xx spike', 'https://example.com/?q=x', 'a=b', 'unknown=5',
    'error: count > 5', 'upstream failed with code > 500', 'status', 'status>', 'status=500 and',
    'status=500 or ()', '(status=500', 'status=500)', 'status=500 method=GET', 'status=500 | message',
    'status==500', 'status=>500', 'status=500 trailing words', 'status=error.value',
    'message="unterminated', "message='unterminated", 'attributes.=api', 'attributes..service=api',
    '"attributes". =api', 'status in (500)', 'status=500;method=GET', 'constructor=5',
    'status=500 or unknown=5', 'status=500 and (method=GET or a=b)',
    'status=500 and and method=GET', '? status=500', 'status={{value}}',
  ])('keeps ordinary or malformed input %j as text', query => {
    expect(recognizeLogchefQL(query, fields).kind).toBe('text');
  });

  it('uses exact known names or their parsed base, with no prototype or quoted-name fallback', () => {
    expect(recognizeLogchefQL('a=b', new Set(['a']))).toEqual({ kind: 'query' });
    expect(recognizeLogchefQL('attributes.service=x', new Set(['attributes.service']))).toEqual({ kind: 'query' });
    expect(recognizeLogchefQL('"literal.unknown"=x', new Set(['literal']))).toEqual({ kind: 'text', reason: 'unknownField' });
    expect(recognizeLogchefQL('status=500', new Set())).toEqual({ kind: 'text', reason: 'unknownField' });
  });

  it('bounds input length and parentheses with the server nesting limit', () => {
    expect(recognizeLogchefQL('('.repeat(100) + 'status=500' + ')'.repeat(100), fields)).toEqual({ kind: 'query' });
    expect(recognizeLogchefQL('('.repeat(101) + 'status=500' + ')'.repeat(101), fields)).toEqual({ kind: 'text', reason: 'limit' });
    expect(recognizeLogchefQL('message="' + 'x'.repeat(MAX_INPUT_LENGTH) + '"', fields)).toEqual({ kind: 'text', reason: 'limit' });
  });
});
