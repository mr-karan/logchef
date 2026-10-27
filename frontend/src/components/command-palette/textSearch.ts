import type { ColumnInfo } from '@/api/explore';
import { sourcesApi, type Source } from '@/api/sources';
import type { TextSearchTarget } from './palette';

export const TEXT_SEARCH_LIMITS = { characters: 2_000, fields: 16 };

export type LiteralSearch =
  | { readonly kind: 'ready'; readonly query: string; readonly fields: readonly string[] }
  | { readonly kind: 'noFields' | 'invalidText' };

function quote(text: string): string {
  return '"' + text.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\n/g, '\\n').replace(/\r/g, '\\r').replace(/\t/g, '\\t') + '"';
}

export function buildLiteralSearch(text: string, columns: readonly ColumnInfo[]): LiteralSearch {
  if (!text.trim() || text.length > TEXT_SEARCH_LIMITS.characters || /[\x00-\x08\x0b\x0c\x0e-\x1f]/.test(text)) return { kind: 'invalidText' };
  const fields = [...new Set(columns.filter(column => {
    const type = column.type.replace(/\s/g, '').replace(/(?:Nullable|LowCardinality)\(/g, '').replace(/\)+$/g, '');
    return /^(?:String|FixedString\(\d+|string|text|varchar)$/i.test(type)
      && column.name.length > 0 && column.name.length <= 128 && !/[\x00-\x1f]/.test(column.name);
  }).map(column => column.name))].slice(0, TEXT_SEARCH_LIMITS.fields);
  if (fields.length === 0) return { kind: 'noFields' };
  return { kind: 'ready', fields, query: fields.map(field => `${quote(field)}~${quote(text)}`).join(' or ') };
}

/** Reuses confirmed current metadata or makes one cancellable source-details request. */
export async function resolveTextSearch(target: TextSearchTarget, cached: Source | null, signal: AbortSignal): Promise<LiteralSearch> {
  let source = cached?.id === target.sourceId ? cached : null;
  if (!source) {
    const response = await sourcesApi.getTeamSource(target.teamId, target.sourceId, {
      timeout: 8, signal, suppressErrorToast: true, suppressForbiddenRedirect: true,
    });
    if (signal.aborted) throw new DOMException('Request canceled', 'AbortError');
    if (response.status !== 'success' || !response.data) throw new Error('source details unavailable');
    source = response.data;
  }
  if (signal.aborted) throw new DOMException('Request canceled', 'AbortError');
  return buildLiteralSearch(target.text, source.columns ?? []);
}
