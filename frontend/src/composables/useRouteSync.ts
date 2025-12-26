import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { useExploreStore } from '@/stores/explore';
import { useContextStore } from '@/stores/context';

export function useRouteSync() {
  const route = useRoute();
  const router = useRouter();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();
  const exploreStore = useExploreStore();
  const contextStore = useContextStore();

  const isHydrating = ref(false);
  const hydrationError = ref<string | null>(null);

  async function ensureTeamsLoaded() {
    if (!teamsStore.teams.length) {
      await teamsStore.loadUserTeams();
    }
    if (!contextStore.teamId && teamsStore.teams.length) {
      contextStore.selectTeam(teamsStore.teams[0].id);
    }
  }

  function parseTeamFromUrl(): number | null {
    const t = route.query.team as string | undefined;
    if (!t) return null;
    const id = parseInt(t, 10);
    return isNaN(id) ? null : id;
  }

  function parseSourceFromUrl(): number | null {
    const s = route.query.source as string | undefined;
    if (!s) return null;
    const id = parseInt(s, 10);
    return isNaN(id) ? null : id;
  }

  async function hydrateFromUrl() {
    isHydrating.value = true;
    hydrationError.value = null;
    try {
      // Teams
      await ensureTeamsLoaded();
      let teamId = parseTeamFromUrl();
      if (teamId && !teamsStore.userBelongsToTeam(teamId)) {
        teamId = null;
      }
      if (!teamId && teamsStore.teams.length) {
        teamId = teamsStore.teams[0].id;
      }
      if (teamId && contextStore.teamId !== teamId) {
        contextStore.selectTeam(teamId);
      }

      if (contextStore.teamId) {
        await sourcesStore.loadTeamSources(contextStore.teamId);
      }

      // Source from URL or first
      const urlSource = parseSourceFromUrl();
      let sourceId: number | null = null;
      if (urlSource && sourcesStore.teamSources.some(s => s.id === urlSource)) {
        sourceId = urlSource;
      } else if (sourcesStore.teamSources.length) {
        sourceId = sourcesStore.teamSources[0].id;
      }

      if (sourceId) {
        if (contextStore.sourceId !== sourceId) {
          contextStore.selectSource(sourceId);
        }
        await sourcesStore.loadSourceDetails(sourceId);
      } else {
        contextStore.selectSource(0);
        sourcesStore.clearCurrentSourceDetails();
      }

      // Initialize remaining explore state from URL
      const params: Record<string, string | undefined> = {};
      const q = route.query;
      if (q.source) params.source = String(q.source);
      if (q.t) params.t = String(q.t);
      if (q.time) params.time = String(q.time);
      if (q.start) params.start = String(q.start);
      if (q.end) params.end = String(q.end);
      if (q.limit) params.limit = String(q.limit);
      if (q.mode) params.mode = String(q.mode);
      if (q.q) params.q = String(q.q);
      if (q.sql) params.sql = String(q.sql);
      if (q.id) params.id = String(q.id);
      if (q.query_id) params.query_id = String(q.query_id);

      exploreStore.initializeFromUrl(params);
    } catch (e: any) {
      hydrationError.value = e?.message || 'Failed to hydrate from URL';
    } finally {
      isHydrating.value = false;
    }
  }

  async function changeTeam(teamId: number) {
    if (contextStore.teamId !== teamId) {
      contextStore.selectTeam(teamId);
    }
    await sourcesStore.loadTeamSources(teamId);
    const first = sourcesStore.teamSources[0]?.id;
    if (first) {
      await changeSource(first);
    }
    await router.replace({ query: { ...route.query, team: String(teamId), source: first ? String(first) : undefined } });
  }

  async function changeSource(sourceId: number) {
    if (contextStore.sourceId !== sourceId) {
      contextStore.selectSource(sourceId);
    }
    await sourcesStore.loadSourceDetails(sourceId);
    await router.replace({ query: { ...route.query, source: String(sourceId) } });
  }

  return {
    isHydrating,
    hydrationError,
    hydrateFromUrl,
    changeTeam,
    changeSource,
  };
}
