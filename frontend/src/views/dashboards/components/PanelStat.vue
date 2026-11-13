<script setup lang="ts">
import { usePreferencesStore } from "@/stores/preferences";
import { computed, ref } from "vue";
import { useResizeObserver } from "@vueuse/core";
import type { DashboardQueryPanel } from "@/api/dashboards";
import { frameWithKinds, reduceStat, type PanelFrame } from "@/utils/panelFrame";
import { formatNumber } from "@/utils/numberFormat";

const preferences = usePreferencesStore();
const props = defineProps<{ frame: PanelFrame; panel: DashboardQueryPanel }>();
const container = ref<HTMLElement>();
const measure = ref<HTMLElement>();
const width = ref(0);
useResizeObserver(container, ([entry]) => { width.value = entry.contentRect.width; });
const result = computed(() => {
  const viz = props.panel.viz;
  if (viz.type !== "stat") return { error: "Invalid stat visualization" };
  try {
    const frame = frameWithKinds(props.frame, viz.columns);
    return { stat: reduceStat(frame, { value: props.panel.query.mode === "count" ? "count" : viz.value ?? "", reduce: viz.reduce ?? (props.panel.query.mode === "count" ? "sum" : "last"), orderBy: viz.order_by, count: props.panel.query.mode === "count", zone: preferences.zone }) };
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
  const compact = width.value > 0 && (measure.value?.getBoundingClientRect().width ?? 0) > width.value - 16;
  return formatNumber(value, format.value, { compact });
});
</script>

<template>
  <div ref="container" class="panel-stat">
    <p v-if="result.error" class="text-xs text-destructive" role="alert">{{ result.error }}</p>
    <template v-else>
      <span ref="measure" class="panel-stat__measure" aria-hidden="true">{{ standard?.text }}</span>
      <span class="panel-stat__value" :title="formatted?.exact">{{ formatted?.text ?? (result.stat?.rowLabel ? 'NULL' : 'No data') }}</span>
      <span v-if="result.stat?.rowLabel" class="text-xs text-muted-foreground">{{ result.stat.rowLabel }}</span>
      <p v-for="notice in result.stat?.notices" :key="notice" class="text-xs text-muted-foreground" role="status">{{ notice }}</p>
    </template>
  </div>
</template>

<style scoped>
.panel-stat { display: flex; flex-direction: column; gap: 0.5rem; align-items: center; justify-content: center; width: 100%; height: 100%; padding: 0.5rem; container-type: inline-size; overflow: hidden; position: relative; }
.panel-stat__value { font-size: clamp(1.75rem, 6cqw, 3rem); font-weight: 650; line-height: 1.1; font-variant-numeric: tabular-nums; color: var(--foreground); letter-spacing: -0.02em; overflow-wrap: anywhere; }
.panel-stat__measure { position: absolute; visibility: hidden; white-space: nowrap; font-size: 1.75rem; font-weight: 650; }
</style>
