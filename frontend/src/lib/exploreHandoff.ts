import { shallowRef, watch } from 'vue';
import type { RouteLocationRaw, Router } from 'vue-router';
import { EXPLORE_PATH, parseExploreLocation, sameExploreLocation } from './exploreLocation';

export const exploreReopen = shallowRef<{ readonly fullPath: string } | null>(null);

type Scope = { readonly teamId: number; readonly sourceId: number | null };
let landing: { fullPath: string; scope: Scope; finish: (applied: boolean) => void } | null = null;

export function reportExploreLanding(fullPath: string, scope: Scope | null): void {
  if (landing?.fullPath !== fullPath) return;
  landing.finish(scope !== null && scope.teamId === landing.scope.teamId && scope.sourceId === landing.scope.sourceId);
}

export async function navigateFromPalette(router: Router, to: RouteLocationRaw, requireApplied = false): Promise<boolean> {
  landing?.finish(false);
  const destination = router.resolve(to);
  const current = router.currentRoute.value;
  const comparison = { ...current.query };
  if (!Object.hasOwn(destination.query, 'limit')) delete comparison.limit;
  const reopen = destination.path === EXPLORE_PATH && current.path === EXPLORE_PATH
    && parseExploreLocation(destination.query).target.kind === 'query'
    && sameExploreLocation(destination.query, comparison);
  const targetPath = reopen ? current.fullPath : destination.fullPath;
  let completed = false;
  let finish = (_applied: boolean) => {};
  const applied = requireApplied ? new Promise<boolean>(resolve => {
    const scope = parseExploreLocation(destination.query);
    const timer = setTimeout(() => finish(false), 15_000);
    const stop = watch(router.currentRoute, to => {
      if (to.fullPath !== targetPath) finish(false);
    }, { flush: 'sync' });
    finish = value => {
      if (completed) return;
      completed = true;
      clearTimeout(timer);
      stop();
      if (landing?.finish === finish) landing = null;
      resolve(value);
    };
    if (scope.teamId === null || scope.sourceId === null) finish(false);
    else landing = { fullPath: targetPath, scope: { teamId: scope.teamId, sourceId: scope.sourceId }, finish };
  }) : Promise.resolve(true);
  const navigation = (async () => {
    if (reopen) {
      exploreReopen.value = { fullPath: current.fullPath };
    } else {
      const failure = await router.push(to);
      if (failure || router.currentRoute.value.fullPath !== destination.fullPath) {
        finish(false);
        return false;
      }
    }
    return await applied;
  })();
  try {
    return await (requireApplied ? Promise.race([navigation, applied]) : navigation);
  } catch (error) {
    finish(false);
    throw error;
  }
}
