<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter, onBeforeRouteLeave, onBeforeRouteUpdate } from "vue-router";
import {
  ArrowLeft,
  LayoutDashboard,
  Pencil,
  Trash2,
  Plus,
  Save,
  X,
  GripVertical,
  Database,
  ChevronDown,
} from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import DashboardToolbar from "./components/DashboardToolbar.vue";
import DashboardPanel from "./components/DashboardPanel.vue";
import PanelBuilderDrawer from "./components/PanelBuilderDrawer.vue";
import { useDashboardsStore, DEFAULT_DASHBOARD_CACHE_TTL_SECONDS } from "@/stores/dashboards";
import { useMetaStore } from "@/stores/meta";
import {
  normalizeDashboardLayout,
  cellRect,
  pointToCell,
  cellToMoveIndex,
  previewMoveLayout,
  snapResize,
  previewResizeLayout,
  addTileSlot,
  layoutRowCount,
  columnTrackWidth,
  type PanelSize,
  type GridGeometry,
} from "@/utils/dashboardPanels";
import type { DashboardPanel as PanelModel, DashboardLayoutItem } from "@/api/dashboards";

const route = useRoute();
const router = useRouter();
const store = useDashboardsStore();
const metaStore = useMetaStore();

// Grid geometry. 12 columns; each layout row is ROW_HEIGHT px tall.
const ROW_HEIGHT_PX = 60;
const GRID_GAP_PX = 12;
const ROW_STRIDE = ROW_HEIGHT_PX + GRID_GAP_PX;

const dashboardId = computed(() => Number(route.params.id));
const isLoading = computed(() => store.isLoadingOperation("loadDashboard"));
const isSaving = computed(() => store.isLoadingOperation("saveDashboard"));
const dashboard = computed(() => store.current);
const notFound = ref(false);

const isEditing = computed(() => store.isEditing);
const canEdit = computed(() => store.canEdit);

// In edit mode the grid renders from the working draft; otherwise from the live
// dashboard. Both go through the same normalizer for stable placements.
const activeBlob = computed(() => (store.isEditing ? store.editDraft : store.current?.panels));

const placements = computed(() => {
  const blob = activeBlob.value;
  if (!blob) return [];
  return normalizeDashboardLayout(blob.panels ?? [], blob.layout ?? []);
});

// Panel model lookup + draft-start ordering, used by the edit canvas.
const panelById = computed(() => {
  const m = new Map<string, PanelModel>();
  for (const p of placements.value) m.set(p.panel.id, p.panel);
  return m;
});
const baseOrder = computed<PanelSize[]>(() =>
  placements.value.map((p) => ({ id: p.panel.id, w: p.w, h: p.h }))
);
const baseLayout = computed<DashboardLayoutItem[]>(() =>
  placements.value.map((p) => ({ id: p.panel.id, x: p.x, y: p.y, w: p.w, h: p.h }))
);

// --- Panel builder drawer ----------------------------------------------------
// DashboardView owns editorOpen + editingPanelId; the drawer is draft-first
// (it reads/writes editDraft directly by id, see PanelBuilderDrawer.vue), so
// there's no panel object to pass in or a "save" event to listen for. Closing
// the drawer only exits panel-editing — the dashboard stays in edit mode.
const editorOpen = ref(false);
const editingPanelId = ref<string | null>(null);
// Tracks a panel created via "Add panel" so a cancel-while-pristine (no
// team/source chosen, title untouched) can drop the shell instead of leaving
// a dead panel in the draft.
const pendingNewPanelId = ref<string | null>(null);

function openAddPanel() {
  const id = store.createDraftShell();
  if (!id) return;
  // Persist an explicit chart choice on new panels so a future change to the
  // "absent chart" default can't silently restyle already-saved panels.
  store.updateDraftPanel(id, { options: { chart: "line" } });
  pendingNewPanelId.value = id;
  editingPanelId.value = id;
  editorOpen.value = true;
}
function openEditPanel(id: string) {
  pendingNewPanelId.value = null;
  editingPanelId.value = id;
  editorOpen.value = true;
}
function handleEditorOpenChange(open: boolean) {
  if (open) {
    editorOpen.value = true;
    return;
  }
  const id = editingPanelId.value;
  if (id && pendingNewPanelId.value === id) {
    const p = store.editDraft?.panels.find((pp) => pp.id === id);
    // A newly-added panel that never got a source can't execute or be saved
    // (the blob validator rejects source_id <= 0). Drop it on close instead of
    // leaving a half-configured shell that dead-ends Save behind a validation
    // alert with no obvious way out (#120 A10).
    const incomplete = !p || !(p.team_id > 0) || !(p.source_id > 0);
    if (incomplete) store.removeDraftPanel(id);
  }
  pendingNewPanelId.value = null;
  editingPanelId.value = null;
  editorOpen.value = false;
}
function removePanel(id: string) {
  store.removeDraftPanel(id);
}

