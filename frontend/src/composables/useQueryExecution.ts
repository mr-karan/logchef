import { ref, computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { useTeamsStore } from '@/stores/teams'
import { QueryBuilder } from '@/utils/query-builder'
import { getErrorMessage } from '@/api/types'
import type { QueryBuildOptions } from '@/views/explore/types'

export function useQueryExecution() {
  const exploreStore = useExploreStore()
  const sourcesStore = useSourcesStore()
  const teamsStore = useTeamsStore()

  const localQueryError = ref('')
  const isExecutingQuery = computed(() => exploreStore.isLoadingOperation('executeQuery'))

  // Check if we can execute the query
  const canExecuteQuery = computed(() => {
    return exploreStore.sourceId > 0 &&
      teamsStore.currentTeamId > 0 &&
      sourcesStore.hasValidCurrentSource
  })

  // Check if query state is dirty (needs execution)
  const isDirty = computed(() => {
    const currentQuery = exploreStore.activeMode === 'logchefql'
      ? exploreStore.logchefqlCode
      : exploreStore.rawSql

    const currentTimeRange = JSON.stringify(exploreStore.timeRange)
    const currentLimit = exploreStore.limit
    const lastState = exploreStore.lastExecutedState

    if (!lastState) return true

    return currentTimeRange !== lastState.timeRange ||
      currentLimit !== lastState.limit ||
      currentQuery !== lastState.query
  })

  // Execute query with proper error handling
  async function executeQuery() {
    if (!canExecuteQuery.value) {
      throw new Error('Cannot execute query: missing team, source, or source details')
    }

    const mode = exploreStore.activeMode
    const query = mode === 'logchefql' ? exploreStore.logchefqlCode : exploreStore.rawSql

    if (!query?.trim()) {
      throw new Error(`Cannot execute empty ${mode} query`)
    }

    let sqlToExecute: string
    if (mode === 'sql') {
      sqlToExecute = query
    } else {
      const buildOptions: QueryBuildOptions = {
        tableName: sourcesStore.getCurrentSourceTableName || '',
        tsField: sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp',
        startDateTime: exploreStore.timeRange!.start,
        endDateTime: exploreStore.timeRange!.end,
        limit: exploreStore.limit,
        logchefqlQuery: query
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
      query
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
