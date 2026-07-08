<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter, onBeforeRouteLeave } from "vue-router";
import { ArrowLeft, LayoutDashboard } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import DashboardToolbar from "./components/DashboardToolbar.vue";
import DashboardPanel from "./components/DashboardPanel.vue";
import { useDashboardsStore } from "@/stores/dashboards";
import { normalizeDashboardLayout } from "@/utils/dashboardPanels";

const route = useRoute();
const router = useRouter();
const store = useDashboardsStore();

// Grid geometry. 12 columns; each layout row is ROW_HEIGHT px tall.
const ROW_HEIGHT_PX = 80;
const GRID_GAP_PX = 12;

const dashboardId = computed(() => Number(route.params.id));
const isLoading = computed(() => store.isLoadingOperation("loadDashboard"));
const dashboard = computed(() => store.current);
const notFound = ref(false);

const placements = computed(() => {
  const d = store.current;
  if (!d?.panels) return [];
  return normalizeDashboardLayout(d.panels.panels ?? [], d.panels.layout ?? []);
});

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
  clearRefreshTimer();
});
</script>

<template>
  <div class="mx-auto w-full max-w-[1600px] px-4 py-4">
    <!-- Header -->
    <div class="mb-4 flex items-start justify-between gap-4 flex-wrap">
      <div class="min-w-0">
        <div class="flex items-center gap-2">
          <Button variant="ghost" size="sm" class="h-7 px-2 -ml-2" @click="router.push('/dashboards')">
            <ArrowLeft class="h-4 w-4" />
          </Button>
          <LayoutDashboard class="h-5 w-5 text-muted-foreground shrink-0" />
          <h1 class="text-lg font-semibold truncate">
            {{ dashboard?.name || (isLoading ? "Loading…" : "Dashboard") }}
          </h1>
        </div>
        <p v-if="dashboard?.description" class="mt-1 text-sm text-muted-foreground">
          {{ dashboard.description }}
        </p>
        <p v-if="dashboard" class="mt-1 text-xs text-muted-foreground">
          Created by {{ creatorLabel }}<span v-if="updatedLabel"> · Updated {{ updatedLabel }}</span>
        </p>
      </div>
      <DashboardToolbar v-if="dashboard" />
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
      class="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed py-16 text-center"
    >
      <p class="text-sm text-muted-foreground">This dashboard has no panels yet.</p>
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
        :style="{
          gridColumn: `${p.x + 1} / span ${p.w}`,
          gridRow: `${p.y + 1} / span ${p.h}`,
        }"
      >
        <DashboardPanel :panel="p.panel" :height-px="panelOuterHeight(p.h)" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.dash-grid {
  display: grid;
  grid-auto-flow: row;
}
</style>