// --- Cache TTL (edit mode) ---------------------------------------------------
// Presets for the per-dashboard result-cache TTL, persisted in the panels blob
// as cache_ttl_seconds. Off (0) disables caching. When a dashboard sets no
// value, the effective TTL is the SERVER's advertised default — so the "Default"
// shown here is derived from the server policy (falling back to a constant only
// when the server advertises no policy). We never rewrite a dashboard's saved
// cache_ttl_seconds just because the current server would clamp it differently.
const CACHE_TTL_OPTIONS = [
  { label: "Off", seconds: 0 },
  { label: "1m", seconds: 60 },
  { label: "5m", seconds: 300 },
  { label: "10m", seconds: 600 },
  { label: "30m", seconds: 1800 },
  { label: "1h", seconds: 3600 },
];

const defaultCacheTtlSeconds = computed(
  () => metaStore.dashboardCachePolicy?.default_ttl_seconds ?? DEFAULT_DASHBOARD_CACHE_TTL_SECONDS
);
const cacheTtlSeconds = computed(
  () => store.editDraft?.cache_ttl_seconds ?? defaultCacheTtlSeconds.value
);
const cacheTtlLabel = computed(() => {
  const match = CACHE_TTL_OPTIONS.find((o) => o.seconds === cacheTtlSeconds.value)?.label ?? "Custom";
  // When the dashboard hasn't set its own TTL, annotate that this value is the
  // server default rather than an explicit per-dashboard choice.
  return store.editDraft?.cache_ttl_seconds == null ? `${match} (default)` : match;
});

function selectCacheTtl(seconds: number) {
  store.setDraftCacheTtl(seconds);
}

// --- Edit-mode grid canvas (pointer-driven direct manipulation) -------------
const gridEl = ref<HTMLElement | null>(null);
const containerWidth = ref(1200);
let resizeObserver: ResizeObserver | null = null;

// Geometry used purely for RENDERING placements to pixel rects (reactive to
// container width; left/top don't matter for rects).
const renderGeom = computed(() => ({
  width: containerWidth.value || 1200,
  gap: GRID_GAP_PX,
  rowHeight: ROW_HEIGHT_PX,
}));

interface DragState {
  kind: "move" | "resize";
  id: string;
  pointerId: number;
  originX: number;
  originY: number;
  // Canvas viewport origin captured at drag start (for pointToCell).
  canvasLeft: number;
  canvasTop: number;
  // Grab offset within the panel so it doesn't jump under the cursor (move).
  grabDX: number;
  grabDY: number;
  startW: number;
  startH: number;
  startLayout: DashboardLayoutItem[];
  startOrder: PanelSize[];
  // Live state
  previewLayout: DashboardLayoutItem[];
  visualLeft: number;
  visualTop: number;
  visualW: number;
  visualH: number;
  curW: number;
  curH: number;
}
const drag = ref<DragState | null>(null);

// The layout the canvas renders right now — the live preview while dragging,
// otherwise the packed draft layout.
const renderLayout = computed<DashboardLayoutItem[]>(() =>
  drag.value ? drag.value.previewLayout : baseLayout.value
);

function pointerGeom(d: DragState): GridGeometry {
  return {
    left: d.canvasLeft,
    top: d.canvasTop,
    width: containerWidth.value || 1200,
    gap: GRID_GAP_PX,
    rowHeight: ROW_HEIGHT_PX,
  };
}

// Ghost "+ Add panel" tile sits in the first free slot (hidden mid-drag).
const addTile = computed(() => (drag.value ? null : addTileSlot(baseOrder.value)));

