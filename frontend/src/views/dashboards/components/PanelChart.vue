<script setup lang="ts">
import { useElementSize } from "@vueuse/core";
import { computed, markRaw, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { VisPlotline, VisAxis, VisStackedBar, VisGroupedBar, VisLine, VisArea, VisScatter, VisXYContainer, VisCrosshair, VisTooltip } from "@unovis/vue";
import { Scale, StackedBar, GroupedBar, Scatter, Direction, type Crosshair, type Tooltip } from "@unovis/ts";
import { getStackedData } from "@unovis/ts/utils/data";
import { getHexValue } from "@unovis/ts/utils/color";
import { useThemeStore } from "@/stores/theme";
import { usePreferencesStore } from "@/stores/preferences";
import type { PanelFrame } from "@/utils/panelFrame";
import { buildPanelChart, chartStacked, chartValue, chartLabelColor, type ChartModel, type ChartRow, type ChartViz } from "@/utils/panelCharts";
import { chartTicks, chartTimeFormatter } from "@/utils/panelChartAxes";
import { addExact, isExact, parseExact, type FrameNumber } from "@/utils/exactNumber";
import { escapeHtml } from "@/lib/html";
import type { ResolvedWindow } from "@/utils/timeWindow";

const props = withDefaults(defineProps<{ frame: PanelFrame; viz: ChartViz; count: boolean; height?: number; range?: ResolvedWindow }>(), { height: 200 });
const { t } = useI18n();
const preferences = usePreferencesStore();
const theme = useThemeStore();
watch(() => theme.isDark, async () => {
  await nextTick();
  const svg = plotHost.value?.querySelector('svg');
  if (svg) barLabels(svg);
});
const result = computed(() => {
  try {
    const model = buildPanelChart(props.frame, props.viz, props.count, preferences.zone);
    markRaw(model.rows);
    const thresholdLines = (props.viz.thresholds ?? []).flatMap(step => {
      if (step.value === null) return [];
      const exact = parseExact(step.value);
      return exact ? [{ color: step.color, ...chartValue(exact, props.viz.format) }] : [];
    });
    const approximateNotice = "Chart coordinates are approximate. Tooltips show exact values.";
    if (thresholdLines.some(line => line.approximate) && !model.notices.includes(approximateNotice)) model.notices.push(approximateNotice);
    return { model, error: null, thresholdLines };
  }
  catch (error) { return { model: null, thresholdLines: [], error: error instanceof Error ? error.message : String(error) }; }
});
const emptyModel: ChartModel = { rows: [], series: [], time: false, notices: [] };
const model = computed(() => result.value.model ?? emptyModel);
const horizontal = computed(() => props.viz.orientation === "horizontal" && mark.value === "bar");
const mark = computed(() => props.viz.mark ?? "line");
const stacked = computed(() => chartStacked(props.viz));
const x = (row: ChartRow) => row.x;
const accessors = computed(() => model.value.series.map(s => (row: ChartRow) => row.values.get(s.key)?.coordinate ?? Number.NaN));
const colors = computed(() => model.value.series.map(s => s.color));
const plotHost = ref<HTMLElement | null>(null);
const { height: plotHeight, width: plotWidth } = useElementSize(plotHost);
const chartHeight = computed(() => Math.max(60, plotHeight.value || props.height - 30));
const thresholdLines = computed(() => result.value.thresholdLines);
const extent = computed(() => {
  let low = 0, high = 0;
  for (const row of model.value.rows) {
    let positive = 0, negative = 0;
    for (const value of row.values.values()) {
      const n = value?.coordinate ?? 0;
      if (stacked.value) { if (n < 0) negative += n; else positive += n; }
      else { low = Math.min(low, n); high = Math.max(high, n); }
    }
    if (stacked.value) { low = Math.min(low, negative); high = Math.max(high, positive); }
  }
  for (const line of thresholdLines.value) { low = Math.min(low, line.coordinate); high = Math.max(high, line.coordinate); }
  return chartTicks(low, high, props.count);
});
const domain = computed<[number, number]>(() => [extent.value[0], extent.value.at(-1) ?? 1]);
const xDomain = computed<[number, number]>(() => {
  const rows = model.value.rows;
  const first = rows[0]?.x ?? 0, last = rows.at(-1)?.x ?? 0;
  if (!model.value.time) return [-0.5, Math.max(0.5, rows.length - 0.5)];
  let step = Infinity;
  for (let i = 1; i < rows.length; i++) if (rows[i].x > rows[i - 1].x) step = Math.min(step, rows[i].x - rows[i - 1].x);
  if (!Number.isFinite(step)) step = 60000;
  return [first - step / 2, last + step / 2];
});
const timeFormatter = computed(() => chartTimeFormatter(props.frame, props.viz, props.count, preferences.zone));
const xScale = computed(() => model.value.time && !horizontal.value ? Scale.scaleUtc() : Scale.scaleLinear());
const plotWidthAvailable = computed(() => Math.max(0, (plotWidth.value || plotHost.value?.clientWidth || 0) - 80));
const categoryTicks = computed(() => {
  if (model.value.time) return undefined;
  const step = Math.max(1, Math.ceil(model.value.rows.length / Math.max(1, Math.floor((horizontal.value ? chartHeight.value : plotWidthAvailable.value) / 20))));
  return model.value.rows.filter((_, i) => i % step === 0).map(r => r.x);
});
const categoryAngle = computed(() => {
  if (horizontal.value || model.value.time) return 0;
  const space = plotWidthAvailable.value / Math.max(1, model.value.rows.length);
  return model.value.rows.some(row => row.label().length * 6 > space) ? -45 : 0;
});
const labelCapacity = computed(() => {
  const length = horizontal.value ? chartHeight.value : plotWidthAvailable.value;
  const spacing = mark.value !== "bar" ? 40 : horizontal.value ? 20 : 32;
  return Math.min(500, Math.floor(length / spacing));
});
const labelStride = computed(() => Math.max(1, Math.ceil(model.value.rows.length * model.value.series.length / Math.max(1, labelCapacity.value))));
const labelsFit = computed(() => {
  const rows = model.value.rows.length, series = model.value.series.length;
  const slots = mark.value === "bar" && stacked.value ? rows : rows * series;
  return rows * series <= 500 && slots <= labelCapacity.value;
});
const pointRows = computed(() => model.value.series.map((series, i) => {
  const count = model.value.series.length;
  const budget = Math.floor(labelCapacity.value / count) + (i < labelCapacity.value % count ? 1 : 0);
  const stride = Math.ceil(model.value.rows.length / Math.max(1, budget));
  return model.value.rows.filter((row, j) => budget > 0 && j % stride === i % stride && row.values.get(series.key));
}));
const labelAccessors = computed(() => {
  if (mark.value !== "area" || !stacked.value) return accessors.value;
  const tops = getStackedData(model.value.rows, 0, accessors.value);
  return tops.map(stack => {
    const coordinates = new Map(model.value.rows.map((row, i) => [row.key, stack[i][1]]));
    return (row: ChartRow) => coordinates.get(row.key) ?? Number.NaN;
  });
});
const pointLabels = computed(() => model.value.series.map(series => (row: ChartRow) => row.values.get(series.key)?.text ?? ""));
const pointLabelSelector = `.${Scatter.selectors.point} > text`;
function xTick(value: number | Date) {
  return model.value.time ? timeFormatter.value.format(Number(value)).replaceAll(",", "") : model.value.rows[Number(value)]?.label() ?? "";
}
const yTick = (value: number | Date) => chartValue(Number(value), props.viz.format).text;
const active = ref<number | null>(null);
const sharedTooltip = ref<{ component?: Tooltip }>();
const crosshair = ref<{ component?: Crosshair<ChartRow> }>();
watch(active, index => {
  if (!horizontal.value) return;
  const tooltipComponent = sharedTooltip.value?.component;
  if (index === null) tooltipComponent?.hide();
  else tooltipComponent?.show(tooltip(model.value.rows[index]), { x: plotWidthAvailable.value / 2, y: chartHeight.value * (index + 0.5) / model.value.rows.length });
});
// The container no longer receives data, so Unovis does not hide a hover readout of the old frame on refresh.
watch(model, () => {
  active.value = null;
  sharedTooltip.value?.component?.hide();
  crosshair.value?.component?.hide();
});
function keydown(event: KeyboardEvent) {
  if (!["ArrowRight", "ArrowLeft", "ArrowUp", "ArrowDown", "Home", "End", "Escape"].includes(event.key)) return;
  event.preventDefault();
  const last = model.value.rows.length - 1;
  if (event.key === "Escape") active.value = null;
  else if (event.key === "Home") active.value = 0;
  else if (event.key === "End") active.value = last;
  else active.value = Math.max(0, Math.min(last, (active.value ?? 0) + (["ArrowRight", "ArrowDown"].includes(event.key) ? 1 : -1)));
}
function tooltip(row: ChartRow): string {
  let total: FrameNumber = 0;
  const lines = model.value.series.map(s => {
    const value = row.values.get(s.key);
    if (value) {
      // Totals use the raw frame values, independent of display units and formatting.
      const raw = value.raw;
      if (typeof total === "number" && typeof raw === "number") total += raw;
      else { const a = isExact(total) ? total : parseExact(total), b = isExact(raw) ? raw : parseExact(raw); if (a && b) total = addExact(a, b); }
    }
    return `<div class="panel-chart__tooltip-row"><span><i style="background:${s.color}"></i>${escapeHtml(s.label)}</span><strong>${escapeHtml(value?.exact ?? t("panelChart.null"))}</strong></div>`;
  }).join("");
  return `<div role="tooltip" class="panel-chart__tooltip"><strong>${escapeHtml(row.label())}</strong>${lines}<div class="panel-chart__tooltip-row panel-chart__total"><span>${escapeHtml(t('common.total'))}</span><strong>${escapeHtml(chartValue(total, props.viz.format).exact)}</strong></div></div>`;
}
const barTooltipTriggers = computed(() => horizontal.value ? { [StackedBar.selectors.barGroup]: tooltip, [GroupedBar.selectors.barGroup]: tooltip } : {});
const tooltipTemplate = (datum: ChartRow | undefined) => datum ? tooltip(datum) : "";
function barLabels(svg: SVGSVGElement) {
  svg.querySelectorAll(pointLabelSelector).forEach(e => e.classList.add('panel-chart__point-value'));
  svg.querySelectorAll('.panel-chart__value').forEach(e => e.remove());
  if (!props.viz.show_values || !labelsFit.value || mark.value !== "bar") return;
  const groups = svg.querySelectorAll(`.${StackedBar.selectors.barGroup}, .${GroupedBar.selectors.barGroup}`);
  groups.forEach((group, rowIndex) => {
    const row = model.value.rows[rowIndex];
    if (!row) return;
    group.querySelectorAll<SVGPathElement>(`.${StackedBar.selectors.bar}, .${GroupedBar.selectors.bar}`).forEach((bar) => {
      // Unovis filters zero stacked segments, but preserves the original accessor index.
      const seriesIndex = Number(bar.getAttribute('unovis-pattern-index'));
      const series = model.value.series[seriesIndex];
      const value = series && row.values.get(series.key);
      if (!value || value.coordinate === 0) return;
      const box = bar.getBBox();
      const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
      text.setAttribute('class', 'panel-chart__value');
      text.setAttribute('x', String(box.x + box.width / 2));
      text.setAttribute('y', String(box.y + box.height / 2));
      text.setAttribute('text-anchor', 'middle');
      text.setAttribute('dominant-baseline', 'central');
      text.textContent = value.text;
      bar.parentNode?.append(text);
      // Unovis cannot resolve a CSS variable nested inside hsl(), so fall back to the fill the browser computed.
      const fill = chartLabelColor(getHexValue(series.color, text) ?? getHexValue(getComputedStyle(bar).fill, bar));
      if (fill) {
        text.style.fill = fill;
        text.style.stroke = fill === '#ffffff' ? '#000000' : '#ffffff';
      }
      if (box.height < 16 || text.getComputedTextLength() + 6 > box.width) text.remove();
    });
  });
}
</script>

<template>
  <div class="panel-chart">
    <p v-if="result.error" role="alert" class="text-xs text-destructive">{{ result.error }}</p>
    <p v-else-if="!model.rows.length" class="text-xs text-muted-foreground">{{ t('ui.noDataForThisTimeRange') }}</p>
    <template v-else>
      <div ref="plotHost" class="panel-chart__plot" tabindex="0" role="img" :aria-label="t('panelChart.inspect', { mark })" @keydown="keydown" @pointerdown="active = null" @pointermove="active = null" @pointerenter="active = null" @blur="active = null">
        <VisXYContainer :height="chartHeight" :duration="0" :x-scale="xScale" :x-domain="horizontal ? domain : xDomain" :y-domain="horizontal ? xDomain : domain" :yDirection="horizontal ? Direction.South : Direction.North" :padding="{ left: 8, right: 8, top: 8, bottom: 0 }" :margin="{ left: 8, right: 12, top: 10, bottom: 5 }" :on-render-complete="barLabels">
          <VisAxis :type="horizontal ? 'y' : 'x'" :tick-format="xTick" tick-text-color="var(--muted-foreground)" :tick-values="categoryTicks" :num-ticks="6" :tick-text-angle="categoryAngle" :tick-text-width="horizontal ? 100 : 80" :tick-text-hide-overlapping="model.time" :min-max-ticks-only-when-width-is-less="0" :grid-line="false" :tick-line="false" />
          <VisAxis :type="horizontal ? 'x' : 'y'" :tick-values="extent" tick-text-color="var(--muted-foreground)" :tick-format="yTick" :tick-line="false" :domain-line="false" />
          <VisStackedBar :data="model.rows" v-if="mark === 'bar' && stacked" :x="x" :y="accessors" :color="colors" :orientation="horizontal ? 'horizontal' : 'vertical'" :bar-padding="0.2" :rounded-corners="2" />
          <VisGroupedBar :data="model.rows" v-else-if="mark === 'bar'" :x="x" :y="accessors" :color="colors" :orientation="horizontal ? 'horizontal' : 'vertical'" :group-padding="0.2" :bar-padding="0.05" :bar-min-height="0" />
          <VisLine :data="model.rows" v-else-if="mark === 'line'" :x="x" :y="accessors" :color="colors" />
          <VisArea :data="model.rows" v-else-if="stacked" :x="x" :y="accessors" :color="colors" :opacity="0.35" :line="true" />
          <template v-else>
            <VisArea :data="model.rows" v-for="(accessor, i) in accessors" :key="model.series[i].key" :x="x" :y="accessor" :color="colors[i]" :opacity="0.35" :line="true" />
          </template>
          <template v-if="viz.show_values && mark !== 'bar'">
            <VisScatter v-for="(accessor, i) in labelAccessors" :key="model.series[i].key" :data="pointRows[i]" :x="x" :y="accessor" :size="0" :label="pointLabels[i]" label-color="var(--foreground)" label-position="top" />
          </template>
          <VisPlotline v-for="(line, i) in thresholdLines" :key="i" :axis="horizontal ? 'x' : 'y'" :value="line.coordinate" :color="`var(--panel-${line.color})`" :label-text="line.exact" :label-color="`var(--panel-${line.color})`" :duration="0" :line-style="[5, 3]" />
          <VisTooltip ref="sharedTooltip" :triggers="barTooltipTriggers" />
          <VisCrosshair ref="crosshair" :data="model.rows" v-if="!horizontal" :duration="0" :x="x" :y="stacked ? undefined : accessors" :yStacked="stacked ? accessors : undefined" :color="colors" :template="tooltipTemplate" :hideWhenFarFromPointer="false" :forceShowAt="active === null ? undefined : model.rows[active]?.x" />
        </VisXYContainer>
      </div>
      <div class="sr-only" aria-live="polite" aria-atomic="true">
        <template v-if="active !== null && model.rows[active]">
          {{ model.rows[active].label() }}
          <span v-for="series in model.series" :key="series.key">{{ series.label }} {{ model.rows[active].values.get(series.key)?.exact ?? t('panelChart.null') }}. </span>
        </template>
      </div>
      <p v-if="viz.show_values && plotWidthAvailable > 0 && (mark === 'bar' ? !labelsFit : labelStride > 1)" role="status" class="text-xs text-muted-foreground">{{ t('panelChart.labelsHidden') }}</p>
      <div v-if="viz.legend !== false && model.series.length > 1" class="panel-chart__legend" :aria-label="t('panelChart.seriesLegend')"><span v-for="series in model.series" :key="series.key"><i :style="{ background: series.color }" />{{ series.label }}</span></div>
      <p v-for="notice in model.notices" :key="notice" role="status" class="text-xs text-muted-foreground">{{ notice }}</p>
    </template>
  </div>
</template>

<style>
.panel-chart { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; position: relative; --vis-tooltip-padding: 0px; --vis-tooltip-background-color: transparent; --vis-tooltip-border-color: transparent; --vis-tooltip-shadow-color: transparent; --vis-axis-tick-label-color: var(--muted-foreground); --vis-axis-grid-color: var(--border); --vis-crosshair-line-stroke-color: var(--muted-foreground); }
.panel-chart__plot { flex: 1 1 0; min-height: 60px; min-width: 200px; overflow: hidden; }
.panel-chart__value, .panel-chart text.panel-chart__point-value { fill: var(--foreground); stroke: var(--background); stroke-width: 2px; stroke-linejoin: round; font-weight: 650; paint-order: stroke; font-size: 10px; font-variant-numeric: tabular-nums; pointer-events: none; }
.panel-chart__legend { flex-shrink: 0; display: flex; flex-wrap: wrap; gap: 6px 12px; justify-content: center; font-size: 11px; }
.panel-chart__legend span { display: inline-flex; align-items: center; gap: 4px; }
.panel-chart__legend i, .panel-chart__tooltip i { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 4px; }
.panel-chart__tooltip { min-width: 160px; background: var(--popover); color: var(--popover-foreground); border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; font-size: 11px; }
.panel-chart__tooltip-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 4px; }
.panel-chart__total { border-top: 1px solid var(--border); padding-top: 4px; }
</style>
