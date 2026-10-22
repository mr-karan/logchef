import { computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { generateDefaultSql } from '@/services/SqlManager';
import { logchefqlApi } from '@/api/logchefql';
import { useVariables } from "@/composables/useVariables";
import { toRfc3339 } from '@/utils/timeWindow';

// Define the valid editor modes
type EditorMode = 'logchefql' | 'native';

// Interface for tracking why a query is dirty (used in computed)
interface DirtyStateReason {
  timeRangeChanged: boolean;
  limitChanged: boolean;
  queryChanged: boolean;
  modeChanged: boolean;
}

/**
 * Refactored query management composable that delegates most logic to the explore store
 */
export function useQuery() {
  // Store access
  const exploreStore = useExploreStore();
  const sourcesStore = useSourcesStore();
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

  // Current query based on active mode
  const currentQuery = computed(() =>
      activeMode.value === 'logchefql' ? logchefQuery.value : sqlQuery.value
  );

  // Source and execution state - delegate to store with enhanced loading checks
  const canExecuteQuery = computed(() => {
    // Use the store's enhanced canExecuteQuery which includes loading state checks
    return exploreStore.canExecuteQuery;
  });

  const isExecutingQuery = computed(() => exploreStore.isExecutingQuery);

  // Check if query state is dirty using store's computed property
  const isDirty = computed(() => exploreStore.isQueryStateDirty);

  // Get dirtyReason from the store's computed property
  const dirtyReason = computed((): DirtyStateReason => {
    const dirtyState: DirtyStateReason = {
      timeRangeChanged: false,
      limitChanged: false,
      queryChanged: false,
      modeChanged: false
    };

    // Only populate if we can get the info from the store
    if (exploreStore.lastExecutedState) {
      // Time range changed
      dirtyState.timeRangeChanged = JSON.stringify(exploreStore.window) !== exploreStore.lastExecutedState.window;

      // Limit changed
      dirtyState.limitChanged = exploreStore.limit !== exploreStore.lastExecutedState.limit;

      // Mode changed
      dirtyState.modeChanged = exploreStore.lastExecutedState.mode !== exploreStore.activeMode;

      // Query content changed
      if (exploreStore.activeMode === 'logchefql') {
        dirtyState.queryChanged = exploreStore.logchefqlCode !== exploreStore.lastExecutedState.logchefqlQuery;
      } else {
        dirtyState.queryChanged = exploreStore.sqlForExecution !== exploreStore.lastExecutedState.sqlQuery;
      }
    }

    return dirtyState;
  });

  // Change query mode - uses backend as single source of truth for SQL generation
  // When isModeSwitchOnly is true, skip LogchefQL→SQL translation (e.g. AI insert,
  // loading a saved SQL query) so existing nativeQuery content is preserved.
  const changeMode = async (newMode: EditorMode, isModeSwitchOnly: boolean = false) => {
    exploreStore.clearError();

    // If switching to SQL mode and caller hasn't already set nativeQuery
    if (newMode === 'native' && activeMode.value === 'logchefql' && !isModeSwitchOnly) {
      // First, check if we have the actual generated SQL from a previous execution
      if (exploreStore.generatedDisplayQuery) {
        exploreStore.setNativeQuery(exploreStore.generatedDisplayQuery);
      } else {
        // No executed query yet - ask backend for full SQL (even if LogchefQL is empty)
        const currentTeamId = teamsStore.currentTeamId;
        const sourceId = exploreStore.sourceId;
        
        if (currentTeamId && sourceId) {
          try {
            const { start, end, zone } = exploreStore.resolved;

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
    }

    // Delegate to store action
    exploreStore.setActiveMode(newMode);
  };

  // Generate default SQL - now uses SqlManager
  const generateDefaultSQL = () => {
    try {
      const sourceDetails = sourcesStore.currentSourceDetails;
      if (!sourceDetails) {
        throw new Error('No source selected');
      }

      const params = {
        tableName: sourcesStore.getCurrentSourceTableName || '',
        tsField: sourceDetails._meta_ts_field || 'timestamp',
        window: exploreStore.resolved,
        limit: exploreStore.limit,
      };

      return { success: true, sql: generateDefaultSql(params) };
    } catch (error) {
      return {
        success: false,
        sql: '',
        error: error instanceof Error ? error.message : 'Failed to generate SQL'
      };
    }
  };

  // Return public API
  return {
    // Query content
    logchefQuery,
    sqlQuery,
    activeMode,
    currentQuery,

    // State
    isDirty,
    dirtyReason,
    isExecutingQuery,
    canExecuteQuery,

    // Actions
    changeMode,
    generateDefaultSQL,
  };
}
