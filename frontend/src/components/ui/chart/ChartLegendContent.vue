<script setup lang="ts">
import { computed, inject } from "vue";
import { cn } from "@/lib/utils";
import { chartContextKey } from "./types";

interface ChartLegendContentProps {
  class?: string;
  verticalAlign?: "top" | "bottom";
}

const props = withDefaults(defineProps<ChartLegendContentProps>(), {
  class: undefined,
  verticalAlign: "bottom",
});

const chartContext = inject(chartContextKey, null);

const entries = computed(() => {
  const config = chartContext?.config.value ?? {};
  return Object.entries(config).map(([key, item]) => ({
    key,
    label: item.label ?? key,
    color: `var(--color-${key})`,
  }));
});
</script>

<template>
  <div
    v-if="entries.length > 1"
    :class="cn('chart-legend-content', props.verticalAlign === 'top' ? 'chart-legend-content--top' : 'chart-legend-content--bottom', props.class)"
  >
    <div v-for="entry in entries" :key="entry.key" class="chart-legend-content__item">
      <span class="chart-legend-content__dot" :style="{ backgroundColor: entry.color }" />
      <span>{{ entry.label }}</span>
    </div>
  </div>
</template>

<style scoped>
.chart-legend-content {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  color: hsl(var(--muted-foreground));
  font-size: 0.75rem;
}

.chart-legend-content--top {
  margin-bottom: 0.75rem;
}

.chart-legend-content--bottom {
  margin-top: 0.75rem;
}

.chart-legend-content__item {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
}

.chart-legend-content__dot {
  width: 0.625rem;
  height: 0.625rem;
  border-radius: 9999px;
  flex-shrink: 0;
}
</style>
