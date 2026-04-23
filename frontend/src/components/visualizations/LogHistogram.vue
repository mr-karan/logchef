<script setup lang="ts">
import { computed, ref } from "vue";
import {
  VisAxis,
  VisStackedBar,
  VisStackedBarSelectors,
  VisXYContainer,
} from "@unovis/vue";
import { useExploreStore } from "@/stores/explore";
import {
  ChartContainer,
  ChartCrosshair,
  ChartLegendContent,
  ChartTooltip,
} from "@/components/ui/chart";
import {
  buildHistogramChartModel,
  formatCompactCount,
  formatHistogramTimestamp,
  formatTooltipTimestamp,
  type HistogramChartRow,
} from "@/utils/histogram-chart";
import { useHistogramBrush } from "@/composables/useHistogramBrush";

interface Props {
  isLoading?: boolean;
  height?: string;
  groupBy?: string | null;
}

const props = withDefaults(defineProps<Props>(), {
  isLoading: false,
  height: "180px",
  groupBy: null,
});

const emit = defineEmits<{
  (e: "zoom-time-range", range: { start: Date; end: Date }): void;
}>();

const exploreStore = useExploreStore();

const histogramData = computed(() => exploreStore.histogramData);
const histogramError = computed(() => exploreStore.histogramError);
const isChartLoading = computed(
  () => props.isLoading || exploreStore.isLoadingHistogram,
);
const currentGranularity = computed(
  () => exploreStore.histogramGranularity || undefined,
);

const chartHeight = computed(() => Number.parseInt(props.height, 10) || 180);

// Must match the VisXYContainer margin prop below
const CHART_MARGIN = { top: 12, right: 12, bottom: 24, left: 8 };

const chartModel = computed(() =>
  buildHistogramChartModel(histogramData.value, currentGranularity.value),
);

const seriesAccessors = computed(() =>
  chartModel.value.series.map(
    (series) => (row: HistogramChartRow) => Number(row[series.key] ?? 0),
  ),
);

const seriesColors = computed(() =>
  chartModel.value.series.map((series) => series.color),
);

const chartRange = computed(() => {
  if (!chartModel.value.rows.length) {
    return {
      start: Date.now() - chartModel.value.bucketWidthMs,
      end: Date.now(),
    };
  }

  const first = chartModel.value.rows[0];
  const last = chartModel.value.rows[chartModel.value.rows.length - 1];
  return {
    start: first.ts,
    end: last.bucketEndTs,
  };
});

const chartSubtitle = computed(() => {
  if (!chartModel.value.rows.length) {
    return null;
  }

  const parts = ["Hover to inspect", "drag to select range", "click bar to zoom"];
  if (currentGranularity.value) {
    parts.unshift(`${currentGranularity.value} buckets`);
  }
  if (props.groupBy) {
    parts.unshift(`Grouped by ${props.groupBy}`);
  }
  return parts.join(" • ");
});

function buildTooltipHtml(datum: HistogramChartRow): string {
  const timeLabel = formatTooltipTimestamp(datum.ts, chartRange.value);
  const model = chartModel.value;
  const totalCount = typeof datum.total === "number" ? datum.total : 0;

  let seriesHtml = "";
  for (const s of model.series) {
    const val = typeof datum[s.key] === "number" ? (datum[s.key] as number) : 0;
    seriesHtml += `
      <div style="display:flex;align-items:center;justify-content:space-between;gap:1rem;">
        <div style="display:flex;align-items:center;gap:0.5rem;">
          <span style="width:8px;height:8px;border-radius:50%;background:${s.color};flex-shrink:0;"></span>
          <span>${s.label}</span>
        </div>
        <strong style="font-variant-numeric:tabular-nums;">${val.toLocaleString()}</strong>
      </div>`;
  }

  const totalHtml = model.series.length > 1
    ? `<div style="display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-bottom:0.5rem;padding-bottom:0.5rem;border-bottom:1px solid var(--border);font-size:0.75rem;color:var(--muted-foreground);">
        <span>Total</span><strong>${totalCount.toLocaleString()}</strong>
       </div>`
    : "";

  return `<div style="min-width:160px;padding:0.625rem 0.75rem;background:var(--popover);color:var(--popover-foreground);border-radius:0.5rem;border:1px solid var(--border);box-shadow:0 4px 12px rgba(0,0,0,0.12);font-size:0.8125rem;line-height:1.5;">
    <div style="margin-bottom:0.375rem;font-size:0.75rem;font-weight:600;">${timeLabel}</div>
    ${totalHtml}
    <div style="display:flex;flex-direction:column;gap:0.25rem;">${seriesHtml}</div>
  </div>`;
}

