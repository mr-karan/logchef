import { ref, computed, watch, type Ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useExploreStore } from '@/stores/explore';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { useContextStore } from '@/stores/context';
import { savedQueriesApi } from '@/api/savedQueries';

export type UrlSyncState = 'idle' | 'loading' | 'ready' | 'error';

interface UrlStateReturn {
  state: Ref<UrlSyncState>;
  error: Ref<string | null>;
  isReady: Ref<boolean>;
  initialize: () => Promise<void>;
  pushHistoryEntry: () => void;
}

export function useUrlState(): UrlStateReturn {
  const route = useRoute();
  const router = useRouter();
  const exploreStore = useExploreStore();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();
  const contextStore = useContextStore();

  const state = ref<UrlSyncState>('idle');
  const error = ref<string | null>(null);
  const isReady = computed(() => state.value === 'ready');

  const pendingQueryResolve = ref(false);
  let skipNextSync = false;
  let pendingRouteSyncKey: string | null = null;

  function getRouteQueryParams(): Record<string, string> {
    const fullPath = route.fullPath || '';
    const queryString = fullPath.split('?')[1] ?? '';
    const cleanQuery = queryString.split('#')[0] ?? '';
    const searchParams = new URLSearchParams(cleanQuery);
    const parsed: Record<string, string> = {};

    for (const [key, value] of searchParams.entries()) {
      parsed[key] = value;
    }

    return parsed;
  }

  function normalizeQueryParams(query: Record<string, unknown>): Record<string, string | undefined> {
    const normalized: Record<string, string | undefined> = {};

    const getValue = (key: string) => {
      const value = query[key];
      if (Array.isArray(value)) {
        const first = value[0];
        return first === undefined || first === null || first === '' ? undefined : String(first);
      }
      if (value === undefined || value === null || value === '') {
        return undefined;
      }
      return String(value);
    };

    const team = getValue('team');
    if (team) normalized.team = team;
    const source = getValue('source');
    if (source) normalized.source = source;

    const relativeTime = getValue('t') ?? getValue('time');
    if (relativeTime) normalized.t = relativeTime;

    const start = getValue('start') ?? getValue('start_time');
    if (start) normalized.start = start;
    const end = getValue('end') ?? getValue('end_time');
    if (end) normalized.end = end;

    const limit = getValue('limit');
    if (limit) normalized.limit = limit;

    const mode = getValue('mode');
    if (mode === 'sql') {
      normalized.mode = 'sql';
    }

    const q = getValue('q');
    if (q) normalized.q = q;
    const sql = getValue('sql');
    if (sql) normalized.sql = sql;

    const id = getValue('id');
    if (id) normalized.id = id;

    return normalized;
  }

  function buildQueryKey(query: Record<string, string | undefined>): string {
    return Object.keys(query)
      .sort()
      .map((key) => `${key}=${query[key] ?? ''}`)
      .join('&');
  }

  function checkReadiness(): boolean {
    if (!teamsStore.teams || teamsStore.teams.length === 0) {
      return false;
    }

    if (!contextStore.teamId) {
      return false;
    }

    if (sourcesStore.isLoadingTeamSources) {
      return false;
    }

    if (contextStore.sourceId && sourcesStore.isLoadingSourceDetails) {
      return false;
    }

    if (pendingQueryResolve.value) {
      return false;
    }

    return true;
  }

  async function initialize(): Promise<void> {
    if (state.value === 'loading') {
      return;
    }

    state.value = 'loading';
    error.value = null;
    let shouldExecute = false;

    try {
      if (!teamsStore.teams || teamsStore.teams.length === 0) {
        await teamsStore.loadTeams(false, false);
      }

      if (teamsStore.teams.length === 0) {
        // Check if we got an error from the API (e.g., 500) vs genuinely no teams
        if (teamsStore.error) {
          const apiError = teamsStore.error;
          if (apiError.message?.includes('500') || apiError.error_type === 'ServerError') {
            error.value = 'Unable to load teams. The server may be experiencing issues.';
          } else if (apiError.error_type === 'AuthenticationError') {
            error.value = 'Please log in to access teams.';
          } else {
            error.value = apiError.message || 'Failed to load teams.';
          }
        } else {
          error.value = 'You don\'t have access to any teams yet. Contact your administrator.';
        }
        state.value = 'error';
        return;
      }

      const params = normalizeQueryParams(getRouteQueryParams() as Record<string, unknown>);
      
      if (!params.team || !params.source) {
        const storedDefaults = contextStore.getStoredDefaults();
        const teamId = params.team 
          ? parseInt(params.team, 10) 
          : (storedDefaults.teamId ?? teamsStore.currentTeamId ?? teamsStore.teams?.[0]?.id);
        
        let sourceId = params.source ? parseInt(params.source, 10) : null;
        
        if (!sourceId && teamId) {
          sourceId = storedDefaults.sourceId ?? contextStore.getStoredSourceForTeam(teamId);
          if (!sourceId && sourcesStore.teamSources?.length > 0) {
            sourceId = sourcesStore.teamSources[0].id;
          }
        }

        if (teamId && (!params.team || (!params.source && sourceId))) {
          const newQuery: Record<string, string> = { ...route.query as Record<string, string> };
          if (!params.team) newQuery.team = teamId.toString();
          if (!params.source && sourceId) newQuery.source = sourceId.toString();
          
          await router.replace({ query: newQuery });
          params.team = newQuery.team;
          params.source = newQuery.source;
        }
      }

      const result = exploreStore.initializeFromUrl(params);
      shouldExecute = result.shouldExecute;

      if (result.needsResolve && result.queryId) {
        pendingQueryResolve.value = true;

        try {
          const teamId = teamsStore.currentTeamId;
          const sourceId = exploreStore.sourceId;

          if (teamId && sourceId) {
            const response = await savedQueriesApi.resolveQuery(
              teamId,
              sourceId,
              parseInt(result.queryId)
            );

            if (response.data) {
              const hydrateResult = exploreStore.hydrateFromResolvedQuery(response.data);
              shouldExecute = hydrateResult.shouldExecute;
            }
          }
        } finally {
          pendingQueryResolve.value = false;
        }
      }

      await waitForReadiness();
      state.value = 'ready';

      if (shouldExecute) {
        exploreStore.executeQuery().catch(err => {
          console.error('useUrlState: Error executing initial query:', err);
        });
      }

    } catch (err: any) {
      console.error('useUrlState: Initialization error:', err);
      error.value = err.message || 'Failed to initialize from URL.';
      state.value = 'error';
    }
  }

  function waitForReadiness(maxWaitMs = 5000): Promise<void> {
    if (checkReadiness()) {
      return Promise.resolve();
    }

    return new Promise((resolve) => {
      const timeout = setTimeout(() => {
        stopWatch();
        console.warn('useUrlState: Readiness timeout, proceeding anyway');
        resolve();
      }, maxWaitMs);

      const stopWatch = watch(
        [
          () => teamsStore.teams,
          () => teamsStore.currentTeamId,
          () => sourcesStore.isLoadingTeamSources,
          () => sourcesStore.isLoadingSourceDetails,
          () => pendingQueryResolve.value,
        ],
        () => {
          if (checkReadiness()) {
            clearTimeout(timeout);
            stopWatch();
            resolve();
          }
        },
        { immediate: true }
      );
    });
  }

  function syncUrlFromStore(): void {
    if (state.value !== 'ready') {
      return;
    }

    if (skipNextSync) {
      skipNextSync = false;
      return;
    }

    const query = normalizeQueryParams(exploreStore.urlQueryParameters as Record<string, unknown>);
    const currentQuery = normalizeQueryParams(getRouteQueryParams() as Record<string, unknown>);

    const nextKey = buildQueryKey(query);
    const currentKey = buildQueryKey(currentQuery);

    if (nextKey !== currentKey) {
      pendingRouteSyncKey = nextKey;
      router.replace({ query }).catch(err => {
        pendingRouteSyncKey = null;
        if (err.name !== 'NavigationDuplicated') {
          console.error('useUrlState: Error updating URL:', err);
        }
      });
    }
  }

  function pushHistoryEntry(): void {
    if (state.value !== 'ready') {
      return;
    }

    skipNextSync = true;
    const query = normalizeQueryParams(exploreStore.urlQueryParameters as Record<string, unknown>);
    pendingRouteSyncKey = buildQueryKey(query);

    router.push({ query }).catch(err => {
      pendingRouteSyncKey = null;
      if (err.name !== 'NavigationDuplicated') {
        console.error('useUrlState: Error pushing history:', err);
      }
    });
  }

  watch(
    [
      () => teamsStore.currentTeamId,
      () => exploreStore.sourceId,
      () => exploreStore.limit,
      () => exploreStore.timeRange,
      () => exploreStore.selectedRelativeTime,
      () => exploreStore.activeMode,
      () => exploreStore.selectedQueryId,
    ],
    () => {
      syncUrlFromStore();
    },
    { deep: true }
  );

  watch(
    () => route.fullPath,
    async () => {
      if (state.value !== 'ready') {
        return;
      }

      const normalized = normalizeQueryParams(getRouteQueryParams() as Record<string, unknown>);
      const routeKey = buildQueryKey(normalized);

      if (pendingRouteSyncKey && routeKey === pendingRouteSyncKey) {
        pendingRouteSyncKey = null;
        return;
      }

      const storeKey = buildQueryKey(
        normalizeQueryParams(exploreStore.urlQueryParameters as Record<string, unknown>)
      );
      if (routeKey === storeKey) {
        return;
      }

      if (normalized.id) {
        return;
      }

      const result = exploreStore.initializeFromUrl(normalized, { updateLastExecutedState: false });

      await waitForReadiness();

      if (result.shouldExecute) {
        exploreStore.executeQuery().catch(err => {
          console.error('useUrlState: Error executing query from history navigation:', err);
        });
      }
    },
    { deep: true }
  );

  return {
    state,
    error,
    isReady,
    initialize,
    pushHistoryEntry,
  };
}
