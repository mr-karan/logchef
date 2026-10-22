import { shallowRef, type ShallowRef } from 'vue';
import type { RouteLocationNormalized, Router } from 'vue-router';

const pendingRoutes = new WeakMap<Router, ShallowRef<RouteLocationNormalized | null>>();

/** Tracks the latest navigation until it commits, fails, or is cancelled. */
export function usePendingNavigation(router: Router) {
  const existing = pendingRoutes.get(router);
  if (existing) return existing;

  const pending = shallowRef<RouteLocationNormalized | null>(null);
  pendingRoutes.set(router, pending);
  router.beforeEach(to => { pending.value = to; });
  const finish = (to: RouteLocationNormalized) => {
    if (pending.value === to) pending.value = null;
  };
  router.afterEach(finish);
  router.onError((_error, to) => finish(to));
  return pending;
}
