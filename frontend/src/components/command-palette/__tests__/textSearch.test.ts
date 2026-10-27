import { afterEach, describe, expect, it, vi } from 'vitest';
import { sourcesApi, type Source } from '@/api/sources';
import { buildLiteralSearch, resolveTextSearch, TEXT_SEARCH_LIMITS } from '../textSearch';

const source: Source = { id: 1, name: 'Application', _meta_is_auto_created: false, source_type: 'clickhouse', _meta_ts_field: 'timestamp', connection: {}, ttl_days: 1, created_at: '', updated_at: '', is_connected: true, columns: [{ name: 'message', type: 'String' }] };
const target = { teamId: 2, sourceId: 1, text: 'timeout', range: '15m' };
afterEach(() => vi.restoreAllMocks());

describe('literal log search', () => {
  it('searches one literal phrase across verified text columns', () => {
    expect(buildLiteralSearch('upstream timeout', [
      { name: 'message', type: 'String' }, { name: 'status', type: 'UInt16' },
      { name: 'service', type: 'LowCardinality(Nullable(String))' }, { name: 'code', type: 'FixedString(8)' },
    ])).toEqual({ kind: 'ready', fields: ['message', 'service', 'code'], query: '"message"~"upstream timeout" or "service"~"upstream timeout" or "code"~"upstream timeout"' });
  });

  it('quotes punctuation, quotes, slashes, line breaks and query syntax literally', () => {
    expect(buildLiteralSearch('" or status=500 | count \\.*\n\t', [{ name: 'log"message', type: 'String' }])).toMatchObject({
      kind: 'ready', query: '"log\\"message"~"\\" or status=500 | count \\\\.*\\n\\t"',
    });
  });

  it('does not invent fields and rejects empty or excessive input', () => {
    expect(buildLiteralSearch('timeout', [{ name: 'status', type: 'UInt16' }])).toEqual({ kind: 'noFields' });
    expect(buildLiteralSearch(' ', source.columns ?? [])).toEqual({ kind: 'invalidText' });
    expect(buildLiteralSearch('x'.repeat(TEXT_SEARCH_LIMITS.characters + 1), source.columns ?? [])).toEqual({ kind: 'invalidText' });
    expect(buildLiteralSearch('\0', source.columns ?? [])).toEqual({ kind: 'invalidText' });
  });

  it('bounds and deduplicates text columns', () => {
    const result = buildLiteralSearch('timeout', Array.from({ length: 100 }, (_, index) => ({ name: `field${index % 25}`, type: 'String' })));
    expect(result.kind === 'ready' && result.fields.length).toBe(TEXT_SEARCH_LIMITS.fields);
    expect(result.kind === 'ready' && result.query.includes('field16')).toBe(false);
  });

  it('reuses the current source metadata without another request', async () => {
    const get = vi.spyOn(sourcesApi, 'getTeamSource');
    const result = await resolveTextSearch(target, source, new AbortController().signal);
    expect(result).toMatchObject({ kind: 'ready', query: '"message"~"timeout"' });
    expect(get).not.toHaveBeenCalled();
  });

  it('loads exactly one team-scoped source with timeout and cancellation', async () => {
    const get = vi.spyOn(sourcesApi, 'getTeamSource').mockResolvedValue({ status: 'success', data: source });
    const signal = new AbortController().signal;
    expect(await resolveTextSearch(target, null, signal)).toMatchObject({ kind: 'ready' });
    expect(get).toHaveBeenCalledExactlyOnceWith(2, 1, { timeout: 8, signal, suppressErrorToast: true, suppressForbiddenRedirect: true });
  });

  it('ignores a late details response after closing', async () => {
    const controller = new AbortController();
    vi.spyOn(sourcesApi, 'getTeamSource').mockImplementation(async () => {
      controller.abort();
      return { status: 'success', data: source };
    });
    await expect(resolveTextSearch(target, null, controller.signal)).rejects.toMatchObject({ name: 'AbortError' });
  });
});
