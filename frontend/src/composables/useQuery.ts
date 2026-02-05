import { ref, computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { QueryService } from '@/services/QueryService';
import { SqlManager } from '@/services/SqlManager';
import { getErrorMessage } from '@/api/types';
import type { TimeRange } from '@/types/query';
import { logchefqlApi } from '@/api/logchefql';
import { useVariables } from "@/composables/useVariables";

// Define the valid editor modes
type EditorMode = 'logchefql' | 'sql';

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
  const { convertVariables } = useVariables();
  // Local state that isn't persisted in the store
  const queryError = ref<string>('');
  const sqlWarnings = ref<string[]>([]);

  // Computed query content
  const logchefQuery = computed({
    get: () => exploreStore.logchefqlCode,
    set: (value) => exploreStore.setLogchefqlCode(value)
  });

  const sqlQuery = computed({
    get: () => exploreStore.rawSql,
    set: (value) => exploreStore.setRawSql(value)
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

  const isExecutingQuery = computed(() =>
      exploreStore.isLoadingOperation('executeQuery')
  );

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
      const currentTimeRangeJSON = JSON.stringify(exploreStore.timeRange);
      const lastTimeRangeJSON = exploreStore.lastExecutedState.timeRange;
      dirtyState.timeRangeChanged = currentTimeRangeJSON !== lastTimeRangeJSON;

      // Limit changed
      dirtyState.limitChanged = exploreStore.limit !== exploreStore.lastExecutedState.limit;

      // Mode changed
      dirtyState.modeChanged = exploreStore.lastExecutedState.mode !== exploreStore.activeMode;

      // Query content changed
      if (exploreStore.activeMode === 'logchefql') {
        dirtyState.queryChanged = exploreStore.logchefqlCode !== exploreStore.lastExecutedState.logchefqlQuery;
      } else {
        dirtyState.queryChanged = exploreStore.rawSql !== exploreStore.lastExecutedState.sqlQuery;
      }
    }

    return dirtyState;
  });

  // Validate LogchefQL query via backend API
  const validateLogchefQL = async (query: string): Promise<{ valid: boolean; error?: string }> => {
    try {
      const currentTeamId = teamsStore.currentTeamId;
      const sourceId = exploreStore.sourceId;
      
      if (!currentTeamId || !sourceId) {
        return { valid: true }; // Fail open if no context
      }

      const response = await logchefqlApi.validate(currentTeamId, sourceId, query);
      if (response.data) {
        return {
          valid: response.data.valid,
          error: response.data.error?.message
        };
      }
      return { valid: true }; // Fail open on API error
    } catch (error) {
      console.warn("LogchefQL validation API error:", error);
      return { valid: true }; // Fail open
    }
  };

  // Change query mode - uses backend as single source of truth for SQL generation
  const changeMode = async (newMode: EditorMode, _isModeSwitchOnly: boolean = false) => {
    // Clear any validation errors when changing modes
    queryError.value = '';

    // If switching to SQL mode
    if (newMode === 'sql' && activeMode.value === 'logchefql') {
      // First, check if we have the actual generated SQL from a previous execution
      if (exploreStore.generatedDisplaySql) {
        exploreStore.setRawSql(exploreStore.generatedDisplaySql);
        console.log("useQuery: Using generated SQL from last execution");
      } else {
        // No executed query yet - ask backend for full SQL (even if LogchefQL is empty)
        const currentTeamId = teamsStore.currentTeamId;
        const sourceId = exploreStore.sourceId;
        
        if (currentTeamId && sourceId) {
          try {
            // Format time range for backend
            const timeRange = exploreStore.timeRange as TimeRange;
            const formatDateTime = (dt: any) => {
              if (!dt) return '';
              const year = dt.year;
              const month = String(dt.month).padStart(2, '0');
              const day = String(dt.day).padStart(2, '0');
              const hour = String(dt.hour || 0).padStart(2, '0');
              const minute = String(dt.minute || 0).padStart(2, '0');
              const second = String(dt.second || 0).padStart(2, '0');
              return `${year}-${month}-${day} ${hour}:${minute}:${second}`;
            };

            // Replace variables with placeholders for translation (if query exists)
            const query = logchefQuery.value?.trim() || '';
            const queryWithPlaceholders = query.replace(/{{(\w+)}}/g, '"placeholder"');

            console.log("useQuery: LogchefQL query from store:", logchefQuery.value);
            console.log("useQuery: Query after trim:", query);
            console.log("useQuery: Calling backend /translate for full SQL", {
              query: queryWithPlaceholders,
              start_time: formatDateTime(timeRange?.start),
              end_time: formatDateTime(timeRange?.end),
              timezone: exploreStore.getTimezoneIdentifier(),
              limit: exploreStore.limit
            });

            const response = await logchefqlApi.translate(currentTeamId, sourceId, {
              query: queryWithPlaceholders,
              start_time: formatDateTime(timeRange?.start),
              end_time: formatDateTime(timeRange?.end),
              timezone: exploreStore.getTimezoneIdentifier(),
              limit: exploreStore.limit
            });

            console.log("useQuery: Backend response", response.data);

            if (response.data && !response.data.valid) {
              queryError.value = response.data.error?.message || "Invalid LogchefQL syntax";
              return; // Don't switch modes if validation fails
            }

            if (response.data?.full_sql) {
              exploreStore.setRawSql(response.data.full_sql);
              console.log("useQuery: Set SQL from backend full_sql");
            } else {
              console.warn("useQuery: Backend did not return full_sql, response:", response.data);
            }
          } catch (error: any) {
            console.error("useQuery: Failed to get full SQL from backend:", error);
          }
        }
      }
    }

    // Delegate to store action
    exploreStore.setActiveMode(newMode);
  };

  // Handle time range update
  // NOTE: In SQL mode, we do NOT modify the user's raw SQL
  // The time picker in SQL mode is informational only - users have full control over their query
  const handleTimeRangeUpdate = () => {
    // In SQL mode, user has full control - don't modify their query
    if (exploreStore.activeMode === 'sql') {
      console.log("useQuery: SQL mode - not modifying user's raw SQL on time range change");
      return;
    }

    // In LogchefQL mode, the time range is passed to the backend separately
    // No need to modify the LogchefQL query itself
    console.log("useQuery: LogchefQL mode - time range will be applied at execution");
  };

  // Handle limit update
  // NOTE: In SQL mode, we do NOT modify the user's raw SQL
  // Users have full control over their LIMIT clause
  const handleLimitUpdate = () => {
    // In SQL mode, user has full control - don't modify their query
    if (exploreStore.activeMode === 'sql') {
      console.log("useQuery: SQL mode - not modifying user's raw SQL on limit change");
    }

    // In LogchefQL mode, the limit is passed to the backend separately
    // No need to modify the LogchefQL query itself
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
        timeRange: exploreStore.timeRange as TimeRange,
        limit: exploreStore.limit,
        timezone: exploreStore.selectedTimezoneIdentifier || undefined
      };

      return SqlManager.generateDefaultSql(params);
    } catch (error) {
      return {
        success: false,
        sql: '',
        error: error instanceof Error ? error.message : 'Failed to generate SQL'
      };
    }
  };

  // Translate LogchefQL to SQL - delegates to QueryService (synchronous fallback)
  const translateLogchefQLToSQL = (logchefqlQuery: string) => {
    try {
      const sourceDetails = sourcesStore.currentSourceDetails;
      if (!sourceDetails) {
        throw new Error('No source selected');
      }

      const params = {
        tableName: sourcesStore.getCurrentSourceTableName || '',
        tsField: sourceDetails._meta_ts_field || 'timestamp',
        timeRange: exploreStore.timeRange as TimeRange,
        limit: exploreStore.limit,
        logchefqlQuery
      };

      return QueryService.translateLogchefQLToSQL(params);
    } catch (error) {
      return {
        success: false,
        sql: '',
        error: error instanceof Error ? error.message : 'Failed to translate LogchefQL'
      };
    }
  };

  // Prepare query for execution - now uses SqlManager
  const prepareQueryForExecution = async () => {
    try {
      const sourceDetails = sourcesStore.currentSourceDetails;
      if (!sourceDetails) {
        throw new Error('No source selected');
      }

      const mode = activeMode.value;

      let query = mode === 'logchefql' ? logchefQuery.value : sqlQuery.value;

      console.log("useQuery: Preparing query - mode:", mode, "query:", query ? (query.length > 50 ? query.substring(0, 50) + '...' : query) : '(empty)');

      // Validate query before execution
      if (mode === 'logchefql' && query.trim()) {
        // For LogchefQL validation, use placeholder values
        const queryForValidation = query.replace(/{{(\w+)}}/g, '"placeholder"');
        const validation = await validateLogchefQL(queryForValidation);
        if (!validation.valid) {
          console.log("useQuery: LogchefQL validation failed:", validation.error);
          queryError.value = validation.error || 'Invalid LogchefQL syntax';
          return {
            success: false,
            sql: '',
            error: validation.error || 'Invalid LogchefQL syntax'
          };
        }
      }

      if (mode === 'sql') {
        // For SQL mode, apply variable substitution ONLY - DO NOT modify the query
        // User has full control over their raw SQL including time ranges
        const queryWithVariables = convertVariables(query);
        
        return {
          success: true,
          sql: queryWithVariables,
          warnings: [],
          error: undefined
        };
      } else {
        // For LogchefQL mode, use QueryService
        const params = {
          mode: 'logchefql' as const,
          query,
          tableName: sourcesStore.getCurrentSourceTableName || '',
          tsField: sourceDetails._meta_ts_field || 'timestamp',
          timeRange: exploreStore.timeRange as TimeRange,
          limit: exploreStore.limit,
          timezone: exploreStore.selectedTimezoneIdentifier || undefined
        };

        // Delegate to QueryService
        const result = QueryService.prepareQueryForExecution(params);

        // Track warnings and errors
        sqlWarnings.value = result.warnings || [];
        queryError.value = result.error || '';

        return result;
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      queryError.value = errorMessage;
      return {
        success: false,
        sql: '',
        error: errorMessage
      };
    }
  };

  // Execute query - now delegates to store
  const executeQuery = async () => {
    // Clear any previous errors from both local state and store
    queryError.value = '';
    exploreStore.clearError();

    try {
      // Make sure query is valid before execution
      const result = await prepareQueryForExecution();
      if (!result.success) {
        throw new Error(result.error || 'Failed to prepare query for execution');
      }

      // Execute via store action
      const execResult = await exploreStore.executeQuery();

      if (!execResult.success) {
        queryError.value = execResult.error?.message || 'Query execution failed';
      }

      return execResult;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : getErrorMessage(error);
      queryError.value = errorMessage;
      console.error('Query execution error:', errorMessage);
      return {
        success: false,
        error: { message: errorMessage },
        data: null
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
    queryError,
    sqlWarnings,
    isDirty,
    dirtyReason,
    isExecutingQuery,
    canExecuteQuery,

    // Actions
    changeMode,
    validateLogchefQL,
    handleTimeRangeUpdate,
    handleLimitUpdate,
    generateDefaultSQL,
    translateLogchefQLToSQL,
    prepareQueryForExecution,
    executeQuery
  };
}
