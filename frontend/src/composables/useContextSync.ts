import { useRoute, useRouter } from 'vue-router';
import { watch } from 'vue';
import { pickTeam, useContextStore } from '@/stores/context';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';

/** Team and source selection for pages other than Explorer, kept in the page's URL. */
export function useContextSync(options: { basePath?: string } = {}) {
  const route = useRoute();
  const router = useRouter();
  const contextStore = useContextStore();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();
  // LogsLayout caches views, so a cached page must never rewrite another route.
  const syncPath = options.basePath ?? route.path;

  async function syncUrl() {
    if (route.path !== syncPath) return;
    const query: Record<string, string> = {};
    if (contextStore.teamId) query.team = String(contextStore.teamId);
    if (contextStore.sourceId) query.source = String(contextStore.sourceId);
    if (route.query.team !== query.team || route.query.source !== query.source) {
      await router.replace({ path: syncPath, query });
    }
  }

  watch(() => [contextStore.teamId, contextStore.sourceId], () => { void syncUrl(); });

  /** Keeps the router's context and records it after its source list loads. */
  async function initialize() {
    if (teamsStore.teams.length === 0) await teamsStore.loadTeams(false, false);
    const teamId = contextStore.teamId ?? pickTeam(teamsStore.teams.map((team) => team.id), null);
    if (!teamId) return;
    contextStore.selectTeam(teamId);
    try {
      await sourcesStore.loadTeamSources(teamId);
      await syncUrl();
    } catch (error) {
      console.error('error loading team sources', error);
    }
  }

  function handleTeamChange(teamId: number) {
    contextStore.selectTeam(teamId);
  }

  function handleSourceChange(sourceId: number) {
    contextStore.selectSource(sourceId);
  }

  return { initialize, handleTeamChange, handleSourceChange };
}
