import { afterEach, describe, expect, it, vi } from 'vitest';
import { createMemoryHistory, createRouter } from 'vue-router';
import { exploreReopen, navigateFromPalette, reportExploreLanding } from '../exploreHandoff';

const component = { render: () => null };
const to = { path: '/logs/explore', query: { team: '1', source: '3', q: 'status=500', t: '1h' } };
async function routerAt(path = '/admin') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin', component }, { path: '/logs/explore', component }, { path: '/forbidden', component }] });
  await router.push(path);
  return router;
}

afterEach(() => { vi.useRealTimers(); });

describe('palette applied handoff', () => {
  it('waits for application and checks the actual source', async () => {
    const router = await routerAt();
    const pending = navigateFromPalette(router, to, true);
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/logs/explore'));
    reportExploreLanding(router.currentRoute.value.fullPath, { teamId: 1, sourceId: 4 });
    expect(await pending).toBe(false);
  });

  it.each([null, { teamId: 1, sourceId: 3 }])('reports application %j', async scope => {
    const router = await routerAt();
    const pending = navigateFromPalette(router, to, true);
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/logs/explore'));
    reportExploreLanding(router.currentRoute.value.fullPath, scope);
    expect(await pending).toBe(scope !== null);
  });

  it('reopens a matching URL while retaining an existing limit', async () => {
    const router = await routerAt('/logs/explore?team=1&source=3&q=status%3D500&t=1h&limit=100');
    const pending = navigateFromPalette(router, to, true);
    expect(exploreReopen.value?.fullPath).toBe(router.currentRoute.value.fullPath);
    reportExploreLanding(router.currentRoute.value.fullPath, { teamId: 1, sourceId: 3 });
    expect(await pending).toBe(true);
  });

  it.each(['reject', 'redirect'])('does not accept a %s navigation', async kind => {
    const router = await routerAt();
    router.beforeEach(next => next.path === '/logs/explore' ? kind === 'reject' ? false : '/forbidden' : true);
    expect(await navigateFromPalette(router, to, true)).toBe(false);
  });

  it('supersedes an unfinished navigation without accepting its late result', async () => {
    const router = await routerAt();
    const first = navigateFromPalette(router, to, true);
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/logs/explore'));
    const firstPath = router.currentRoute.value.fullPath;
    const second = navigateFromPalette(router, { ...to, query: { ...to.query, source: '4' } }, true);
    expect(await first).toBe(false);
    await vi.waitFor(() => expect(router.currentRoute.value.query.source).toBe('4'));
    reportExploreLanding(firstPath, { teamId: 1, sourceId: 3 });
    reportExploreLanding(router.currentRoute.value.fullPath, { teamId: 1, sourceId: 4 });
    expect(await second).toBe(true);
  });

  it('bounds a navigation guard that never finishes', async () => {
    const router = await routerAt();
    router.beforeEach(() => new Promise<boolean>(() => {}));
    vi.useFakeTimers();
    const pending = navigateFromPalette(router, to, true);
    await vi.advanceTimersByTimeAsync(15000);
    expect(await pending).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('bounds application waiting and removes the abandoned acknowledgement', async () => {
    const router = await routerAt();
    vi.useFakeTimers();
    const pending = navigateFromPalette(router, to, true);
    await vi.advanceTimersByTimeAsync(15000);
    expect(await pending).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });
});
