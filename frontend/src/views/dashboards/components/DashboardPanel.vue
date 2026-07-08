<script setup lang="ts">
import { computed } from "vue";
import { Lock, AlertCircle, BarChart3, Hash, Table2 } from "lucide-vue-next";
import { Skeleton } from "@/components/ui/skeleton";
import PanelTimeseries from "./PanelTimeseries.vue";
import PanelStat from "./PanelStat.vue";
import PanelTable from "./PanelTable.vue";
import type { DashboardPanel } from "@/api/dashboards";
import { useDashboardsStore, type PanelState } from "@/stores/dashboards";

interface Props {
  panel: DashboardPanel;
  /** Outer pixel height of the panel's grid cell (used to size the chart). */
  heightPx: number;
  /**
   * Optional explicit panel state. When provided (e.g. the editor's Preview
   * pane), it overrides the per-id lookup in the store so the same panel chrome
   * and sub-components render a preview result.
   */
  state?: PanelState;
  /**
   * Whether to render the panel's own header + card border. Defaults to true.
   * Edit mode wraps each panel in its own chrome (drag header, resize handle),
   * so it passes false to avoid a doubled header/border.
   */
  chrome?: boolean;
}
const props = withDefaults(defineProps<Props>(), { chrome: true });

const store = useDashboardsStore();
const panelState = computed<PanelState>(
  () => props.state ?? store.panelStates[props.panel.id] ?? { status: "idle" as const }
);

// Chart body height = outer height minus the header row (when shown) and the
// body padding.
const BODY_PADDING_PX = 16;
const HEADER_PX = 32;
const chartHeight = computed(() =>
  Math.max(70, props.heightPx - BODY_PADDING_PX - (props.chrome ? HEADER_PX : 0))
);

const typeIcon = computed(() => {
  switch (props.panel.type) {
    case "timeseries":
      return BarChart3;
    case "stat":
      return Hash;
    default:
      return Table2;
  }
});
</script>

<template>
  <div
    class="dash-panel"
    :class="{ 'dash-panel--locked': panelState.status === 'locked', 'dash-panel--bare': !chrome }"
  >
    <div v-if="chrome" class="dash-panel__header">
      <component :is="typeIcon" class="dash-panel__icon" />
      <span class="dash-panel__title" :title="panel.title">{{ panel.title || "Untitled" }}</span>
      <Lock
        v-if="panelState.status === 'locked'"
        class="dash-panel__lock"
        title="You don't have access to this panel's source."
      />
    </div>

    <div class="dash-panel__body">
      <!-- Loading -->
      <div v-if="panelState.status === 'loading' || panelState.status === 'idle'" class="dash-panel__fill">
        <Skeleton class="h-full w-full" />
      </div>

      <!-- Locked (viewer lacks team/source access) -->
      <div v-else-if="panelState.status === 'locked'" class="dash-panel__message">
        <Lock class="dash-panel__message-icon" />
        <span>No access to this source</span>
      </div>

      <!-- Error -->
      <div v-else-if="panelState.status === 'error'" class="dash-panel__message dash-panel__message--error">
        <AlertCircle class="dash-panel__message-icon" />
        <span class="dash-panel__error-text" :title="panelState.error">{{ panelState.error || "Failed to load" }}</span>
      </div>

      <!-- Empty -->
      <div v-else-if="panelState.status === 'empty'" class="dash-panel__message">
        <span>No data for this time range</span>
      </div>

      <!-- Success -->
      <template v-else>
        <PanelTimeseries
          v-if="panel.type === 'timeseries' && panelState.timeseries"
          :buckets="panelState.timeseries.buckets"
          :granularity="panelState.timeseries.granularity"
          :group-by="panelState.timeseries.groupBy"
          :height="chartHeight"
          :chart="panel.options?.chart"
        />
        <PanelStat
          v-else-if="panel.type === 'stat' && panelState.stat"
          :value="panelState.stat.value"
        />
        <PanelTable
          v-else-if="panel.type === 'table' && panelState.table"
          :columns="panelState.table.columns"
          :rows="panelState.table.rows"
          :column-subset="panel.options?.columns"
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
.dash-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: 0.6rem;
  background: var(--card);
  overflow: hidden;
  box-shadow: 0 1px 2px 0 rgba(0, 0, 0, 0.04);
}
.dash-panel--locked {
  opacity: 0.6;
}
/* In edit mode the .dash-item wrapper owns the border/header, so the panel
   renders body-only and transparent. */
.dash-panel--bare {
  border: 0;
  border-radius: 0;
  background: transparent;
  box-shadow: none;
}
.dash-panel__header {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.35rem 0.65rem;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  height: 32px;
  min-height: 32px;
  /* Same tint as the edit-mode drag header (DashboardView.vue's
     .dash-item__header) so a dashboard reads identically in view vs. edit
     mode instead of one being flat and the other tinted. */
  background: color-mix(in srgb, var(--muted) 35%, transparent);
}
.dash-panel__icon {
  width: 0.85rem;
  height: 0.85rem;
  color: var(--muted-foreground);
  flex-shrink: 0;
}
.dash-panel__title {
  font-size: 0.78rem;
  font-weight: 600;
  color: var(--foreground);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dash-panel__lock {
  width: 0.8rem;
  height: 0.8rem;
  color: var(--muted-foreground);
  margin-left: auto;
  flex-shrink: 0;
}
.dash-panel__body {
  flex: 1 1 auto;
  min-height: 0;
  padding: 0.5rem;
  position: relative;
}
.dash-panel__fill {
  width: 100%;
  height: 100%;
}
.dash-panel__message {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.4rem;
  height: 100%;
  color: var(--muted-foreground);
  font-size: 0.78rem;
  text-align: center;
  padding: 0.5rem;
}
.dash-panel__message--error {
  color: var(--destructive);
}
.dash-panel__message-icon {
  width: 1.1rem;
  height: 1.1rem;
}
.dash-panel__error-text {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}
</style>
