<script setup lang="ts">
import { computed, ref } from "vue";
import { useElementSize } from "@vueuse/core";
import { usePreferencesStore } from "@/stores/preferences";
import type { PanelFrame } from "@/utils/panelFrame";
import { buildPanelChart, type ChartModel, type ChartViz } from "@/utils/panelCharts";
import { formatNumber } from "@/utils/numberFormat";
import type { ResolvedWindow } from "@/utils/timeWindow";

const props = withDefaults(defineProps<{ frame: PanelFrame; viz: ChartViz; count: boolean; height?: number; range?: ResolvedWindow }>(), { height: 200 });
const preferences = usePreferencesStore();
const host = ref<HTMLElement | null>(null);
const { width, height: hostHeight } = useElementSize(host);
const result = computed(() => {
  try { return { model: buildPanelChart(props.frame, props.viz, props.count, preferences.zone), error: null }; }
  catch (error) { return { model: null, error: error instanceof Error ? error.message : String(error) }; }
});
const emptyModel: ChartModel = { rows: [], series: [], time: false, notices: [] };
const model = computed(() => result.value.model ?? emptyModel);
const approximate = computed(() => model.value.rows.some((r) => [...r.values.values()].some((v) => v?.approximate)));
const horizontal = computed(() => props.viz.orientation === "horizontal");
const mark = computed(() => props.viz.mark ?? "line");
const stacked = computed(() => props.viz.stack === "stacked");
const plot = computed(() => {
  const w = Math.max(240, width.value || 640);
  const h = Math.max(90, (hostHeight.value > 0 ? Math.min(props.height, hostHeight.value) : props.height) - 2 - (props.viz.legend !== false && model.value.series.length > 1 ? 26 : 0) - model.value.notices.length * 18);
  return { w, h, left: horizontal.value ? 105 : 60, right: w - 20, top: 18, bottom: h - 32 };
});
const geometry = computed(() => {
  const { rows, series, time } = model.value;
  const cells = rows.map((row) => {
    let positive = 0;
    let negative = 0;
    return series.map((s) => {
      const value = row.values.get(s.key);
      const n = value?.coordinate ?? 0;
      const start = stacked.value ? n < 0 ? negative : positive : 0;
      if (stacked.value) { if (n < 0) negative += n; else positive += n; }
      return { series: s, row, value, start, end: start + n };
    });
  });
  const bounds = cells.flat().flatMap((c) => [c.start, c.end]);
  let low = bounds.reduce((a, b) => Math.min(a, b), 0);
  let high = bounds.reduce((a, b) => Math.max(a, b), 0);
  if (low === high) high = low + 1;
  const span = high - low;
  if (!Number.isFinite(span)) return { cells: [], low: 0, high: 1, error: "The chart's numeric range is too wide. Use a table to see exact values.", x: (_v: number) => 0, y: (_v: number) => 0, band: 1 };
  if (low < 0) low -= span * 0.03;
  high += span * 0.12;
  let xLow = rows[0]?.x ?? 0;
  let xHigh = rows.at(-1)?.x ?? 1;
  let step = 1;
  if (time) {
    const differences = rows.slice(1).map((r, i) => r.x - rows[i].x).filter((v) => v > 0);
    step = differences.length ? Math.min(...differences) : 86400000;
    xLow -= step / 2;
    xHigh += step / 2;
    if (props.count && props.range) { xLow = props.range.start; xHigh = props.range.end; }
  } else { xLow = -0.5; xHigh = Math.max(0.5, rows.length - 0.5); }
  const p = plot.value;
  const xLength = horizontal.value ? p.bottom - p.top : p.right - p.left;
  const x = (value: number) => (horizontal.value ? p.top : p.left) + (value - xLow) / (xHigh - xLow || 1) * xLength;
  const y = (value: number) => horizontal.value ? p.left + (value - low) / (high - low) * (p.right - p.left) : p.bottom - (value - low) / (high - low) * (p.bottom - p.top);
  return { cells, low, high, x, y, band: Math.min(60, step / (xHigh - xLow || 1) * xLength * 0.75), error: null };
});
function point(cell: (typeof geometry.value.cells)[number][number], seriesIndex: number) {
  const g = geometry.value;
  const band = stacked.value ? g.band : g.band / Math.max(1, model.value.series.length);
  const center = g.x(cell.row.x) + (mark.value === "bar" && !stacked.value ? -g.band / 2 + band * (seriesIndex + 0.5) : 0);
  const end = g.y(cell.end);
  const start = g.y(cell.start);
  return { x: horizontal.value ? end : center, y: horizontal.value ? center : end, bx: horizontal.value ? Math.min(start, end) : center - band / 2, by: horizontal.value ? center - band / 2 : Math.min(start, end), width: horizontal.value ? Math.abs(end - start) : band * 0.9, height: horizontal.value ? band * 0.9 : Math.abs(end - start) };
}
function path(index: number, area = false): string {
  const paths: string[] = [];
  let segment: [number, number, number][] = [];
  const flush = () => {
    if (!segment.length) return;
    const pairs = segment.map(([x, y]) => `${x},${y}`);
    let d = `M${pairs.join(" L")}`;
    if (area) d += ` L${[...segment].reverse().map(([x, _y, base]) => horizontal.value ? `${base},${_y}` : `${x},${base}`).join(" L")} Z`;
    paths.push(d);
    segment = [];
  };
  for (const row of geometry.value.cells) {
    const cell = row[index];
    if (!cell?.value) { flush(); continue; }
    const p = point(cell, index);
    segment.push([p.x, p.y, geometry.value.y(cell.start)]);
  }
  flush();
  return paths.join(" ");
}
const ticks = computed(() => Array.from({ length: 4 }, (_, i) => geometry.value.low + (geometry.value.high - geometry.value.low) * i / 3));
const xTicks = computed(() => {
  let last = -Infinity;
  const spacing = horizontal.value ? 20 : 110;
  return model.value.rows.filter((row) => {
    const position = geometry.value.x(row.x);
    if (position - last < spacing) return false;
    last = position;
    return true;
  });
});
const tooltip = ref("");
</script>