// Canvas height must contain the tallest of the live layout and the add tile.
const canvasRows = computed(() => {
  const rows = layoutRowCount(renderLayout.value);
  const tile = addTile.value;
  return Math.max(rows, tile ? tile.y + tile.h : 0, 1);
});
const canvasHeightPx = computed(() => canvasRows.value * ROW_STRIDE - GRID_GAP_PX);

// Dot-grid spacing in real px (column stride × row stride) so the dots line up
// exactly with the actual column/row tracks instead of a viewport-relative
// percentage that drifts out of sync with the grid as the canvas resizes.
const dotGridBackgroundSize = computed(() => {
  const colStride = columnTrackWidth(renderGeom.value) + GRID_GAP_PX;
  return `${colStride}px ${ROW_STRIDE}px`;
});

function rectStyle(item: DashboardLayoutItem) {
  const r = cellRect(item, renderGeom.value);
  return {
    transform: `translate(${r.left}px, ${r.top}px)`,
    width: `${r.width}px`,
    height: `${r.height}px`,
  };
}
function rectStylePx(r: { left: number; top: number; width: number; height: number }) {
  return {
    transform: `translate(${r.left}px, ${r.top}px)`,
    width: `${r.width}px`,
    height: `${r.height}px`,
  };
}

function panelPixelHeight(h: number): number {
  return h * ROW_HEIGHT_PX + (h - 1) * GRID_GAP_PX;
}

function beginDrag(kind: "move" | "resize", panelId: string, event: PointerEvent) {
  if (!isEditing.value || !gridEl.value) return;
  event.preventDefault();
  const rect = gridEl.value.getBoundingClientRect();
  const item = baseLayout.value.find((i) => i.id === panelId);
  if (!item) return;
  const pr = cellRect(item, renderGeom.value);
  drag.value = {
    kind,
    id: panelId,
    pointerId: event.pointerId,
    originX: event.clientX,
    originY: event.clientY,
    canvasLeft: rect.left,
    canvasTop: rect.top,
    grabDX: event.clientX - (rect.left + pr.left),
    grabDY: event.clientY - (rect.top + pr.top),
    startW: item.w,
    startH: item.h,
    startLayout: baseLayout.value,
    startOrder: baseOrder.value,
    previewLayout: baseLayout.value,
    visualLeft: pr.left,
    visualTop: pr.top,
    visualW: pr.width,
    visualH: pr.height,
    curW: item.w,
    curH: item.h,
  };
  window.addEventListener("pointermove", onPointerMove);
  window.addEventListener("pointerup", onPointerUp);
  window.addEventListener("keydown", onDragKey);
}

function onPointerMove(event: PointerEvent) {
  const d = drag.value;
  if (!d || event.pointerId !== d.pointerId) return;
  if (d.kind === "move") {
    d.visualLeft = event.clientX - d.canvasLeft - d.grabDX;
    d.visualTop = event.clientY - d.canvasTop - d.grabDY;
    const cell = pointToCell(event.clientX, event.clientY, pointerGeom(d));
    const insertIndex = cellToMoveIndex(d.startLayout, d.id, cell);
    d.previewLayout = previewMoveLayout(d.startOrder, d.id, insertIndex);
  } else {
    const dx = event.clientX - d.originX;
    const dy = event.clientY - d.originY;
    const { w, h } = snapResize(d.startW, d.startH, dx, dy, renderGeom.value);
    d.curW = w;
    d.curH = h;
    d.previewLayout = previewResizeLayout(d.startOrder, d.id, w, h);
  }
}

function onPointerUp() {
  const d = drag.value;
  teardownDrag();
  if (!d) return;
  if (d.kind === "move") {
    store.reorderDraftPanels(d.previewLayout.map((i) => i.id));
  } else if (d.curW !== d.startW || d.curH !== d.startH) {
    store.resizeDraftPanel(d.id, d.curW, d.curH);
  }
}

function onDragKey(event: KeyboardEvent) {
  if (event.key === "Escape") {
    teardownDrag(); // cancel: drop the preview, base layout re-renders untouched
  }
}

function teardownDrag() {
  drag.value = null;
  window.removeEventListener("pointermove", onPointerMove);
  window.removeEventListener("pointerup", onPointerUp);
  window.removeEventListener("keydown", onDragKey);
}

// Placeholder rect (the dragged panel's landing slot) while moving.
const movePlaceholder = computed(() => {
  const d = drag.value;
  if (!d || d.kind !== "move") return null;
  const item = d.previewLayout.find((i) => i.id === d.id);
  return item ? cellRect(item, renderGeom.value) : null;
});

