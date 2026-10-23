import { watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { usePendingNavigation } from '@/router/navigation';
import { pickTeam, useContextStore } from '@/stores/context';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';

/** Team and source selection for pages other than Explorer, kept in the page's URL. */
export function useContextSync(options: { basePath?: string } = {}) {
  const route = useRoute();
  const router = useRouter();
  const pendingNavigation = usePendingNavigation(router);
  const contextStore = useContextStore();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();
  // LogsLayout caches views, so a cached page must never rewrite another route.
  const syncPath = options.basePath ?? route.path;
  let requestId = 0;

  watch(() => [contextStore.teamId, contextStore.sourceId, route.path, pendingNavigation.value], async (_value, _oldValue, onCleanup) => {
    let current = true;
    onCleanup(() => { current = false; });
    if (route.path !== syncPath || pendingNavigation.value) return;
    try {
      await sourcesStore.loadContext();
    } catch (error) {
      console.error('error loading team sources', error);
      return;
    }
    if (!current || route.path !== syncPath || pendingNavigation.value) return;
    const query: Record<string, string> = {};
    if (contextStore.teamId) query.team = String(contextStore.teamId);
    if (contextStore.sourceId) query.source = String(contextStore.sourceId);
    if (route.query.team !== query.team || route.query.source !== query.source) {
      await router.replace({ path: syncPath, query });
    }
  }, { immediate: true });

  async function select(teamId: number, sourceId: number | null) {
    requestId++;
    contextStore.selectTeam(teamId);
    if (sourceId !== null) contextStore.selectSource(sourceId);
    try {
      await sourcesStore.loadContext();
    } catch (error) {
      console.error('error loading team sources', error);
    }
  }

  /** Keeps the router's context and records it after its source list loads. */
  async function initialize() {
    const id = ++requestId;
    if (teamsStore.teams.length === 0) await teamsStore.loadTeams(false, false);
    if (id !== requestId) return;
    const teamId = contextStore.teamId ?? pickTeam(teamsStore.teams.map((team) => team.id), null);
    if (teamId) await select(teamId, contextStore.sourceId);
  }

  function handleTeamChange(teamId: number) {
    return select(teamId, null);
  }

  function handleSourceChange(sourceId: number) {
    if (contextStore.teamId) return select(contextStore.teamId, sourceId);
  }

  return { initialize, handleTeamChange, handleSourceChange };
}