const tooltipTemplate = computed(() => {
  return (
    _payload: unknown,
    _x: number | Date,
    data?: HistogramChartRow[],
    nearestIndex?: number,
  ) => {
    if (!data || nearestIndex == null || !data[nearestIndex]) return "";
    return buildTooltipHtml(data[nearestIndex]);
  };
});

const stackedBarAttributes = computed(() => ({
  [VisStackedBarSelectors.barGroup]: {
    cursor: "pointer",
  },
}));

const stackedBarEvents = computed(() => ({
  [VisStackedBarSelectors.barGroup]: {
    click: (datum: unknown) => {
      // Suppress bar click when finishing a brush drag
      if (isDragging.value || justFinishedDrag.value) {
        return;
      }

      const bucket = normalizeBucketDatum(datum);
      if (!bucket) {
        return;
      }

      emit("zoom-time-range", {
        start: new Date(bucket.ts),
        end: new Date(bucket.bucketEndTs),
      });
    },
  },
}));

function normalizeBucketDatum(datum: unknown): HistogramChartRow | null {
  if (!datum || typeof datum !== "object") {
    return null;
  }

  const candidate =
    "datum" in datum && datum.datum && typeof datum.datum === "object"
      ? (datum.datum as Record<string, unknown>)
      : (datum as Record<string, unknown>);

  if (
    typeof candidate.ts !== "number" ||
    typeof candidate.bucketEndTs !== "number"
  ) {
    return null;
  }

  return candidate as HistogramChartRow;
}

// --- Brush selection ---
const chartWrapperRef = ref<HTMLElement | null>(null);
const justFinishedDrag = ref(false);

const {
  isDragging,
  selectionStyle,
  onPointerDown,
  onPointerMove,
  onPointerUp,
  onPointerCancel,
  onLostPointerCapture,
  onContextMenu,
} = useHistogramBrush(chartWrapperRef, chartRange, CHART_MARGIN);

function handlePointerUp(e: PointerEvent) {
  const range = onPointerUp(e);
  if (range) {
    justFinishedDrag.value = true;
    setTimeout(() => {
      justFinishedDrag.value = false;
    }, 100);

    emit("zoom-time-range", range);
  }
}

function formatYAxisTick(value: number | Date) {
  return formatCompactCount(Number(value));
}

function formatXAxisTick(value: number | Date) {
  return formatHistogramTimestamp(value, chartRange.value, currentGranularity.value);
}
</script>

