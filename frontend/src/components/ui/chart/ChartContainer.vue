<script setup lang="ts">
import { computed, provide, toRef, useId } from "vue";
import { cn } from "@/lib/utils";
import ChartStyle from "./ChartStyle.vue";
import { chartContextKey, type ChartConfig } from "./types";

interface ChartContainerProps {
  id?: string;
  config: ChartConfig;
  class?: string;
  cursor?: boolean;
}

const props = withDefaults(defineProps<ChartContainerProps>(), {
  id: undefined,
  class: undefined,
  cursor: true,
});

const generatedId = useId();
const containerId = computed(() => props.id ?? `chart-${generatedId}`);
const configRef = toRef(props, "config");

provide(chartContextKey, {
  id: containerId.value,
  config: configRef,
});

const containerStyle = computed(() => ({
  "--vis-tooltip-padding": "0px",
  "--vis-tooltip-background-color": "hsl(var(--popover))",
  "--vis-tooltip-border-color": "hsl(var(--border))",
  "--vis-tooltip-text-color": "hsl(var(--popover-foreground))",
  "--vis-tooltip-shadow-color": "rgba(0, 0, 0, 0.12)",
  "--vis-tooltip-backdrop-filter": "blur(12px)",
  "--vis-crosshair-circle-stroke-color": "hsl(var(--border))",
  "--vis-crosshair-line-stroke-width": "1",
  "--vis-font-family": "Inter, ui-sans-serif, system-ui, sans-serif",
  cursor: props.cursor ? "default" : "inherit",
}));
</script>

<template>
  <div
    :data-chart="containerId"
    :class="cn('chart-container relative flex w-full min-w-0 flex-col justify-center text-xs', props.class)"
    :style="containerStyle"
  >
    <ChartStyle :id="containerId" :config="props.config" />
    <slot :id="containerId" :config="props.config" />
  </div>
</template>

<style scoped>
.chart-container :deep(.vis-tooltip) {
  border-radius: 0.75rem;
  border: 1px solid hsl(var(--border));
  box-shadow: 0 12px 32px rgba(15, 23, 42, 0.18);
  overflow: hidden;
}

.chart-container :deep(.vis-axis-grid-line) {
  stroke: hsl(var(--border));
  stroke-dasharray: 3 3;
  opacity: 0.7;
}

.chart-container :deep(.vis-axis-tick text) {
  fill: hsl(var(--muted-foreground));
}

.chart-container :deep(.vis-crosshair-line) {
  stroke: hsl(var(--border));
}
</style>
