<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter, onBeforeRouteLeave } from "vue-router";
import {
  ArrowLeft,
  LayoutDashboard,
  Pencil,
  Trash2,
  Plus,
  Save,
  X,
  GripVertical,
} from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import DashboardToolbar from "./components/DashboardToolbar.vue";
import DashboardPanel from "./components/DashboardPanel.vue";
import PanelEditorSheet from "./components/PanelEditorSheet.vue";
import { useDashboardsStore } from "@/stores/dashboards";
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
  type PanelSize,
  type GridGeometry,
} from "@/utils/dashboardPanels";
import type { DashboardPanel as PanelModel, DashboardLayoutItem } from "@/api/dashboards";

const route = useRoute();
const router = useRouter();
const store = useDashboardsStore();

// Grid geometry. 12 columns; each layout row is ROW_HEIGHT px tall.
const ROW_HEIGHT_PX = 80;
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

// --- Panel editor sheet -----------------------------------------------------
const editorOpen = ref(false);
const editingPanel = ref<PanelModel | null>(null);

function openAddPanel() {
  editingPanel.value = null;
  editorOpen.value = true;
}
function openEditPanel(panel: PanelModel) {
  editingPanel.value = panel;
  editorOpen.value = true;
}
function onPanelSave(panel: PanelModel) {
  store.upsertDraftPanel(panel);
}
function removePanel(id: string) {
  store.removeDraftPanel(id);
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

watch(() => store.refreshIntervalMs, setupRefreshTimer);

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
</script>

<template>
  <div class="mx-auto w-full max-w-[1600px] px-4 py-4">
    <!-- Header -->
    <div class="mb-4 flex items-start justify-between gap-4 flex-wrap">
      <div class="min-w-0">
        <div class="flex items-center gap-2">
          <Button variant="ghost" size="sm" class="h-7 px-2 -ml-2" @click="goBack">
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
        backgroundSize: `${100 / 12}% ${ROW_STRIDE}px`,
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
          <GripVertical class="h-3.5 w-3.5 opacity-60" />
          <span class="dash-item__title">{{ panelById.get(item.id)?.title || "Panel" }}</span>
          <span class="dash-item__actions">
            <button class="dash-item__btn" title="Edit panel" @pointerdown.stop @click="openEditPanel(panelById.get(item.id)!)">
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
            :height-px="panelPixelHeight(item.h) - 34"
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

      <!-- Ghost add-panel tile -->
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
    <div
      v-else-if="dashboard && placements.length === 0"
      class="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed py-16 text-center"
    >
      <p class="text-sm text-muted-foreground">This dashboard has no panels yet.</p>
      <Button v-if="canEdit" variant="outline" size="sm" class="gap-1.5" @click="startEdit">
        <Pencil class="h-4 w-4" />
        Edit to add panels
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

    <!-- Panel editor side sheet -->
    <PanelEditorSheet v-model:open="editorOpen" :panel="editingPanel" @save="onPanelSave" />
  </div>
</template>

<style scoped>
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
  border-radius: 0.6rem;
  /* Dot-grid: one dot at every column/row corner. */
  background-image: radial-gradient(
    circle at 1px 1px,
    color-mix(in srgb, var(--muted-foreground) 28%, transparent) 1.5px,
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
}
.dash-item--animate {
  transition: transform 0.16s ease, width 0.16s ease, height 0.16s ease;
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
  gap: 0.35rem;
  height: 30px;
  padding: 0 0.35rem 0 0.5rem;
  border-bottom: 1px solid var(--border);
  cursor: grab;
  user-select: none;
  background: color-mix(in srgb, var(--muted) 40%, transparent);
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
  gap: 0.1rem;
}
.dash-item__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.4rem;
  height: 1.4rem;
  border-radius: 0.3rem;
  color: var(--muted-foreground);
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
.dash-addtile {
  position: absolute;
  top: 0;
  left: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  border: 2px dashed var(--border);
  border-radius: 0.6rem;
  color: var(--muted-foreground);
  transition: border-color 0.12s ease, color 0.12s ease, background 0.12s ease;
}
.dash-addtile:hover {
  border-color: color-mix(in srgb, var(--primary) 60%, transparent);
  color: var(--foreground);
  background: color-mix(in srgb, var(--primary) 5%, transparent);
}
</style>
