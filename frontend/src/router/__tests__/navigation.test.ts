import { describe, expect, it } from 'vitest';
import { createMemoryHistory, createRouter } from 'vue-router';
import { usePendingNavigation } from '../navigation';

function fixture() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: { render: () => null } },
    { path: '/first', component: { render: () => null } },
    { path: '/second', component: { render: () => null } },
  ] });
  return { router, pending: usePendingNavigation(router) };
}

describe('pending router navigation', () => {
  it('clears committed, rejected, and duplicate navigations', async () => {
    const { router, pending } = fixture();
    await router.push('/');
    expect(pending.value).toBeNull();
    router.beforeEach(to => to.path !== '/first');

    await router.push('/first');
    expect(pending.value).toBeNull();
    await router.push('/');
    expect(pending.value).toBeNull();
  });

  it('clears a navigation when a guard throws', async () => {
    const { router, pending } = fixture();
    await router.push('/');
    router.beforeEach(() => { throw new Error('Could not load route'); });

    await expect(router.push('/first')).rejects.toThrow('Could not load route');
    expect(pending.value).toBeNull();
  });

  it('keeps the newer navigation pending when a superseded one finishes', async () => {
    const { router, pending } = fixture();
    await router.push('/');
    const releases = new Map<string, () => void>();
    router.beforeResolve(to => new Promise<void>(resolve => { releases.set(to.path, resolve); }));
    const first = router.push('/first');
    await expect.poll(() => releases.has('/first')).toBe(true);
    const second = router.push('/second');
    await expect.poll(() => releases.has('/second')).toBe(true);

    releases.get('/first')?.();
    await first;
    expect(pending.value?.path).toBe('/second');
    releases.get('/second')?.();
    await second;
    expect(router.currentRoute.value.path).toBe('/second');
    expect(pending.value).toBeNull();
  });
});