// --- Edit lifecycle + dirty guard -------------------------------------------
function startEdit() {
  store.enterEdit();
}
function cancelEdit() {
  if (store.isDirty && !window.confirm("Discard unsaved changes to this dashboard?")) {
    return;
  }
  editorOpen.value = false;
  editingPanelId.value = null;
  pendingNewPanelId.value = null;
  store.cancelEdit();
}
async function saveEdit() {
  const result = await store.saveEdit();
  if (!result.success && result.error?.message) {
    window.alert(result.error.message);
  }
}

function goBack() {
  // The in-app Back control honors the same dirty guard as route navigation.
  if (store.isEditing && store.isDirty && !window.confirm("Discard unsaved changes and leave?")) {
    return;
  }
  router.push("/dashboards");
}

function panelOuterHeight(rowSpan: number): number {
  return rowSpan * ROW_HEIGHT_PX + (rowSpan - 1) * GRID_GAP_PX;
}

const updatedLabel = computed(() => {
  const ts = store.current?.updated_at;
  if (!ts) return "";
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleString();
});

const creatorLabel = computed(
  () => store.current?.created_by_name || store.current?.created_by_email || "Unknown"
);

// --- Auto-refresh timer (cleaned up on unmount and route leave) -------------
let refreshTimer: ReturnType<typeof setInterval> | null = null;

function clearRefreshTimer() {
  if (refreshTimer !== null) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
}

function setupRefreshTimer() {
  clearRefreshTimer();
  if (store.refreshIntervalMs > 0) {
    refreshTimer = setInterval(() => {
      void store.refreshAllPanels();
    }, store.refreshIntervalMs);
  }
}

// immediate so a refresh interval already set on the store (e.g. chosen before
// this dashboard mounted) starts ticking on mount, not only on the next change.
watch(() => store.refreshIntervalMs, setupRefreshTimer, { immediate: true });

async function load() {
  notFound.value = false;
  const result = await store.loadDashboard(dashboardId.value);
  if (!result.success || !store.current) {
    notFound.value = true;
  }
}

watch(dashboardId, () => {
  if (!Number.isNaN(dashboardId.value)) {
    void load();
  }
});

function observeWidth() {
  if (!gridEl.value) return;
  containerWidth.value = gridEl.value.clientWidth;
  resizeObserver = new ResizeObserver((entries) => {
    for (const e of entries) containerWidth.value = e.contentRect.width;
  });
  resizeObserver.observe(gridEl.value);
}

// Re-attach the width observer whenever the edit canvas mounts/unmounts.
watch(gridEl, (el) => {
  if (resizeObserver) {
    resizeObserver.disconnect();
    resizeObserver = null;
  }
  if (el) observeWidth();
});

onMounted(() => {
  if (!Number.isNaN(dashboardId.value)) {
    void load();
  } else {
    notFound.value = true;
  }
});

onBeforeUnmount(() => {
  clearRefreshTimer();
  teardownDrag();
  if (resizeObserver) resizeObserver.disconnect();
  store.clearCurrent();
});

onBeforeRouteLeave(() => {
  // Guard against leaving with unsaved edits (browser confirm; the in-app Back
  // control routes through goBack() which applies the same check).
  if (store.isEditing && store.isDirty) {
    if (!window.confirm("You have unsaved changes to this dashboard. Leave anyway?")) {
      return false;
    }
  }
  clearRefreshTimer();
});

// Param-only navigation (/dashboards/1 → /dashboards/2) reuses this component
// instance, so the previous dashboard's edit draft would otherwise ride along
// and Save could write #1's panels onto #2's id (#119 A1). Confirm on a dirty
// draft, then clear the store so the incoming load() starts from a clean slate.
onBeforeRouteUpdate((to, from) => {
  if (to.params.id === from.params.id) return true;
  if (store.isEditing && store.isDirty) {
    if (!window.confirm("You have unsaved changes to this dashboard. Leave anyway?")) {
      return false;
    }
  }
  store.clearCurrent();
  return true;
});
</script>