<template>
  <div class="log-histogram" :style="{ minHeight: props.height }">
    <div class="log-histogram__header" v-if="chartSubtitle">
      <p class="log-histogram__subtitle">{{ chartSubtitle }}</p>
    </div>

    <div v-if="isChartLoading" class="histogram-loading-overlay">
      <div class="loading-spinner"></div>
      <span>Loading histogram data...</span>
    </div>

    <div
      v-else-if="!chartModel.rows.length"
      class="histogram-empty-state"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="24"
        height="24"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        class="mb-2"
      >
        <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
        <line x1="9" y1="9" x2="9" y2="15"></line>
        <line x1="15" y1="9" x2="15" y2="15"></line>
      </svg>
      <span v-if="histogramError">{{ histogramError }}</span>
      <span v-else>No histogram data available</span>
    </div>

    <ChartContainer
      v-else
      :config="chartModel.chartConfig"
      class="log-histogram__chart"
      :style="{ height: props.height }"
    >
      <!-- Chart wrapper: pointer events go here for both Unovis tooltip AND brush drag -->
      <div
        ref="chartWrapperRef"
        class="chart-wrapper"
        :class="{ 'chart-wrapper--dragging': isDragging }"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="handlePointerUp"
        @pointercancel="onPointerCancel"
        @lostpointercapture="onLostPointerCapture"
        @contextmenu="onContextMenu"
      >
        <VisXYContainer
          :data="chartModel.rows"
          :height="chartHeight"
          :margin="CHART_MARGIN"
          :padding="{ top: 8, right: 4, bottom: 0, left: 0 }"
        >
          <VisAxis
            type="x"
            :x="(row: HistogramChartRow) => row?.ts"
            :tick-line="false"
            :domain-line="false"
            :grid-line="false"
            :tick-format="formatXAxisTick"
            :tick-text-hide-overlapping="true"
            :num-ticks="8"
          />
          <VisAxis
            type="y"
            :tick-line="false"
            :domain-line="false"
            :grid-line="true"
            :tick-format="formatYAxisTick"
            :num-ticks="5"
          />
          <VisStackedBar
            :x="(row: HistogramChartRow) => row?.ts"
            :y="seriesAccessors"
            :color="seriesColors"
            :rounded-corners="4"
            :bar-padding="0.15"
            :bar-max-width="36"
            :bar-min-height1-px="true"
            cursor="pointer"
            :attributes="stackedBarAttributes"
            :events="stackedBarEvents"
          />
          <ChartTooltip />
          <ChartCrosshair
            :x="(row: HistogramChartRow) => row?.ts"
            :y-stacked="seriesAccessors"
            :color="seriesColors"
            :template="tooltipTemplate"
            :snap-to-data="true"
            :hide-when-far-from-pointer="false"
          />
        </VisXYContainer>

        <!-- Selection rectangle (pointer-events: none, only visible during drag) -->
        <div
          v-if="isDragging && selectionStyle"
          class="brush-selection"
          :style="{
            left: `calc(${CHART_MARGIN.left}px + ${selectionStyle.left})`,
            width: selectionStyle.width,
            top: `${CHART_MARGIN.top}px`,
            bottom: `${CHART_MARGIN.bottom}px`,
          }"
        />
      </div>
      <ChartLegendContent v-if="chartModel.isGrouped && chartModel.series.length > 1" />
    </ChartContainer>
  </div>
</template>

<style scoped>
.log-histogram {
  position: relative;
  width: 100%;
  border-radius: 0.75rem;
  border: 1px solid var(--border);
  background-color: var(--card);
  box-shadow: 0 2px 4px 0 rgba(0, 0, 0, 0.05);
  overflow: hidden;
  margin-bottom: 1rem;
}

.log-histogram__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.75rem 1rem 0;
}

.log-histogram__subtitle {
  color: var(--muted-foreground);
  font-size: 0.75rem;
}

.log-histogram__chart {
  padding: 0.5rem 0.5rem 0.5rem;
}

/* Histogram contrast overrides — bars slightly desaturated, grid visible, axis readable */
.log-histogram :deep(.vis-stacked-bar-bar) {
  opacity: 0.85;
  transition: opacity 0.15s ease;
}

.log-histogram :deep(.vis-stacked-bar-bar:hover) {
  opacity: 1;
}

.log-histogram :deep(.vis-axis-grid-line) {
  stroke: hsl(var(--border) / 0.5);
}

.log-histogram :deep(.vis-axis-tick-label) {
  font-size: 11px;
  fill: hsl(var(--muted-foreground));
}

.chart-wrapper {
  position: relative;
  cursor: crosshair;
  touch-action: none;
}

.chart-wrapper--dragging {
  cursor: col-resize;
  /* Prevent text selection during drag */
  user-select: none;
}

.brush-selection {
  position: absolute;
  background: rgba(236, 72, 153, 0.15);
  border-left: 2px solid rgba(236, 72, 153, 0.7);
  border-right: 2px solid rgba(236, 72, 153, 0.7);
  border-radius: 2px;
  pointer-events: none;
  z-index: 5;
}

.histogram-loading-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  background-color: color-mix(in oklch, var(--card) 88%, transparent);
  z-index: 10;
  font-size: 0.875rem;
  color: var(--muted-foreground);
  backdrop-filter: blur(2px);
}

.loading-spinner {
  width: 1.75rem;
  height: 1.75rem;
  border: 3px solid var(--muted);
  border-top-color: var(--primary);
  border-radius: 9999px;
  animation: spinner 0.9s ease-in-out infinite;
}

.histogram-empty-state {
  min-height: 180px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--muted-foreground);
  font-size: 0.875rem;
  gap: 0.5rem;
  opacity: 0.8;
}

@keyframes spinner {
  0% {
    transform: rotate(0deg);
  }

  100% {
    transform: rotate(360deg);
  }
}
</style>
