import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { FieldValuesResult } from '@/api/sources'

export interface FieldSummary {
  type: string
  totalDistinct: number
  isLowCardinality: boolean
}

export const useFieldValuesStore = defineStore('exploreFieldValues', () => {
  const entries = ref<Map<string, FieldValuesResult>>(new Map())

  function fieldKey(sourceId: number, fieldName: string): string {
    return `${sourceId}|${fieldName}`
  }

  function populateFromFetch(
    sourceId: number, fieldName: string, result: FieldValuesResult,
  ): void {
    result.values.sort((a, b) => b.count - a.count)
    entries.value.set(fieldKey(sourceId, fieldName), result)
  }

  function getFieldSummary(sourceId: number, fieldName: string): FieldSummary | null {
    const result = findCachedValues(sourceId, fieldName)
    return result ? {
      type: result.field_type,
      totalDistinct: result.total_distinct,
      isLowCardinality: result.is_low_cardinality,
    } : null
  }

  function findCachedValues(sourceId: number, fieldName: string): FieldValuesResult | null {
    return entries.value.get(fieldKey(sourceId, fieldName)) ?? null
  }

  return {
    entries,
    populateFromFetch,
    getFieldSummary,
    findCachedValues,
  }
})
