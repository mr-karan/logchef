<script setup lang="ts">
import { computed } from "vue";
import { VisArea, VisAxis, VisLine, VisStackedBar, VisXYContainer } from "@unovis/vue";
import { ChartContainer, ChartCrosshair, ChartLegendContent, ChartTooltip } from "@/components/ui/chart";
import {
  buildHistogramChartModel,
  formatCompactCount,
  formatHistogramTimestamp,
  formatTooltipTimestamp,
  type HistogramChartRow,
} from "@/utils/histogram-chart";
import type { HistogramData } from "@/services/HistogramService";

// A prop-driven timeseries panel. It reuses the same chart model + unovis stacked
// bar the explorer histogram uses, but takes its data via props (the explorer's
// LogHistogram reads straight from the explore store, so it can't be shared here).
interface Props {
  buckets: HistogramData[];
  granularity?: string | null;
  groupBy?: string | null;
  height?: number;
  /** Render style. Absent/undefined defaults to 'line' (Grafana-like). */
  chart?: "bars" | "line" | "area";
}
const props = withDefaults(defineProps<Props>(), {
  granularity: null,
  groupBy: null,
  height: 160,
});

// 'line' is the default render style when the panel hasn't set one explicitly.
const effectiveChart = computed<"bars" | "line" | "area">(() => props.chart ?? "line");

const CHART_MARGIN = { top: 8, right: 12, bottom: 22, left: 8 };

const chartModel = computed(() =>
  buildHistogramChartModel(props.buckets, props.granularity ?? undefined)
);

const seriesAccessors = computed(() =>
  chartModel.value.series.map(
    (series) => (row: HistogramChartRow) => Number(row[series.key] ?? 0)
  )
);
const seriesColors = computed(() => chartModel.value.series.map((series) => series.color));

// Bars and area are drawn stacked (crosshair circles must land on the cumulative
// height), while line series are drawn independently at their own value.
const crosshairYProps = computed(() =>
  effectiveChart.value === "line"
    ? { y: seriesAccessors.value }
    : { yStacked: seriesAccessors.value }
);

const chartRange = computed(() => {
  if (!chartModel.value.rows.length) {
    return { start: Date.now() - chartModel.value.bucketWidthMs, end: Date.now() };
  }
  const first = chartModel.value.rows[0];
  const last = chartModel.value.rows[chartModel.value.rows.length - 1];
  return { start: first.ts, end: last.bucketEndTs };
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

  const totalHtml =
    model.series.length > 1
      ? `<div style="display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-bottom:0.5rem;padding-bottom:0.5rem;border-bottom:1px solid var(--border);font-size:0.75rem;color:var(--muted-foreground);"><span>Total</span><strong>${totalCount.toLocaleString()}</strong></div>`
      : "";

  return `<div style="min-width:150px;padding:0.5rem 0.65rem;background:var(--popover);color:var(--popover-foreground);border-radius:0.5rem;border:1px solid var(--border);box-shadow:0 4px 12px rgba(0,0,0,0.12);font-size:0.8125rem;line-height:1.5;">
    <div style="margin-bottom:0.35rem;font-size:0.75rem;font-weight:600;">${timeLabel}</div>
    ${totalHtml}
    <div style="display:flex;flex-direction:column;gap:0.25rem;">${seriesHtml}</div>
  </div>`;
}

const tooltipTemplate = computed(() => {
  return (
    _payload: unknown,
    _x: number | Date,
    data?: HistogramChartRow[],
    nearestIndex?: number
  ) => {
    if (!data || nearestIndex == null || !data[nearestIndex]) return "";
    return buildTooltipHtml(data[nearestIndex]);
  };
});

function formatYAxisTick(value: number | Date) {
  return formatCompactCount(Number(value));
}
function formatXAxisTick(value: number | Date) {
  return formatHistogramTimestamp(value, chartRange.value, props.granularity ?? undefined);
}
</script>

<template>
  <ChartContainer :config="chartModel.chartConfig" class="panel-timeseries">
    <VisXYContainer
      :data="chartModel.rows"
      :height="props.height"
      :margin="CHART_MARGIN"
      :padding="{ top: 6, right: 4, bottom: 0, left: 0 }"
      class="panel-timeseries__chart"
    >
      <VisAxis
        type="x"
        :x="(row: HistogramChartRow) => row?.ts"
        :tick-line="false"
        :domain-line="false"
        :grid-line="false"
        :tick-format="formatXAxisTick"
        :tick-text-hide-overlapping="true"
        :num-ticks="6"
      />
      <VisAxis
        type="y"
        :tick-line="false"
        :domain-line="false"
        :grid-line="true"
        :tick-format="formatYAxisTick"
        :num-ticks="4"
      />
      <VisStackedBar
        v-if="effectiveChart === 'bars'"
        :x="(row: HistogramChartRow) => row?.ts"
        :y="seriesAccessors"
        :color="seriesColors"
        :rounded-corners="3"
        :bar-padding="0.15"
        :bar-max-width="36"
        :bar-min-height1-px="true"
      />
      <VisArea
        v-else-if="effectiveChart === 'area'"
        :x="(row: HistogramChartRow) => row?.ts"
        :y="seriesAccessors"
        :color="seriesColors"
        :opacity="0.25"
        :line="true"
        :line-color="seriesColors"
        :line-width="2"
      />
      <VisLine
        v-else
        :x="(row: HistogramChartRow) => row?.ts"
        :y="seriesAccessors"
        :color="seriesColors"
        :line-width="2"
      />
      <ChartTooltip />
      <ChartCrosshair
        :x="(row: HistogramChartRow) => row?.ts"
        v-bind="crosshairYProps"
        :color="seriesColors"
        :template="tooltipTemplate"
        :snap-to-data="true"
        :hide-when-far-from-pointer="false"
      />
    </VisXYContainer>
    <ChartLegendContent
      v-if="chartModel.isGrouped && chartModel.series.length > 1"
      class="panel-timeseries__legend"
    />
  </ChartContainer>
</template>

<style scoped>
.panel-timeseries {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.panel-timeseries__chart {
  flex: 1 1 auto;
  min-height: 0;
}

.panel-timeseries :deep(.vis-stacked-bar-bar) {
  opacity: 0.85;
  transition: opacity 0.15s ease;
}
.panel-timeseries :deep(.vis-stacked-bar-bar:hover) {
  opacity: 1;
}
.panel-timeseries :deep(.vis-axis-grid-line) {
  stroke: hsl(var(--border) / 0.5);
}
.panel-timeseries :deep(.vis-axis-tick-label) {
  font-size: 10px;
  fill: hsl(var(--muted-foreground));
}
.panel-timeseries__legend {
  flex-shrink: 0;
  padding-top: 0.25rem;
}
</style>
