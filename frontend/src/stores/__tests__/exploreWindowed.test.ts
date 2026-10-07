import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createServer, type Server } from 'node:http';
import { createPinia, setActivePinia } from 'pinia';
import { useExploreWindowedStore } from '../exploreWindowed';
import type { LogRow, WindowedEvent, WindowedRequest } from '@/api/windowed';

const windows = [
  { index: 0, start: '2026-09-01T00:01:00Z', end: '2026-09-01T00:02:00Z' },
  { index: 1, start: '2026-09-01T00:00:00Z', end: '2026-09-01T00:01:00Z' },
];
const request: WindowedRequest = { query_text: '*', query_language: 'logsql', start_time: windows[1].start, end_time: windows[0].end, limit: 2 };
const row = (message: string, timestamp = windows[0].start): LogRow => ({ _time: timestamp, _msg: message });
let server: Server;
let requests: WindowedRequest[];
let cancellations: number;
let respond: (request: WindowedRequest) => WindowedEvent[] | null;
// Events written before a stream stays open, used when respond returns null.
let openEvents: WindowedEvent[];
const originalFetch = globalThis.fetch;

beforeEach(async () => {
  setActivePinia(createPinia());
  requests = [];
  cancellations = 0;
  openEvents = [
    { type: 'plan', windows, cursor: 'first', complete: false },
    { type: 'window', window: windows[0], status: 'running', cursor: 'first', complete: false },
  ];
  server = createServer(async (incoming, response) => {
    if (incoming.url?.endsWith('/cancel')) {
      cancellations++;
      response.writeHead(200);
      response.end('{}');
      return;
    }
    let body = '';
    for await (const chunk of incoming) body += chunk;
    // Requests arrive through the real HTTP and SSE parser boundaries.
    const parsed = JSON.parse(body) as WindowedRequest;
    requests.push(parsed);
    response.writeHead(200, { 'Content-Type': 'text/event-stream', 'X-LogChef-Query-ID': 'windowed-test' });
    response.write(': ok\n\n');
    const events = respond(parsed);
    if (events) {
      for (const event of events) response.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
      response.end();
    } else {
      for (const event of openEvents) response.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Test server has no address');
  const baseURL = `http://127.0.0.1:${address.port}`;
  globalThis.fetch = (input, options) => originalFetch(typeof input === 'string' ? new URL(input, baseURL) : input, options);
});

afterEach(async () => {
  useExploreWindowedStore().stop();
  globalThis.fetch = originalFetch;
  server.closeAllConnections();
  await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
});

function event(value: Omit<WindowedEvent, 'complete'> & { complete?: boolean }): WindowedEvent {
  return { complete: false, ...value };
}
function start(histogram = true, overrides: Partial<WindowedRequest> = {}) {
  const store = useExploreWindowedStore();
  const loaded: LogRow[][] = [];
  const finished = store.start({ teamId: 1, sourceId: 1, request: { ...request, ...overrides }, histogram, onRows: rows => loaded.push(rows) }, new AbortController().signal);
  return { store, loaded, finished };
}

describe('windowed explorer over HTTP', () => {
  it('uses exact JSON stream filters and builds a complete first-page histogram without counts', async () => {
    respond = () => [event({ type: 'plan', windows }), event({ type: 'rows', window: windows[0], status: 'complete', logs: [row('one')] }), event({ type: 'end', complete: true })];
    const { store, loaded, finished } = start();
    store.selectedStreams = { 'kubernetes.namespace': ['a.b', 'a|b', 'quoted"value'] };
    expect(store.streamFilters).toEqual([JSON.stringify({ 'kubernetes.namespace': ['a.b', 'a|b', 'quoted"value'] })]);
    await finished;
    expect(loaded.at(-1)).toEqual([row('one')]);
    expect(requests).toHaveLength(1);
    expect(store.total).toBe(1);
    expect(store.countComplete).toBe(true);
  });

  it('replaces a retried count window and loads older with the original range', async () => {
    respond = req => {
      if (req.count) return req.window_index === 1
        ? [event({ type: 'count', window: windows[1], status: 'complete', buckets: [{ bucket: windows[1].start, log_count: 3 }] }), event({ type: 'end', complete: true })]
        : [event({ type: 'count', window: windows[0], status: 'complete', buckets: [{ bucket: windows[0].start, log_count: 2 }] }), event({ type: 'window', window: windows[1], status: 'failed' }), event({ type: 'end', complete: true })];
      return req.cursor
        ? [event({ type: 'rows', window: windows[1], status: 'complete', logs: [row('older', windows[1].start)] }), event({ type: 'end', complete: true })]
        : [event({ type: 'plan', windows, cursor: 'first' }), event({ type: 'rows', window: windows[0], status: 'complete', logs: [row('newer')], cursor: 'older' }), event({ type: 'end', cursor: 'older' })];
    };
    const { store, loaded, finished } = start(false);
    await finished;
    await vi.waitFor(() => expect(store.isCounting).toBe(false));
    expect(store.total).toBe(2);
    expect(store.countComplete).toBe(false);
    await store.retryCount(1);
    await store.retryCount(1);
    expect(store.total).toBe(5);
    expect(store.countComplete).toBe(true);
    await store.loadOlder();
    expect(requests.at(-1)?.start_time).toBe(windows[1].start);
    expect(loaded.at(-1)?.map(log => log._msg)).toEqual(['newer', 'older']);
    expect(store.histogramEnabled).toBe(false);
  });

  it('aborts a slow window, keeps skipped coverage, and inserts retry rows in newest-first order', async () => {
    respond = req => {
      if (req.count) return [event({ type: 'end' })];
      if (req.skip_window === 0) return [event({ type: 'window', window: windows[0], status: 'skipped', cursor: 'older' }), event({ type: 'rows', window: windows[1], status: 'complete', logs: [row('older', windows[1].start)] }), event({ type: 'end', complete: true })];
      if (req.window_index === 0) return [event({ type: 'rows', window: windows[0], status: 'complete', logs: [row('newer')], cursor: 'older' }), event({ type: 'end', complete: true })];
      return null;
    };
    const { store, loaded, finished } = start();
    await vi.waitFor(() => expect(store.skippableWindow?.index).toBe(0));
    await store.skipWindow();
    await finished;
    expect(store.coverage[0].search).toBe('skipped');
    expect(store.complete).toBe(false);
    await store.retrySearch(store.coverage[0]);
    expect(loaded.at(-1)?.map(log => log._msg)).toEqual(['newer', 'older']);
    expect(requests.find(req => req.skip_window === 0)?.cursor).toBe('first');
    expect(cancellations).toBe(1);
  });

  it.each(['stop', 'end message', 'closed stream'])('fails only the head window after %s and pages prefetched windows once', async interruption => {
    const running = [
      event({ type: 'plan', windows, cursor: 'first' }),
      event({ type: 'window', window: windows[0], status: 'running', cursor: 'first' }),
      event({ type: 'window', window: windows[1], status: 'running', cursor: 'second' }),
    ];
    respond = req => {
      if (req.count) return [event({ type: 'end' })];
      if (req.window_index === 0) return [event({ type: 'window', window: windows[0], status: 'running', cursor: 'first' }), event({ type: 'rows', window: windows[0], status: 'complete', logs: [row('newer')], cursor: 'second' }), event({ type: 'end' })];
      if (req.window_index === 1) return [event({ type: 'rows', window: windows[1], status: 'complete', logs: [row('older', windows[1].start)] }), event({ type: 'end', complete: true })];
      if (req.cursor === 'second') return [event({ type: 'rows', window: windows[1], status: 'complete', logs: [row('older', windows[1].start)] }), event({ type: 'end', complete: true })];
      if (interruption === 'end message') return [...running, event({ type: 'end', message: 'Windowed query stopped before completion.' })];
      if (interruption === 'closed stream') return running;
      return null;
    };
    openEvents = running;
    const { store, loaded, finished } = start();
    if (interruption === 'stop') {
      await vi.waitFor(() => expect(store.coverage[1]?.search).toBe('running'));
      store.stop();
    }
    await finished;
    expect(store.coverage.map(w => w.search)).toEqual(['failed', 'pending']);
    expect(store.cursor).toBe('first');
    await store.retrySearch(store.coverage[1]);
    expect(requests.some(req => req.window_index === 1)).toBe(false);
    await store.retrySearch(store.coverage[0]);
    expect(store.cursor).toBe('second');
    await store.loadOlder();
    expect(loaded.at(-1)?.map(log => log._msg)).toEqual(['newer', 'older']);
    expect(store.coverage.map(w => w.search)).toEqual(['complete', 'complete']);
    expect(store.complete).toBe(true);
  });

  it('skips a failed head window after a byte-limit failure and keeps the gap retryable', async () => {
    const message = 'A log in this time range exceeds the response size limit. Skip this window or narrow the query.';
    respond = req => {
      if (req.count) return [event({ type: 'end' })];
      if (req.skip_window === 0) return [event({ type: 'plan', windows, cursor: 'first' }), event({ type: 'window', window: windows[0], status: 'skipped', cursor: 'second' }), event({ type: 'rows', window: windows[1], status: 'complete', logs: [row('older', windows[1].start)] }), event({ type: 'end', complete: true })];
      if (req.window_index === 0) return [event({ type: 'rows', window: windows[0], status: 'complete', logs: [row('newer')], cursor: 'second' }), event({ type: 'end' })];
      return [event({ type: 'plan', windows, cursor: 'first' }), event({ type: 'window', window: windows[0], status: 'running', cursor: 'first' }), event({ type: 'window', window: windows[0], status: 'failed', cursor: 'first', message }), event({ type: 'end', cursor: 'first' })];
    };
    const { store, loaded, finished } = start();
    await finished;
    expect(store.coverage[0]).toMatchObject({ search: 'failed', message });
    expect(store.skippableWindow?.index).toBe(0);
    expect(store.canRetrySearch(store.coverage[1])).toBe(false);
    await store.skipWindow();
    expect(requests.find(req => req.skip_window === 0)?.cursor).toBe('first');
    expect(loaded.at(-1)?.map(log => log._msg)).toEqual(['older']);
    expect(store.coverage.map(w => w.search)).toEqual(['skipped', 'complete']);
    expect(store.skippableWindow).toBeUndefined();
    expect(store.complete).toBe(false);
    expect(store.canRetrySearch(store.coverage[0])).toBe(true);
    await store.retrySearch(store.coverage[0]);
    expect(loaded.at(-1)?.map(log => log._msg)).toEqual(['newer', 'older']);
  });

  it('keeps a historical retry retryable when skipping the failed head supersedes it', async () => {
    const three = [
      { index: 0, start: '2026-09-01T00:02:00Z', end: '2026-09-01T00:03:00Z' },
      { index: 1, start: '2026-09-01T00:01:00Z', end: '2026-09-01T00:02:00Z' },
      { index: 2, start: '2026-09-01T00:00:00Z', end: '2026-09-01T00:01:00Z' },
    ];
    let slow = true;
    respond = req => {
      if (req.count) return [event({ type: 'end' })];
      if (req.skip_window === 0) return [event({ type: 'plan', windows: three, cursor: 'c0' }), event({ type: 'window', window: three[0], status: 'skipped', cursor: 'c1' }), event({ type: 'window', window: three[1], status: 'running', cursor: 'c1' }), event({ type: 'window', window: three[1], status: 'failed', cursor: 'c1' }), event({ type: 'end', cursor: 'c1' })];
      if (req.skip_window === 1) return [event({ type: 'plan', windows: three, cursor: 'c1' }), event({ type: 'window', window: three[1], status: 'skipped', cursor: 'c2' }), event({ type: 'window', window: three[2], status: 'running', cursor: 'c2' }), event({ type: 'rows', window: three[2], status: 'partial', logs: [row('oldest', three[2].start)], cursor: 'c2b' }), event({ type: 'end', cursor: 'c2b' })];
      if (req.window_index === 0 && !slow) return [event({ type: 'rows', window: three[0], status: 'complete', logs: [row('newest', three[0].start)], cursor: 'c1' }), event({ type: 'end' })];
      return null;
    };
    openEvents = [event({ type: 'plan', windows: three, cursor: 'c0' }), event({ type: 'window', window: three[0], status: 'running', cursor: 'c0' })];
    const { store, loaded, finished } = start(true, { start_time: three[2].start, end_time: three[0].end, limit: 1 });
    await vi.waitFor(() => expect(store.skippableWindow?.index).toBe(0));
    await store.skipWindow();
    await finished;
    expect(store.coverage.map(w => w.search)).toEqual(['skipped', 'failed', 'pending']);
    const retry = store.retrySearch(store.coverage[0]);
    await vi.waitFor(() => expect(store.coverage[0].search).toBe('running'));
    expect(store.skippableWindow?.index).toBe(1);
    await store.skipWindow();
    await retry;
    expect(store.coverage.map(w => w.search)).toEqual(['failed', 'skipped', 'partial']);
    expect(store.cursor).toBe('c2b');
    expect(store.canRetrySearch(store.coverage[0])).toBe(true);
    expect(store.canRetrySearch(store.coverage[1])).toBe(true);
    expect(store.canRetrySearch(store.coverage[2])).toBe(false);
    slow = false;
    await store.retrySearch(store.coverage[0]);
    expect(store.cursor).toBe('c2b');
    expect(loaded.at(-1)?.map(log => log._msg)).toEqual(['newest', 'oldest']);
  });

  it('keeps an aborted head skippable and retryable when the skip request fails', async () => {
    let skipFails = true;
    respond = req => {
      if (req.count) return [event({ type: 'end' })];
      // A closed stream with no end event is a fetch error before the skipped event.
      if (req.skip_window === 0 && skipFails) return [];
      if (req.skip_window === 0) return [event({ type: 'window', window: windows[0], status: 'skipped', cursor: 'older' }), event({ type: 'rows', window: windows[1], status: 'complete', logs: [row('older', windows[1].start)] }), event({ type: 'end', complete: true })];
      return null;
    };
    const { store, loaded, finished } = start();
    await vi.waitFor(() => expect(store.skippableWindow?.index).toBe(0));
    await store.skipWindow();
    await finished;
    expect(store.error).toBe('Connection closed before the windowed query finished');
    expect(store.coverage.map(w => w.search)).toEqual(['failed', 'pending']);
    expect(store.cursor).toBe('first');
    expect(store.skippableWindow?.index).toBe(0);
    expect(store.canRetrySearch(store.coverage[0])).toBe(true);
    expect(store.canRetrySearch(store.coverage[1])).toBe(false);
    skipFails = false;
    await store.skipWindow();
    expect(requests.filter(req => req.skip_window === 0).map(req => req.cursor)).toEqual(['first', 'first']);
    expect(store.coverage.map(w => w.search)).toEqual(['skipped', 'complete']);
    expect(loaded.at(-1)?.map(log => log._msg)).toEqual(['older']);
  });

  // The same rows and calendar buckets as TestIntegrationWindowedCountsUseTimezone, so row and count histograms agree.
  it.each([
    ['Asia/Kolkata', '2026-10-04T12:00:00Z', '1h', [['2026-10-06T16:30:00.000Z', 1], ['2026-10-06T18:30:00.000Z', 1], ['2026-10-06T23:30:00.000Z', 1]]],
    ['Asia/Kolkata', '2026-08-08T00:00:00Z', '24h', [['2026-10-05T18:30:00.000Z', 1], ['2026-10-06T18:30:00.000Z', 2]]],
    ['UTC', '2026-10-04T12:00:00Z', '1h', [['2026-10-06T17:00:00.000Z', 1], ['2026-10-06T19:00:00.000Z', 1], ['2026-10-06T23:00:00.000Z', 1]]],
    ['UTC', '2026-08-08T00:00:00Z', '24h', [['2026-10-06T00:00:00.000Z', 3]]],
  ] as const)('buckets an exhaustive first page in %s from %s at %s', async (timezone, startTime, step, expected) => {
    const logs = ['2026-10-06T17:00:00Z', '2026-10-06T19:00:00Z', '2026-10-06T23:30:00Z'].reverse().map(time => row(time, time));
    respond = () => [event({ type: 'plan', windows }), event({ type: 'rows', window: windows[0], status: 'complete', logs }), event({ type: 'end', complete: true })];
    const { store, finished } = start(true, { timezone, start_time: startTime, end_time: '2026-10-07T00:00:00Z' });
    await finished;
    expect(store.granularity).toBe(step);
    expect(store.histogram).toEqual(expected.map(([bucket, log_count]) => ({ bucket, log_count })));
    expect(requests.every(req => !req.count)).toBe(true);
  });
});
