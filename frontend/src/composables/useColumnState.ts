import { computed, onScopeDispose, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { z } from 'zod'
import type { ColumnSizingState, ColumnVisibilityState, Updater } from '@tanstack/vue-table'
import type { Source } from '@/api/sources'
import type { ColumnDef } from '@/views/explore/table/tableFeatures'
import {
  COLUMN_VISIBILITY_VERSION,
  getDefaultColumnVisibility,
  getTrustedSavedVisibility,
  resolveColumnVisibility,
} from '@/views/explore/table/defaultColumnVisibility'

const savedColumnState = z.object({
  columnOrder: z.array(z.string()).default([]),
  columnSizing: z.record(z.string(), z.number()).default({}),
  columnVisibility: z.record(z.string(), z.boolean()).default({}),
  visibilityVersion: z.number().optional(),
})

type SavedColumnState = z.infer<typeof savedColumnState>

interface ColumnStateOptions {
  columns: MaybeRefOrGetter<ColumnDef<Record<string, unknown>>[]>
  teamId: MaybeRefOrGetter<number | null>
  sourceId: MaybeRefOrGetter<string>
  source: MaybeRefOrGetter<Pick<Source, 'source_type'> | null | undefined>
  timestampField: MaybeRefOrGetter<string>
  severityField: MaybeRefOrGetter<string>
  defaultSize: number
}

export function useColumnState(options: ColumnStateOptions) {
  const columnOrder = ref<string[]>([])
  const columnSizing = ref<ColumnSizingState>({})
  const columnVisibility = ref<ColumnVisibilityState>({})
  const isReadyToPersist = ref(false)
  const storageKey = computed(() => {
    const teamId = toValue(options.teamId)
    const sourceId = toValue(options.sourceId)
    return teamId == null || !sourceId ? null : `logchef-tableState-${teamId}-${sourceId}`
  })

  function loadState(): SavedColumnState | null {
    if (!storageKey.value) return null
    try {
      const stored = localStorage.getItem(storageKey.value)
      if (!stored) return null
      return savedColumnState.parse(JSON.parse(stored))
    } catch (error) {
      console.error('Error loading table state from localStorage:', error)
      return null
    }
  }

  function timestampFirst(order: string[]): string[] {
    const timestampField = toValue(options.timestampField)
    return order.includes(timestampField)
      ? [timestampField, ...order.filter(id => id !== timestampField)]
      : order
  }

  function setColumnOrder(updater: Updater<string[]>) {
    columnOrder.value = timestampFirst(typeof updater === 'function' ? updater(columnOrder.value) : updater)
  }

  function initializeState(ignoreSavedState = false) {
    const columns = toValue(options.columns)
    if (columns.length === 0) {
      columnOrder.value = []
      columnSizing.value = {}
      columnVisibility.value = {}
      isReadyToPersist.value = false
      return
    }

    const ids = columns.map(column => column.id).filter((id): id is string => Boolean(id))
    const saved = ignoreSavedState ? null : loadState()
    const restored = saved?.columnOrder.length ? saved : null
    const source = toValue(options.source)
    const timestampField = toValue(options.timestampField)
    const severityField = toValue(options.severityField)
    const savedOrder = restored?.columnOrder ?? []
    const order = [...savedOrder, ...ids.filter(id => !savedOrder.includes(id))]
    const initialOrder = timestampFirst(order)
    const initialSizing = restored ? { ...restored.columnSizing } : {}
    for (const column of columns) {
      if (column.id && initialSizing[column.id] === undefined) {
        initialSizing[column.id] = column.size ?? options.defaultSize
      }
    }
    const initialVisibility = restored
      ? resolveColumnVisibility(columns, source,
        getTrustedSavedVisibility(source, restored.columnVisibility, restored.visibilityVersion),
        timestampField, severityField)
      : getDefaultColumnVisibility(columns, source, timestampField, severityField)

    if (JSON.stringify(columnOrder.value) !== JSON.stringify(initialOrder)) columnOrder.value = initialOrder
    if (JSON.stringify(columnSizing.value) !== JSON.stringify(initialSizing)) columnSizing.value = initialSizing
    if (JSON.stringify(columnVisibility.value) !== JSON.stringify(initialVisibility)) columnVisibility.value = initialVisibility
    // An unresolved source must not stamp legacy visibility with the current version.
    isReadyToPersist.value = Boolean(source?.source_type)
  }

  let pendingWrite: { key: string; state: SavedColumnState } | null = null
  let saveTimer: ReturnType<typeof setTimeout> | null = null

  function flushPendingWrite() {
    if (saveTimer !== null) clearTimeout(saveTimer)
    saveTimer = null
    if (!pendingWrite) return
    const { key, state } = pendingWrite
    pendingWrite = null
    try {
      localStorage.setItem(key, JSON.stringify(state))
    } catch (error) {
      console.error('Error saving table state to localStorage:', error)
    }
  }

  onScopeDispose(flushPendingWrite)

  watch(() => ({
    columns: toValue(options.columns),
    storageKey: storageKey.value,
    sourceType: toValue(options.source)?.source_type,
    timestampField: toValue(options.timestampField),
    severityField: toValue(options.severityField),
  }), (context, previous) => {
    if (context.storageKey !== previous?.storageKey) flushPendingWrite()
    initializeState()
  }, { immediate: true })

  watch([columnOrder, columnSizing, columnVisibility], () => {
    if (!storageKey.value || !isReadyToPersist.value || !toValue(options.columns).length) return
    if (saveTimer !== null) clearTimeout(saveTimer)
    pendingWrite = {
      key: storageKey.value,
      state: {
        columnOrder: [...columnOrder.value],
        columnSizing: { ...columnSizing.value },
        columnVisibility: { ...columnVisibility.value },
        visibilityVersion: COLUMN_VISIBILITY_VERSION,
      },
    }
    saveTimer = setTimeout(flushPendingWrite, 300)
  }, { deep: true })

  function resetTableStateToDefaults() {
    if (storageKey.value) localStorage.removeItem(storageKey.value)
    if (!toValue(options.columns).length) return
    initializeState(true)
    isReadyToPersist.value = true
  }

  return { columnOrder, columnSizing, columnVisibility, setColumnOrder, resetTableStateToDefaults }
}
