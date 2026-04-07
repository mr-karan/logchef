import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { FieldValuesResult } from '@/api/sources'

export interface FieldValuesCacheEntry {
  result: FieldValuesResult
  fetchedAt: number
  contextQuery: string
}

export interface FieldSummary {
  type: string
  totalDistinct: number
  isLowCardinality: boolean
}

export const useFieldValuesStore = defineStore('exploreFieldValues', () => {
  // Reactive cache of field values keyed by teamId|sourceId|fieldName|timeRange|logchefql
  const entries = ref<Map<string, FieldValuesCacheEntry>>(new Map())

  // Per-field summary (type + cardinality), keyed by sourceId|fieldName
  const summaries = ref<Map<string, FieldSummary>>(new Map())

  // Secondary index: sourceId|fieldName → Set of full cache keys (avoids linear scan)
  const fieldIndex = new Map<string, Set<string>>()

  function fieldKey(sourceId: number, fieldName: string): string {
    return `${sourceId}|${fieldName}`
  }

  function buildCacheKey(
    teamId: number, sourceId: number, fieldName: string,
    startTime: string, endTime: string, logchefql: string,
  ): string {
    return `${teamId}|${sourceId}|${fieldName}|${startTime}|${endTime}|${logchefql}`
  }

  /**
   * Populate the cache from an external fetch (called by sidebar loader).
   * Sorts values by count desc and updates the secondary index + summary.
   */
  function populateFromFetch(
    teamId: number, sourceId: number, fieldName: string,
    startTime: string, endTime: string, logchefql: string,
    result: FieldValuesResult,
  ): void {
    result.values.sort((a, b) => b.count - a.count)

    const key = buildCacheKey(teamId, sourceId, fieldName, startTime, endTime, logchefql)
    entries.value.set(key, {
      result,
      fetchedAt: Date.now(),
      contextQuery: logchefql,
    })
    entries.value = new Map(entries.value)

    // Update secondary index
    const fKey = fieldKey(sourceId, fieldName)
    let keys = fieldIndex.get(fKey)
    if (!keys) {
      keys = new Set()
      fieldIndex.set(fKey, keys)
    }
    keys.add(key)

    // Update summary
    summaries.value.set(fKey, {
      type: result.field_type,
      totalDistinct: result.total_distinct,
      isLowCardinality: result.is_low_cardinality,
    })
    summaries.value = new Map(summaries.value)
  }

  /**
   * Get summary (type + cardinality) for a field, if known.
   */
  function getFieldSummary(sourceId: number, fieldName: string): FieldSummary | null {
    return summaries.value.get(fieldKey(sourceId, fieldName)) ?? null
  }

  /**
   * Find the most recent cached values for a field via the secondary index.
   */
  function findCachedValues(sourceId: number, fieldName: string): FieldValuesResult | null {
    const keys = fieldIndex.get(fieldKey(sourceId, fieldName))
    if (!keys || keys.size === 0) return null

    let best: FieldValuesCacheEntry | null = null
    for (const key of keys) {
      const entry = entries.value.get(key)
      if (entry && (!best || entry.fetchedAt > best.fetchedAt)) {
        best = entry
      }
    }
    return best?.result ?? null
  }

  /**
   * Clear all cached data.
   */
  function clearAll() {
    entries.value = new Map()
    summaries.value = new Map()
    fieldIndex.clear()
  }

  return {
    entries,
    summaries,
    populateFromFetch,
    getFieldSummary,
    findCachedValues,
    clearAll,
  }
})
