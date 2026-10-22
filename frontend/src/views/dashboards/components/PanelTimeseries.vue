<script lang="ts">
/**
 * Resolve the effective bar mode from chart style and explicit bar_mode.
 * - When chart is "bars": barMode (defaulting to "stacked") wins.
 * - Otherwise returns "stacked" (irrelevant but well-defined).
 */
export function resolveBarMode(
  chart: string | undefined,
  barMode: string | undefined
): "stacked" | "grouped" {
  if (chart === "bars") {
    return barMode === "grouped" ? "grouped" : "stacked";
  }
  return "stacked";
}

/**
 * Whether the crosshair should use yStacked accessors (cumulative height).
 * Line charts and grouped bars use regular y; stacked bars and area use yStacked.
 */
export function shouldUseCrosshairYStacked(
  chart: string | undefined,
  barMode: string | undefined
): boolean {
  if (chart === "line") return false;
  if (chart === "bars" && barMode === "grouped") return false;
  return true;
}
</script>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed } from "vue";
import { VisArea, VisAxis, VisGroupedBar, VisLine, VisStackedBar, VisXYContainer } from "@unovis/vue";
import { ChartContainer, ChartCrosshair, ChartLegendContent, ChartTooltip } from "@/components/ui/chart";
import {
  buildHistogramChartModel,
  formatCompactCount,
  formatHistogramTimestamp,
  buildHistogramTooltip,
  type HistogramChartRow,
} from "@/utils/histogram-chart";
import type { HistogramData } from "@/services/HistogramService";
import { usePreferencesStore } from "@/stores/preferences";
import type { ResolvedWindow } from "@/utils/timeWindow";

const { t } = useI18n();
const preferencesStore = usePreferencesStore();

// A prop-driven timeseries panel. It reuses the same chart model + unovis stacked
// bar the explorer histogram uses, but takes its data via props (the explorer's
// LogHistogram reads straight from the explore store, so it can't be shared here).
interface Props {
  buckets: HistogramData[];
  granularity?: string | null;
  groupBy?: string | null;
  /** Non-fatal backend notice, such as a grouped-series cap. */
  notice?: string | null;
  height?: number;
  /** Render style. Absent/undefined defaults to 'line' (Grafana-like). */
  chart?: "bars" | "line" | "area";
  /** Bar render mode. Only meaningful when chart is 'bars'. Absent defaults to 'stacked'. */
  barMode?: "stacked" | "grouped";
  /**
   * The panel's queried window and the zone it ran in. The chart's x-domain
   * and labels use it instead of the first/last returned bucket, so missing
   * leading/trailing empty buckets (ClickHouse only returns buckets that
   * matched at least one log) don't shrink the visible window.
   */
  range?: ResolvedWindow | null;
}
const props = withDefaults(defineProps<Props>(), {
  granularity: null,
  groupBy: null,
  notice: null,
  height: 160,
  range: null,
});

// A queried window wide enough to plot. Zero-width falls back to the data.
const plotRange = computed(() => {
  const range = props.range;
  return range && Number.isFinite(range.start) && Number.isFinite(range.end) && range.end > range.start ? range : null;
});

// 'line' is the default render style when the panel hasn't set one explicitly.
const effectiveChart = computed<"bars" | "line" | "area">(() => props.chart ?? "line");

const effectiveBarMode = computed<"stacked" | "grouped">(() =>
  resolveBarMode(props.chart, props.barMode)
);

const CHART_MARGIN = { top: 8, right: 12, bottom: 22, left: 8 };

const chartModel = computed(() =>
  buildHistogramChartModel(props.buckets, props.granularity ?? undefined, Boolean(props.groupBy))
);

const seriesAccessors = computed(() =>
  chartModel.value.series.map(
    (series) => (row: HistogramChartRow) => Number(row[series.key] ?? 0)
  )
);
const seriesColors = computed(() => chartModel.value.series.map((series) => series.color));

// Stacked bars and area are drawn cumulatively (crosshair circles land on the
// cumulative height). Line and grouped bars are drawn at their own value.
const crosshairYProps = computed(() =>
  shouldUseCrosshairYStacked(effectiveChart.value, effectiveBarMode.value)
    ? { yStacked: seriesAccessors.value }
    : { y: seriesAccessors.value }
);

// Labels follow the preference zone; the queried instants do not depend on it.
const chartRange = computed(() => {
  const zone = preferencesStore.zone;
  if (plotRange.value) return { ...plotRange.value, zone };
  if (!chartModel.value.rows.length) {
    return { start: Date.now() - chartModel.value.bucketWidthMs, end: Date.now(), zone };
  }
  const first = chartModel.value.rows[0];
  const last = chartModel.value.rows[chartModel.value.rows.length - 1];
  return { start: first.ts, end: last.bucketEndTs, zone };
});

// Explicit x-domain for the chart canvas itself (not just tick/tooltip
// formatting) - without this, unovis auto-derives the domain from the data's
// own min/max ts, so missing leading/trailing empty buckets shrink the
// visible plotted window even though chartRange above "knows" the real range.
const xDomain = computed<[number, number] | undefined>(() =>
  plotRange.value ? [plotRange.value.start, plotRange.value.end] : undefined
);

const tooltipTemplate = computed(() => {
  return (
    _payload: unknown,
    _x: number | Date,
    data?: HistogramChartRow[],
    nearestIndex?: number
  ) => {
    if (!data || nearestIndex == null || !data[nearestIndex]) return "";
    return buildHistogramTooltip({
      datum: data[nearestIndex],
      series: chartModel.value.series,
      range: chartRange.value,
      totalLabel: t('common.total'),
    });
  };
});

function formatYAxisTick(value: number | Date) {
  return formatCompactCount(Number(value));
}
// Unovis redraws the axis only when the formatter itself changes, so build a
// new one whenever the range or zone does.
const formatXAxisTick = computed(() => {
  const range = chartRange.value;
  const granularity = props.granularity ?? undefined;
  return (value: number | Date) => formatHistogramTimestamp(value, range, granularity);
});
</script>

<template>
  <ChartContainer :config="chartModel.chartConfig" class="panel-timeseries">
    <VisXYContainer
      :data="chartModel.rows"
      :height="props.height"
      :margin="CHART_MARGIN"
      :padding="{ top: 6, right: 4, bottom: 0, left: 0 }"
      :x-domain="xDomain"
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
        v-if="effectiveChart === 'bars' && effectiveBarMode !== 'grouped'"
        :x="(row: HistogramChartRow) => row?.ts"
        :y="seriesAccessors"
        :color="seriesColors"
        :rounded-corners="3"
        :bar-padding="0.15"
        :bar-max-width="36"
        :bar-min-height1-px="true"
      />
      <VisGroupedBar
        v-else-if="effectiveChart === 'bars' && effectiveBarMode === 'grouped'"
        :x="(row: HistogramChartRow) => row?.ts"
        :y="seriesAccessors"
        :color="seriesColors"
        :rounded-corners="3"
        :bar-padding="0.15"
        :bar-min-height="1"
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
    <p v-if="props.notice" class="panel-timeseries__notice" role="status">{{ props.notice }}</p>
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
.panel-timeseries__notice {
  flex-shrink: 0;
  margin: 0.25rem 0 0;
  color: var(--muted-foreground);
  font-size: 0.75rem;
}
.panel-timeseries__legend {
  flex-shrink: 0;
  padding-top: 0.25rem;
}
</style>