<template>
  <div ref="host" class="panel-chart">
    <p v-if="result.error || geometry.error" role="alert" class="text-xs text-destructive">{{ result.error || geometry.error }}</p>
    <p v-else-if="!model.rows.length" class="text-xs text-muted-foreground">No data</p>
    <template v-else>
      <svg :width="plot.w" :height="plot.h" role="img" :aria-label="`${mark} chart`" class="panel-chart__plot">
        <text v-if="approximate" class="panel-chart__approximate" :x="plot.left - 15" :y="plot.top - 4">≈<title>Approximate coordinates. Tooltips show exact values.</title></text>
        <g v-for="tick in ticks" :key="tick" class="panel-chart__axis">
          <line :x1="horizontal ? geometry.y(tick) : plot.left" :x2="horizontal ? geometry.y(tick) : plot.right" :y1="horizontal ? plot.top : geometry.y(tick)" :y2="horizontal ? plot.bottom : geometry.y(tick)" />
          <text :x="horizontal ? geometry.y(tick) : plot.left - 5" :y="horizontal ? plot.bottom + 18 : geometry.y(tick) + 3" :text-anchor="horizontal ? 'middle' : 'end'">{{ formatNumber(tick, viz.format, { compact: true }).text }}</text>
        </g>
        <text v-for="row in xTicks" :key="row.key" class="panel-chart__tick" :x="horizontal ? plot.left - 5 : geometry.x(row.x)" :y="horizontal ? geometry.x(row.x) + 3 : plot.bottom + 18" :text-anchor="horizontal ? 'end' : 'middle'"><title>{{ row.label }}</title>{{ row.label.length > 18 ? row.label.slice(0, 16) + '…' : row.label }}</text>
        <template v-if="mark !== 'bar'">
          <path v-for="(series, i) in model.series" :key="series.key" :d="path(i, mark === 'area')" :fill="mark === 'area' ? series.color : 'none'" :fill-opacity="0.25" :stroke="series.color" stroke-width="2" />
        </template>
        <g v-for="row in geometry.cells" :key="row[0]?.row.key">
          <template v-for="(cell, i) in row" :key="cell.series.key">
            <g v-if="cell.value" class="panel-chart__point" tabindex="0" :aria-label="`${cell.row.label}, ${cell.series.label}: ${cell.value.exact}`" @mouseenter="tooltip = `${cell.row.label} · ${cell.series.label}: ${cell.value.exact}`" @mouseleave="tooltip = ''" @focus="tooltip = `${cell.row.label} · ${cell.series.label}: ${cell.value.exact}`" @blur="tooltip = ''">
              <title>{{ cell.row.label }} · {{ cell.series.label }}: {{ cell.value.exact }}</title>
              <rect v-if="mark === 'bar'" class="panel-chart__bar" :x="point(cell, i).bx" :y="point(cell, i).by" :width="point(cell, i).width" :height="point(cell, i).height" :fill="cell.series.color" rx="2" />
              <circle v-else :cx="point(cell, i).x" :cy="point(cell, i).y" r="3" :fill="cell.series.color" />
              <text v-if="viz.show_values && (!stacked || mark !== 'bar' || (horizontal ? point(cell, i).width : point(cell, i).height) >= 12)" class="panel-chart__value" :class="{ 'panel-chart__value--stacked': stacked && mark === 'bar' }" :x="stacked && mark === 'bar' ? point(cell, i).bx + point(cell, i).width / 2 : point(cell, i).x + (horizontal ? 4 : 0)" :y="stacked && mark === 'bar' ? point(cell, i).by + point(cell, i).height / 2 + 3 : point(cell, i).y - (horizontal ? 0 : 5)" :text-anchor="horizontal && !(stacked && mark === 'bar') ? 'start' : 'middle'">{{ cell.value.text }}</text>
            </g>
          </template>
        </g>
      </svg>
      <p v-if="tooltip" class="panel-chart__tooltip" role="tooltip">{{ tooltip }}</p>
      <div v-if="viz.legend !== false && model.series.length > 1" class="panel-chart__legend" aria-label="Series legend"><span v-for="series in model.series" :key="series.key"><i :style="{ background: series.color }" />{{ series.label }}</span></div>
      <p v-for="notice in model.notices" :key="notice" role="status" class="text-xs text-muted-foreground">{{ notice }}</p>
    </template>
  </div>
