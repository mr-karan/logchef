import { computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useTeamsStore } from '@/stores/teams';
import { logchefqlApi } from '@/api/logchefql';
import { useVariables } from "@/composables/useVariables";
import { toRfc3339 } from '@/utils/timeWindow';

// Define the valid editor modes
type EditorMode = 'logchefql' | 'native';

/**
 * Refactored query management composable that delegates most logic to the explore store
 */
export function useQuery() {
  // Store access
  const exploreStore = useExploreStore();
  const teamsStore = useTeamsStore();
  const { getVariablesForApi } = useVariables();

  // Computed query content
  const logchefQuery = computed({
    get: () => exploreStore.logchefqlCode,
    set: (value) => exploreStore.setLogchefqlCode(value)
  });

  const sqlQuery = computed({
    get: () => exploreStore.nativeQuery,
    set: (value) => exploreStore.setNativeQuery(value)
  });

  // Active mode computed property
  const activeMode = computed({
    get: () => exploreStore.activeMode as EditorMode,
    set: (value: EditorMode) => exploreStore.setActiveMode(value)
  });

  // Change query mode - uses backend as single source of truth for SQL generation
  // When isModeSwitchOnly is true, skip LogchefQL→SQL translation (e.g. AI insert,
  // loading a saved SQL query) so existing nativeQuery content is preserved.
  const changeMode = async (newMode: EditorMode, isModeSwitchOnly: boolean = false) => {
    exploreStore.clearError();

    // If switching to SQL mode and caller hasn't already set nativeQuery, ask
    // the backend for the editor's LogchefQL as SQL (even when it is empty).
    if (newMode === 'native' && activeMode.value === 'logchefql' && !isModeSwitchOnly) {
      const currentTeamId = teamsStore.currentTeamId;
      const sourceId = exploreStore.sourceId;

      if (currentTeamId && sourceId) {
        try {
          const { start, end, zone } = exploreStore.resolveNow();

          // Let the backend substitute typed variables exactly once.
          const query = logchefQuery.value?.trim() || '';
          const variables = getVariablesForApi();

          const response = await logchefqlApi.translate(currentTeamId, sourceId, {
            query,
            start_time: toRfc3339(start),
            end_time: toRfc3339(end),
            timezone: zone,
            limit: exploreStore.limit,
            variables: variables.length > 0 ? variables : undefined,
          });

          if (response.data && !response.data.valid) {
            exploreStore.setQueryError(response.data.error?.message || "Invalid LogchefQL syntax");
            return; // Don't switch modes if validation fails
          }

          const generatedQuery = response.data?.generated_query || response.data?.full_sql;
          if (generatedQuery) {
            exploreStore.setNativeQuery(generatedQuery);
          } else {
            exploreStore.setQueryError("Could not generate SQL from your query. Please refine and try again.");
            return;
          }
        } catch (error: any) {
          console.error("useQuery: Failed to get full SQL from backend:", error);
          exploreStore.setQueryError(error?.message || "Failed to translate query to SQL. Please try again.");
          return;
        }
      }
    }

    // Delegate to store action
    exploreStore.setActiveMode(newMode);
  };

  // Return public API
  return {
    logchefQuery,
    sqlQuery,
    activeMode,
    changeMode,
  };
}
