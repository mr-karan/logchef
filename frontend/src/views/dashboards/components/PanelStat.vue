<script setup lang="ts">
import { VisXYContainer, VisLine, VisCrosshair, VisTooltip } from "@unovis/vue";
import { thresholdColor, paletteStyle } from "@/utils/panelPresentation";
import { escapeHtml } from "@/lib/html";
import { chartValue } from "@/utils/panelCharts";
import { useI18n } from "vue-i18n";
import type { CrosshairConfigInterface, Crosshair, Tooltip } from "@unovis/ts";
import { usePreferencesStore } from "@/stores/preferences";
import { computed, ref, watch } from "vue";
import { useResizeObserver } from "@vueuse/core";
import type { DashboardQueryPanel } from "@/api/dashboards";
import { frameWithKinds, reduceStat, type PanelFrame } from "@/utils/panelFrame";
import { formatNumber } from "@/utils/numberFormat";

const { t } = useI18n();
const preferences = usePreferencesStore();
const props = defineProps<{ frame: PanelFrame; panel: DashboardQueryPanel }>();
const container = ref<HTMLElement>();
const measure = ref<HTMLElement>();
const width = ref(0);
const measuredWidth = ref(0);
useResizeObserver(container, ([entry]) => { width.value = entry.contentRect.width; });
useResizeObserver(measure, ([entry]) => { measuredWidth.value = entry.contentRect.width; });
const kinds = computed(() => props.panel.viz.type === "stat" ? props.panel.viz.columns : undefined);
const typedFrame = computed(() => frameWithKinds(props.frame, kinds.value));
const result = computed(() => {
  const viz = props.panel.viz;
  if (viz.type !== "stat") return { error: "Invalid stat visualization" };
  try {
    return { stat: reduceStat(typedFrame.value, { value: props.panel.query.mode === "count" ? "count" : viz.value ?? "", reduce: viz.reduce ?? (props.panel.query.mode === "count" ? "sum" : "last"), orderBy: viz.order_by, count: props.panel.query.mode === "count", zone: preferences.zone, compare: viz.compare, sparkline: viz.sparkline }) };
  } catch (error) {
    return { error: error instanceof Error ? error.message : "Unable to read stat" };
  }
});
const format = computed(() => props.panel.viz.type === "stat" ? props.panel.viz.format : undefined);
const standard = computed(() => {
  const value = result.value.stat?.value;
  return value === null || value === undefined ? null : formatNumber(value, { ...format.value, notation: "standard" });
});
const formatted = computed(() => {
  const value = result.value.stat?.value;
  if (value === null || value === undefined) return null;
  const compact = width.value > 0 && measuredWidth.value > width.value - 16;
  return formatNumber(value, format.value, { compact });
});
const thresholdStyle = computed(() => props.panel.viz.type === "stat" ? paletteStyle(thresholdColor(result.value.stat?.value, props.panel.viz.thresholds), props.panel.viz.color_mode === "background") : {});
const backgroundColor = computed(() => props.panel.viz.type === "stat" && props.panel.viz.color_mode === "background" ? thresholdStyle.value.color : undefined);
const comparison = computed(() => result.value.stat?.comparison);
const delta = computed(() => comparison.value ? formatNumber(comparison.value.delta, format.value) : null);
const percent = computed(() => comparison.value?.percent === null || comparison.value?.percent === undefined ? null : formatNumber(comparison.value.percent, { decimals: 2, suffix: "%" }));
const deltaClass = computed(() => {
  const good = props.panel.viz.type === "stat" ? props.panel.viz.compare?.good ?? "neutral" : "neutral";
  const direction = comparison.value?.direction ?? 0;
  return !direction || good === "neutral" ? "text-muted-foreground" : (direction > 0) === (good === "up") ? "text-green-600 dark:text-green-400" : "text-red-600 dark:text-red-400";
});
const points = computed(() => result.value.stat?.points ?? []);
const sparkline = computed(() => {
  try {
    const rows = points.value.map(point => ({ ...point, plot: point.value === null ? null : chartValue(point.value) }));
    return { rows, approximate: rows.some(point => point.plot?.approximate), error: null };
  } catch (error) {
    return { rows: [], approximate: false, error: error instanceof Error ? error.message : String(error) };
  }
});
type PlotPoint = (typeof sparkline.value.rows)[number];
const x = (point: PlotPoint) => point.x;
const y = (point: PlotPoint) => point.plot?.coordinate ?? Number.NaN;
const pointText = (point: PlotPoint) => `${point.label}: ${point.value === null ? "NULL" : formatNumber(point.value, format.value).exact}`;
const sparklineTemplate: NonNullable<CrosshairConfigInterface<PlotPoint>["template"]> = point => escapeHtml(pointText(point));
const active = ref<number | null>(null);
const crosshair = ref<{ component?: Crosshair<PlotPoint> }>();
const tooltip = ref<{ component?: Tooltip }>();
watch(sparkline, () => {
  active.value = null;
  tooltip.value?.component?.hide();
  crosshair.value?.component?.hide();
});
function keydown(event: KeyboardEvent) {
  if (!["ArrowRight", "ArrowLeft", "ArrowUp", "ArrowDown", "Home", "End", "Escape"].includes(event.key)) return;
  event.preventDefault();
  const last = sparkline.value.rows.length - 1;
  if (event.key === "Escape") active.value = null;
  else if (event.key === "Home") active.value = 0;
  else if (event.key === "End") active.value = last;
  else active.value = Math.max(0, Math.min(last, (active.value ?? 0) + (["ArrowRight", "ArrowDown"].includes(event.key) ? 1 : -1)));
}
</script>

