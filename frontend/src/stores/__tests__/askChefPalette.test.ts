import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { metaApi } from '@/api/meta';
import { useMetaStore } from '../meta';
import { useContextStore } from '../context';
import { useExploreStore } from '../explore';
import { useAskChefStore, MAX_MESSAGE_LENGTH, type ExplorerAskContext } from '../askChef';
import { zoneFromPreference } from '@/utils/timeWindow';

const captured: ExplorerAskContext = { subject: { kind: 'explorer' }, teamId: 2, sourceId: 7, start: 1000, end: 2000, zone: zoneFromPreference('utc'), mode: 'logchefql', query: 'status>=500' };
const wire = 'event: text\ndata: {"delta":"Found it."}\n\nevent: done\ndata: {"stop_reason":"end","tool_calls":0,"input_tokens":0,"output_tokens":0,"duration_ms":1}\n\n';

function response() { return new Response(wire, { headers: { 'Content-Type': 'text/event-stream' } }); }

describe('palette conversations', () => {
  let pinia = createPinia();
  const requests: unknown[] = [];
  beforeEach(async () => {
    pinia = createPinia();
    setActivePinia(pinia);
    requests.length = 0;
    vi.spyOn(metaApi, 'getMeta').mockResolvedValue({ status: 'success', data: { version: 'test', http_server_timeout: '5s', max_query_limit: 100, ask_chef_enabled: true } });
    await useMetaStore().loadMeta();
    useContextStore().selectTeam(1);
    useContextStore().selectSource(3);
    useExploreStore().setWindow({ kind: 'relative', spec: '15m' });
    vi.stubGlobal('fetch', vi.fn((_url: string, init: RequestInit) => {
      requests.push(JSON.parse(String(init.body)));
      return Promise.resolve(response());
    }));
  });
  afterEach(() => { disposePinia(pinia); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

  it('uses the captured context while old Explorer stores still show another source', async () => {
    const store = useAskChefStore();
    await store.openFromPalette(captured, 'why?');
    expect(requests).toEqual([expect.objectContaining({ team_id: 2, source_id: 7, message: 'why?', context: { kind: 'explorer', start: 1000, end: 2000, mode: 'logchefql', query: 'status>=500' } })]);
    useContextStore().selectTeam(2);
    useContextStore().selectSource(7);
    await nextTick();
    expect(store.transcript).toHaveLength(2);
  });

  it('continues the same source and resets for a different source', async () => {
    const store = useAskChefStore();
    await store.openFromPalette(captured, 'first');
    await store.openFromPalette(captured, 'second');
    expect(requests[1]).toMatchObject({ history: [{ role: 'user', text: 'first' }, { role: 'assistant', text: 'Found it.' }] });
    await store.openFromPalette({ ...captured, sourceId: 8 }, 'third');
    expect(requests[2]).toMatchObject({ source_id: 8, history: [], evidence: [] });
    expect(store.transcript).toHaveLength(2);
  });

  it('keeps subsequent drawer turns tied to live Explorer edits', async () => {
    const store = useAskChefStore();
    useContextStore().selectTeam(2);
    useContextStore().selectSource(7);
    await nextTick();
    await store.openFromPalette(captured, 'first');
    useExploreStore().setLogchefqlCode('status>=400');
    useExploreStore().setWindow({ kind: 'relative', spec: '1h' });
    await store.send('next');
    expect(requests[1]).toMatchObject({ context: { kind: 'explorer', query: 'status>=400' } });
    const live = store.context;
    expect(live && live.end - live.start).toBe(3600000);
  });

  it('opens without creating a paid turn and enforces the store message limit', async () => {
    const store = useAskChefStore();
    await store.openFromPalette(captured, null);
    expect(store.open).toBe(true);
    expect(requests).toEqual([]);
    expect(store.transcript).toEqual([]);
    await store.openFromPalette(captured, 'x'.repeat(MAX_MESSAGE_LENGTH + 100));
    expect(requests[0]).toMatchObject({ message: 'x'.repeat(MAX_MESSAGE_LENGTH) });
  });

  it('rejects an accepted effect if the feature was disabled before activation', async () => {
    useMetaStore().clearState();
    const store = useAskChefStore();
    await store.openFromPalette(captured, 'first');
    expect(store.open).toBe(false);
    expect(store.transcript).toEqual([]);
    expect(requests).toEqual([]);
  });

  it('preempts a streaming answer instead of dropping the explicit question', async () => {
    let aborted = false;
    vi.stubGlobal('fetch', vi.fn((_url: string, init: RequestInit) => {
      requests.push(JSON.parse(String(init.body)));
      if (requests.length > 1) return Promise.resolve(response());
      const body = new ReadableStream<Uint8Array>({ start(controller) {
        init.signal?.addEventListener('abort', () => { aborted = true; controller.error(new DOMException('aborted', 'AbortError')); });
      } });
      return Promise.resolve(new Response(body));
    }));
    const store = useAskChefStore();
    const first = store.openFromPalette(captured, 'first');
    await vi.waitFor(() => expect(store.status.kind).toBe('streaming'));
    await store.openFromPalette(captured, 'second');
    await first;
    expect(aborted).toBe(true);
    expect(requests).toHaveLength(2);
    expect(requests[1]).toMatchObject({ message: 'second', history: [] });
    expect(store.status.kind).toBe('idle');
  });
});
