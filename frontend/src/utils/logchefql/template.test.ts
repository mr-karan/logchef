import { describe, expect, it } from 'vitest';
import { prepareLogchefQLTemplate } from './template';

describe('LogchefQL template validation', () => {
  it.each([
    ['p.tcode={{code}}', 'p.tcode="__VAR_code__"'],
    ['p.tcode="{{ code }}" | p.order_number', 'p.tcode="__VAR_code__" | p.order_number'],
    ["p.tcode='{{code}}'", "p.tcode='__VAR_code__'"],
    ['msg~"prefix {{code}} suffix"', 'msg~"prefix __VAR_code__ suffix"'],
    ['msg="escaped \\" {{code}}"', 'msg="escaped \\" __VAR_code__"'],
    ['p.tcode="10023"', 'p.tcode="10023"'],
    ['level="ERROR" [[and p.tcode={{code}}]]', 'level="ERROR" and p.tcode="__VAR_code__"'],
    ['msg="[[{{code}}]]"', 'msg="[[__VAR_code__]]"'],
  ])('prepares %s without changing quote boundaries', (query, expected) => {
    expect(prepareLogchefQLTemplate(query)).toBe(expected);
  });
});
