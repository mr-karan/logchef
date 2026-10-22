import { useRoute, useRouter } from 'vue-router';
import { pickSource, pickTeam, useContextStore } from '@/stores/context';
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

  async function select(teamId: number, requestedSourceId: number | null) {
    if (teamId !== contextStore.teamId) contextStore.selectTeam(teamId);
    const sources = await sourcesStore.loadTeamSources(teamId).catch((error) => {
      console.error('error loading team sources', error);
      return [];
    });
    if (contextStore.teamId !== teamId) return;
    const sourceId = pickSource(teamId, sources, requestedSourceId);
    if (sourceId && sourceId !== contextStore.sourceId) contextStore.selectSource(sourceId);
    await syncUrl();
  }

  /** Keeps the team the router guard chose, picks a source and writes both to the URL. */
  async function initialize() {
    if (teamsStore.teams.length === 0) await teamsStore.loadTeams(false, false);
    const teamId = contextStore.teamId ?? pickTeam(teamsStore.teams.map((team) => team.id), null);
    if (teamId) await select(teamId, contextStore.sourceId);
  }

  async function handleTeamChange(teamId: number) {
    if (teamId !== contextStore.teamId) await select(teamId, null);
  }

  async function handleSourceChange(sourceId: number) {
    if (contextStore.teamId && sourceId !== contextStore.sourceId) await select(contextStore.teamId, sourceId);
  }

  return { initialize, handleTeamChange, handleSourceChange };
}
