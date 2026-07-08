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
  Move,
  Maximize2,
} from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import DashboardToolbar from "./components/DashboardToolbar.vue";
import DashboardPanel from "./components/DashboardPanel.vue";
import PanelEditorSheet from "./components/PanelEditorSheet.vue";
import { useDashboardsStore } from "@/stores/dashboards";
import {
  normalizeDashboardLayout,
  reorderByTarget,
  ALLOWED_PANEL_WIDTHS,
  MIN_PANEL_HEIGHT,
  MAX_PANEL_HEIGHT,
} from "@/utils/dashboardPanels";
import type { DashboardPanel as PanelModel } from "@/api/dashboards";

const route = useRoute();
const router = useRouter();
const store = useDashboardsStore();

// Grid geometry. 12 columns; each layout row is ROW_HEIGHT px tall.
const ROW_HEIGHT_PX = 80;
const GRID_GAP_PX = 12;

const dashboardId = computed(() => Number(route.params.id));
const isLoading = computed(() => store.isLoadingOperation("loadDashboard"));
const isSaving = computed(() => store.isLoadingOperation("saveDashboard"));
const dashboard = computed(() => store.current);
const notFound = ref(false);

const isEditing = computed(() => store.isEditing);
const canEdit = computed(() => store.canEdit);
const heightOptions = Array.from(
  { length: MAX_PANEL_HEIGHT - MIN_PANEL_HEIGHT + 1 },
  (_, i) => MIN_PANEL_HEIGHT + i
);

// In edit mode the grid renders from the working draft; otherwise from the live
// dashboard. Both go through the same normalizer for stable placements.
const activeBlob = computed(() => (store.isEditing ? store.editDraft : store.current?.panels));

const placements = computed(() => {
  const blob = activeBlob.value;
  if (!blob) return [];
  return normalizeDashboardLayout(blob.panels ?? [], blob.layout ?? []);
});

const orderedIds = computed(() => placements.value.map((p) => p.panel.id));

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
function setWidth(id: string, w: number, currentH: number) {
  store.resizeDraftPanel(id, w, currentH);
}
function setHeight(id: string, currentW: number, h: number) {
  store.resizeDraftPanel(id, currentW, h);
}

// --- Native HTML5 drag reorder ----------------------------------------------
const draggedId = ref<string | null>(null);
const dragOverId = ref<string | null>(null);

function onDragStart(id: string, event: DragEvent) {
  draggedId.value = id;
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", id);
  }
}
function onDragOver(id: string) {
  if (draggedId.value && draggedId.value !== id) {
    dragOverId.value = id;
  }
}
function onDrop(targetId: string) {
  const dragged = draggedId.value;
  draggedId.value = null;
  dragOverId.value = null;
  if (!dragged || dragged === targetId) return;
  store.reorderDraftPanels(reorderByTarget(orderedIds.value, dragged, targetId));
}
function onDragEnd() {
  draggedId.value = null;
  dragOverId.value = null;
}

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

onMounted(() => {
  if (!Number.isNaN(dashboardId.value)) {
    void load();
  } else {
    notFound.value = true;
  }
});

