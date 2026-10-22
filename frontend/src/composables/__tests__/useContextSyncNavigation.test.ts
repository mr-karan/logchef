import { afterEach, describe, expect, it, vi } from 'vitest';
import { createApp, defineComponent, h, KeepAlive, type Component, type VNode } from 'vue';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { createMemoryHistory, createRouter, RouterView } from 'vue-router';
import { sourcesApi, type Source } from '@/api/sources';
import { teamsApi, type UserTeamMembership } from '@/api/teams';
import { contextRouterGuard } from '@/router/contextGuard';
import { useContextStore } from '@/stores/context';
import { useSourcesStore } from '@/stores/sources';
import { useContextSync } from '../useContextSync';

const ok = <T>(data: T) => ({ status: 'success' as const, data });
const team: UserTeamMembership = { id: 3, name: 'Team', description: '', created_at: '', updated_at: '', role: 'member', member_count: 1 };
const source: Source = {
  id: 7, name: 'Logs', source_type: 'clickhouse', _meta_is_auto_created: false,
  _meta_ts_field: 'timestamp', connection: { host: '', database: 'logs', table_name: 'logs' },
  ttl_days: 30, created_at: '', updated_at: '', is_connected: true,
};

function deferred() {
  let release = () => {};
  const done = new Promise<void>(resolve => { release = resolve; });
  return { done, release };
}

const flush = () => new Promise(resolve => setImmediate(resolve));

describe('Alerts context navigation', () => {
  let cleanup: (() => void) | undefined;

  afterEach(() => {
    cleanup?.();
    vi.restoreAllMocks();
    localStorage.clear();
  });

  async function mountAlerts(stub?: () => void, library?: () => Promise<Component>) {
    const pinia = createPinia();
    setActivePinia(pinia);
    vi.spyOn(teamsApi, 'listUserTeams').mockResolvedValue(ok([team, { ...team, id: 4 }]));
    vi.spyOn(sourcesApi, 'listTeamSources').mockImplementation(async id => ok(id === 3 ? [source, { ...source, id: 8 }] : [{ ...source, id: 9 }]));
    vi.spyOn(sourcesApi, 'getTeamSource').mockImplementation(async (_team, id) => ok({ ...source, id }));
    stub?.();
    const page = () => defineComponent({ setup() {
      const sync = useContextSync();
      void sync.initialize();
      return () => null;
    } });
    const Layout = defineComponent({ render: () => h(RouterView, null, {
      default: ({ Component }: { Component?: VNode }) => h(KeepAlive, null, { default: () => Component }),
    }) });
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/logs', component: Layout, children: [
      { path: 'alerts', component: page() },
      { path: 'alerts/new', component: page() },
      { path: 'library', component: library ?? { render: () => null } },
    ] }] });
    router.beforeEach(contextRouterGuard);
    await router.push({ path: '/logs/alerts', query: { team: '3', source: '7' } });
    const app = createApp({ render: () => h(RouterView) }).use(pinia).use(router);
    app.mount(document.createElement('div'));
    cleanup = () => { app.unmount(); disposePinia(pinia); };
    await new Promise(resolve => setImmediate(resolve));
    return router;
  }

  it('leaves Alerts when the router guard changes context before the route commits', async () => {
    const router = await mountAlerts();
    await router.push({ path: '/logs/library', query: { team: '4', source: '9' } });
    await new Promise(resolve => setImmediate(resolve));

    expect(router.currentRoute.value.path).toBe('/logs/library');
    expect(router.currentRoute.value.query).toEqual({ team: '4', source: '9' });
  });

  it('goes Back from New Alert when its context differs from the Alerts entry', async () => {
    const router = await mountAlerts();
    await router.push({ path: '/logs/alerts/new', query: { team: '3', source: '7' } });
    await new Promise(resolve => setImmediate(resolve));
    await router.replace({ path: '/logs/alerts/new', query: { team: '3', source: '8' } });
    await new Promise(resolve => setImmediate(resolve));
    expect(router.currentRoute.value.path).toBe('/logs/alerts/new');
    router.back();
    await new Promise(resolve => setImmediate(resolve));
    await new Promise(resolve => setImmediate(resolve));

    expect(router.currentRoute.value.path).toBe('/logs/alerts');
    expect(router.currentRoute.value.query).toEqual({ team: '3', source: '7' });
  });

  it('leaves Alerts for a lazy same-team route while its initial source list finishes', async () => {
    const listed = deferred();
    const chunk = deferred();
    const library = vi.fn(async () => {
      await chunk.done;
      return defineComponent({ render: () => null });
    });
    const router = await mountAlerts(() => {
      vi.mocked(sourcesApi.listTeamSources).mockImplementation(async () => {
        await listed.done;
        return ok([source, { ...source, id: 8 }]);
      });
    }, library);
    const leaving = router.push({ path: '/logs/library', query: { team: '3', source: '8' } });
    await vi.waitFor(() => expect(library).toHaveBeenCalledOnce());
    listed.release();
    await flush();
    chunk.release();
    const failure = await leaving;
    await flush();

    expect(failure?.message).toBeUndefined();
    expect(router.currentRoute.value.path).toBe('/logs/library');
    expect(router.currentRoute.value.query).toEqual({ team: '3', source: '8' });
    expect(useSourcesStore().currentSourceDetails?.id).toBe(8);
  });

  it('records a source replacement during an Alerts refresh', async () => {
    const router = await mountAlerts();
    vi.mocked(sourcesApi.listTeamSources).mockResolvedValue(ok([{ ...source, id: 8 }]));

    useSourcesStore().invalidateTeamSources();
    await vi.waitFor(() => expect(useSourcesStore().currentSourceDetails?.id).toBe(8));
    await flush();

    expect(useContextStore().sourceId).toBe(8);
    expect(router.currentRoute.value.query).toEqual({ team: '3', source: '8' });
  });

  it('records the available source on Back to cached Alerts after source removal', async () => {
    const router = await mountAlerts();
    await router.push({ path: '/logs/library', query: { team: '3', source: '7' } });
    vi.mocked(sourcesApi.listTeamSources).mockResolvedValue(ok([{ ...source, id: 8 }]));
    useSourcesStore().invalidateTeamSources();
    await vi.waitFor(() => expect(useSourcesStore().currentSourceDetails?.id).toBe(8));
    expect(router.currentRoute.value.path).toBe('/logs/library');

    router.back();
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/logs/alerts'));
    await flush();

    expect(useContextStore().sourceId).toBe(8);
    expect(useSourcesStore().currentSourceDetails?.id).toBe(8);
    expect(router.currentRoute.value.query).toEqual({ team: '3', source: '8' });
  });
});
