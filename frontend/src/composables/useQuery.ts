import { ref, computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { QueryService } from '@/services/QueryService';
import { getErrorMessage } from '@/api/types';
import type { TimeRange, QueryResult } from '@/types/query';
import { validateLogchefQLWithDetails } from '@/utils/logchefql/api';
import { validateSQLWithDetails } from '@/utils/clickhouse-sql';
import { createTimeRangeCondition } from '@/utils/time-utils';
import { useExploreUrlSync } from './useExploreUrlSync';

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
 * Comprehensive query management composable
 * Combines query building, mode switching, and execution
 */
export function useQuery() {
  // Store access
  const exploreStore = useExploreStore();
  const sourcesStore = useSourcesStore();
  const teamsStore = useTeamsStore();
  const { pushQueryHistoryEntry } = useExploreUrlSync();

  // Local state
  const queryError = ref<string>('');
  const sqlWarnings = ref<string[]>([]);
  const hasRunQuery = ref(false);
  const isFromUrl = ref(true); // Track if current content came from URL
  const initialQueryExecution = ref(true); // Flag to track initial execution from URL
  const dirtyReason = ref<DirtyStateReason>({
    timeRangeChanged: false,
    limitChanged: false,
    queryChanged: false,
    modeChanged: false
  });

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
    // Reset the dirty reasons
    dirtyReason.value = {
      timeRangeChanged: false,
      limitChanged: false,
      queryChanged: false,
      modeChanged: false
    };

    // If we're still in the initial query execution phase and the content came from URL,
    // it shouldn't be marked as dirty
    if (initialQueryExecution.value && isFromUrl.value) {
      return false;
    }

    const lastState = exploreStore.lastExecutedState;
    if (!lastState) {
      // If no previous state, only consider dirty if there's actually query content
      // AND it was not loaded from URL
      const hasQuery = ((logchefQuery.value && logchefQuery.value.trim() !== '') ||
             (sqlQuery.value && sqlQuery.value.trim() !== '')) &&
             !isFromUrl.value;
              
      if (hasQuery) {
        dirtyReason.value.queryChanged = true;
      }
      
      return hasQuery;
    }

    // Determine if time range changed
    // Force trim any long strings for more reliable comparison
    const currentTimeRangeJSON = JSON.stringify(exploreStore.timeRange);
    const lastTimeRangeJSON = lastState.timeRange;
    
    const timeRangeChanged = currentTimeRangeJSON !== lastTimeRangeJSON;
    dirtyReason.value.timeRangeChanged = timeRangeChanged;
    
    // Debug timeRange comparison
    console.log("useQuery: isDirty calculation - timeRange comparison:",
               "current:", currentTimeRangeJSON.substring(0, 50) + "...",
               "lastState:", lastTimeRangeJSON.substring(0, 50) + "...",
               "areEqual:", currentTimeRangeJSON === lastTimeRangeJSON,
               "Definitely dirty?", timeRangeChanged);

    // Determine if limit changed
    const limitChanged = exploreStore.limit !== lastState.limit;
    dirtyReason.value.limitChanged = limitChanged;

    // Check if the mode has changed
    // Provide a default value (false) if lastState.mode is undefined
    const modeChanged = lastState?.mode ? lastState.mode !== activeMode.value : false;
    dirtyReason.value.modeChanged = modeChanged;

    // If mode has changed, handle special cases
    if (modeChanged) {
      // If switching with empty queries, not dirty
      if ((!logchefQuery.value || logchefQuery.value.trim() === '') &&
          (!sqlQuery.value || sqlQuery.value.trim() === '')) {
        dirtyReason.value.modeChanged = false;
        return false;
      }

      // Special case: empty LogchefQL to default SQL is not dirty
      if (lastState.mode === 'logchefql' &&
          activeMode.value === 'sql' &&
          (!lastState.logchefqlQuery || lastState.logchefqlQuery.trim() === '')) {
        dirtyReason.value.modeChanged = false;
        return false;
      }

      // Special case: when switching from LogChefQL to SQL with auto-translation
      // If we're in SQL mode with content marked as from URL (not manually edited),
      // and we have valid LogChefQL content that matches the last execution state,
      // then this is just an auto-translation and should not be considered dirty
      if (lastState.mode === 'logchefql' &&
          activeMode.value === 'sql' &&
          isFromUrl.value &&
          logchefQuery.value?.trim() === lastState.logchefqlQuery?.trim()) {
        dirtyReason.value.modeChanged = false;
        return false;
      }

      // Additional special case: URL-loaded LogChefQL to auto-translated SQL
      // If we're in SQL mode with LogChefQL content, check if the SQL matches what would be generated
      if (activeMode.value === 'sql' && logchefQuery.value?.trim()) {
        try {
          // Generate SQL from the current LogChefQL query
          const result = translateLogchefQLToSQL(logchefQuery.value);
          if (result.success && sqlQuery.value?.trim() === result.sql.trim()) {
            // The SQL matches what would be auto-generated, so not dirty
            dirtyReason.value.modeChanged = false;
            return false;
          }
        } catch (err) {
          console.error("Error checking SQL translation:", err);
          // On error, fall back to standard checks
        }
      }
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
    
    dirtyReason.value.queryChanged = queryChanged;

    // Consider dirty if any parameter changed
    return timeRangeChanged || limitChanged || queryChanged || dirtyReason.value.modeChanged;
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
  const changeMode = (newMode: EditorMode, isModeSwitchOnly: boolean = false) => {
    const currentMode = activeMode.value;
    if (newMode === currentMode) return;

    // When switching TO SQL from LogchefQL
    if (newMode === 'sql' && currentMode === 'logchefql') {
      // Store original SQL query before potentially overwriting it
      const originalSql = sqlQuery.value;
      const isEmptyLogchefQL = !logchefQuery.value?.trim();

      // Check if we have LogchefQL content that can be translated
      if (!isEmptyLogchefQL) {
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
          // Keep isFromUrl true when it's just a mode switch to prevent marking as dirty
          // This is the key fix - we don't want auto-translated content to be considered dirty
          if (!isModeSwitchOnly) {
            isFromUrl.value = false;
          }
        } else {
          // If translation fails, fall back to original SQL or default
          if (!originalSql) {
            generateAndSetDefaultSQL();
            // Only mark as user-generated content if not just a mode switch
            if (!isModeSwitchOnly) {
              isFromUrl.value = false;
            }
          }
        }
      } else if (!originalSql) {
        // Empty LogchefQL query AND no original SQL, generate default SQL without marking dirty
        generateAndSetDefaultSQL();
        // Empty LogchefQL to default SQL shouldn't be considered user-generated content
        // This prevents marking as dirty
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
    // Only update SQL query if in SQL mode
    if (activeMode.value === 'sql') {
      const currentSql = sqlQuery.value?.trim() || '';
      if (!currentSql) return;
      
      try {
        // Get current source details
        const tableName = sourcesStore.getCurrentSourceTableName;
        const tsField = sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp';
        
        // Check if the SQL query contains toDateTime function calls
        if (currentSql.includes('toDateTime(')) {
          // Instead of generating an entirely new query, we'll selectively update
          // the time range portion while preserving the rest of the query
          
          // Extract the WHERE clause and analyze it
          const whereClauseMatch = /WHERE\s+(.*?)(?:\s+ORDER\s+BY|\s+GROUP\s+BY|\s+LIMIT|\s*$)/is.exec(currentSql);
          if (whereClauseMatch) {
            const whereClause = whereClauseMatch[1];
            
            // Look for time range condition with toDateTime
            const timeConditionRegex = new RegExp(`\`?${tsField}\`?\\s+BETWEEN\\s+toDateTime\\([^)]+\\)\\s+AND\\s+toDateTime\\([^)]+\\)`, 'i');
            const timeConditionMatch = timeConditionRegex.exec(whereClause);
            
            if (timeConditionMatch) {
              // Generate the new time condition
              const newTimeCondition = createTimeRangeCondition(tsField, exploreStore.timeRange as TimeRange, true);
              
              // Replace the old time condition with the new one in the full SQL
              const updatedSql = currentSql.replace(timeConditionMatch[0], newTimeCondition);
              
              if (updatedSql !== currentSql) {
                sqlQuery.value = updatedSql;
                exploreStore.setRawSql(updatedSql);
                console.log("useQuery: Updated time range in SQL query while preserving other conditions");
              }
            } else {
              console.log("useQuery: Couldn't find time range condition pattern to update");
            }
          } else {
            console.log("useQuery: Couldn't find WHERE clause to update time range");
          }
        } else {
          console.log("useQuery: SQL query doesn't contain toDateTime calls, skipping time update");
        }
      } catch (error) {
        console.error("useQuery: Error updating time range in SQL:", error);
      }
    }
    
    // Log for debugging
    console.log("useQuery: handleTimeRangeUpdate called, current dirty state:", isDirty.value, 
                "dirtyReason:", dirtyReason.value,
                "timeRange comparison:", JSON.stringify(exploreStore.timeRange), 
                "vs lastState:", exploreStore.lastExecutedState?.timeRange);
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
      console.log("useQuery: Starting prepareQueryForExecution");
      const params = getCommonQueryParams();
      const mode = activeMode.value;
      const query = mode === 'logchefql' ? logchefQuery.value : sqlQuery.value;

      console.log("useQuery: Preparing query - mode:", mode, "query:", query ? (query.length > 50 ? query.substring(0, 50) + '...' : query) : '(empty)');

      // Validate LogchefQL query before execution
      if (mode === 'logchefql' && query.trim()) {
        const validation = validateLogchefQLWithDetails(query);
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
      // Validate SQL query before execution
      else if (mode === 'sql' && query.trim()) {
        const validation = validateSQLWithDetails(query);
        if (!validation.valid) {
          console.log("useQuery: SQL validation failed:", validation.error);
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

      console.log("useQuery: Calling QueryService.prepareQueryForExecution");
      const result = QueryService.prepareQueryForExecution({
        mode: queryServiceMode,
        query,
        ...params
      });

      console.log("useQuery: Query preparation result:", result.success ? "success" : "failed",
                  result.error ? `Error: ${result.error}` : '',
                  result.warnings?.length ? `Warnings: ${result.warnings.length}` : '');

      // Track warnings and errors
      sqlWarnings.value = result.warnings || [];
      queryError.value = result.error || '';

      return result;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      console.error("useQuery: Exception in prepareQueryForExecution:", errorMessage);
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
      console.log("useQuery: Setting lastExecutedState with timeRange:", 
                  JSON.stringify(exploreStore.timeRange).substring(0, 50) + "...");
                  
      exploreStore.setLastExecutedState({
        timeRange: JSON.stringify(exploreStore.timeRange),
        limit: exploreStore.limit,
        query: currentQuery.value,
        mode: activeMode.value,
        logchefqlQuery: logchefQuery.value,
        sqlQuery: sqlQuery.value
      });

      // Only reset isFromUrl if this is not the initial query execution from URL
      if (!initialQueryExecution.value) {
        isFromUrl.value = false;
      }

      // Execute the query via store
      const execResult = await exploreStore.executeQuery(result.sql);

      // Always update last execution timestamp even if query fails
      exploreStore.updateExecutionTimestamp();

      if (!execResult.success) {
        queryError.value = execResult.error?.message || 'Query execution failed';
      } else {
        // Create a new history entry in the browser history
        // But only do this for successful queries to avoid cluttering history with errors
        // NOTE: This is now handled by the LogExplorer component's handleQueryExecution
        // pushQueryHistoryEntry();
      }

      // Mark that we've run a query and no longer in initial execution
      hasRunQuery.value = true;
      initialQueryExecution.value = false;

      return {
        success: execResult.success,
        error: execResult.error,
        data: execResult.data
      };
    } catch (error) {
      // Still update timestamp on error
      exploreStore.updateExecutionTimestamp();

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
    hasRunQuery,
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
