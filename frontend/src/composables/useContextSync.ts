import { ref, computed, watch, type Ref } from 'vue';
import { useRoute } from 'vue-router';
import { useContextStore } from '@/stores/context';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { useTeamSourceRouteSync } from '@/composables/useTeamSourceRouteSync';

export type ContextSyncState = 'idle' | 'loading' | 'ready' | 'error';

interface UseContextSyncOptions {
  syncUrl?: boolean;
  basePath?: string;
  allowMissingSource?: boolean;
}

interface UseContextSyncReturn {
  state: Ref<ContextSyncState>;
  error: Ref<string | null>;
  isReady: Ref<boolean>;
  isLoading: Ref<boolean>;
  teamId: Ref<number | null>;
  sourceId: Ref<number | null>;
  initialize: () => Promise<void>;
  handleTeamChange: (teamId: number, options?: { clearSource?: boolean }) => Promise<void>;
  handleSourceChange: (sourceId: number) => Promise<void>;
  clearSourceSelection: () => Promise<void>;
}

export function useContextSync(options: UseContextSyncOptions = {}): UseContextSyncReturn {
  const { syncUrl = true, basePath, allowMissingSource = false } = options;

  const contextStore = useContextStore();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();
  const route = useRoute();
  const routeSync = useTeamSourceRouteSync(basePath);

  const state = ref<ContextSyncState>('idle');
  const error = ref<string | null>(null);
  const isReady = computed(() => state.value === 'ready');
  const isLoading = computed(() => state.value === 'loading');

  const teamId = computed(() => contextStore.teamId);
  const sourceId = computed(() => contextStore.sourceId);

  async function initialize(): Promise<void> {
    if (state.value === 'loading') return;

    state.value = 'loading';
    error.value = null;

    try {
      if (!teamsStore.teams || teamsStore.teams.length === 0) {
        await teamsStore.loadTeams(false, false);
      }

      if (teamsStore.teams.length === 0) {
        error.value = 'No teams available.';
        state.value = 'error';
        return;
      }

      const { teamId: targetTeamId } = await routeSync.applyRouteContext({ allowMissingSource });

      if (!targetTeamId) {
        error.value = 'No team available.';
        state.value = 'error';
        return;
      }
      
      if (syncUrl) {
        await routeSync.syncUrlToContext();
      }

      state.value = 'ready';

    } catch (err: any) {
      console.error('useContextSync: Initialization error:', err);
      error.value = err.message || 'Failed to initialize context.';
      state.value = 'error';
    }
  }

  async function syncUrlToContext(): Promise<void> {
    await routeSync.syncUrlToContext();
  }

  async function handleTeamChange(
    newTeamId: number,
    changeOptions: { clearSource?: boolean } = {}
  ): Promise<void> {
    if (newTeamId === contextStore.teamId) return;

    await routeSync.selectTeam(newTeamId, {
      clearSource: changeOptions.clearSource,
      syncUrl,
    });
  }

  async function handleSourceChange(newSourceId: number): Promise<void> {
    if (newSourceId === contextStore.sourceId) return;
    if (!contextStore.teamId) return;

    if (!sourcesStore.teamSources.some(s => s.id === newSourceId)) {
      console.warn(`useContextSync: Source ${newSourceId} not found`);
      return;
    }

    await routeSync.selectSource(newSourceId, { syncUrl });
  }

  async function clearSourceSelection(): Promise<void> {
    await routeSync.clearSourceSelection({ syncUrl });
  }

  watch(
    () => [route.query.team, route.query.source] as const,
    async ([nextRouteTeam, nextRouteSource], previousRouteSelection) => {
      if (state.value !== 'ready') return;
      const [previousRouteTeam, previousRouteSource] = previousRouteSelection ?? [undefined, undefined];
      if (nextRouteTeam === previousRouteTeam && nextRouteSource === previousRouteSource) {
        return;
      }

      const { teamId: nextTeamId, sourceId: nextSourceId } = await routeSync.applyRouteContext({
        allowMissingSource,
      });

      if (syncUrl && (nextTeamId !== null || nextSourceId !== null || contextStore.teamId !== null)) {
        await syncUrlToContext();
      }
    }
  );

  return {
    state,
    error,
    isReady,
    isLoading,
    teamId,
    sourceId,
    initialize,
    handleTeamChange,
    handleSourceChange,
    clearSourceSelection,
  };
}
