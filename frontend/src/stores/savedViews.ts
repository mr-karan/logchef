import { defineStore, storeToRefs } from "pinia";
import { computed, nextTick, ref, shallowRef, watch } from "vue";
import { z } from "zod";
import { savedViewsApi, type SavedView, type SavedViewList, type SavedViewScope } from "@/api/savedViews";
import { getErrorType, isAbortError, showErrorToast } from "@/api/error-handler";
import { useActiveSavedViewStore } from "@/stores/activeSavedView";
import {
  tableLayoutToDefinition,
  withoutColumns,
  type SavedViewColumn,
  type SavedViewDefinition,
  type SavedViewLayout,
  type SavedViewSort,
  type TableViewSeed,
} from "@/lib/savedViewLayout";

export type DisplayMode = NonNullable<SavedViewDefinition["display_mode"]>;

/** What happened to the view a location asked for. */
export type ViewEntry = "ready" | "unavailable" | "failed";

/** The parts a new view takes from the Explorer. */
export interface SaveViewParts {
  columns: boolean;
  sort: boolean;
  displayMode: boolean;
  filter: boolean;
  timeRange: boolean;
}

/** What the Explorer shows besides the table layout. Null where a view cannot hold it. */
export interface ShownExplorerState {
  displayMode: DisplayMode;
  filter: string | null;
  timeRange: string | null;
}

const SCOPE_ORDER: Record<SavedViewScope, number> = { personal: 0, team: 1, source: 2 };

// A share's layout as the server stored it. Shares made before the server
// validated layouts may hold anything under the key.
const sharedLayoutSchema = z.object({
  version: z.literal(1),
  columns: z.array(z.object({ name: z.string().min(1), width: z.number().optional() })).min(1).optional(),
  sort: z.array(z.object({ column: z.string().min(1), desc: z.boolean() })).min(1).optional(),
  display_mode: z.enum(["table", "compact", "json"]).optional(),
});

const displayModeState = z.object({
  viewId: z.number(),
  revision: z.number(),
  displayMode: z.enum(["table", "compact", "json"]),
});

// The table's persisted layout: useColumnState's working copy of a view, or
// its layout with no view. Only the fields that make up a layout are read.
const storedTableState = z.object({
  viewId: z.number().optional(),
  revision: z.number().optional(),
  columnOrder: z.array(z.string()).default([]),
  columnSizing: z.record(z.string(), z.number()).default({}),
  columnVisibility: z.record(z.string(), z.boolean()).default({}),
  sorting: z.array(z.object({ id: z.string(), desc: z.boolean() })).default([]),
});

function readStorage(key: string): unknown {
  try {
    const stored = localStorage.getItem(key);
    return stored === null ? undefined : JSON.parse(stored);
  } catch {
    return undefined;
  }
}

function writeStorage(key: string, value: unknown) {
  try {
    if (value === undefined) localStorage.removeItem(key);
    else localStorage.setItem(key, JSON.stringify(value));
  } catch (error) {
    console.error("Error saving view state to localStorage:", error);
  }
}

function sameColumns(a: SavedViewColumn[] | undefined, b: SavedViewColumn[] | undefined) {
  const key = (columns: SavedViewColumn[] | undefined) => (columns ?? []).map((c) => `${c.name}\u0000${c.width ?? ""}`).join("\u0001");
  return key(a) === key(b);
}

function sameSort(a: SavedViewSort[] | undefined, b: SavedViewSort[] | undefined) {
  const key = (sort: SavedViewSort[] | undefined) => (sort ?? []).map((s) => `${s.column}\u0000${s.desc}`).join("\u0001");
  return key(a) === key(b);
}

function byScopeThenName(a: SavedView, b: SavedView) {
  return SCOPE_ORDER[a.scope] - SCOPE_ORDER[b.scope] || (a.name < b.name ? -1 : a.name > b.name ? 1 : a.id - b.id);
}

/**
 * The saved views of the Explorer's team and source, the active view and its
 * working copy outside the table. The table's own working copy lives in
 * useColumnState; this store keeps the display mode and the last layout the
 * table reported.
 */
