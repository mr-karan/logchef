import { ref, computed, watch, type Ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useContextStore } from '@/stores/context';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { useTeamSourceContext } from '@/composables/useTeamSourceContext';

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
  const teamSourceContext = useTeamSourceContext();

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
      if (!teamsStore.userTeams || teamsStore.userTeams.length === 0) {
        await teamsStore.loadUserTeams();
      }

      if (teamsStore.teams.length === 0) {
        error.value = 'No teams available.';
        state.value = 'error';
        return;
      }

      const urlTeam = teamSourceContext.parseId(route.query.team);
      const urlSource = teamSourceContext.parseId(route.query.source);
      const targetTeamId = teamSourceContext.resolveTeamId(urlTeam);

      if (!targetTeamId) {
        error.value = 'No team available.';
        state.value = 'error';
        return;
      }

      await teamSourceContext.applyContextSelection(targetTeamId, urlSource);
      
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

    await teamSourceContext.applyContextSelection(newTeamId, null);
    
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

    await teamSourceContext.applyContextSelection(contextStore.teamId, newSourceId);

    if (syncUrl) {
      await syncUrlToContext();
    }
  }

  watch(
    () => [route.query.team, route.query.source] as const,
    async ([urlTeam, urlSource], [prevTeam, prevSource]) => {
      if (state.value !== 'ready') return;
      
      const newTeam = teamSourceContext.parseId(urlTeam);
      const newSource = teamSourceContext.parseId(urlSource);
      const prevTeamId = teamSourceContext.parseId(prevTeam);
      const prevSourceId = teamSourceContext.parseId(prevSource);

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
