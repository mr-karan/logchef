<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from "vue";
import { RouterLink } from "vue-router";
import { Lock, AlertCircle, BarChart3, Hash, Table2, ListFilter } from "lucide-vue-next";
import { Skeleton } from "@/components/ui/skeleton";
import PanelChart from "./PanelChart.vue";
import PanelStat from "./PanelStat.vue";
import PanelPie from "./PanelPie.vue";
import PanelTopList from "./PanelTopList.vue";
import PanelTable from "./PanelTable.vue";
import type { DashboardPanel } from "@/api/dashboards";
import { useDashboardsStore, type PanelState } from "@/stores/dashboards";
import { panelExploreQuery } from "@/utils/panelExplore";
import { truncationMessage } from "@/utils/truncation";

const { t } = useI18n();

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
  /** Link the title to the panel's logs in Explorer. Off in edit mode and previews. */
  linkTitle?: boolean;
}
const props = withDefaults(defineProps<Props>(), { chrome: true, linkTitle: false });

const store = useDashboardsStore();
const panelState = computed<PanelState>(
  () => props.state ?? store.panelStates[props.panel.id] ?? { status: "idle" as const }
);

// A server-redacted panel (viewer lacks source access) carries `locked` and has
// no query to run. Treat it as locked straight from the prop so it renders the
// locked placeholder immediately, without a loading-skeleton flash before the
// store's refresh sets the state.
const isLocked = computed(() => props.panel.locked === true || panelState.value.status === "locked");

// The title opens the panel's logs in Explorer over the window its shown result
// ran over, which can lag the dashboard's window while a refresh is queued.
const exploreLink = computed(() => {
  if (!props.linkTitle || isLocked.value || panelState.value.status === "unsupported") return null;
  if (props.panel.locked) return null;
  const range = panelState.value.range ?? store.appliedRange ?? store.effectiveRange;
  return { name: "LogExplorer", query: panelExploreQuery(props.panel, range) };
});

// Chart body height = outer height minus the header row (when shown) and the
// body padding.
const BODY_PADDING_PX = 16;
const HEADER_PX = 32;
const chartHeight = computed(() =>
  Math.max(70, props.heightPx - BODY_PADDING_PX - (props.chrome ? HEADER_PX : 0))
);

const vizType = computed(() => props.panel.viz?.type);

// A cut result says so on every visualization, and so does an empty one: a
// byte budget can stop the reader before its first row. A cap that cut
// nothing is silent.
const truncationText = computed(() => {
  if (panelState.value.status !== "success" && panelState.value.status !== "empty") return null;
  const message = truncationMessage(panelState.value.stats);
  return message ? t(message.key, { count: message.count }, message.count) : null;
});

const typeIcon = computed(() => {
  switch (vizType.value) {
    case "chart":
      return BarChart3;
    case "stat":
      return Hash;
    case "pie":
    case "toplist":
      return ListFilter;
    default:
      return Table2;
  }
});

const columnSubset = computed(() => {
  const panel = props.panel;
  return !panel.locked && panel.viz.type === "table" ? panel.viz.visible_columns : undefined;
});
</script>

<template>
  <div
    class="dash-panel"
    :class="{ 'dash-panel--locked': isLocked, 'dash-panel--bare': !chrome }"
  >
    <div v-if="chrome" class="dash-panel__header">
      <component :is="typeIcon" class="dash-panel__icon" />
      <RouterLink
        v-if="exploreLink"
        :to="exploreLink"
        class="dash-panel__title dash-panel__title--link"
        :title="t('dashboards.openPanelInExplorer')"
      >{{ panel.title || "Untitled" }}</RouterLink>
      <span v-else class="dash-panel__title" :title="panel.title">{{ panel.title || "Untitled" }}</span>
      <span
        v-if="truncationText"
        class="dash-panel__chip dash-panel__chip--header border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400"
        :title="truncationText"
      >{{ truncationText }}</span>
      <Lock
        v-if="isLocked"
        class="dash-panel__lock"
        :title="t('ui.youDonTHaveAccessToThisPanelSSource')"
      />
    </div>

    <div class="dash-panel__body">
      <!-- Without the header (edit mode, preview) the chip floats over the
           body's top-right corner instead. -->
      <span
        v-if="!chrome && truncationText"
        class="dash-panel__chip dash-panel__chip--overlay border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400"
        :title="truncationText"
      >{{ truncationText }}</span>
      <!-- Locked: viewer lacks source access. Covers both the server-redacted
           panel (panel.locked) and the query-time AuthorizationError path
           (panelState.status === 'locked'), unified via isLocked. -->
      <div v-if="isLocked" class="dash-panel__message">
        <Lock class="dash-panel__message-icon" />
        <span>{{ t('ui.noAccessToThisSource') }}</span>
      </div>

      <div v-else-if="panelState.status === 'unsupported'" class="dash-panel__message">
        <AlertCircle class="dash-panel__message-icon" />
        <span>{{ t('ui.panelNeedsReload') }}</span>
      </div>

      <!-- Loading -->
      <div v-else-if="panelState.status === 'loading' || panelState.status === 'idle'" class="dash-panel__fill">
        <Skeleton class="h-full w-full" />
      </div>

      <!-- Error -->
      <div v-else-if="panelState.status === 'error'" class="dash-panel__message dash-panel__message--error">
        <AlertCircle class="dash-panel__message-icon" />
        <span class="dash-panel__error-text" :title="panelState.error">{{ panelState.error || "Failed to load" }}</span>
      </div>

      <!-- Empty -->
      <div v-else-if="panelState.status === 'empty' && (!panelState.frame || (!panel.locked && panel.query.mode === 'count' && vizType !== 'stat'))" class="dash-panel__message">
        <span>{{ t('ui.noDataForThisTimeRange') }}</span>
      </div>

      <!-- Success -->
      <template v-else>
        <PanelChart
          v-if="!panel.locked && panel.viz.type === 'chart' && panelState.frame"
          :frame="panelState.frame" :viz="panel.viz" :count="panel.query.mode === 'count'" :range="panelState.range" :height="chartHeight"
        />
        <PanelStat
          v-else-if="vizType === 'stat' && panelState.frame && !panel.locked"
          :frame="panelState.frame"
          :panel="panel"
        />
        <PanelPie v-else-if="!panel.locked && panel.viz.type === 'pie' && panelState.frame" :frame="panelState.frame" :viz="panel.viz" :count="panel.query.mode === 'count'" :height="chartHeight" />
        <PanelTopList v-else-if="!panel.locked && panel.viz.type === 'toplist' && panelState.frame" :frame="panelState.frame" :viz="panel.viz" :count="panel.query.mode === 'count'" />
        <PanelTable
          v-else-if="vizType === 'table' && panelState.frame"
          :frame="panelState.frame"
          :kinds="!panel.locked && panel.viz.type === 'table' ? panel.viz.columns : undefined"
          :column-subset="columnSubset"
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
.dash-panel__title--link:hover,
.dash-panel__title--link:focus-visible {
  text-decoration: underline;
  text-underline-offset: 2px;
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
.dash-panel__chip {
  padding: 0.1rem 0.45rem;
  border-width: 1px;
  border-radius: 999px;
  font-size: 0.65rem;
  line-height: 1.2;
  white-space: nowrap;
  flex-shrink: 0;
}
.dash-panel__chip--header {
  margin-left: auto;
}
.dash-panel__chip--header + .dash-panel__lock {
  margin-left: 0;
}
.dash-panel__chip--overlay {
  position: absolute;
  top: 0.4rem;
  right: 0.5rem;
  z-index: 2;
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
