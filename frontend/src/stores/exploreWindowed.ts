import { computed, markRaw, ref } from 'vue';
import { defineStore } from 'pinia';
import { fromAbsolute } from '@internationalized/date';
import { streamWindowedQuery, WindowedHTTPError, type LogRow, type SearchWindow, type WindowStatus, type WindowedRequest, type WindowedEvent } from '@/api/windowed';
import type { HistogramDataPoint, QueryStats } from '@/api/explore';
import { HistogramService } from '@/services/HistogramService';

export interface WindowCoverage extends SearchWindow {
  search: WindowStatus;
  count: WindowStatus;
  cursor: string;
  message: string;
}
interface SearchOptions { cursor?: string; skip?: number; retry?: number; signal?: AbortSignal }
interface SearchSnapshot {
  teamId: number;
  sourceId: number;
  request: WindowedRequest;
  histogram: boolean;
  onRows: (rows: LogRow[], stats: QueryStats | null) => void;
}

export const useExploreWindowedStore = defineStore('exploreWindowed', () => {
  const active = ref(false);
  const coverage = ref<WindowCoverage[]>([]);
  const cursor = ref('');
  const isSearching = ref(false);
  const isCounting = ref(false);
  const complete = ref(false);
  const error = ref<string | null>(null);
  const histogramEnabled = ref(true);
  const selectedStreams = ref<Record<string, string[]>>({});
  const histogram = ref<HistogramDataPoint[]>([]);
  const granularity = ref('1m');
  const rowsByWindow = new Map<number, LogRow[]>();
  const countsByWindow = new Map<number, HistogramDataPoint[]>();
  const countControllers = new Set<AbortController>();
  let snapshot: SearchSnapshot | null = null;
  let searchController: AbortController | null = null;
  let searchTask: Promise<void> | null = null;
  let generation = 0;
  let countsStarted = false;
  let stats: QueryStats | null = null;

  const searchedWindows = computed(() => coverage.value.filter(w => w.search === 'complete' || w.search === 'partial').length);
  const total = computed(() => histogram.value.reduce((sum, bucket) => sum + bucket.log_count, 0));
  const countComplete = computed(() => complete.value && !countsStarted || coverage.value.length > 0 && coverage.value.every(w => w.count === 'complete'));
  // The head is the window the main cursor continues. Windows before it are history, windows after it were never shown.
  const headIndex = computed(() => cursor.value ? coverage.value.find(w => w.search === 'pending' || w.cursor === cursor.value)?.index ?? coverage.value.length : coverage.value.length);
  const skippableWindow = computed(() => coverage.value.find(w => (w.search === 'running' || w.search === 'failed') && w.index === headIndex.value && w.cursor === cursor.value));
  const streamFilters = computed(() => Object.entries(selectedStreams.value)
    .filter(([, values]) => values.length > 0)
    .map(([field, values]) => JSON.stringify({ [field]: values })));

  function stop() {
    generation++;
    searchController?.abort();
    searchController = null;
    for (const controller of countControllers) controller.abort();
    countControllers.clear();
    isSearching.value = false;
    isCounting.value = false;
    interruptSearch();
    for (const window of coverage.value) if (countsStarted && (window.count === 'pending' || window.count === 'running')) window.count = 'failed';
  }

  // Only the oldest running window was interrupted. Later running windows were prefetched, so they return to pending.
  function interruptSearch() {
    const running = coverage.value.filter(w => w.search === 'running');
    running.forEach((window, i) => {
      if (i === 0) window.search = 'failed';
      else { window.search = 'pending'; window.cursor = ''; }
    });
  }

  function canRetrySearch(window: WindowCoverage) {
    if (!window.cursor || !['failed', 'skipped', 'partial'].includes(window.search)) return false;
    return window.index < headIndex.value || window.search === 'failed' && window.index === headIndex.value && window.cursor === cursor.value;
  }

  function reset(clearFilters = false) {
    stop();
    active.value = false;
    snapshot = null;
    coverage.value = [];
    cursor.value = '';
    complete.value = false;
    error.value = null;
    rowsByWindow.clear();
    countsByWindow.clear();
    histogram.value = [];
    countsStarted = false;
    stats = null;
    if (clearFilters) selectedStreams.value = {};
  }

  function publishRows() {
    const rows = [...rowsByWindow.entries()].sort(([a], [b]) => a - b).flatMap(([, rows]) => rows);
    snapshot?.onRows(markRaw(rows), stats);
  }

  function publishCounts() {
    const buckets = new Map<string, number>();
    for (const data of countsByWindow.values()) for (const bucket of data) buckets.set(bucket.bucket, (buckets.get(bucket.bucket) ?? 0) + bucket.log_count);
    histogram.value = [...buckets].sort(([a], [b]) => a.localeCompare(b)).map(([bucket, log_count]) => ({ bucket, log_count }));
  }

  function initializeCoverage(windows: SearchWindow[]) {
    if (coverage.value.length === 0) coverage.value = windows.map(window => ({ ...window, search: 'pending', count: 'pending', cursor: '', message: '' }));
  }

  function applyEvent(event: WindowedEvent, count: boolean, retry: boolean, advanceRetry = false) {
    if (event.type === 'plan' && event.windows) {
      initializeCoverage(event.windows);
      if (!count && !retry && event.cursor) cursor.value = event.cursor;
    }
    const window = event.window ? coverage.value[event.window.index] : undefined;
    if (window) {
      if (count) window.count = event.status ?? window.count;
      else window.search = event.status ?? window.search;
      if (!count && event.cursor && event.status !== 'complete' && event.status !== 'skipped') window.cursor = event.cursor;
      if (event.message) window.message = event.message;
    }
    if (event.status === 'skipped') cursor.value = event.cursor ?? '';
    if (event.type === 'rows' && window) {
      rowsByWindow.set(window.index, [...(rowsByWindow.get(window.index) ?? []), ...(event.logs ?? [])]);
      if (event.stats) {
        stats = { ...event.stats, rows_returned: [...rowsByWindow.values()].reduce((n, rows) => n + rows.length, 0), rows_read: (stats?.rows_read ?? 0) + event.stats.rows_read, bytes_read: (stats?.bytes_read ?? 0) + event.stats.bytes_read, execution_time_ms: (stats?.execution_time_ms ?? 0) + event.stats.execution_time_ms };
      }
      if (!retry || advanceRetry) cursor.value = event.cursor ?? '';
      publishRows();
    }
    if (event.type === 'count' && window) {
      countsByWindow.set(window.index, event.buckets ?? []);
      publishCounts();
    }
    if (event.type === 'end' && !count && !retry) {
      if (!event.message) cursor.value = event.cursor ?? '';
      complete.value = event.complete && !coverage.value.some(w => w.search === 'failed' || w.search === 'skipped');
    }
    if (event.type === 'end' && event.message) {
      error.value = event.message;
      if (count) {
        for (const window of coverage.value) if (window.count === 'pending' || window.count === 'running') window.count = 'failed';
      } else interruptSearch();
    }
  }

  // Matches the backend count buckets: one zone offset, taken at the range start, shifts every bucket.
  function zoneOffsetMs(request: WindowedRequest) {
    const reference = Date.parse(request.start_time ?? '');
    if (!request.timezone || !Number.isFinite(reference)) return 0;
    try { return fromAbsolute(reference, request.timezone).offset; }
    catch { return 0; }
  }

  function histogramFromRows() {
    const stepMs = Number.parseFloat(granularity.value) * (granularity.value.endsWith('h') ? 3600000 : granularity.value.endsWith('m') ? 60000 : 1000);
    const offsetMs = snapshot ? zoneOffsetMs(snapshot.request) : 0;
    const buckets = new Map<string, number>();
    for (const rows of rowsByWindow.values()) for (const row of rows) {
      if (typeof row._time !== 'string') continue;
      const timestamp = Date.parse(row._time);
      if (!Number.isFinite(timestamp)) continue;
      const bucket = new Date(Math.floor((timestamp + offsetMs) / stepMs) * stepMs - offsetMs).toISOString();
      buckets.set(bucket, (buckets.get(bucket) ?? 0) + 1);
    }
    histogram.value = [...buckets].sort(([a], [b]) => a.localeCompare(b)).map(([bucket, log_count]) => ({ bucket, log_count }));
    for (const window of coverage.value) window.count = 'complete';
  }

  async function countWindows(windowIndex?: number) {
    if (!snapshot) return;
    const run = generation;
    const controller = new AbortController();
    countControllers.add(controller);
    isCounting.value = true;
    const request = { ...snapshot.request, count: true, step: granularity.value, window_index: windowIndex };
    try {
      await streamWindowedQuery(snapshot.teamId, snapshot.sourceId, request, controller.signal, event => {
        if (run === generation) applyEvent(event, true, windowIndex !== undefined);
      });
    } catch (err) {
      if (run === generation && !controller.signal.aborted) {
        error.value = err instanceof Error ? err.message : 'Count request failed';
        for (const window of coverage.value) if (window.count === 'running' || window.count === 'pending' && (windowIndex === undefined || window.index === windowIndex)) window.count = 'failed';
      }
    } finally {
      countControllers.delete(controller);
      if (run === generation) isCounting.value = countControllers.size > 0;
    }
  }

  async function executeSearch(options: SearchOptions) {
    if (!snapshot || options.signal?.aborted) return;
    searchController?.abort();
    const controller = new AbortController();
    searchController = controller;
    const cancel = () => controller.abort();
    options.signal?.addEventListener('abort', cancel, { once: true });
    const run = generation;
    isSearching.value = true;
    error.value = null;
    const advanceRetry = options.retry !== undefined && cursor.value === options.cursor;
    try {
      await streamWindowedQuery(snapshot.teamId, snapshot.sourceId, { ...snapshot.request, cursor: options.cursor, skip_window: options.skip, window_index: options.retry }, controller.signal, event => {
        if (run === generation && searchController === controller) applyEvent(event, false, options.retry !== undefined, advanceRetry);
      });
      if (run !== generation || controller.signal.aborted) return;
      if (!countsStarted) {
        if (complete.value) histogramFromRows();
        else { countsStarted = true; void countWindows(); }
      }
    } catch (err) {
      if (run === generation && !controller.signal.aborted) {
        if (err instanceof WindowedHTTPError && err.status === 422) { reset(); throw err; }
        error.value = err instanceof Error ? err.message : 'Search request failed';
        interruptSearch();
      }
    } finally {
      options.signal?.removeEventListener('abort', cancel);
      if (searchController === controller) {
        searchController = null;
        isSearching.value = false;
        // A superseding search interrupted this one, like a stop. A finished page leaves only prefetched windows running.
        if (controller.signal.aborted) interruptSearch();
        else for (const window of coverage.value) if (window.search === 'running') window.search = 'pending';
      }
    }
  }

  async function search(options: SearchOptions = {}) {
    searchController?.abort();
    if (searchTask) await searchTask.catch(() => undefined);
    const task = executeSearch(options);
    searchTask = task;
    try { await task; }
    finally { if (searchTask === task) searchTask = null; }
  }

  async function start(input: SearchSnapshot, signal: AbortSignal) {
    reset();
    snapshot = input;
    active.value = true;
    histogramEnabled.value = input.histogram;
    granularity.value = HistogramService.calculateOptimalGranularity(input.request.start_time ?? '', input.request.end_time ?? '');
    await search({ signal });
  }
  function loadOlder() { if (!isSearching.value && cursor.value) return search({ cursor: cursor.value }); }
  async function skipWindow() {
    const window = skippableWindow.value;
    if (!window) return;
    searchController?.abort();
    await search({ cursor: window.cursor, skip: window.index });
  }
  function retrySearch(window: WindowCoverage) { if (!isSearching.value && canRetrySearch(window)) return search({ cursor: window.cursor, retry: window.index }); }

  return { active, coverage, cursor, isSearching, isCounting, complete, error, histogramEnabled, selectedStreams, histogram, granularity, searchedWindows, total, countComplete, skippableWindow, streamFilters, start, stop, reset, loadOlder, skipWindow, canRetrySearch, retrySearch, retryCount: countWindows };
});
