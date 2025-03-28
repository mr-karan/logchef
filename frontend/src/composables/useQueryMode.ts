import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { QueryBuilder } from '@/utils/query-builder'
import type { EditorMode } from '@/views/explore/types'

export function useQueryMode() {
  const exploreStore = useExploreStore()
  const sourcesStore = useSourcesStore()

  // Computed properties for query content
  const currentLogchefQuery = computed({
    get: () => exploreStore.logchefqlCode,
    set: (value) => exploreStore.setLogchefqlCode(value)
  })

  const currentSqlQuery = computed({
    get: () => exploreStore.rawSql,
    set: (value) => exploreStore.setRawSql(value)
  })

  // Handle mode changes from QueryEditor
  async function handleModeChange(newEditorMode: EditorMode) {
    const newStoreMode = newEditorMode === 'logchefql' ? 'logchefql' : 'sql'
    const oldStoreMode = exploreStore.activeMode

    if (newStoreMode === oldStoreMode) return

    // Get current content before switching mode
    const currentContent = oldStoreMode === 'logchefql'
      ? exploreStore.logchefqlCode
      : exploreStore.rawSql

    // Handle translation when switching TO SQL mode
    if (newStoreMode === 'sql') {
      let sqlToSet = ''

      // Try translating from LogchefQL if switching from it
      if (oldStoreMode === 'logchefql' && currentContent?.trim() &&
          sourcesStore.hasValidCurrentSource && exploreStore.timeRange) {
        const buildResult = QueryBuilder.buildSqlFromLogchefQL({
          tableName: sourcesStore.getCurrentSourceTableName || '',
          tsField: sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp',
          startDateTime: exploreStore.timeRange.start,
          endDateTime: exploreStore.timeRange.end,
          limit: exploreStore.limit,
          logchefqlQuery: currentContent
        })

        if (buildResult.success) {
          sqlToSet = buildResult.sql
        } else {
          console.warn("Failed to translate LogchefQL to SQL:", buildResult.error)
          sqlToSet = `-- Error translating LogchefQL: ${buildResult.error}\n`
        }
      }

      // If SQL is empty after translation, generate default
      if (!sqlToSet.trim() && sourcesStore.hasValidCurrentSource && exploreStore.timeRange) {
        const defaultResult = QueryBuilder.getDefaultSQLQuery({
          tableName: sourcesStore.getCurrentSourceTableName || '',
          tsField: sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp',
          startDateTime: exploreStore.timeRange.start,
          endDateTime: exploreStore.timeRange.end,
          limit: exploreStore.limit
        })

        if (defaultResult.success) {
          sqlToSet = defaultResult.sql
        } else {
          console.warn("Failed to generate default SQL:", defaultResult.error)
          sqlToSet = `-- Error generating default SQL: ${defaultResult.error}\n`
        }
      }

      exploreStore.setRawSql(sqlToSet)
    }
    // No translation needed when switching TO LogchefQL

    exploreStore.setActiveMode(newStoreMode)
  }

  return {
    currentLogchefQuery,
    currentSqlQuery,
    handleModeChange
  }
}