</template>

<style scoped>
.panel-chart { width: 100%; height: 100%; overflow: auto; position: relative; }
.panel-chart__plot { display: block; overflow: visible; }
.panel-chart__approximate { fill: var(--muted-foreground); font-size: 12px; }
.panel-chart__axis line { stroke: var(--border); stroke-opacity: 0.5; }
.panel-chart__axis text, .panel-chart__tick { fill: var(--muted-foreground); font-size: 10px; }
.panel-chart__value { fill: var(--foreground); font-size: 10px; font-variant-numeric: tabular-nums; }
.panel-chart__value--stacked { fill: white; font-size: 9px; }
.panel-chart__legend { display: flex; flex-wrap: wrap; gap: 12px; justify-content: center; font-size: 11px; }
.panel-chart__legend span { display: inline-flex; align-items: center; gap: 4px; }
.panel-chart__legend i { width: 8px; height: 8px; border-radius: 2px; }
.panel-chart__tooltip { position: absolute; top: 0; right: 0; background: var(--popover); color: var(--popover-foreground); border: 1px solid var(--border); border-radius: 4px; padding: 5px 8px; font-size: 11px; pointer-events: none; max-width: 100%; overflow-wrap: anywhere; z-index: 1; }
.panel-chart__point:hover { opacity: 0.8; }
</style>
