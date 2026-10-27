import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createApp, defineComponent, h, nextTick, ref, type App, type Component } from 'vue';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import { authApi } from '@/api/auth';
import { metaApi } from '@/api/meta';
import { teamsApi } from '@/api/teams';
import { savedQueriesApi } from '@/api/savedQueries';
import { dashboardsApi } from '@/api/dashboards';
import { collectionsApi } from '@/api/collections';
import type { APIResponse } from '@/api/types';
import { sourcesApi, type Source } from '@/api/sources';
import { i18n } from '@/i18n';
import { useAuthStore } from '@/stores/auth';
import { usePreferencesStore } from '@/stores/preferences';
import { useContextStore } from '@/stores/context';
import CommandPalette from '../CommandPalette.vue';
import { usePaletteHost } from '../usePaletteHost';

const source: Source = { id: 3, name: 'app_logs', _meta_is_auto_created: false, source_type: 'clickhouse', _meta_ts_field: 'timestamp', connection: {}, ttl_days: 1, created_at: '', updated_at: '', is_connected: true };
const ok = <T>(data: T): Promise<APIResponse<T>> => Promise.resolve({ status: 'success', data });
const settle = async () => { await nextTick(); await new Promise(resolve => setTimeout(resolve, 20)); await nextTick(); };
let app: App | null = null;
let dispose: (() => void) | null = null;
beforeAll(() => { if (!Element.prototype.scrollIntoView) Element.prototype.scrollIntoView = () => {}; });
afterEach(() => {
  app?.unmount(); app = null;
  dispose?.(); dispose = null;
  vi.restoreAllMocks();
  vi.useRealTimers();
  document.body.innerHTML = '';
  localStorage.clear();
});

async function setup() {
  const pinia = createPinia();
  setActivePinia(pinia);
  dispose = () => disposePinia(pinia);
  vi.spyOn(authApi, 'getSession').mockImplementation(() => ok({ user: { id: '1', email: 'owner@example.com', full_name: 'Owner', role: 'admin' satisfies 'admin', status: 'active' satisfies 'active', account_type: 'human' satisfies 'human', created_at: '', updated_at: '' }, session: { id: 'test-session', user_id: '1', expires_at: '', created_at: '' } }));
  vi.spyOn(metaApi, 'getMeta').mockImplementation(() => ok({ version: 'test', http_server_timeout: '30s', max_query_limit: 1000, alerts_enabled: false }));
  const teams = vi.spyOn(teamsApi, 'listAllTeams').mockImplementation(() => ok([{ id: 1, name: 'Platform', member_count: 1, created_by: '1', description: '', created_at: '', updated_at: '' }]));
  vi.spyOn(teamsApi, 'listTeamSources').mockImplementation(() => ok([source]));
  vi.spyOn(sourcesApi, 'listTeamSources').mockImplementation(() => ok([source]));
  vi.spyOn(savedQueriesApi, 'list').mockImplementation(() => ok([]));
  vi.spyOn(dashboardsApi, 'list').mockImplementation(() => ok([]));
  vi.spyOn(collectionsApi, 'list').mockImplementation(() => ok([]));
  await useAuthStore().initialize();
  useContextStore().setFromRoute(1, 3);
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', name: 'Home', component: { render: () => null }, meta: { title: 'ui.profile', palette: { icon: 'profile' } } },
    { path: '/logs/explore', name: 'LogExplorer', component: { render: () => null } },
  ] });
  await router.push('/');
  const root = document.createElement('div'); document.body.append(root);
  return { pinia, router, root, teams };
}

function type(text: string) {
  const input = document.querySelector('input');
  if (!input) throw new Error('palette input missing');
  input.value = text;
  input.setSelectionRange(text.length, text.length);
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return input;
}
const key = (element: Element | Window, value: string, options: KeyboardEventInit = {}) => element.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true, ...options }));

