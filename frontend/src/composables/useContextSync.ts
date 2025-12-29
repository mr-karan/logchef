import { ref, computed, watch, type Ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useContextStore } from '@/stores/context';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';

export type ContextSyncState = 'idle' | 'loading' | 'ready' | 'error';

interface UseContextSyncOptions {
  syncUrl?: boolean;
  basePath?: string;
}

interface UseContextSyncReturn {
  state: Ref<ContextSyncState>;
  error: Ref<string | null>;
  isReady: Ref<boolean>;
  isLoading: Ref<boolean>;
  teamId: Ref<number | null>;
  sourceId: Ref<number | null>;
  initialize: () => Promise<void>;
  handleTeamChange: (teamId: number) => Promise<void>;
  handleSourceChange: (sourceId: number) => Promise<void>;
}

export function useContextSync(options: UseContextSyncOptions = {}): UseContextSyncReturn {
  const { syncUrl = true, basePath } = options;
  
  const route = useRoute();
  const router = useRouter();
  const contextStore = useContextStore();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();

  const state = ref<ContextSyncState>('idle');
  const error = ref<string | null>(null);
  const isReady = computed(() => state.value === 'ready');
  const isLoading = computed(() => state.value === 'loading');

  const teamId = computed(() => contextStore.teamId);
  const sourceId = computed(() => contextStore.sourceId);

  function parseId(value: unknown): number | null {
    if (value == null) return null;
    const parsed = parseInt(String(value), 10);
    return Number.isNaN(parsed) ? null : parsed;
  }

  async function waitForSourcesLoaded(timeoutMs = 5000): Promise<void> {
    if (!sourcesStore.isLoadingTeamSources && contextStore.sourceId) {
      return;
    }
    
    return new Promise((resolve) => {
      const timeout = setTimeout(() => {
        stopWatch();
        resolve();
      }, timeoutMs);

      const stopWatch = watch(
        () => [sourcesStore.isLoadingTeamSources, contextStore.sourceId] as const,
        ([loading, srcId]) => {
          if (!loading && srcId) {
            clearTimeout(timeout);
            stopWatch();
            resolve();
          }
        },
        { immediate: true }
      );
    });
  }

  async function initialize(): Promise<void> {
    if (state.value === 'loading') return;

    state.value = 'loading';
    error.value = null;

    try {
      if (!teamsStore.userTeams || teamsStore.userTeams.length === 0) {
        await teamsStore.loadUserTeams();
      }

      if (teamsStore.teams.length === 0) {
        error.value = 'No teams available.';
        state.value = 'error';
        return;
      }

      const urlTeam = parseId(route.query.team);
      const urlSource = parseId(route.query.source);
      const storedDefaults = contextStore.getStoredDefaults();
      
      let targetTeamId = urlTeam;
      if (!targetTeamId || !teamsStore.teams.some(t => t.id === targetTeamId)) {
        targetTeamId = storedDefaults.teamId;
      }
      if (!targetTeamId || !teamsStore.teams.some(t => t.id === targetTeamId)) {
        targetTeamId = teamsStore.teams[0]?.id ?? null;
      }

      if (!targetTeamId) {
        error.value = 'No team available.';
        state.value = 'error';
        return;
      }

      contextStore.setFromRoute(targetTeamId, urlSource);
      
      await waitForSourcesLoaded();
      
      if (syncUrl) {
        await syncUrlToContext();
      }

      state.value = 'ready';

    } catch (err: any) {
      console.error('useContextSync: Initialization error:', err);
      error.value = err.message || 'Failed to initialize context.';
      state.value = 'error';
    }
  }

  async function syncUrlToContext(): Promise<void> {
    const query: Record<string, string> = {};
    
    if (contextStore.teamId) {
      query.team = String(contextStore.teamId);
    }
    if (contextStore.sourceId) {
      query.source = String(contextStore.sourceId);
    }
    
    const currentTeam = route.query.team;
    const currentSource = route.query.source;
    
    const needsUpdate = 
      (query.team && currentTeam !== query.team) ||
      (query.source && currentSource !== query.source) ||
      (!query.source && currentSource);
    
    if (needsUpdate) {
      await router.replace({ path: basePath ?? route.path, query });
    }
  }

  async function handleTeamChange(newTeamId: number): Promise<void> {
    if (newTeamId === contextStore.teamId) return;

    contextStore.selectTeam(newTeamId);
    
    await waitForSourcesLoaded();
    
    if (syncUrl) {
      await syncUrlToContext();
    }
  }

  async function handleSourceChange(newSourceId: number): Promise<void> {
    if (newSourceId === contextStore.sourceId) return;
    if (!contextStore.teamId) return;

    if (!sourcesStore.teamSources.some(s => s.id === newSourceId)) {
      console.warn(`useContextSync: Source ${newSourceId} not found`);
      return;
    }

    contextStore.selectSource(newSourceId);

    if (syncUrl) {
      await syncUrlToContext();
    }
  }

  watch(
    () => [route.query.team, route.query.source] as const,
    async ([urlTeam, urlSource], [prevTeam, prevSource]) => {
      if (state.value !== 'ready') return;
      
      const newTeam = parseId(urlTeam);
      const newSource = parseId(urlSource);
      const prevTeamId = parseId(prevTeam);
      const prevSourceId = parseId(prevSource);

      if (newTeam === prevTeamId && newSource === prevSourceId) return;

      if (newTeam && newTeam !== contextStore.teamId) {
        await handleTeamChange(newTeam);
        if (newSource && newSource !== contextStore.sourceId) {
          await handleSourceChange(newSource);
        }
      } else if (newSource && newSource !== contextStore.sourceId) {
        await handleSourceChange(newSource);
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
  };
}
