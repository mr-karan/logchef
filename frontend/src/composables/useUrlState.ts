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

  function checkReadiness(): boolean {
    if (!teamsStore.teams || teamsStore.teams.length === 0) {
      return false;
    }

    const teamId = teamsStore.currentTeamId;
    if (!teamId) {
      return false;
    }

    if (sourcesStore.isLoadingTeamSources) {
      return false;
    }

    const sourceId = exploreStore.sourceId;
    if (sourceId && sourcesStore.isLoadingSourceDetails) {
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
        error.value = 'No teams available or accessible.';
        state.value = 'error';
        return;
      }

      const params = route.query as Record<string, string | undefined>;
      
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
          state.value = 'idle';
          return;
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

    const query = exploreStore.urlQueryParameters;
    const currentQuery = route.query;

    const queryChanged = JSON.stringify(query) !== JSON.stringify(currentQuery);

    if (queryChanged) {
      router.replace({ query }).catch(err => {
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
    const query = exploreStore.urlQueryParameters;

    router.push({ query }).catch(err => {
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
      () => exploreStore.logchefqlCode,
      () => exploreStore.rawSql,
    ],
    () => {
      syncUrlFromStore();
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