onBeforeUnmount(() => {
  clearRefreshTimer();
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
        <p v-if="dashboard" class="mt-1 text-xs text-muted-foreground">
          Created by {{ creatorLabel }}<span v-if="updatedLabel"> · Updated {{ updatedLabel }}</span>
        </p>
      </div>

      <div class="flex items-center gap-2 flex-wrap">
        <DashboardToolbar v-if="dashboard" />
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

    <!-- Empty dashboard -->
    <div
      v-else-if="dashboard && placements.length === 0"
      class="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed py-16 text-center"
    >
      <p class="text-sm text-muted-foreground">This dashboard has no panels yet.</p>
      <Button v-if="isEditing" size="sm" class="gap-1.5" @click="openAddPanel">
        <Plus class="h-4 w-4" />
        Add panel
      </Button>
      <Button v-else-if="canEdit" variant="outline" size="sm" class="gap-1.5" @click="startEdit">
        <Pencil class="h-4 w-4" />
        Edit to add panels
      </Button>
    </div>

    <!-- Grid -->
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
        :class="{ 'dash-cell--dragover': dragOverId === p.panel.id, 'dash-cell--editing': isEditing }"
        :style="{
          gridColumn: `${p.x + 1} / span ${p.w}`,
          gridRow: `${p.y + 1} / span ${p.h}`,
        }"
        :draggable="isEditing"
        @dragstart="isEditing && onDragStart(p.panel.id, $event)"
        @dragover.prevent="isEditing && onDragOver(p.panel.id)"
        @drop.prevent="isEditing && onDrop(p.panel.id)"
        @dragend="onDragEnd"
      >
        <DashboardPanel :panel="p.panel" :height-px="panelOuterHeight(p.h)" />

        <!-- Edit-mode controls -->
        <div v-if="isEditing" class="dash-cell__controls" @dragstart.stop>
          <span class="dash-cell__drag" title="Drag to reorder">
            <Move class="h-3.5 w-3.5" />
          </span>
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <button class="dash-cell__btn" title="Resize panel" @mousedown.stop>
                <Maximize2 class="h-3.5 w-3.5" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="w-44">
              <DropdownMenuLabel class="text-xs">Width</DropdownMenuLabel>
              <div class="grid grid-cols-4 gap-1 px-2 pb-1.5">
                <button
                  v-for="w in ALLOWED_PANEL_WIDTHS"
                  :key="`w-${w}`"
                  class="rounded border px-1 py-0.5 text-xs hover:bg-accent"
                  :class="{ 'bg-primary text-primary-foreground': p.w === w }"
                  @click="setWidth(p.panel.id, w, p.h)"
                >
                  {{ w }}
                </button>
              </div>
              <DropdownMenuSeparator />
              <DropdownMenuLabel class="text-xs">Height</DropdownMenuLabel>
              <div class="grid grid-cols-6 gap-1 px-2 pb-1.5">
                <button
                  v-for="h in heightOptions"
                  :key="`h-${h}`"
                  class="rounded border px-1 py-0.5 text-xs hover:bg-accent"
                  :class="{ 'bg-primary text-primary-foreground': p.h === h }"
                  @click="setHeight(p.panel.id, p.w, h)"
                >
                  {{ h }}
                </button>
              </div>
            </DropdownMenuContent>
          </DropdownMenu>
          <button class="dash-cell__btn" title="Edit panel" @mousedown.stop @click="openEditPanel(p.panel)">
            <Pencil class="h-3.5 w-3.5" />
          </button>
          <button
            class="dash-cell__btn dash-cell__btn--danger"
            title="Remove panel"
            @mousedown.stop
            @click="removePanel(p.panel.id)"
          >
            <Trash2 class="h-3.5 w-3.5" />
          </button>
        </div>
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
.dash-cell--editing {
  cursor: grab;
}
.dash-cell--editing:active {
  cursor: grabbing;
}
.dash-cell--dragover {
  outline: 2px dashed var(--primary);
  outline-offset: 2px;
  border-radius: 0.6rem;
}
.dash-cell__controls {
  position: absolute;
  top: 0.3rem;
  right: 0.3rem;
  display: flex;
  align-items: center;
  gap: 0.2rem;
  padding: 0.15rem;
  border-radius: 0.4rem;
  background: color-mix(in srgb, var(--card) 88%, transparent);
  border: 1px solid var(--border);
  opacity: 0;
  transition: opacity 0.12s ease;
  z-index: 2;
}
.dash-cell:hover .dash-cell__controls,
.dash-cell:focus-within .dash-cell__controls {
  opacity: 1;
}
.dash-cell__drag,
.dash-cell__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.5rem;
  height: 1.5rem;
  border-radius: 0.3rem;
  color: var(--muted-foreground);
}
.dash-cell__drag {
  cursor: grab;
}
.dash-cell__btn:hover {
  background: var(--accent);
  color: var(--foreground);
}
.dash-cell__btn--danger:hover {
  background: color-mix(in srgb, var(--destructive) 15%, transparent);
  color: var(--destructive);
}
</style>