describe('palette dialog', () => {
  it('renders the blocked run and completes its time range with Tab before Enter navigates', async () => {
    const { pinia, router, root } = await setup();
    app = createApp({ render: () => h(CommandPalette, { open: true }) }).use(pinia).use(router).use(i18n);
    app.mount(root); await settle();
    const input = type('@platform/app_logs last:1'); await settle();
    expect(document.body.textContent).toContain('Unknown time range last:1');
    key(input, 'Tab'); await settle();
    expect(input.value).toBe('@platform/app_logs last:1h ');
    expect(document.body.textContent).toContain('Run in Explore');
    key(input, 'Enter', { isComposing: true }); await settle();
    expect(router.currentRoute.value.path).toBe('/');
    key(input, 'Enter'); await settle();
    expect(router.currentRoute.value.query).toMatchObject({ team: '1', source: '3', q: '', t: '1h' });
  });

  it('runs a range-only fallback without loading source schema', async () => {
    const { pinia, router, root } = await setup();
    app = createApp({ render: () => h(CommandPalette, { open: true }) }).use(pinia).use(router).use(i18n);
    app.mount(root); await settle();
    const details = vi.spyOn(sourcesApi, 'getTeamSource');
    const input = type('last:1h'); await settle();
    key(input, 'Enter', { metaKey: true }); await settle();
    expect(router.currentRoute.value.query).toMatchObject({ team: '1', source: '3', q: '', t: '1h' });
    expect(details).not.toHaveBeenCalled();
  });

  it('keeps the input focused after clicking a heading and continues filtering, arrows and Enter', async () => {
    const { pinia, router, root } = await setup();
    app = createApp({ render: () => h(CommandPalette, { open: true }) }).use(pinia).use(router).use(i18n);
    app.mount(root); await settle();
    const input = type('@platform/app'); await settle();
    const list = document.querySelector('[role="listbox"]');
    expect(list?.getAttribute('tabindex')).toBeNull();
    const heading = document.querySelector('[data-slot="command-group-heading"]');
    if (!heading) throw new Error('heading missing');
    heading.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
    heading.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(document.activeElement).toBe(input);
    type('@platform/app_logs last:1'); await settle();
    key(input, 'ArrowDown'); await settle(); key(input, 'Enter'); await settle();
    expect(input.value).toContain('last:1h');
    key(input, 'Enter', { ctrlKey: true }); await settle();
    expect(router.currentRoute.value.query).toMatchObject({ source: '3', t: '1h' });
  });

  it('renders the selected ghost from the pure model', async () => {
    const { pinia, router, root } = await setup();
    vi.mocked(teamsApi.listTeamSources).mockImplementation(() => ok([source, { ...source, id: 4, name: 'api_logs' }]));
    app = createApp({ render: () => h(CommandPalette, { open: true }) }).use(pinia).use(router).use(i18n);
    app.mount(root); await settle();
    const input = type('@platform/a'); await settle();
    key(input, 'ArrowDown'); await settle();
    expect(document.querySelector('[aria-hidden="true"] .text-muted-foreground\\/50')?.textContent).toBe('pp_logs');
    input.setSelectionRange(2, 4); input.dispatchEvent(new Event('select')); await settle();
    expect(document.querySelector('[aria-hidden="true"] .text-muted-foreground\\/50')?.textContent).toBe('');
  });

  it('retains a duplicate source through keyboard completion and query typing', async () => {
    const { pinia, router, root } = await setup();
    vi.mocked(teamsApi.listTeamSources).mockImplementation(() => ok([source, { ...source, id: 4 }]));
    app = createApp({ render: () => h(CommandPalette, { open: true }) }).use(pinia).use(router).use(i18n);
    app.mount(root); await settle();
    const input = type('@platform/app_logs'); await settle();
    key(input, 'ArrowDown'); await settle(); key(input, 'Tab'); await settle();
    expect(input.value).toBe('@platform/app_logs#4 ');
    type(input.value + 'level=error last:1'); await settle();
    key(input, 'Tab'); await settle(); key(input, 'Enter'); await settle();
    expect(router.currentRoute.value.query).toMatchObject({ source: '4', q: 'level=error', t: '1h' });
  });

  it.each([['1h', '22:58 to 23:58', 'Oct 27, 23:03 to Oct 28, 00:03'], ['today', '00:00 to 23:58', '00:00 to 00:03']])('refreshes %s across five minutes and midnight and owns its clock lifecycle', async (range, initial, updated) => {
    const { pinia, router, root } = await setup();
    usePreferencesStore().preferences.timezone = 'utc';
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-27T23:58:00Z'));
    const open = ref(true);
    app = createApp({ render: () => h(CommandPalette, { open: open.value }) }).use(pinia).use(router).use(i18n);
    app.mount(root); await vi.advanceTimersByTimeAsync(50); await nextTick();
    type(`@platform/app_logs last:${range}`); await nextTick();
    expect(document.body.textContent).toContain(initial);
    const before = vi.getTimerCount();
    await vi.advanceTimersByTimeAsync(300_000); await nextTick();
    expect(document.body.textContent).toContain(updated);
    open.value = false; await nextTick(); await vi.advanceTimersByTimeAsync(1000);
    expect(vi.getTimerCount()).toBeLessThan(before);
    open.value = true; await nextTick(); await vi.advanceTimersByTimeAsync(50);
    app.unmount(); app = null;
    await vi.advanceTimersByTimeAsync(1000);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('loads only on the capture shortcut, restores focus and blocks Escape from Explore', async () => {
    const { pinia, router, root, teams } = await setup();
    const outside = document.createElement('textarea'); document.body.append(outside); outside.focus();
    let hostComponent: () => Component | null = () => null;
    const Harness = defineComponent({ setup() {
      const palette = usePaletteHost(); hostComponent = () => palette.component.value;
      return () => palette.component.value ? h(palette.component.value, { open: palette.state.value.kind === 'open', cancellation: palette.cancellation.value, onClose: () => palette.close() }) : null;
    } });
    app = createApp(Harness).use(pinia).use(router).use(i18n); app.mount(root);
    expect(teams).not.toHaveBeenCalled();
    const bubble = vi.fn(); window.addEventListener('keydown', bubble);
    try {
      key(outside, 'k', { ctrlKey: true, isComposing: true }); await settle();
      expect(document.querySelector('[role="dialog"]')).toBeNull();
      key(outside, 'k', { ctrlKey: true });
      await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull()); await settle();
      expect(teams).toHaveBeenCalledTimes(1);
      expect(bubble).toHaveBeenCalledTimes(1);
      await router.push('/logs/explore?team=1&source=3'); await settle();
      expect(document.querySelector('[role="dialog"]')).toBeNull();
      outside.focus(); key(outside, 'k', { ctrlKey: true }); await settle();
      const typing = type('unfinished search'); await settle();
      await router.push('/logs/explore?team=1&source=3&mode=logchefql&q=finished&limit=100'); await settle();
      expect(document.querySelector('[role="dialog"]')).not.toBeNull();
      expect(typing.value).toBe('unfinished search');
      key(window, 'Escape'); await settle();
      expect(document.activeElement).toBe(outside);
      expect(bubble).toHaveBeenCalledTimes(1);
      expect(document.querySelector('[role="dialog"]')).toBeNull();
      key(outside, 'л', { ctrlKey: true, code: 'KeyK' }); await settle();
      expect(document.querySelector('[role="dialog"]')).not.toBeNull();
      key(window, 'Escape'); await settle();
      key(outside, 'e', { ctrlKey: true, code: 'KeyK' }); await settle();
      expect(document.querySelector('[role="dialog"]')).toBeNull();
      useAuthStore().clearState(); await settle();
      expect(hostComponent()).toBeNull();
    } finally { window.removeEventListener('keydown', bubble); }
  });
});