<template>
  <div class="mx-auto w-full max-w-[1600px] px-4 py-4">
    <!-- Header -->
    <div class="mb-4 flex items-start justify-between gap-4 flex-wrap">
      <div class="min-w-0">
        <div class="flex items-center gap-2">
          <Button variant="ghost" size="sm" class="h-8 px-2 -ml-2" @click="goBack">
            <ArrowLeft class="h-4 w-4" />
          </Button>
          <LayoutDashboard class="h-5 w-5 text-muted-foreground shrink-0" />
          <h1 class="text-lg font-semibold truncate">
            {{ dashboard?.name || (isLoading ? "Loading…" : "Dashboard") }}
          </h1>
          <span
            v-if="isEditing"
            class="rounded bg-amber-500/15 px-1.5 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400"
          >
            Editing{{ store.isDirty ? " · unsaved" : "" }}
          </span>
        </div>
        <p v-if="dashboard?.description" class="mt-1 text-sm text-muted-foreground">
          {{ dashboard.description }}
        </p>
        <p v-if="dashboard && !isEditing" class="mt-1 text-xs text-muted-foreground">
          Created by {{ creatorLabel }}<span v-if="updatedLabel"> · Updated {{ updatedLabel }}</span>
        </p>
        <p v-else-if="isEditing" class="mt-1 text-xs text-muted-foreground">
          Drag a panel by its header to move it · drag the bottom-right corner to resize.
        </p>
      </div>

      <div class="flex items-center gap-2 flex-wrap">
        <DashboardToolbar v-if="dashboard && !isEditing" />
        <template v-if="dashboard && !isEditing && canEdit">
          <Button variant="outline" size="sm" class="h-8 gap-1.5 text-xs" @click="startEdit">
            <Pencil class="h-3.5 w-3.5" />
            Edit
          </Button>
        </template>
        <template v-else-if="dashboard && isEditing">
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button
                variant="outline"
                size="sm"
                class="h-8 gap-1.5 text-xs"
                title="Per-dashboard result cache TTL"
              >
                <Database class="h-3.5 w-3.5" />
                <span>Cache: {{ cacheTtlLabel }}</span>
                <ChevronDown class="h-3 w-3 opacity-60" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="w-28">
              <DropdownMenuItem
                v-for="opt in CACHE_TTL_OPTIONS"
                :key="opt.seconds"
                class="text-xs"
                :class="{ 'font-semibold': opt.seconds === cacheTtlSeconds }"
                @click="selectCacheTtl(opt.seconds)"
              >
                {{ opt.label }}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant="outline" size="sm" class="h-8 gap-1.5 text-xs" @click="openAddPanel">
            <Plus class="h-3.5 w-3.5" />
            Add panel
          </Button>
          <Button variant="ghost" size="sm" class="h-8 gap-1.5 text-xs" :disabled="isSaving" @click="cancelEdit">
            <X class="h-3.5 w-3.5" />
            Cancel
          </Button>
          <Button
            size="sm"
            class="h-8 gap-1.5 text-xs"
            :disabled="isSaving || !store.isDirty"
            @click="saveEdit"
          >
            <Save class="h-3.5 w-3.5" />
            {{ isSaving ? "Saving…" : "Save" }}
          </Button>
        </template>
      </div>
    </div>

    <!-- Not found -->
    <div
      v-if="notFound"
      class="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed py-16 text-center"
    >
      <LayoutDashboard class="h-8 w-8 text-muted-foreground" />
      <p class="text-sm text-muted-foreground">Dashboard not found or you don't have access.</p>
      <Button variant="outline" size="sm" @click="router.push('/dashboards')">Back to dashboards</Button>
    </div>

    <!-- Edit-mode grid canvas: dot-grid background, absolute-positioned panels,
         pointer drag-to-move + corner resize, live placeholder, ghost add tile. -->
    <div
      v-else-if="dashboard && isEditing"
      ref="gridEl"
      class="dash-canvas"
      :style="{
        height: `${canvasHeightPx}px`,
        backgroundSize: dotGridBackgroundSize,
      }"
    >
      <!-- Move placeholder (drop slot) -->
      <div v-if="movePlaceholder" class="dash-placeholder" :style="rectStylePx(movePlaceholder)" />

      <!-- Panels -->
      <div
        v-for="item in renderLayout"
        :key="item.id"
        class="dash-item"
        :class="{
          'dash-item--dragging': drag && drag.id === item.id,
          'dash-item--animate': !drag || drag.id !== item.id,
        }"
        :style="
          drag && drag.kind === 'move' && drag.id === item.id
            ? rectStylePx({ left: drag.visualLeft, top: drag.visualTop, width: drag.visualW, height: drag.visualH })
            : rectStyle(item)
        "
      >
        <div
          class="dash-item__header"
          title="Drag to move"
          @pointerdown="beginDrag('move', item.id, $event)"
        >
          <GripVertical class="h-3.5 w-3.5 opacity-60 shrink-0" />
          <span class="dash-item__title">{{ panelById.get(item.id)?.title || "Panel" }}</span>
          <span class="dash-item__actions">
            <button class="dash-item__btn" title="Edit panel" @pointerdown.stop @click="openEditPanel(item.id)">
              <Pencil class="h-3.5 w-3.5" />
            </button>
            <button
              class="dash-item__btn dash-item__btn--danger"
              title="Remove panel"
              @pointerdown.stop
              @click="removePanel(item.id)"
            >
              <Trash2 class="h-3.5 w-3.5" />
            </button>
          </span>
        </div>
        <div class="dash-item__body">
          <DashboardPanel
            :panel="panelById.get(item.id)!"
            :height-px="panelPixelHeight(item.h) - 36"
            :chrome="false"
          />
        </div>
        <!-- SE resize handle -->
        <span
          class="dash-item__resize"
          title="Drag to resize"
          @pointerdown="beginDrag('resize', item.id, $event)"
        />
      </div>

      <!-- Ghost add-panel tile: fills the real remaining space in the bottom row -->
      <button
        v-if="addTile"
        class="dash-addtile"
        :style="rectStyle(addTile)"
        @click="openAddPanel"
      >
        <Plus class="h-5 w-5" />
        <span class="text-xs font-medium">Add panel</span>
      </button>
    </div>

    <!-- Empty dashboard (view mode) -->
    <div v-else-if="dashboard && placements.length === 0" class="dash-empty">
      <div class="dash-empty__icon">
        <LayoutDashboard class="h-7 w-7" />
      </div>
      <div>
        <p class="text-base font-semibold">This dashboard is empty</p>
        <p class="mx-auto mt-1 max-w-sm text-sm text-muted-foreground">
          Add a panel to start visualizing logs — pick a source, write a query, and choose how to chart it.
        </p>
      </div>
      <Button v-if="canEdit" size="sm" class="mt-1 gap-1.5" @click="startEdit">
        <Plus class="h-4 w-4" />
        Add your first panel
      </Button>
    </div>

    <!-- View-mode grid (CSS grid, static) -->
    <div
      v-else-if="dashboard"
      class="dash-grid"
      :style="{
        gridTemplateColumns: 'repeat(12, minmax(0, 1fr))',
        gridAutoRows: `${ROW_HEIGHT_PX}px`,
        gap: `${GRID_GAP_PX}px`,
      }"
    >
      <div
        v-for="p in placements"
        :key="p.panel.id"
        class="dash-cell"
        :style="{
          gridColumn: `${p.x + 1} / span ${p.w}`,
          gridRow: `${p.y + 1} / span ${p.h}`,
        }"
      >
        <DashboardPanel :panel="p.panel" :height-px="panelOuterHeight(p.h)" />
      </div>
    </div>

    <!-- Panel builder drawer -->
    <PanelBuilderDrawer :open="editorOpen" :panel-id="editingPanelId" @update:open="handleEditorOpenChange" />
  </div>
