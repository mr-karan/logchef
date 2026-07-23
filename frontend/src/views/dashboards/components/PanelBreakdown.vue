<script setup lang="ts">
import { computed } from "vue";
import { VisDonut, VisSingleContainer } from "@unovis/vue";
import type { HistogramData } from "@/services/HistogramService";
import {
  buildBreakdownChartModel,
  buildDonutBreakdownChartModel,
  type BreakdownCategory,
} from "@/utils/breakdown-chart";

interface Props {
  buckets: HistogramData[];
  groupBy: string;
  notice?: string | null;
  height?: number;
  view?: "horizontal-bars" | "donut";
}
const props = withDefaults(defineProps<Props>(), { notice: null, height: 160, view: "horizontal-bars" });

const bars = computed(() => buildBreakdownChartModel(props.buckets));
const donut = computed(() => buildDonutBreakdownChartModel(props.buckets));
const isDonut = computed(() => props.view === "donut");
const formatCount = (value: number) => value.toLocaleString();
const formatPercentage = (value: number) => `${value.toFixed(value >= 10 ? 0 : 1)}%`;
const donutHeight = computed(() => Math.max(90, props.height - (props.notice ? 24 : 0)));
</script>

<template>
  <div class="panel-breakdown" :class="{ 'panel-breakdown--donut': isDonut }">
    <div v-if="isDonut" class="panel-breakdown__donut-wrap">
      <VisSingleContainer :data="donut.categories" :height="donutHeight" class="panel-breakdown__donut">
        <VisDonut
          :value="(category: BreakdownCategory) => category.count"
          :color="(category: BreakdownCategory) => category.color"
          :arc-width="18"
          :central-label="formatCount(donut.total)"
          central-sub-label="Total"
        />
      </VisSingleContainer>
      <div class="panel-breakdown__legend" aria-label="Breakdown legend">
        <div v-for="category in donut.categories" :key="`${category.isOther}:${category.value}`" class="panel-breakdown__legend-row">
          <span class="panel-breakdown__swatch" :style="{ backgroundColor: category.color }" />
          <span class="panel-breakdown__label" :title="category.label">{{ category.label }}</span>
          <span class="panel-breakdown__metric">{{ formatCount(category.count) }} · {{ formatPercentage(category.percentage) }}</span>
        </div>
      </div>
    </div>
    <div v-else class="panel-breakdown__bars" aria-label="Breakdown bars">
      <div v-for="category in bars.categories" :key="`${category.isOther}:${category.value}`" class="panel-breakdown__bar-row">
        <span class="panel-breakdown__label" :title="category.label">{{ category.label }}</span>
        <div class="panel-breakdown__track" aria-hidden="true">
          <div class="panel-breakdown__bar" :style="{ width: `${Math.max(0, category.percentage)}%`, backgroundColor: category.color }" />
        </div>
        <span class="panel-breakdown__metric">{{ formatCount(category.count) }} · {{ formatPercentage(category.percentage) }}</span>
      </div>
    </div>
    <p v-if="notice" class="panel-breakdown__notice" role="status">{{ notice }}</p>
  </div>
</template>

<style scoped>
.panel-breakdown { display: flex; flex-direction: column; width: 100%; height: 100%; min-height: 0; }
.panel-breakdown__bars { display: flex; flex: 1 1 auto; min-height: 0; flex-direction: column; gap: 0.35rem; overflow: auto; }
.panel-breakdown__bar-row { display: grid; grid-template-columns: minmax(4rem, 30%) minmax(2rem, 1fr) auto; align-items: center; gap: 0.45rem; font-size: 0.72rem; min-width: 0; }
.panel-breakdown__label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.panel-breakdown__track { height: 0.65rem; overflow: hidden; border-radius: 999px; background: var(--muted); }
.panel-breakdown__bar { height: 100%; min-width: 1px; border-radius: inherit; opacity: 0.9; }
.panel-breakdown__metric { color: var(--muted-foreground); font-variant-numeric: tabular-nums; white-space: nowrap; }
.panel-breakdown__donut-wrap { display: flex; flex: 1 1 auto; min-height: 0; gap: 0.5rem; }
.panel-breakdown__donut { flex: 1 1 50%; min-width: 0; min-height: 0; }
.panel-breakdown__legend { flex: 1 1 50%; min-width: 0; overflow: auto; display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.72rem; }
.panel-breakdown__legend-row { display: grid; grid-template-columns: auto minmax(2rem, 1fr) auto; align-items: center; gap: 0.35rem; min-width: 0; }
.panel-breakdown__swatch { width: 0.55rem; height: 0.55rem; border-radius: 999px; }
.panel-breakdown__notice { flex-shrink: 0; margin: 0.25rem 0 0; color: var(--muted-foreground); font-size: 0.75rem; }
@media (max-width: 420px) { .panel-breakdown__donut-wrap { flex-direction: column; } .panel-breakdown__legend { max-height: 40%; } }
</style>
