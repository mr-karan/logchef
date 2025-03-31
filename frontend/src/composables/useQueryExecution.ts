import { ref, computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { useTeamsStore } from '@/stores/teams'
import { QueryBuilder } from '@/utils/query-builder'
import { getErrorMessage } from '@/api/types'
import type { QueryBuildOptions } from '@/views/explore/types'
import type { CalendarDateTime } from '@internationalized/date'

export function useQueryExecution() {
  const exploreStore = useExploreStore()
  const sourcesStore = useSourcesStore()
  const teamsStore = useTeamsStore()

  const localQueryError = ref('')
  const isExecutingQuery = computed(() => exploreStore.isLoadingOperation('executeQuery'))

  // Check if we can execute the query
  const canExecuteQuery = computed(() => {
    return exploreStore.sourceId > 0 &&
      teamsStore.currentTeamId !== null && teamsStore.currentTeamId > 0 &&
      sourcesStore.hasValidCurrentSource
  })

  // Check if query state is dirty (needs execution)
  const isDirty = computed(() => {
    const currentMode = exploreStore.activeMode;
    const currentQueryRaw = currentMode === 'logchefql'
      ? exploreStore.logchefqlCode
      : exploreStore.rawSql
    const currentQuery = currentQueryRaw || '' // Ensure string for comparison

    const currentTimeRangeObj = exploreStore.timeRange;
    const currentTimeRange = JSON.stringify(currentTimeRangeObj);
    const currentLimit = exploreStore.limit
    const lastState = exploreStore.lastExecutedState

    if (!lastState) {
      return true // Always dirty if no query has run yet
    }

    const timeRangeChanged = currentTimeRange !== lastState.timeRange;
    const limitChanged = currentLimit !== lastState.limit;
    // Ensure lastState.query is treated as a string for comparison (using explicit typing and ternary)
    const lastQuery: string = lastState.query !== undefined ? lastState.query : '';
    const queryChanged = currentQuery !== lastQuery;

    const dirtyResult = timeRangeChanged || limitChanged || queryChanged;

    return dirtyResult;
  })

  // Execute query with proper error handling
  async function executeQuery() {
    if (!canExecuteQuery.value) {
      throw new Error('Cannot execute query: missing team, source, or source details')
    }

    const mode = exploreStore.activeMode
    const query = mode === 'logchefql' ? exploreStore.logchefqlCode : exploreStore.rawSql

    if (mode === 'sql' && !query?.trim()) {
      throw new Error(`Cannot execute empty ${mode} query`)
    }

    let sqlToExecute: string
    if (mode === 'sql') {
      // Ensure query is a string, default to empty if undefined
      sqlToExecute = query || ''
    } else {
      const startDateTime = exploreStore.timeRange!.start as CalendarDateTime
      const endDateTime = exploreStore.timeRange!.end as CalendarDateTime

      // Explicitly handle potential undefined for tsField
      let effectiveTsField: string;
      if (sourcesStore.currentSourceDetails?._meta_ts_field) {
        effectiveTsField = sourcesStore.currentSourceDetails._meta_ts_field;
      } else {
        effectiveTsField = 'timestamp';
      }

      const buildOptions: QueryBuildOptions = {
        tableName: sourcesStore.getCurrentSourceTableName || '',
        tsField: effectiveTsField,
        startDateTime: startDateTime,
        endDateTime: endDateTime,
        limit: exploreStore.limit,
        logchefqlQuery: query || ''
      }

      const buildResult = QueryBuilder.buildSqlFromLogchefQL(buildOptions)
      if (!buildResult.success) {
        throw new Error(buildResult.error || 'Failed to build SQL from LogchefQL')
      }
      sqlToExecute = buildResult.sql
    }

    // Store current state before execution
    exploreStore.setLastExecutedState({
      timeRange: JSON.stringify(exploreStore.timeRange),
      limit: exploreStore.limit,
      query: query || ''
    })

    return await exploreStore.executeQuery(sqlToExecute)
  }

  // Wrapper for executeQuery with error handling
  async function triggerQueryExecution() {
    localQueryError.value = ''
    if (exploreStore.clearError) {
      exploreStore.clearError()
    }

    try {
      const result = await executeQuery()
      if (!result.success) {
        localQueryError.value = result.error?.message || 'Query execution failed'
      }
      return result
    } catch (error) {
      const errorMessage = getErrorMessage(error)
      localQueryError.value = errorMessage
      console.error('Query execution error:', errorMessage)
      return { success: false, error: { message: errorMessage } }
    }
  }

  return {
    localQueryError,
    isExecutingQuery,
    canExecuteQuery,
    isDirty,
    triggerQueryExecution
  }
}