</template>

<style scoped>
.dash-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
  background: color-mix(in srgb, var(--muted) 12%, transparent);
  padding: 3.5rem 1.5rem;
  text-align: center;
}
.dash-empty__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 3.25rem;
  height: 3.25rem;
  border-radius: 9999px;
  background: color-mix(in srgb, var(--primary) 12%, transparent);
  color: var(--primary);
}
.dash-grid {
  display: grid;
  grid-auto-flow: row;
}
.dash-cell {
  position: relative;
  min-height: 0;
}

/* --- Edit-mode canvas ------------------------------------------------------ */
.dash-canvas {
  position: relative;
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
  /* Distinct surface from the page so "canvas" reads as its own zone in edit
     mode, matching the tinted panel headers. Note: no padding here — this
     element is both the positioned ancestor for absolutely-positioned panels
     and the ref used for pixel-geometry math (containerWidth, drag hit
     testing), so its content box must equal the canvas coordinate space. */
  background-color: color-mix(in srgb, var(--muted) 18%, var(--background));
  /* Dot-grid: one dot at every column/row corner, spaced to the real column
     and row stride (in px) so it reads as a uniform grid instead of a
     viewport-relative smear. */
  background-image: radial-gradient(
    circle at 1px 1px,
    color-mix(in srgb, var(--muted-foreground) 45%, transparent) 1.5px,
    transparent 0
  );
  background-position: 0 0;
}
.dash-item {
  position: absolute;
  top: 0;
  left: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 0.6rem;
  background: var(--card);
  will-change: transform;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.dash-item:hover {
  border-color: color-mix(in srgb, var(--foreground) 20%, var(--border));
  box-shadow: 0 2px 10px -4px rgb(0 0 0 / 0.18);
}
.dash-item--animate {
  transition: transform 0.16s ease, width 0.16s ease, height 0.16s ease, border-color 0.12s ease, box-shadow 0.12s ease;
}
.dash-item--dragging {
  z-index: 20;
  box-shadow: 0 12px 30px -8px rgb(0 0 0 / 0.35);
  scale: 1.01;
  cursor: grabbing;
  opacity: 0.95;
}
.dash-item__header {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  height: 32px;
  padding: 0 0.4rem 0 0.65rem;
  border-bottom: 1px solid var(--border);
  cursor: grab;
  user-select: none;
  /* Same tint as the view-mode panel header (DashboardPanel.vue) so a
     dashboard looks identical whether you're editing it or not. */
  background: color-mix(in srgb, var(--muted) 35%, transparent);
}
.dash-item__header:hover {
  background: color-mix(in srgb, var(--muted) 55%, transparent);
}
.dash-item__header:active {
  cursor: grabbing;
}
.dash-item__title {
  flex: 1;
  min-width: 0;
  font-size: 0.75rem;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dash-item__actions {
  display: inline-flex;
  align-items: center;
  gap: 0.15rem;
  padding-left: 0.4rem;
  margin-left: 0.15rem;
  border-left: 1px solid var(--border);
}
.dash-item__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.4rem;
  height: 1.4rem;
  border-radius: 0.3rem;
  color: var(--muted-foreground);
  cursor: pointer;
}
.dash-item__btn:hover {
  background: var(--accent);
  color: var(--foreground);
}
.dash-item__btn--danger:hover {
  background: color-mix(in srgb, var(--destructive) 15%, transparent);
  color: var(--destructive);
}
.dash-item__body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.dash-item__resize {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 16px;
  height: 16px;
  cursor: nwse-resize;
  background: linear-gradient(
    135deg,
    transparent 0 50%,
    color-mix(in srgb, var(--muted-foreground) 55%, transparent) 50% 100%
  );
  border-bottom-right-radius: 0.6rem;
  transition: background 0.12s ease;
}
.dash-item:hover .dash-item__resize,
.dash-item__resize:hover {
  background: linear-gradient(
    135deg,
    transparent 0 50%,
    color-mix(in srgb, var(--primary) 65%, transparent) 50% 100%
  );
}
.dash-placeholder {
  position: absolute;
  top: 0;
  left: 0;
  border: 2px dashed color-mix(in srgb, var(--primary) 70%, transparent);
  border-radius: 0.6rem;
  background: color-mix(in srgb, var(--primary) 8%, transparent);
  pointer-events: none;
  z-index: 1;
}
/* Add-tile deliberately uses a SOLID border + flat fill (vs. the dashed,
   primary-tinted move placeholder above) so the two states — "drop here"
   during a drag vs. "click to add" at rest — read as distinct affordances
   instead of one shared dashed idiom. */
.dash-addtile {
  position: absolute;
  top: 0;
  left: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  border: 1px solid var(--border);
  border-radius: 0.6rem;
  background: color-mix(in srgb, var(--muted) 25%, transparent);
  color: var(--muted-foreground);
  cursor: pointer;
  transition: border-color 0.12s ease, color 0.12s ease, background 0.12s ease;
}
.dash-addtile:hover {
  border-color: color-mix(in srgb, var(--primary) 55%, transparent);
  color: var(--foreground);
  background: color-mix(in srgb, var(--primary) 8%, transparent);
}
</style>
