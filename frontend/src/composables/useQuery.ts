import { ref, computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { QueryService } from '@/services/QueryService';
import { getErrorMessage } from '@/api/types';
import type { TimeRange, QueryResult } from '@/types/query';
import { validateLogchefQLWithDetails } from '@/utils/logchefql/api';
import { validateSQLWithDetails } from '@/utils/clickhouse-sql';

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
  const isFromUrl = ref(true); // Track if current content came from URL

  // Computed query content
  const logchefQuery = computed({
    get: () => exploreStore.logchefqlCode || '',
    set: (value) => {
      exploreStore.setLogchefqlCode(value);
      // User changed content if they set it programmatically
      if (value !== exploreStore.logchefqlCode) {
        isFromUrl.value = false;
      }
    }
  });

  const sqlQuery = computed({
    get: () => exploreStore.rawSql,
    set: (value) => {
      exploreStore.setRawSql(value);
      // User changed content if they set it programmatically
      if (value !== exploreStore.rawSql) {
        isFromUrl.value = false;
      }
    }
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
    if (!lastState) {
      // If no previous state, only consider dirty if there's actually query content
      // AND it was not loaded from URL
      return ((logchefQuery.value && logchefQuery.value.trim() !== '') ||
             (sqlQuery.value && sqlQuery.value.trim() !== '')) &&
             !isFromUrl.value;
    }

    // Determine if time range changed
    const timeRangeChanged = JSON.stringify(exploreStore.timeRange) !== lastState.timeRange;

    // Determine if limit changed
    const limitChanged = exploreStore.limit !== lastState.limit;

    // Check if the mode has changed
    const modeChanged = lastState.mode && lastState.mode !== activeMode.value;

    // If mode has changed but neither query has content, not dirty
    if (modeChanged &&
        (!logchefQuery.value || logchefQuery.value.trim() === '') &&
        (!sqlQuery.value || sqlQuery.value.trim() === '')) {
      return false;
    }

    // Compare with appropriate last query depending on current mode
    let queryChanged = false;

    if (activeMode.value === 'logchefql') {
      // Compare current logchefQL with last executed logchefQL
      const currentContent = logchefQuery.value?.trim() || '';
      const lastContent = lastState.logchefqlQuery?.trim() || '';

      queryChanged = currentContent !== lastContent &&
                    (currentContent !== '' || lastContent !== '');
    } else {
      // Compare current SQL with last executed SQL
      const currentContent = sqlQuery.value?.trim() || '';
      const lastContent = lastState.sqlQuery?.trim() || '';

      queryChanged = currentContent !== lastContent &&
                    (currentContent !== '' || lastContent !== '');
    }

    // Consider dirty if any parameter changed
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
        // Validate LogchefQL before translating
        const validation = validateLogchefQLWithDetails(logchefQuery.value);
        if (!validation.valid) {
          // Show error when switching modes with invalid LogchefQL
          queryError.value = `Invalid LogchefQL syntax: ${validation.error}`;

          // Don't switch modes if LogchefQL is invalid
          return;
        }

        // If there's LogchefQL content, always translate it
        const result = translateLogchefQLToSQL(logchefQuery.value);

        if (result.success) {
          // Always set SQL when logchefQL exists and translation succeeds
          sqlQuery.value = result.sql;
          isFromUrl.value = false; // Mark as user-generated content
        } else {
          // If translation fails, fall back to original SQL or default
          if (!originalSql) {
            generateAndSetDefaultSQL();
            isFromUrl.value = false; // Mark as generated content
          }
        }
      } else if (!originalSql) {
        // No LogchefQL content AND no original SQL, generate default
        generateAndSetDefaultSQL();
        isFromUrl.value = false; // Mark as generated content
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

  // New method to get detailed SQL validation results
  const validateSQLWithErrorDetails = (sql: string) => {
    return validateSQLWithDetails(sql);
  };

  // Handle time/limit changes
  const handleTimeRangeUpdate = () => {
    // In SQL mode, we might want to update the time range in the query
    // but this is now handled in LogExplorer.vue with more comprehensive patterns
    // This function now just acts as a notification handler for dirty state
  };

  const handleLimitUpdate = () => {
    // In SQL mode, we should attempt to update the LIMIT clause in the query
    if (activeMode.value === 'sql') {
      const currentSql = sqlQuery.value?.trim() || '';
      if (!currentSql) return;

      try {
        // Use the parser to analyze the current query
        const analysis = validateSQLWithDetails(currentSql);

        if (analysis.valid && analysis.ast) {
          // Get the current limit from the store
          const newLimit = exploreStore.limit;

          // Get query analysis to check for existing LIMIT
          const queryAnalysis = QueryService.analyzeQuery(currentSql);

          if (queryAnalysis) {
            let updatedSql = currentSql;

            if (queryAnalysis.hasLimit) {
              // Replace existing LIMIT clause
              updatedSql = updatedSql.replace(/LIMIT\s+\d+/i, `LIMIT ${newLimit}`);
            } else {
              // Add LIMIT clause at the end if not present
              updatedSql = `${updatedSql}\nLIMIT ${newLimit}`;
            }

            // Only update if changed
            if (updatedSql !== currentSql) {
              sqlQuery.value = updatedSql;
            }
          }
        }
      } catch (error) {
        console.error("Error updating LIMIT in SQL query:", error);
        // Don't modify the query if we can't safely parse it
      }
    }
  };

  // Prepare the query for execution
  const prepareQueryForExecution = (): QueryResult => {
    try {
      const params = getCommonQueryParams();
      const mode = activeMode.value;
      const query = mode === 'logchefql' ? logchefQuery.value : sqlQuery.value;

      // Validate LogchefQL query before execution
      if (mode === 'logchefql' && query.trim()) {
        const validation = validateLogchefQLWithDetails(query);
        if (!validation.valid) {
          queryError.value = validation.error || 'Invalid LogchefQL syntax';
          return {
            success: false,
            sql: '',
            error: validation.error || 'Invalid LogchefQL syntax'
          };
        }
      }
      // Validate SQL query before execution
      else if (mode === 'sql' && query.trim()) {
        const validation = validateSQLWithDetails(query);
        if (!validation.valid) {
          queryError.value = validation.error || 'Invalid SQL syntax';
          return {
            success: false,
            sql: query,
            error: validation.error || 'Invalid SQL syntax'
          };
        }
      }

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
        query: currentQuery.value,
        mode: activeMode.value,
        logchefqlQuery: logchefQuery.value,
        sqlQuery: sqlQuery.value
      });

      // After execution, content is no longer considered from URL
      isFromUrl.value = false;

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
    validateSQLWithErrorDetails,
    handleTimeRangeUpdate,
    handleLimitUpdate,
    generateDefaultSQL,
    translateLogchefQLToSQL,
    prepareQueryForExecution,
    executeQuery
  };
}