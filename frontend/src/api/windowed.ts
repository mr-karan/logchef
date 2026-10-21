import { apiBaseURL } from './config';
import { apiClient } from './apiUtils';
import { streamSSE } from './eventStream';
import type { QueryParams, QueryStats, HistogramDataPoint } from './explore';

export type LogRow = Record<string, unknown>;
export type WindowStatus = 'pending' | 'running' | 'complete' | 'partial' | 'failed' | 'skipped';
export interface SearchWindow { index: number; start: string; end: string }
export interface WindowedRequest extends QueryParams {
  query_language: 'logchefql' | 'logsql';
  cursor?: string;
  count?: boolean;
  step?: string;
  window_index?: number;
  skip_window?: number;
  extra_stream_filters?: string[];
}
export interface WindowedEvent {
  type: 'plan' | 'window' | 'rows' | 'count' | 'end';
  window?: SearchWindow;
  status?: WindowStatus;
  windows?: SearchWindow[];
  logs?: LogRow[];
  buckets?: HistogramDataPoint[];
  stats?: QueryStats;
  cursor?: string;
  complete: boolean;
  message?: string;
}
export { StreamHTTPError as WindowedHTTPError } from './eventStream';

export function getStreamFields(teamId: number, sourceId: number, signal: AbortSignal) {
  return apiClient.get<string[]>(`/teams/${teamId}/sources/${sourceId}/logs/stream-fields`, { signal });
}

export async function streamWindowedQuery(teamId: number, sourceId: number, request: WindowedRequest, signal: AbortSignal, onEvent: (event: WindowedEvent) => void): Promise<void> {
  let queryId: string | null = null;
  let ended = false;
  let cancellation: Promise<Response | undefined> | null = null;
  const cancelServerQuery = () => {
    if (!queryId || cancellation) return;
    cancellation = fetch(`${apiBaseURL}/teams/${teamId}/sources/${sourceId}/logs/query/${encodeURIComponent(queryId)}/cancel`, {
      method: 'POST', credentials: 'same-origin', signal: AbortSignal.timeout(5000),
    }).catch(() => undefined);
  };
  signal.addEventListener('abort', cancelServerQuery, { once: true });
  try {
    const outcome = await streamSSE(`${apiBaseURL}/teams/${teamId}/sources/${sourceId}/logs/windowed`, {
      method: 'POST', body: request, signal, label: 'Windowed query',
      onOpen: headers => { queryId = headers.get('X-LogChef-Query-ID'); },
    }, frame => {
      if (frame.type !== 'message') return;
      // SSE frames are untyped external data. Validate the discriminator before dispatch.
      const event: unknown = JSON.parse(frame.data);
      if (!event || typeof event !== 'object' || !('type' in event) || !['plan', 'window', 'rows', 'count', 'end'].includes(String(event.type))) throw new Error('Invalid windowed query event');
      const parsed = event as WindowedEvent;
      onEvent(parsed);
      if (parsed.type === 'end') ended = true;
    });
    if (outcome === 'no-body') throw new Error('Windowed query returned no stream');
    if (outcome === 'eof' && !ended) throw new Error('Connection closed before the windowed query finished');
  } finally {
    signal.removeEventListener('abort', cancelServerQuery);
    if (!ended) {
      // Release source slots immediately. Socket disconnect detection uses heartbeats.
      cancelServerQuery();
      await cancellation;
    }
  }
}