<template>
  <div ref="container" class="panel-stat" :style="panel.viz.type === 'stat' && panel.viz.color_mode === 'background' ? thresholdStyle : {}">
    <p v-if="result.error" class="text-xs text-destructive" :style="{ color: backgroundColor }" role="alert">{{ result.error }}</p>
    <template v-else>
      <span ref="measure" class="panel-stat__measure" aria-hidden="true">{{ standard?.text }}</span>
      <span class="panel-stat__value" :style="{ color: thresholdStyle.color }" :title="formatted?.exact">{{ formatted?.text ?? (result.stat?.rowLabel ? 'NULL' : 'No data') }}</span>
      <span v-if="result.stat?.rowLabel" class="text-xs text-muted-foreground" :style="{ color: backgroundColor }">{{ result.stat.rowLabel }}</span>
      <div v-if="comparison" class="panel-stat__comparison text-xs text-center" :class="deltaClass" :style="{ color: backgroundColor }">
        <span :title="delta?.exact">{{ comparison.direction > 0 ? '+' : '' }}{{ delta?.text }}</span>
        <span v-if="percent"> ({{ comparison.direction > 0 ? '+' : '' }}{{ percent.text }})</span>
        <span class="block text-muted-foreground" :style="{ color: backgroundColor }">{{ t('dashboardBuilder.ComparedWith', { date: comparison.rowLabel }) }}</span>
      </div>
      <template v-if="panel.viz.type === 'stat' && panel.viz.sparkline && points.length">
        <!-- Messages stay outside role="img": assistive technology treats an image's children as decoration. -->
        <p v-if="sparkline.error" class="text-xs text-destructive" :style="{ color: backgroundColor }" role="alert">{{ sparkline.error }}</p>
        <span v-else-if="sparkline.approximate" class="text-xs text-muted-foreground" :style="{ color: backgroundColor }">{{ t('dashboardBuilder.ApproximateSparkline') }}</span>
      </template>
      <div v-if="panel.viz.type === 'stat' && panel.viz.sparkline && points.length && !sparkline.error" class="panel-stat__sparkline w-full" tabindex="0" role="img" :aria-label="t('panelChart.inspect', { mark: t('dashboardBuilder.Sparkline') })" @keydown="keydown" @pointerdown="active = null" @pointermove="active = null" @pointerenter="active = null" @blur="active = null">
        <VisXYContainer aria-hidden="true" :data="sparkline.rows" :height="48" :duration="0" :margin="{ top: 2, bottom: 2, left: 2, right: 2 }">
          <VisLine :x="x" :y="y" :color="backgroundColor ?? 'var(--primary)'" />
          <VisCrosshair ref="crosshair" :x="x" :y="y" :duration="0" :template="sparklineTemplate" :hide-when-far-from-pointer="false" :force-show-at="active === null ? undefined : sparkline.rows[active]?.x" />
          <VisTooltip ref="tooltip" />
        </VisXYContainer>
      </div>
      <div class="sr-only" aria-live="polite" aria-atomic="true"><template v-if="active !== null && sparkline.rows[active]">{{ pointText(sparkline.rows[active]) }}</template></div>
      <p v-for="notice in result.stat?.notices" :key="notice" class="text-xs text-muted-foreground" :style="{ color: backgroundColor }" role="status">{{ notice }}</p>
    </template>
  </div>
</template>

<style scoped>
.panel-stat { --vis-tooltip-background-color: var(--popover); --vis-tooltip-text-color: var(--popover-foreground); --vis-tooltip-border-color: var(--border); display: flex; flex-direction: column; gap: 0.5rem; align-items: center; justify-content: center; width: 100%; height: 100%; padding: 0.5rem; container-type: inline-size; overflow: hidden; position: relative; }
.panel-stat__value { font-size: clamp(1.75rem, 6cqw, 3rem); font-weight: 650; line-height: 1.1; font-variant-numeric: tabular-nums; color: var(--foreground); letter-spacing: -0.02em; overflow-wrap: anywhere; }
.panel-stat__measure { position: absolute; visibility: hidden; white-space: nowrap; font-size: 1.75rem; font-weight: 650; }
</style>
