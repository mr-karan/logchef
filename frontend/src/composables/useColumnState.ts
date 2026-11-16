import { computed, onScopeDispose, ref, shallowRef, toValue, watch, type MaybeRefOrGetter, type Ref } from 'vue'
import { z } from 'zod'
import type { ColumnSizingState, ColumnVisibilityState, SortingState, Updater } from '@tanstack/vue-table'
import type { Source } from '@/api/sources'
import {
  layoutHasResultColumn,
  seedColumnLayout,
  seedSorting,
  tableLayoutToDefinition,
  timestampFirst as putTimestampFirst,
  type TableColumnLayout,
  type TableViewSeed,
} from '@/lib/savedViewLayout'
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

// The working copy of the active view: the view it belongs to, its column
// layout and its sort.
const savedViewState = z.object({
  viewId: z.number(),
  revision: z.number(),
  columnOrder: z.array(z.string()).default([]),
  columnSizing: z.record(z.string(), z.number()).default({}),
  columnVisibility: z.record(z.string(), z.boolean()).default({}),
  sorting: z.array(z.object({ id: z.string(), desc: z.boolean() })).default([]),
})

type SavedViewState = z.infer<typeof savedViewState>

interface ColumnStateOptions {
  columns: MaybeRefOrGetter<ColumnDef<Record<string, unknown>>[]>
  defaultOrder: MaybeRefOrGetter<DefaultColumnOrder | null>
  teamId: MaybeRefOrGetter<number | null>
  sourceId: MaybeRefOrGetter<string>
  source: MaybeRefOrGetter<Pick<Source, 'source_type'> | null | undefined>
  timestampField: MaybeRefOrGetter<string>
  severityField: MaybeRefOrGetter<string>
  // With a seed, the state is the working copy of that view, stored under its
  // own key; the layout with no view stays untouched.
  seed?: MaybeRefOrGetter<TableViewSeed | null | undefined>
  // The table's sort. Seeded and stored with the working copy only.
  sorting?: Ref<SortingState>
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
  const seed = computed(() => toValue(options.seed) ?? null)
  const seedIdentity = computed(() => seed.value ? `${seed.value.viewId}:${seed.value.revision}` : null)
  const scopeSuffix = computed(() => {
    const teamId = toValue(options.teamId)
    const sourceId = toValue(options.sourceId)
    return teamId == null || !sourceId ? null : `${teamId}-${sourceId}`
  })
  const tableStateKey = computed(() => scopeSuffix.value && `logchef-tableState-${scopeSuffix.value}`)
  // A shared layout has no key: it reads and writes no stored state.
  const storageKey = computed(() => {
    if (!seed.value) return tableStateKey.value
    return seed.value.viewId === null ? null : scopeSuffix.value && `logchef-viewState-${scopeSuffix.value}`
  })
  // The view's working copy while a result shows none of its columns; the
  // table shows the automatic layout for that result meanwhile.
  const stashedViewLayout = shallowRef<TableColumnLayout | null>(null)

