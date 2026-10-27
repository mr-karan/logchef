import { describe, expect, it } from 'vitest';
import { classifyCellValue, type CellValueKind } from '../cellTypography';

describe('cell value typography', () => {
  it.each<{ label: string; value: unknown; kind: CellValueKind }>([
    { label: 'hex hash', value: '5d24f63865268abd3e904199ac107fdd', kind: 'identifier' },
    { label: 'prefixed hex', value: '0x5d24f63865268abd', kind: 'identifier' },
    { label: 'UUID', value: '550e8400-e29b-41d4-a716-446655440000', kind: 'identifier' },
    { label: 'phone number', value: '+15551234567', kind: 'identifier' },
    { label: 'formatted phone number', value: '+1 (555) 123-4567', kind: 'identifier' },
    { label: 'long digit string', value: '12345678901234567890', kind: 'number' },
    { label: 'base64 token', value: 'aGVsbG8gd29ybGQgdG9rZW4=', kind: 'identifier' },
    { label: 'URL-safe token', value: 'pK8rV2mY7wQ9sT4u_X5zN0bC'.repeat(3), kind: 'identifier' },
    { label: 'enum word', value: 'DELIVERED', kind: 'text' },
    { label: 'punctuated enum', value: '_JOB.SUCCESS', kind: 'text' },
    { label: 'hostname', value: 'api-01.example.com', kind: 'text' },
    { label: 'project name', value: 'notifications-otp', kind: 'text' },
    { label: 'hyphenated name', value: 'api-gateway', kind: 'text' },
    { label: 'long word', value: 'AuthenticationRequest', kind: 'text' },
    { label: 'free text', value: 'request accepted in 42 ms', kind: 'text' },
    { label: 'non-Latin text', value: 'リクエストを受信しました', kind: 'text' },
    { label: 'short hex word', value: 'decade', kind: 'text' },
    { label: 'number', value: 123456789, kind: 'number' },
    { label: 'decimal', value: 12.5, kind: 'number' },
    { label: 'numeric string', value: '204', kind: 'number' },
    { label: 'decimal string', value: '-1234.5678', kind: 'number' },
    { label: 'scientific notation', value: '1.23e+10', kind: 'number' },
    { label: 'boolean', value: false, kind: 'text' },
    { label: 'empty string', value: '', kind: 'text' },
    { label: 'JSON object string', value: '{"message":"request accepted"}', kind: 'structured' },
    { label: 'JSON array string', value: '[1, 2, 3]', kind: 'structured' },
    { label: 'multiline JSON string', value: '{\n"message": "request accepted"\n}', kind: 'structured' },
  ])('classifies $label by value shape', ({ value, kind }) => {
    expect(classifyCellValue(value)).toBe(kind);
  });
});

it.each([
  ['/api/v1/getUserProfile', 'text'], ['/Users/Shared/Documents', 'text'],
  ['com/example/OrderService', 'text'], ['15-01-2024', 'text'],
  ['12-34-56-78', 'text'], ['2024-01-15 10', 'text'], ['1 234 567', 'text'],
  ['2024-01-15', 'text'], ['192.168.1.100', 'text'], ['2001:db8::1', 'text'],
  ['::ffff:192.168.1.100', 'text'], ['1234:5678:90ab:cdef:1234:5678:90ab:cdef', 'text'],
  ['OAuth2AuthenticationFilter', 'text'], ['ProcessPaymentV2Request', 'text'],
  ['1234567', 'number'], ['1234567890123456', 'number'],
  ['(555) 123-4567', 'identifier'],
])('classifies %s without mistaking readable values for identifiers', (value, kind) => {
  expect(classifyCellValue(value)).toBe(kind)
})
