import { ref, computed, type Ref, type ComputedRef } from 'vue'
import { sourcesApi, type FieldValuesResult } from '@/api/sources'
import type { TemplateVariable } from '@/api/logchefql'
import { useFieldValuesStore } from '@/stores/exploreFieldValues'
import { createConcurrencyLimiter } from '@/utils/promisePool'
import type { QueryLanguage } from '@/lib/queryMetadata'
import {
  isPriorityField,
  isClickToLoadField,
  isFilterableField,
} from '@/lib/sourceFields'

export { isPriorityField, isClickToLoadField, isFilterableField } from '@/lib/sourceFields'

// Field loading states
export type FieldStatus = 'idle' | 'loading' | 'loaded' | 'error' | 'click-to-load'

export interface FieldLoadingState {
  status: FieldStatus
  values?: FieldValuesResult
  error?: string
}

export interface FieldInfo {
  name: string
  type: string
}

export interface LoaderOptions {
  teamId: number | undefined
  sourceId: number | undefined
  getTimeRange: () => { startTime: string; endTime: string } | null
  getFilterQuery: () => string
  getFilterQueryLanguage: () => QueryLanguage | undefined
  getFilterVariables: () => TemplateVariable[]
  timezone?: string
  limit?: number
}

export function useFieldValuesLoader(options: ComputedRef<LoaderOptions> | Ref<LoaderOptions>) {
  const fieldStates = ref<Map<string, FieldLoadingState>>(new Map())

  const abortControllers = new Map<string, AbortController>()
  const runFieldLoad = createConcurrencyLimiter(4)

  /**
   * Get the current state for a field
   */
  const getFieldState = (fieldName: string): FieldLoadingState => {
    return fieldStates.value.get(fieldName) || { status: 'idle' }
  }

  const setFieldState = (fieldName: string, state: FieldLoadingState) => {
    fieldStates.value.set(fieldName, state)
  }

  const loadField = async (fieldName: string, fieldType: string): Promise<void> => {
    const opts = options.value
    const { teamId, sourceId } = opts
    const timeRange = opts.getTimeRange()
    if (!timeRange || !teamId || !sourceId) return

    abortControllers.get(fieldName)?.abort()
    const controller = new AbortController()
    abortControllers.set(fieldName, controller)

    const filterQuery = opts.getFilterQuery()
    const filterQueryLanguage = opts.getFilterQueryLanguage()
    const filterVariables = opts.getFilterVariables()
    setFieldState(fieldName, { status: 'loading' })

    try {
      await runFieldLoad(async () => {
        if (controller.signal.aborted) return
        let response: Awaited<ReturnType<typeof sourcesApi.getFieldValues>>
        try {
          response = await sourcesApi.getFieldValues(
            teamId, sourceId, fieldName, fieldType,
            timeRange.startTime, timeRange.endTime,
            opts.timezone, opts.limit || 10,
            filterQueryLanguage, filterQuery, filterVariables,
            controller.signal,
          )
        } catch {
          if (!controller.signal.aborted) {
            setFieldState(fieldName, { status: 'error', error: 'Failed to load' })
          }
          return
        }

        if (controller.signal.aborted) return
        if (!response.data) {
          setFieldState(fieldName, { status: 'error', error: 'Failed to load' })
          return
        }

        setFieldState(fieldName, { status: 'loaded', values: response.data })
        useFieldValuesStore().populateFromFetch(sourceId, fieldName, response.data)
      })
    } finally {
      if (abortControllers.get(fieldName) === controller) {
        abortControllers.delete(fieldName)
      }
    }
  }

  /**
   * Load the values of Loki stream labels without a state with one preload
   * request. The server reads the first 30 labels from the index, without
   * counts; a label it skips loads on click.
   */
  const preloadLabels = async (fields: FieldInfo[]): Promise<void> => {
    const opts = options.value
    const { teamId, sourceId } = opts
    const timeRange = opts.getTimeRange()
    const labels = fields.filter(field => field.type === 'label' && !fieldStates.value.has(field.name))
    if (!timeRange || !teamId || !sourceId || labels.length === 0) return

    const controller = new AbortController()
    for (const label of labels) {
      setFieldState(label.name, { status: 'loading' })
      abortControllers.set(label.name, controller)
    }
    // A label whose controller changed was cancelled or reloaded on its own.
    const settle = (state: (name: string) => FieldLoadingState) => {
      for (const label of labels) {
        if (abortControllers.get(label.name) !== controller) continue
        abortControllers.delete(label.name)
        const next = state(label.name)
        setFieldState(label.name, next)
        if (next.values) useFieldValuesStore().populateFromFetch(sourceId, label.name, next.values)
      }
    }

    await runFieldLoad(async () => {
      if (controller.signal.aborted) return settle(() => ({ status: 'click-to-load' }))
      let response: Awaited<ReturnType<typeof sourcesApi.getAllFieldValues>>
      try {
        response = await sourcesApi.getAllFieldValues(
          teamId, sourceId,
          timeRange.startTime, timeRange.endTime,
          opts.timezone, opts.limit || 10,
          opts.getFilterQueryLanguage(), opts.getFilterQuery(), opts.getFilterVariables(),
          controller.signal,
        )
      } catch {
        return settle(() => controller.signal.aborted
          ? { status: 'click-to-load' }
          : { status: 'error', error: 'Failed to load' })
      }
      const all = response.data
      if (!all) return settle(() => ({ status: 'error', error: 'Failed to load' }))
      settle(name => all[name] ? { status: 'loaded', values: all[name] } : { status: 'click-to-load' })
    })
  }

  /**
   * Initialize states for fields without one and auto-load the priority ones.
   * Fields that already have a state keep it, so callers can pass a growing
   * list without refetching. Call clearCache() first to start over.
   */
  const loadPriorityFields = (fields: FieldInfo[]) => {
    const priorityFields: FieldInfo[] = []
    for (const field of fields) {
      if (fieldStates.value.has(field.name)) continue
      if (isPriorityField(field.type)) {
        fieldStates.value.set(field.name, { status: 'idle' })
        priorityFields.push(field)
      } else if (isClickToLoadField(field.type)) {
        fieldStates.value.set(field.name, { status: 'click-to-load' })
      }
    }

    return Promise.all(priorityFields.map(field => loadField(field.name, field.type)))
  }

  /**
   * Cancel all in-flight requests
   */
  const cancelAll = () => {
    abortControllers.forEach((controller, fieldName) => {
      controller.abort()
      setFieldState(fieldName, { status: 'idle' })
    })
    abortControllers.clear()
  }

  /**
   * Clear all cached values and reset states
   */
  const clearCache = () => {
    cancelAll()
    fieldStates.value.clear()
  }

  /**
   * Computed: Get all field values as a record (for template compatibility)
   */
  const fieldValues = computed(() => {
    const result: Record<string, FieldValuesResult> = {}
    fieldStates.value.forEach((state, fieldName) => {
      if (state.values) {
        result[fieldName] = state.values
      }
    })
    return result
  })

  /**
   * Computed: Check if any field is currently loading
   */
  const isAnyLoading = computed(() => {
    for (const state of fieldStates.value.values()) {
      if (state.status === 'loading') return true
    }
    return false
  })

  return {
    // State
    fieldStates,
    fieldValues,
    isAnyLoading,

    // Methods
    getFieldState,
    loadField,
    loadPriorityFields,
    preloadLabels,
    cancelAll,
    clearCache,

    // Utilities
    isPriorityField,
    isClickToLoadField,
    isFilterableField
  }
}
