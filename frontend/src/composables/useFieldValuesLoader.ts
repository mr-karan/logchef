import { ref, computed, type Ref, type ComputedRef } from 'vue'
import { sourcesApi, type FieldValuesResult } from '@/api/sources'
import { useFieldValuesStore } from '@/stores/exploreFieldValues'
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
  timezone?: string
  limit?: number
}

/**
 * Concurrency queue to limit parallel requests
 * Prevents overwhelming the active datasource backend with too many simultaneous queries
 */
class ConcurrencyQueue {
  private running = 0
  private queue: Array<() => void> = []

  constructor(private maxConcurrent: number = 4) {}

  async add<T>(task: () => Promise<T>): Promise<T> {
    // Wait for a slot if at capacity
    if (this.running >= this.maxConcurrent) {
      await new Promise<void>(resolve => {
        this.queue.push(resolve)
      })
    }

    this.running++
    try {
      return await task()
    } finally {
      this.running--
      // Release next queued task
      const next = this.queue.shift()
      if (next) next()
    }
  }

  clear() {
    this.queue = []
  }
}

/**
 * Composable for progressive per-field value loading
 *
 * Features:
 * - Parallel requests with concurrency limit (max 4)
 * - Per-field AbortController for cancellation
 * - Hybrid loading: auto-load cheap scalar fields, click-to-load for potentially expensive ones
 * - Progressive updates as each field loads
 */
export function useFieldValuesLoader(options: ComputedRef<LoaderOptions> | Ref<LoaderOptions>) {
  // Per-field loading states
  const fieldStates = ref<Map<string, FieldLoadingState>>(new Map())

  // Per-field abort controllers
  const abortControllers = ref<Map<string, AbortController>>(new Map())

  // Concurrency queue (max 4 parallel requests)
  const concurrencyQueue = new ConcurrencyQueue(4)

  /**
   * Get the current state for a field
   */
  const getFieldState = (fieldName: string): FieldLoadingState => {
    return fieldStates.value.get(fieldName) || { status: 'idle' }
  }

  /**
   * Update state for a field and trigger reactivity
   */
  const setFieldState = (fieldName: string, state: FieldLoadingState) => {
    fieldStates.value.set(fieldName, state)
    // Trigger Vue reactivity by creating new Map reference
    fieldStates.value = new Map(fieldStates.value)
  }

  /**
   * Load values for a single field
   */
  const loadField = async (fieldName: string, fieldType: string): Promise<void> => {
    const opts = options.value
    const timeRange = opts.getTimeRange()

    if (!timeRange || !opts.teamId || !opts.sourceId) {
      return
    }

    // Cancel any existing request for this field
    const existingController = abortControllers.value.get(fieldName)
    if (existingController) {
      existingController.abort()
    }

    // Create new AbortController for this field
    const controller = new AbortController()
    abortControllers.value.set(fieldName, controller)

    // Snapshot query before async work to avoid TOCTOU race
    const filterQuery = opts.getFilterQuery()
    const filterQueryLanguage = opts.getFilterQueryLanguage()

    // Set loading state immediately
    setFieldState(fieldName, { status: 'loading' })

    // Queue the request with concurrency control
    try {
      await concurrencyQueue.add(async () => {
        // Check if aborted before making request
        if (controller.signal.aborted) return

        try {
          const response = await sourcesApi.getFieldValues(
            opts.teamId!,
            opts.sourceId!,
            fieldName,
            fieldType,
            timeRange.startTime,
            timeRange.endTime,
            opts.timezone,
            opts.limit || 10,
            filterQueryLanguage,
            filterQuery,
            controller.signal
          )

          // Only update if not aborted
          if (!controller.signal.aborted && response.data) {
            setFieldState(fieldName, {
              status: 'loaded',
              values: response.data
            })

            // Populate the shared store for editor autocomplete
            try {
              const store = useFieldValuesStore()
              store.populateFromFetch(
                opts.teamId!, opts.sourceId!, fieldName,
                timeRange.startTime, timeRange.endTime,
                filterQuery || '',
                response.data,
              )
            } catch {
              // Store not available yet, ignore
            }
          }
        } catch (error: any) {
          // Ignore abort errors silently
          if (error.name === 'AbortError' || error.name === 'CanceledError') {
            return
          }

          // Set error state
          setFieldState(fieldName, {
            status: 'error',
            error: 'Failed to load'
          })
        }
      })
    } catch {
      // Queue was cleared, ignore
    }
  }

  /**
   * Initialize field states and auto-load priority fields
   */
  const loadPriorityFields = (fields: FieldInfo[]) => {
    // Cancel all existing requests
    cancelAll()

    // Initialize states based on field type
    const newStates = new Map<string, FieldLoadingState>()
    fields.forEach(field => {
      if (isPriorityField(field.type)) {
        newStates.set(field.name, { status: 'idle' })
      } else if (isClickToLoadField(field.type)) {
        newStates.set(field.name, { status: 'click-to-load' })
      }
    })
    fieldStates.value = newStates

    // Auto-load priority fields (LowCardinality, Enum)
    const priorityFields = fields.filter(f => isPriorityField(f.type))
    priorityFields.forEach(field => {
      loadField(field.name, field.type)
    })
  }

  /**
   * Cancel all in-flight requests
   */
  const cancelAll = () => {
    concurrencyQueue.clear()
    abortControllers.value.forEach(controller => {
      controller.abort()
    })
    abortControllers.value.clear()
  }

  /**
   * Clear all cached values and reset states
   */
  const clearCache = () => {
    cancelAll()
    fieldStates.value = new Map()
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
    cancelAll,
    clearCache,

    // Utilities
    isPriorityField,
    isClickToLoadField,
    isFilterableField
  }
}
