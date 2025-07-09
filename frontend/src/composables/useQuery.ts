import { ref, computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { QueryService } from '@/services/QueryService';
import { SqlManager } from '@/services/SqlManager';
import { getErrorMessage } from '@/api/types';
import type { TimeRange, QueryResult } from '@/types/query';
import { validateLogchefQLWithDetails } from '@/utils/logchefql/api';
import { validateSQLWithDetails, analyzeQuery } from '@/utils/clickhouse-sql';
import { useExploreUrlSync } from '@/composables/useExploreUrlSync';
import { useVariables } from "@/composables/useVariables";

// Define the valid editor modes
type EditorMode = 'logchefql' | 'sql';

// Add an interface for tracking why a query is dirty
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
  const { syncUrlFromState } = useExploreUrlSync();
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

  // Source and execution state - delegate to store
  const canExecuteQuery = computed(() =>
      exploreStore.sourceId > 0 &&
      teamsStore.currentTeamId !== null &&
      teamsStore.currentTeamId > 0 &&
      sourcesStore.hasValidCurrentSource
  );

  const isExecutingQuery = computed(() =>
      exploreStore.isLoadingOperation('executeQuery')
  );

  // Check if query state is dirty using store's computed property
  const isDirty = computed(() => exploreStore.isQueryStateDirty);

  // Get dirtyReason from the store's computed property
  const dirtyReason = computed(() => {
    const dirtyState = {
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

  // Validate SQL query syntax
  const validateSQL = (sql: string): boolean => {
    return SqlManager.validateSql(sql).valid;
  };

  // Get detailed SQL validation results
  const validateSQLWithErrorDetails = (sql: string) => {
    return SqlManager.validateSql(sql);
  };

  // Change query mode - now delegates to store
  const changeMode = (newMode: EditorMode, isModeSwitchOnly: boolean = false) => {
    // Clear any validation errors when changing modes
    queryError.value = '';

    // If switching to SQL and we have LogchefQL content, translate it to SQL
    if (newMode === 'sql' && activeMode.value === 'logchefql' && logchefQuery.value?.trim()) {

      // Replace variables with placeholder values for validation
      let sql = logchefQuery.value.replace(/{{(\w+)}}/g, '"placeholder"');

      const validation = validateLogchefQLWithDetails(sql);
      if (!validation.valid) {
        queryError.value = `Invalid LogchefQL syntax: ${validation.error}`;
        return; // Don't switch modes if validation fails
      }

      // If validation passed, translate LogchefQL to SQL
      const translationResult = translateLogchefQLToSQL(logchefQuery.value);
      if (translationResult.success && translationResult.sql) {
        // Update the SQL query in the store with the translated SQL
        exploreStore.setRawSql(translationResult.sql);
        console.log("useQuery: Translated LogchefQL to SQL during mode switch");
      } else {
        console.warn("useQuery: Failed to translate LogchefQL to SQL:", translationResult.error);
      }
    }

    // Delegate to store action
    exploreStore.setActiveMode(newMode);

    // After mode switch, sync URL state
    syncUrlFromState();
  };

  // Handle time range update - now uses SqlManager
  const handleTimeRangeUpdate = () => {
    const exploreStore = useExploreStore();
    const sourcesStore = useSourcesStore();

    // Only proceed if in SQL mode
    if (exploreStore.activeMode !== 'sql') {
      return;
    }

    const currentSql = exploreStore.rawSql;

    if (!currentSql.trim()) {
      return;
    }

    try {
      // Get source details for metadata
      const sourceDetails = sourcesStore.currentSourceDetails;
      if (!sourceDetails) {
        console.error("Cannot update SQL time range: Source details not available");
        return;
      }

      // Get the timestamp field from source metadata
      const tsField = sourceDetails._meta_ts_field || 'timestamp';

      // Get the current time range from the store
      const timeRange = exploreStore.timeRange;
      if (!timeRange) {
        console.error("Cannot update SQL time range: No time range available");
        return;
      }

      // Check if SQL has complex time conditions we shouldn't modify
      const analysis = analyzeQuery(currentSql);
      if (analysis?.timeRangeInfo &&
          (analysis.timeRangeInfo.format === 'now-interval' ||
              analysis.timeRangeInfo.format === 'other')) {
        console.log("useQuery: SQL contains complex time format, preserving user query");
        return;
      }

      // Use SqlManager to update time range
      const updatedSql = SqlManager.updateTimeRange({
        sql: currentSql,
        tsField,
        timeRange,
        timezone: exploreStore.selectedTimezoneIdentifier || undefined
      });

      // If SQL was changed, update the store
      if (updatedSql !== currentSql) {
        exploreStore.setRawSql(updatedSql);
        console.log("useQuery: Successfully updated SQL query with new time range");
      } else {
        console.log("useQuery: SQL query not changed - time condition preserved");
      }

      // Sync URL state with the updated query
      syncUrlFromState();
    } catch (error) {
      console.error("Error updating SQL with new time range:", error);
    }
  };

  // Handle limit update - now uses SqlManager
  const handleLimitUpdate = () => {
    // Update SQL query if in SQL mode
    if (exploreStore.activeMode === 'sql') {
      const currentSql = exploreStore.rawSql;
      const newLimit = exploreStore.limit;

      if (currentSql.trim()) {
        try {
          // Use SqlManager to update limit
          const updatedSql = SqlManager.updateLimit(currentSql, newLimit);

          // Update SQL only if changed
          if (updatedSql !== currentSql) {
            exploreStore.setRawSql(updatedSql);
            console.log("useQuery: Updated SQL query with new limit:", newLimit);
          }
        } catch (error) {
          console.error("Error updating SQL with new limit:", error);
        }
      }
    }

    // Sync URL state after update
    syncUrlFromState();
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

  // Translate LogchefQL to SQL - delegates to QueryService
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
  const prepareQueryForExecution = () => {
    try {
      const sourceDetails = sourcesStore.currentSourceDetails;
      if (!sourceDetails) {
        throw new Error('No source selected');
      }

      const mode = activeMode.value;

      let query = mode === 'logchefql' ? logchefQuery.value : sqlQuery.value;

      console.log("useQuery: Preparing query - mode:", mode, "query:", query ? (query.length > 50 ? query.substring(0, 50) + '...' : query) : '(empty)');

      // Validate query before execution (without variable substitution for LogchefQL)
      if (mode === 'logchefql' && query.trim()) {
        // For LogchefQL validation, use placeholder values
        const queryForValidation = query.replace(/{{(\w+)}}/g, '"placeholder"');
        const validation = validateLogchefQLWithDetails(queryForValidation);
        if (!validation.valid) {
          console.log("useQuery: LogchefQL validation failed:", validation.error);
          queryError.value = validation.error || 'Invalid LogchefQL syntax';
          return {
            success: false,
            sql: '',
            error: validation.error || 'Invalid LogchefQL syntax'
          };
        }
      } else if (mode === 'sql' && query.trim()) {
        // For SQL, apply variable substitution then validate
        const queryWithVars = convertVariables(query);
        const validation = SqlManager.validateSql(queryWithVars);
        if (!validation.valid) {
          console.log("useQuery: SQL validation failed:", validation.error);
          queryError.value = validation.error || 'Invalid SQL syntax';
          return {
            success: false,
            sql: queryWithVars,
            error: validation.error || 'Invalid SQL syntax'
          };
        }
      }

      if (mode === 'sql') {
        // For SQL mode, apply variable substitution then use SqlManager to prepare for execution
        const queryWithVariables = convertVariables(query);
        const result = SqlManager.prepareForExecution({
          sql: queryWithVariables,
          tsField: sourceDetails._meta_ts_field || 'timestamp',
          timeRange: exploreStore.timeRange as TimeRange,
          limit: exploreStore.limit,
          timezone: exploreStore.selectedTimezoneIdentifier || undefined
        });

        queryError.value = result.error || '';
        return result;
      } else {
        // For LogchefQL mode, continue to use QueryService
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
      const result = prepareQueryForExecution();
      if (!result.success) {
        throw new Error(result.error || 'Failed to prepare query for execution');
      }

      // Execute via store action
      const execResult = await exploreStore.executeQuery();

      // Update URL state after successful execution
      if (execResult.success) {
        syncUrlFromState();
      } else {
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
    validateSQL,
    validateSQLWithErrorDetails,
    handleTimeRangeUpdate,
    handleLimitUpdate,
    generateDefaultSQL,
    translateLogchefQLToSQL,
    prepareQueryForExecution,
    executeQuery
  };
}