export const useSavedViewsStore = defineStore("savedViews", () => {
  const active = useActiveSavedViewStore();
  const { scope, viewId: activeViewId } = storeToRefs(active);
  const views = ref<SavedView[]>([]);
  // The view the server's default rules pick for the caller.
  const defaultViewId = ref<number | null>(null);
  const workingDisplayMode = ref<DisplayMode | null>(null);
  // Set when a save was refused because someone saved the view after it loaded.
  const staleSave = ref(false);
  // The last layout the table reported, and the view it belongs to.
  const tableLayout = shallowRef<{ viewId: number | null; layout: SavedViewLayout } | null>(null);
  // Bumped by Reset to view, so a mounted table discards its working copy.
  const layoutResetToken = ref(0);
  // Bumped when this store changes the table's stored layout.
  const storageVersion = ref(0);
  // The layout of the share the Explorer opened, held in memory with no view
  // id. Its revision counts shared layouts, so the table seeds from each one.
  const sharedSeed = shallowRef<TableViewSeed | null>(null);
  let sharedLayoutCount = 0;

  const suffix = computed(() => (scope.value ? `${scope.value.teamId}-${scope.value.sourceId}` : null));
  const activeView = computed(() => views.value.find((v) => v.id === activeViewId.value) ?? null);
  const activeSeed = computed<TableViewSeed | null>(() =>
    activeView.value ? { viewId: activeView.value.id, revision: activeView.value.revision, definition: activeView.value.definition } : sharedSeed.value,
  );
  const sharedLayout = computed(() => sharedSeed.value?.definition ?? null);
  const viewSetsDisplayMode = computed(() => activeSeed.value?.definition.display_mode !== undefined);
  /** The display mode the active view imposes, or null when the user preference applies. */
  const displayMode = computed(() => (viewSetsDisplayMode.value ? workingDisplayMode.value : null));
  // The layout the table last reported, or the one it stored when no table
  // is mounted, as in compact or JSON mode after a reload.
  const currentLayout = computed(() => {
    if (tableLayout.value && tableLayout.value.viewId === activeViewId.value) return tableLayout.value.layout;
    void storageVersion.value;
    return storedLayout(activeSeed.value);
  });

  function storedLayout(seed: TableViewSeed | null): SavedViewLayout | null {
    // A shared layout is never stored.
    if (!suffix.value || seed?.viewId === null) return null;
    const parsed = storedTableState.safeParse(readStorage(`logchef-${seed ? "viewState" : "tableState"}-${suffix.value}`));
    if (!parsed.success) return null;
    if (seed && (parsed.data.viewId !== seed.viewId || parsed.data.revision !== seed.revision)) return null;
    const { columnOrder, columnSizing, columnVisibility, sorting } = parsed.data;
    return tableLayoutToDefinition({ columnOrder, columnSizing, columnVisibility }, seed ? sorting : []);
  }

  /** The working copy differs from the active view in a part the view sets. */
  const isModified = computed(() => {
    const view = activeView.value;
    if (!view) return false;
    const { columns, sort, display_mode } = view.definition;
    const layout = currentLayout.value;
    if (layout && columns && layout.columns && !sameColumns(layout.columns, columns)) return true;
    if (layout && sort && layout.sort && !sameSort(layout.sort, sort)) return true;
    return display_mode !== undefined && workingDisplayMode.value !== display_mode;
  });

  // The display mode working copy follows the active view and its revision.
  watch(activeSeed, (seed) => {
    if (!seed || seed.definition.display_mode === undefined || !suffix.value) {
      workingDisplayMode.value = null;
      return;
    }
    const stored = displayModeState.safeParse(readStorage(`logchef-viewDisplayMode-${suffix.value}`));
    workingDisplayMode.value = stored.success && stored.data.viewId === seed.viewId && stored.data.revision === seed.revision
      ? stored.data.displayMode
      : seed.definition.display_mode;
  }, { immediate: true, flush: "sync" });

  /** Loads the views of a team and source. A failure other than an abort returns null. */
  async function load(teamId: number, sourceId: number): Promise<SavedViewList | null> {
    try {
      const response = await savedViewsApi.list(teamId, sourceId, { suppressErrorToast: true, suppressForbiddenRedirect: true });
      return response.data ?? null;
    } catch (error) {
      if (isAbortError(error)) throw error;
      console.error("error loading saved views", error);
      return null;
    }
  }

  function rememberedKey() {
    return suffix.value && `logchef-activeView-${suffix.value}`;
  }

  /**
   * Shows a location's views. The view the URL names wins when the caller
   * can read it. Then the layout of the share the URL names, held in memory
   * only. Otherwise the browser's remembered choice applies, where a
   * remembered No view stops the search, and then the default view.
   */
  function enter(
    teamId: number,
    sourceId: number | null,
    list: SavedViewList | null,
    requestedViewId: number | null,
    shareLayout?: unknown,
  ): ViewEntry {
    scope.value = sourceId === null ? null : { teamId, sourceId };
    views.value = list ? [...list.views] : [];
    defaultViewId.value = list?.default_view_id ?? null;
    tableLayout.value = null;
    sharedSeed.value = null;
    if (sourceId === null) {
      activeViewId.value = null;
      return "ready";
    }
    const shared = parseSharedLayout(shareLayout);
    const useShared = () => {
      activeViewId.value = null;
      sharedSeed.value = shared && { viewId: null, revision: ++sharedLayoutCount, definition: shared };
    };
    if (!list) {
      useShared();
      return "failed";
    }
    if (requestedViewId !== null) {
      if (views.value.some((v) => v.id === requestedViewId)) {
        activeViewId.value = requestedViewId;
        return "ready";
      }
      useShared();
      return "unavailable";
    }
    if (shared) {
      useShared();
      return "ready";
    }
    const key = rememberedKey();
    const remembered = key ? readStorage(key) : undefined;
    const listed = (id: unknown) => (typeof id === "number" && views.value.some((v) => v.id === id) ? id : null);
    activeViewId.value = remembered === null ? null : listed(remembered) ?? listed(defaultViewId.value);
    return "ready";
  }

  function parseSharedLayout(layout: unknown): SavedViewDefinition | null {
    if (layout === undefined || layout === null) return null;
    const parsed = sharedLayoutSchema.safeParse(layout);
    if (!parsed.success) console.warn("ignoring an invalid shared layout", parsed.error);
    return parsed.success ? parsed.data : null;
  }

  /**
   * Makes a view, or no view, active and remembers the pick for this team and
   * source. A pick ends a shared layout. Leaving a view or a shared layout for
   * no view discards the stored layout, so the table shows the default one.
   */
  function pick(viewId: number | null) {
    if (viewId === null && activeSeed.value && suffix.value) {
      writeStorage(`logchef-tableState-${suffix.value}`, undefined);
      storageVersion.value++;
    }
    sharedSeed.value = null;
    activeViewId.value = viewId;
    tableLayout.value = null;
    const key = rememberedKey();
    if (key) writeStorage(key, viewId);
  }

  function reportTableLayout(viewId: number | null, layout: SavedViewLayout) {
    tableLayout.value = { viewId, layout };
    const shared = sharedSeed.value?.definition;
    if (viewId === null && shared) {
      reviseSharedLayout({
        ...(shared.columns && layout.columns ? { columns: layout.columns } : {}),
        ...(shared.sort && layout.sort ? { sort: layout.sort } : {}),
      });
    }
  }

  // A shared layout has no stored working copy, and the table remounts for
  // every result. The seed takes the edits, so the next table starts from
  // them; its revision stays, so the mounted table does not seed again.
  function reviseSharedLayout(change: Partial<SavedViewDefinition>) {
    const seed = sharedSeed.value;
    if (!seed) return;
    const definition = { ...seed.definition, ...change };
    if (JSON.stringify(definition) !== JSON.stringify(seed.definition)) sharedSeed.value = { ...seed, definition };
  }

  function setDisplayMode(mode: DisplayMode) {
    const seed = activeSeed.value;
    if (!seed || !viewSetsDisplayMode.value || !suffix.value) return;
    workingDisplayMode.value = mode;
    if (seed.viewId === null) {
      reviseSharedLayout({ display_mode: mode });
      return;
    }
    writeStorage(`logchef-viewDisplayMode-${suffix.value}`, { viewId: seed.viewId, revision: seed.revision, displayMode: mode });
  }

  /** The definition a new view takes from the working layout and what the Explorer shows. */
  function newDefinition(parts: SaveViewParts, shown: ShownExplorerState): SavedViewDefinition {
    const layout = currentLayout.value;
    const definition = activeSeed.value?.definition;
    const columns = layout?.columns ?? definition?.columns;
    const sort = layout?.sort ?? definition?.sort;
    return {
      version: 1,
      ...(parts.columns && columns ? { columns } : {}),
      ...(parts.sort && sort ? { sort } : {}),
      ...(parts.displayMode ? { display_mode: shown.displayMode } : {}),
      ...(parts.filter && shown.filter !== null ? { filter: shown.filter } : {}),
      ...(parts.timeRange && shown.timeRange !== null ? { time_range: shown.timeRange } : {}),
    };
  }

  /** The working layout a share carries: its columns, sort and display mode, never a query part. */
  function shareLayout(displayMode: DisplayMode): SavedViewDefinition {
    return newDefinition(
      { columns: true, sort: true, displayMode: true, filter: false, timeRange: false },
      { displayMode, filter: null, timeRange: null },
    );
  }

  function upsert(view: SavedView) {
    views.value = [...views.value.filter((v) => v.id !== view.id), view].sort(byScopeThenName);
  }

  /** Creates a view. Returns null on failure, which the API client reports. */
  async function saveAsNew(input: { name: string; description: string; scope: SavedViewScope; definition: SavedViewDefinition }): Promise<SavedView | null> {
    const target = scope.value;
    if (!target) return null;
    try {
      const created = (await savedViewsApi.create(target.teamId, target.sourceId, input)).data;
      if (!created || scope.value !== target) return null;
      upsert(created);
      return created;
    } catch (error) {
      console.error("error creating saved view", error);
      return null;
    }
  }

  /** Writes a view at the revision it loaded. Returns null on failure, after reporting it. */
  async function updateView(view: SavedView, changes: Partial<Pick<SavedView, "name" | "description" | "definition">>): Promise<SavedView | null> {
    const target = scope.value;
    if (!target) return null;
    try {
      const updated = (await savedViewsApi.update(target.teamId, target.sourceId, view.id, {
        name: changes.name ?? view.name,
        description: changes.description ?? view.description,
        definition: changes.definition ?? view.definition,
        revision: view.revision,
      })).data;
      if (!updated || scope.value !== target) return null;
      upsert(updated);
      return updated;
    } catch (error) {
      // A conflict on a save that keeps the view's own name can only be a
      // stale revision. A rename can also take a name in use, and the
      // server's message tells which.
      const keepsName = changes.name === undefined || changes.name === view.name;
      if (keepsName && getErrorType(error) === "ConflictError") staleSave.value = true;
      else showErrorToast(error);
      return null;
    }
  }

  /** Writes the working copy to the active view. Returns null on failure, which the API client reports. */
  async function updateActive(): Promise<SavedView | null> {
    const view = activeView.value;
    if (!view) return null;
    const layout = currentLayout.value;
    const { columns, sort, display_mode } = view.definition;
    const definition: SavedViewDefinition = {
      ...view.definition,
      ...(columns && layout?.columns ? { columns: layout.columns } : {}),
      ...(sort && layout?.sort ? { sort: layout.sort } : {}),
      ...(display_mode !== undefined && workingDisplayMode.value ? { display_mode: workingDisplayMode.value } : {}),
    };
    return updateView(view, { definition });
  }

  /** Rewrites a view's columns and sort without the names a result lacks. */
  function removeMissingColumns(view: SavedView, missing: readonly string[]): Promise<SavedView | null> {
    return updateView(view, { definition: withoutColumns(view.definition, missing) });
  }

  /**
   * Loads the list again for the shown team and source, keeping the active
   * view. The server picks the default, so a default change or a delete
   * reads it back. Returns whether the list loaded.
   */
  async function refresh(): Promise<boolean> {
    const target = scope.value;
    if (!target) return false;
    try {
      const list = await load(target.teamId, target.sourceId);
      if (!list || scope.value !== target) return false;
      views.value = [...list.views];
      defaultViewId.value = list.default_view_id;
      return true;
    } catch (error) {
      if (isAbortError(error)) return false;
      throw error;
    }
  }

  /** Deletes a view. Returns whether it was deleted; the API client reports a failure. */
  async function remove(view: SavedView): Promise<boolean> {
    const target = scope.value;
    if (!target) return false;
    try {
      await savedViewsApi.remove(target.teamId, target.sourceId, view.id);
    } catch (error) {
      console.error("error deleting saved view", error);
      return false;
    }
    if (scope.value !== target) return false;
    views.value = views.value.filter((v) => v.id !== view.id);
    await refresh();
    return scope.value === target;
  }

  /** Makes a view the default of its scope, or clears its flag. Returns whether it changed. */
  async function setDefault(view: SavedView, isDefault: boolean): Promise<boolean> {
    const target = scope.value;
    if (!target) return false;
    try {
      const request = isDefault ? savedViewsApi.setDefault : savedViewsApi.clearDefault;
      await request(target.teamId, target.sourceId, view.id);
    } catch (error) {
      console.error("error changing the default view", error);
      return false;
    }
    return refresh();
  }

  /** Discards the working copy of the active view and applies the view again. */
  function resetActive() {
    const seed = activeSeed.value;
    const key = suffix.value;
    if (!seed || !key) return;
    writeStorage(`logchef-viewDisplayMode-${key}`, undefined);
    workingDisplayMode.value = seed.definition.display_mode ?? null;
    tableLayout.value = null;
    layoutResetToken.value++;
    writeStorage(`logchef-viewState-${key}`, undefined);
    // A display mode change unmounts the table, which flushes its pending
    // write of the old working copy; remove the copy again after that.
    void nextTick(() => {
      writeStorage(`logchef-viewState-${key}`, undefined);
      storageVersion.value++;
    });
  }

  return {
    views,
    defaultViewId,
    activeViewId,
    activeView,
    activeSeed,
    sharedLayout,
    displayMode,
    isModified,
    staleSave,
    layoutResetToken,
    load,
    enter,
    pick,
    reportTableLayout,
    setDisplayMode,
    newDefinition,
    shareLayout,
    saveAsNew,
    updateView,
    updateActive,
    removeMissingColumns,
    refresh,
    remove,
    setDefault,
    resetActive,
  };
});
