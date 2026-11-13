<script setup lang="ts">
import { useElementSize } from "@vueuse/core";
import { computed, markRaw, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { VisAxis, VisStackedBar, VisGroupedBar, VisLine, VisArea, VisScatter, VisXYContainer, VisCrosshair, VisTooltip } from "@unovis/vue";
import { Scale, StackedBar, GroupedBar } from "@unovis/ts";
import { usePreferencesStore } from "@/stores/preferences";
import type { PanelFrame } from "@/utils/panelFrame";
import { buildPanelChart, chartStacked, chartValue, type ChartModel, type ChartRow, type ChartViz } from "@/utils/panelCharts";
import { chartTicks, chartTimeFormatter } from "@/utils/panelChartAxes";
import { addExact, isExact, parseExact, type FrameNumber } from "@/utils/exactNumber";
import { escapeHtml } from "@/lib/html";
import type { ResolvedWindow } from "@/utils/timeWindow";

const props = withDefaults(defineProps<{ frame: PanelFrame; viz: ChartViz; count: boolean; height?: number; range?: ResolvedWindow }>(), { height: 200 });
const { t } = useI18n();
const preferences = usePreferencesStore();
const result = computed(() => {
  try {
    const model = buildPanelChart(props.frame, props.viz, props.count, preferences.zone);
    markRaw(model.rows);
    return { model, error: null };
  }
  catch (error) { return { model: null, error: error instanceof Error ? error.message : String(error) }; }
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
const { height: plotHeight } = useElementSize(plotHost);
const chartHeight = computed(() => Math.max(60, plotHeight.value || props.height - 30));
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
const categoryTicks = computed(() => model.value.time ? undefined : model.value.rows.map(r => r.x));
function xTick(value: number | Date) {
  return model.value.time ? timeFormatter.value.format(Number(value)).replaceAll(",", "") : model.value.rows[Number(value)]?.label ?? "";
}
const yTick = (value: number | Date) => chartValue(Number(value), props.viz.format).text;
const active = ref<number | null>(null);
watch(model, () => { active.value = null; });
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
    return `<div class="panel-chart__tooltip-row"><span><i style="background:${s.color}"></i>${escapeHtml(s.label)}</span><strong>${escapeHtml(value?.exact ?? "NULL")}</strong></div>`;
  }).join("");
  return `<div role="tooltip" class="panel-chart__tooltip"><strong>${escapeHtml(row.label)}</strong>${lines}<div class="panel-chart__tooltip-row panel-chart__total"><span>${escapeHtml(t('common.total'))}</span><strong>${escapeHtml(chartValue(total, props.viz.format).exact)}</strong></div></div>`;
}
const barTooltipTriggers = computed(() => horizontal.value ? { [StackedBar.selectors.barGroup]: tooltip, [GroupedBar.selectors.barGroup]: tooltip } : {});
const tooltipTemplate = (_datum: unknown, _x: number | Date, data: ChartRow[], index?: number) => index === undefined || !data[index] ? "" : tooltip(data[index]);
const keyboardTooltip = computed(() => active.value === null || !model.value.rows[active.value] ? "" : tooltip(model.value.rows[active.value]));
function barLabels(svg: SVGSVGElement) {
  svg.querySelectorAll('.panel-chart__value').forEach(e => e.remove());
  if (!props.viz.show_values || mark.value !== "bar") return;
  const groups = svg.querySelectorAll(`.${StackedBar.selectors.barGroup}, .${GroupedBar.selectors.barGroup}`);
  groups.forEach((group, rowIndex) => {
    const row = model.value.rows[rowIndex];
    if (!row) return;
    group.querySelectorAll<SVGPathElement>(`.${StackedBar.selectors.bar}, .${GroupedBar.selectors.bar}`).forEach((bar, seriesIndex) => {
      const series = model.value.series[seriesIndex];
      const value = series && row.values.get(series.key);
      if (!value) return;
      const box = bar.getBBox();
      if (stacked.value && (horizontal.value ? box.width < 30 : box.height < 13)) return;
      const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
      text.setAttribute('class', 'panel-chart__value');
      text.setAttribute('x', String(box.x + box.width / 2));
      text.setAttribute('y', String(box.y + box.height / 2 + 3));
      text.setAttribute('text-anchor', 'middle');
      text.textContent = value.text;
      bar.parentNode?.append(text);
    });
  });
}
</script>

