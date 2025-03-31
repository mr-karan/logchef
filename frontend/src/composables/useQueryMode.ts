import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { QueryBuilder } from '@/utils/query-builder'
import type { EditorMode } from '@/views/explore/types'
import { generateDefaultSqlQuery } from '@/utils/query-templates'
import type { DateValue } from '@internationalized/date'

export function useQueryMode() {
  const exploreStore = useExploreStore()
  const sourcesStore = useSourcesStore()

  // Computed properties for query content
  const currentLogchefQuery = computed({
    get: () => exploreStore.logchefqlCode || '',
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
      ? exploreStore.logchefqlCode || ''
      : exploreStore.rawSql

    // Handle translation when switching TO SQL mode
    if (newStoreMode === 'sql') {
      let sqlToSet = ''

      // Try translating from LogchefQL if switching from it
      if (oldStoreMode === 'logchefql' && currentContent?.trim() &&
          sourcesStore.hasValidCurrentSource && exploreStore.timeRange) {

        // Use the existing QueryBuilder directly which requires CalendarDateTime
        // We'll let TypeScript handle the type casting since these are already DateValue objects
        const buildResult = QueryBuilder.buildSqlFromLogchefQL({
          tableName: sourcesStore.getCurrentSourceTableName || '',
          tsField: sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp',
          startDateTime: exploreStore.timeRange.start as any, // Type assertion to bypass strict checks
          endDateTime: exploreStore.timeRange.end as any, // Type assertion to bypass strict checks
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

      // If SQL is empty after translation, generate default using our centralized utility
      if (!sqlToSet.trim() && sourcesStore.hasValidCurrentSource && exploreStore.timeRange) {
        // Use our new centralized template generator
        const tableName = sourcesStore.getCurrentSourceTableName || '';
        const timestampField = sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp';

        sqlToSet = generateDefaultSqlQuery({
          tableName,
          timestampField,
          timeRange: exploreStore.timeRange ? {
            start: exploreStore.timeRange.start,
            end: exploreStore.timeRange.end
          } : null,
          limit: exploreStore.limit
        });
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