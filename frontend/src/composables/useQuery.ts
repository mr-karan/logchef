import { ref, computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { QueryService } from '@/services/QueryService';
import { getErrorMessage } from '@/api/types';
import type { TimeRange, QueryResult } from '@/types/query';

// Define the valid editor modes
type EditorMode = 'logchefql' | 'sql';

/**
 * Comprehensive query management composable
 * Combines query building, mode switching, and execution
 */
export function useQuery() {
  // Store access
  const exploreStore = useExploreStore();
  const sourcesStore = useSourcesStore();
  const teamsStore = useTeamsStore();

  // Local state
  const queryError = ref<string>('');
  const sqlWarnings = ref<string[]>([]);
  const hasRunQuery = ref(false);

  // Computed query content
  const logchefQuery = computed({
    get: () => exploreStore.logchefqlCode || '',
    set: (value) => exploreStore.setLogchefqlCode(value)
  });

  const sqlQuery = computed({
    get: () => exploreStore.rawSql,
    set: (value) => exploreStore.setRawSql(value)
  });

  // Store active mode with type safety
  const activeMode = computed({
    get: () => exploreStore.activeMode as EditorMode,
    set: (value: EditorMode) => exploreStore.setActiveMode(value)
  });

  // Current query based on active mode
  const currentQuery = computed(() =>
    activeMode.value === 'logchefql' ? logchefQuery.value : sqlQuery.value
  );

  // Source and execution state
  const canExecuteQuery = computed(() =>
    exploreStore.sourceId > 0 &&
    teamsStore.currentTeamId !== null &&
    teamsStore.currentTeamId > 0 &&
    sourcesStore.hasValidCurrentSource
  );

  const isExecutingQuery = computed(() => exploreStore.isLoadingOperation('executeQuery'));

  // Check if query state is dirty (needs execution)
  const isDirty = computed(() => {
    const lastState = exploreStore.lastExecutedState;
    if (!lastState) return true;

    const timeRangeChanged = JSON.stringify(exploreStore.timeRange) !== lastState.timeRange;
    const limitChanged = exploreStore.limit !== lastState.limit;
    const queryChanged = currentQuery.value !== (lastState.query || '');

    return timeRangeChanged || limitChanged || queryChanged;
  });

  // Helper for getting common query parameters
  const getCommonQueryParams = () => {
    const sourceDetails = sourcesStore.currentSourceDetails;
    if (!sourceDetails) {
      throw new Error('No source selected');
    }

    return {
      tableName: sourcesStore.getCurrentSourceTableName || '',
      tsField: sourceDetails._meta_ts_field || 'timestamp',
      timeRange: exploreStore.timeRange as TimeRange,
      limit: exploreStore.limit
    };
  };

  // Generate default SQL query
  const generateDefaultSQL = (): QueryResult => {
    try {
      const params = getCommonQueryParams();
      return QueryService.generateDefaultSQL(params);
    } catch (error) {
      return {
        success: false,
        sql: '',
        error: error instanceof Error ? error.message : 'Failed to generate SQL'
      };
    }
  };

  // Translate LogchefQL to SQL
  const translateLogchefQLToSQL = (logchefqlQuery: string): QueryResult => {
    try {
      const params = getCommonQueryParams();
      return QueryService.translateLogchefQLToSQL({
        ...params,
        logchefqlQuery
      });
    } catch (error) {
      return {
        success: false,
        sql: '',
        error: error instanceof Error ? error.message : 'Failed to translate LogchefQL'
      };
    }
  };

  // Change query mode with automatic translation
  const changeMode = (newMode: EditorMode) => {
    const currentMode = activeMode.value;
    if (newMode === currentMode) return;

    // When switching TO SQL from LogchefQL
    if (newMode === 'sql' && currentMode === 'logchefql') {
      // Store original SQL query before potentially overwriting it
      const originalSql = sqlQuery.value;

      // Check if we have LogchefQL content that can be translated
      if (logchefQuery.value.trim()) {
        // If there's LogchefQL content, always translate it
        const result = translateLogchefQLToSQL(logchefQuery.value);

        if (result.success) {
          // Always set SQL when logchefQL exists and translation succeeds
          sqlQuery.value = result.sql;
        } else {
          // If translation fails, fall back to original SQL or default
          if (!originalSql) {
            generateAndSetDefaultSQL();
          }
        }
      } else if (!originalSql) {
        // No LogchefQL content AND no original SQL, generate default
        generateAndSetDefaultSQL();
      }
      // If LogchefQL is empty but we have originalSql, keep the existing SQL
    }

    // Update mode in store
    activeMode.value = newMode;
  };

  // Helper function to generate and set default SQL
  const generateAndSetDefaultSQL = () => {
    const defaultResult = generateDefaultSQL();
    if (defaultResult.success) {
      sqlQuery.value = defaultResult.sql;
    }
  };

  // Validate SQL query syntax
  const validateSQL = (sql: string): boolean => {
    return QueryService.validateSQL(sql);
  };

  // Handle time/limit changes
  const handleTimeRangeUpdate = () => {
    // Silent in production
  };

  const handleLimitUpdate = () => {
    // Silent in production
  };

  // Prepare the query for execution
  const prepareQueryForExecution = (): QueryResult => {
    try {
      const params = getCommonQueryParams();
      const mode = activeMode.value;
      const query = mode === 'logchefql' ? logchefQuery.value : sqlQuery.value;

      // Translate the mode to what QueryService expects
      const queryServiceMode = mode === 'logchefql' ? 'logchefql' : 'clickhouse-sql';

      const result = QueryService.prepareQueryForExecution({
        mode: queryServiceMode,
        query,
        ...params
      });

      // Track warnings and errors
      sqlWarnings.value = result.warnings || [];
      queryError.value = result.error || '';

      return result;
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

  // Execute the query
  const executeQuery = async () => {
    queryError.value = '';
    if (exploreStore.clearError) {
      exploreStore.clearError();
    }

    if (!canExecuteQuery.value) {
      throw new Error('Cannot execute query: missing team, source, or source details');
    }

    try {
      const result = prepareQueryForExecution();
      if (!result.success) {
        throw new Error(result.error || `Failed to prepare query for execution`);
      }

      // Store current state before execution
      exploreStore.setLastExecutedState({
        timeRange: JSON.stringify(exploreStore.timeRange),
        limit: exploreStore.limit,
        query: currentQuery.value
      });

      // Execute the query via store
      const execResult = await exploreStore.executeQuery(result.sql);
      if (!execResult.success) {
        queryError.value = execResult.error?.message || 'Query execution failed';
      }

      // Mark that we've run a query
      hasRunQuery.value = true;

      return execResult;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : getErrorMessage(error);
      queryError.value = errorMessage;
      console.error('Query execution error:', errorMessage);
      return { success: false, error: { message: errorMessage } };
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
    hasRunQuery,
    isDirty,
    isExecutingQuery,
    canExecuteQuery,

    // Actions
    changeMode,
    validateSQL,
    handleTimeRangeUpdate,
    handleLimitUpdate,
    generateDefaultSQL,
    translateLogchefQLToSQL,
    prepareQueryForExecution,
    executeQuery
  };
}