<template>
  <div class="panel-chart">
    <p v-if="result.error" role="alert" class="text-xs text-destructive">{{ result.error }}</p>
    <p v-else-if="!model.rows.length" class="text-xs text-muted-foreground">{{ t('ui.noDataForThisTimeRange') }}</p>
    <template v-else>
      <div ref="plotHost" class="panel-chart__plot" tabindex="0" role="img" :aria-label="`${mark} chart. Use arrow keys to inspect buckets.`" @keydown="keydown" @focus="active = 0" @blur="active = null">
        <VisXYContainer :data="model.rows" :height="chartHeight" :duration="0" :x-scale="xScale" :x-domain="horizontal ? domain : xDomain" :y-domain="horizontal ? xDomain : domain" :padding="{ left: 8, right: 8, top: 8, bottom: 0 }" :margin="{ left: 8, right: 12, top: 10, bottom: 5 }" :on-render-complete="barLabels">
          <VisAxis :type="horizontal ? 'y' : 'x'" :tick-format="xTick" tick-text-color="var(--muted-foreground)" :tick-values="categoryTicks" :num-ticks="6" :tick-text-angle="!horizontal && !model.time ? -45 : 0" :tick-text-width="horizontal ? 100 : 80" :tick-text-hide-overlapping="model.time" :min-max-ticks-only-when-width-is-less="0" :grid-line="false" :tick-line="false" />
          <VisAxis :type="horizontal ? 'x' : 'y'" :tick-values="extent" tick-text-color="var(--muted-foreground)" :tick-format="yTick" :tick-line="false" :domain-line="false" />
          <VisStackedBar v-if="mark === 'bar' && stacked" :x="x" :y="accessors" :color="colors" :orientation="horizontal ? 'horizontal' : 'vertical'" :bar-padding="0.2" :rounded-corners="2" />
          <VisGroupedBar v-else-if="mark === 'bar'" :x="x" :y="accessors" :color="colors" :orientation="horizontal ? 'horizontal' : 'vertical'" :group-padding="0.2" :bar-padding="0.05" :bar-min-height="0" />
          <VisLine v-else-if="mark === 'line'" :x="x" :y="accessors" :color="colors" />
          <VisArea v-else-if="stacked" :x="x" :y="accessors" :color="colors" :opacity="0.35" :line="true" />
          <template v-else>
            <VisArea v-for="(accessor, i) in accessors" :key="model.series[i].key" :x="x" :y="accessor" :color="colors[i]" :opacity="0.35" :line="true" />
          </template>
          <template v-if="viz.show_values && mark !== 'bar'">
            <VisScatter v-for="(accessor, i) in accessors" :key="model.series[i].key" :x="x" :y="accessor" :size="0" :label="(row: ChartRow) => row.values.get(model.series[i].key)?.text ?? ''" label-color="var(--foreground)" label-position="top" />
          </template>
          <VisTooltip :triggers="barTooltipTriggers" />
          <VisCrosshair v-if="!horizontal" :x="x" :y="stacked ? undefined : accessors" :y-stacked="stacked ? accessors : undefined" :color="colors" :template="tooltipTemplate" :hide-when-far-from-pointer="false" :force-show-at="active === null ? undefined : model.rows[active]?.x" />
        </VisXYContainer>
      </div>
      <div v-if="keyboardTooltip" class="panel-chart__keyboard" aria-live="polite" v-html="keyboardTooltip" />
      <div v-if="viz.legend !== false && model.series.length > 1" class="panel-chart__legend" aria-label="Series legend"><span v-for="series in model.series" :key="series.key"><i :style="{ background: series.color }" />{{ series.label }}</span></div>
      <p v-for="notice in model.notices" :key="notice" role="status" class="text-xs text-muted-foreground">{{ notice }}</p>
    </template>
  </div>
</template>

<style>
.panel-chart { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; position: relative; --vis-tooltip-padding: 0px; --vis-tooltip-background-color: transparent; --vis-tooltip-border-color: transparent; --vis-tooltip-shadow-color: transparent; --vis-axis-tick-label-color: var(--muted-foreground); --vis-axis-grid-color: var(--border); --vis-crosshair-line-stroke-color: var(--muted-foreground); }
.panel-chart__plot { flex: 1 1 0; min-height: 60px; min-width: 200px; overflow: hidden; }
.panel-chart__value, .panel-chart .vis-scatter-label { fill: var(--foreground); stroke: var(--background); stroke-width: 3px; paint-order: stroke; font-size: 10px; font-variant-numeric: tabular-nums; pointer-events: none; }
.panel-chart__legend { flex-shrink: 0; display: flex; flex-wrap: wrap; gap: 6px 12px; justify-content: center; font-size: 11px; }
.panel-chart__legend span { display: inline-flex; align-items: center; gap: 4px; }
.panel-chart__legend i, .panel-chart__tooltip i { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 4px; }
.panel-chart__tooltip { min-width: 160px; background: var(--popover); color: var(--popover-foreground); border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; font-size: 11px; }
.panel-chart__tooltip-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 4px; }
.panel-chart__total { border-top: 1px solid var(--border); padding-top: 4px; }
.panel-chart__keyboard { position: absolute; top: 0; right: 0; z-index: 2; pointer-events: none; }
</style>