  function loadState(key = storageKey.value): SavedColumnState | null {
    if (!key) return null
    try {
      const stored = localStorage.getItem(key)
      if (!stored) return null
      const saved = savedColumnState.parse(JSON.parse(stored))
      if (saved.sizingVersion !== COLUMN_SIZING_VERSION) {
        saved.columnSizing = {}
        saved.sizingVersion = COLUMN_SIZING_VERSION
        try {
          localStorage.setItem(key, JSON.stringify(saved))
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
    return putTimestampFirst(order, toValue(options.timestampField))
  }

  function setColumnOrder(updater: Updater<string[]>) {
    orderSource.value = 'explicit'
    columnOrder.value = timestampFirst(typeof updater === 'function' ? updater(columnOrder.value) : updater)
  }

  function initializeState(mode: 'restore' | 'reset' | 'reconcile', saved = mode === 'restore' ? loadState() : null) {
    const ignoreSavedState = mode === 'reset'
    const columns = toValue(options.columns)
    if (columns.length === 0 && mode === 'reconcile') return

    const defaults = defaultOrder.value
    const ids = defaults?.order ?? []
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

  function currentLayout(): TableColumnLayout {
    return {
      columnOrder: [...columnOrder.value],
      columnSizing: { ...columnSizing.value },
      columnVisibility: { ...columnVisibility.value },
    }
  }

  function applyLayout(layout: TableColumnLayout) {
    if (JSON.stringify(columnOrder.value) !== JSON.stringify(layout.columnOrder)) columnOrder.value = layout.columnOrder
    if (JSON.stringify(columnSizing.value) !== JSON.stringify(layout.columnSizing)) columnSizing.value = layout.columnSizing
    if (JSON.stringify(columnVisibility.value) !== JSON.stringify(layout.columnVisibility)) columnVisibility.value = layout.columnVisibility
  }

  function loadViewState(): SavedViewState | null {
    if (!storageKey.value) return null
    try {
      const stored = localStorage.getItem(storageKey.value)
      return stored ? savedViewState.parse(JSON.parse(stored)) : null
    } catch (error) {
      console.error('Error loading view state from localStorage:', error)
      return null
    }
  }

  // Restores the working copy when it belongs to this view and revision, and
  // otherwise seeds from the view. A view that says nothing about columns
  // starts from the layout with no view.
  function restoreViewState(view: TableViewSeed, useStored: boolean) {
    stashedViewLayout.value = null
    const saved = useStored ? loadViewState() : null
    if (saved && saved.viewId === view.viewId && saved.revision === view.revision) {
      applyLayout({ columnOrder: saved.columnOrder, columnSizing: saved.columnSizing, columnVisibility: saved.columnVisibility })
      if (options.sorting) options.sorting.value = saved.sorting
    } else {
      if (options.sorting && view.definition.sort) options.sorting.value = seedSorting(view.definition.sort)
      else if (options.sorting && !options.sorting.value.length) {
        options.sorting.value = [{ id: toValue(options.timestampField), desc: true }]
      }
      if (!view.definition.columns) {
        initializeState('restore', loadState(tableStateKey.value))
        reconcileViewState(view)
        return
      }
      applyLayout(seedColumnLayout(view.definition.columns, defaultOrder.value?.order ?? columnIds.value,
        toValue(options.timestampField)))
    }
    orderSource.value = 'explicit'
    visibilityVersion = COLUMN_VISIBILITY_VERSION
    isReadyToPersist.value = true
    reconcileViewState(view)
  }

  // Merges a new result into the working copy. A view that sets columns hides
  // every result column it does not name.
  function reconcileViewState(view: TableViewSeed) {
    if (!toValue(options.columns).length) return
    if (!view.definition.columns) {
      initializeState('reconcile')
      return
    }
    if (stashedViewLayout.value) {
      applyLayout(stashedViewLayout.value)
      stashedViewLayout.value = null
    }
    const hidden = columnIds.value.filter(id => columnVisibility.value[id] === undefined)
    if (hidden.length) columnVisibility.value = { ...columnVisibility.value, ...Object.fromEntries(hidden.map(id => [id, false])) }
    initializeState('reconcile')
    const unordered = columnIds.value.filter(id => !columnOrder.value.includes(id))
    if (unordered.length) columnOrder.value = timestampFirst([...columnOrder.value, ...unordered])
    if (layoutHasResultColumn(currentLayout(), columnIds.value)) return
    stashedViewLayout.value = currentLayout()
    applyLayout({
      columnOrder: timestampFirst(defaultOrder.value?.order ?? columnIds.value),
      columnSizing: {},
      columnVisibility: resolveColumnVisibility(toValue(options.columns), toValue(options.source), {},
        toValue(options.timestampField), toValue(options.severityField)),
    })
  }

  // The key and view of the layout on screen. A save that Vue runs after the
  // key or seed changed, but before the new layout is restored, still belongs
  // to the old layout.
  let applied: { key: string | null; view: TableViewSeed | null } = { key: null, view: null }
  let pendingWrite: { key: string; state: SavedColumnState | SavedViewState } | null = null
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
    seedIdentity: seedIdentity.value,
    sourceType: toValue(options.source)?.source_type,
    timestampField: toValue(options.timestampField),
    severityField: toValue(options.severityField),
  }), (current, previous) => {
    flushPendingWrite()
    const switched = current.storageKey !== previous?.storageKey || current.seedIdentity !== previous?.seedIdentity
    if (switched) {
      orderSource.value = 'pending'
      columnOrder.value = []
      columnClasses.value = {}
    }
    const view = seed.value
    if (view) {
      if (switched) restoreViewState(view, true)
      else reconcileViewState(view)
    } else {
      stashedViewLayout.value = null
      initializeState(!previous || switched ? 'restore' : 'reconcile')
    }
    applied = { key: current.storageKey, view }
  }, { immediate: true })

  watch([columnOrder, columnClasses, orderSource, columnSizing, columnVisibility, () => seed.value && options.sorting?.value], () => {
    const { key, view } = applied
    if (!key || !isReadyToPersist.value || !toValue(options.columns).length) return
    if (view && stashedViewLayout.value) return
    if (saveTimer !== null) clearTimeout(saveTimer)
    // A shared layout has no key, so a seed here always has a view id.
    pendingWrite = view?.viewId != null ? {
      key,
      state: {
        viewId: view.viewId,
        revision: view.revision,
        ...currentLayout(),
        sorting: (options.sorting?.value ?? []).map(({ id, desc }) => ({ id, desc })),
      },
    } : {
      key,
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

  // With a view, a reset discards the working copy and seeds from the view.
  function resetTableStateToDefaults() {
    flushPendingWrite()
    if (storageKey.value) localStorage.removeItem(storageKey.value)
    if (!toValue(options.columns).length) return
    if (seed.value) {
      restoreViewState(seed.value, false)
      return
    }
    initializeState('reset')
    isReadyToPersist.value = true
  }

  // The working layout of the active view as definition parts, for comparing
  // with the view and for saving. Null with no view.
  const workingLayout = computed(() => seed.value
    ? tableLayoutToDefinition(stashedViewLayout.value ?? {
      columnOrder: columnOrder.value,
      columnSizing: columnSizing.value,
      columnVisibility: columnVisibility.value,
    }, options.sorting?.value ?? [])
    : null)

  return { columnOrder, columnSizing, columnVisibility, setColumnOrder, resetTableStateToDefaults, workingLayout }
}
