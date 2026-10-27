import { computed, onScopeDispose, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { z } from 'zod'
import type { ColumnSizingState, ColumnVisibilityState, Updater } from '@tanstack/vue-table'
import type { Source } from '@/api/sources'
import type { ColumnClass, DefaultColumnOrder } from '@/views/explore/table/defaultColumnOrder'
import type { ColumnDef } from '@/views/explore/table/tableFeatures'
import {
  COLUMN_VISIBILITY_VERSION,
  getTrustedSavedVisibility,
  resolveColumnVisibility,
} from '@/views/explore/table/defaultColumnVisibility'

export const COLUMN_SIZING_VERSION = 1

const savedColumnState = z.object({
  columnOrder: z.array(z.string()).default([]),
  columnClasses: z.record(z.string(), z.enum(['readable', 'opaque', 'unknown'])).default({}),
  orderSource: z.enum(['auto', 'explicit']).optional(),
  orderIsExplicit: z.boolean().optional(),
  columnSizing: z.record(z.string(), z.number()).default({}),
  columnVisibility: z.record(z.string(), z.boolean()).default({}),
  visibilityVersion: z.number().optional(),
  sizingVersion: z.number().optional(),
}).transform(({ orderIsExplicit, ...saved }) => ({
  ...saved,
  orderSource: saved.orderSource ?? (saved.columnOrder.length && orderIsExplicit !== false ? 'explicit' : 'auto'),
}))

type SavedColumnState = z.infer<typeof savedColumnState>

interface ColumnStateOptions {
  columns: MaybeRefOrGetter<ColumnDef<Record<string, unknown>>[]>
  defaultOrder: MaybeRefOrGetter<DefaultColumnOrder | null>
  teamId: MaybeRefOrGetter<number | null>
  sourceId: MaybeRefOrGetter<string>
  source: MaybeRefOrGetter<Pick<Source, 'source_type'> | null | undefined>
  timestampField: MaybeRefOrGetter<string>
  severityField: MaybeRefOrGetter<string>
}

export function useColumnState(options: ColumnStateOptions) {
  const columnOrder = ref<string[]>([])
  const columnClasses = ref<Record<string, ColumnClass>>({})
  const orderSource = ref<'pending' | 'auto' | 'explicit'>('pending')
  const columnSizing = ref<ColumnSizingState>({})
  const columnVisibility = ref<ColumnVisibilityState>({})
  const isReadyToPersist = ref(false)
  let visibilityVersion: number | undefined
  const columnIds = computed(() => toValue(options.columns)
    .map(column => column.id).filter((id): id is string => Boolean(id)))
  const defaultOrder = computed(() => toValue(options.defaultOrder))
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
      const saved = savedColumnState.parse(JSON.parse(stored))
      if (saved.sizingVersion !== COLUMN_SIZING_VERSION) {
        saved.columnSizing = {}
        saved.sizingVersion = COLUMN_SIZING_VERSION
        try {
          localStorage.setItem(storageKey.value, JSON.stringify(saved))
        } catch (error) {
          console.error('Error saving table state to localStorage:', error)
        }
      }
      return saved
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
    orderSource.value = 'explicit'
    columnOrder.value = timestampFirst(typeof updater === 'function' ? updater(columnOrder.value) : updater)
  }

  function initializeState(mode: 'restore' | 'reset' | 'reconcile') {
    const ignoreSavedState = mode === 'reset'
    const columns = toValue(options.columns)
    if (columns.length === 0 && mode === 'reconcile') return

    const defaults = defaultOrder.value
    const ids = defaults?.order ?? []
    const saved = mode === 'restore' ? loadState() : null
    if (mode !== 'reconcile') visibilityVersion = saved?.visibilityVersion
    const source = toValue(options.source)
    const timestampField = toValue(options.timestampField)
    const severityField = toValue(options.severityField)
    const previousOrder = ignoreSavedState ? []
      : orderSource.value !== 'pending' ? columnOrder.value : saved?.columnOrder ?? []
    if (ignoreSavedState || orderSource.value === 'pending') {
      orderSource.value = saved?.columnOrder.length ? saved.orderSource
        : defaultOrder.value === null ? 'pending' : 'auto'
    }
    const classes = ignoreSavedState ? {} : { ...saved?.columnClasses, ...columnClasses.value }
    for (const [id, kind] of Object.entries(defaults?.columnClasses ?? {})) {
      if (kind !== 'unknown' || classes[id] === undefined) classes[id] = kind
    }
    if (JSON.stringify(columnClasses.value) !== JSON.stringify(classes)) columnClasses.value = classes
    const newIds = ids.filter(id => !previousOrder.includes(id))
    const order = [...previousOrder]
    if (orderSource.value === 'explicit') order.push(...newIds)
    else {
      let boundary = 0
      order.forEach((id, index) => { if (classes[id] === 'readable') boundary = index + 1 })
      const firstOpaque = order.findIndex(id => classes[id] === 'opaque')
      if (firstOpaque >= 0) boundary = Math.min(boundary, firstOpaque)
      order.splice(boundary, 0, ...newIds.filter(id => classes[id] !== 'opaque'))
      order.push(...newIds.filter(id => classes[id] === 'opaque'))
    }
    const initialOrder = timestampFirst(orderSource.value === 'pending' ? columnIds.value : order)
    const initialSizing = mode === 'reconcile' ? columnSizing.value : { ...saved?.columnSizing }
    const initialVisibility = resolveColumnVisibility(columns, source,
      getTrustedSavedVisibility(source, mode === 'reconcile' ? columnVisibility.value : saved?.columnVisibility,
        visibilityVersion), timestampField, severityField)

    if (ignoreSavedState || JSON.stringify(columnOrder.value) !== JSON.stringify(initialOrder)) columnOrder.value = initialOrder
    if (JSON.stringify(columnSizing.value) !== JSON.stringify(initialSizing)) columnSizing.value = initialSizing
    if (JSON.stringify(columnVisibility.value) !== JSON.stringify(initialVisibility)) columnVisibility.value = initialVisibility
    // An unresolved source must not stamp legacy visibility with the current version.
    isReadyToPersist.value = Boolean(source?.source_type)
    if (isReadyToPersist.value) visibilityVersion = COLUMN_VISIBILITY_VERSION
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
    defaultOrder: defaultOrder.value,
    storageKey: storageKey.value,
    sourceType: toValue(options.source)?.source_type,
    timestampField: toValue(options.timestampField),
    severityField: toValue(options.severityField),
  }), (current, previous) => {
    flushPendingWrite()
    if (current.storageKey !== previous?.storageKey) {
      orderSource.value = 'pending'
      columnOrder.value = []
      columnClasses.value = {}
    }
    initializeState(!previous || current.storageKey !== previous.storageKey ? 'restore' : 'reconcile')
  }, { immediate: true })

  watch([columnOrder, columnClasses, orderSource, columnSizing, columnVisibility], () => {
    if (!storageKey.value || !isReadyToPersist.value || !toValue(options.columns).length) return
    if (saveTimer !== null) clearTimeout(saveTimer)
    pendingWrite = {
      key: storageKey.value,
      state: {
        columnOrder: orderSource.value === 'pending' ? [] : [...columnOrder.value],
        orderSource: orderSource.value === 'explicit' ? 'explicit' : 'auto',
        columnClasses: { ...columnClasses.value },
        columnSizing: { ...columnSizing.value },
        columnVisibility: { ...columnVisibility.value },
        visibilityVersion: COLUMN_VISIBILITY_VERSION,
        sizingVersion: COLUMN_SIZING_VERSION,
      },
    }
    saveTimer = setTimeout(flushPendingWrite, 300)
  }, { deep: true, immediate: true })

  function resetTableStateToDefaults() {
    flushPendingWrite()
    if (storageKey.value) localStorage.removeItem(storageKey.value)
    if (!toValue(options.columns).length) return
    initializeState('reset')
    isReadyToPersist.value = true
  }

  return { columnOrder, columnSizing, columnVisibility, setColumnOrder, resetTableStateToDefaults }
}
