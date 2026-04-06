<script setup lang="ts">
import { computed, inject } from "vue";
import { cn } from "@/lib/utils";
import { chartContextKey, type ChartConfig } from "./types";

interface TooltipEntry {
  key: string;
  label: string;
  value: number;
  color: string;
}

interface ChartTooltipContentProps {
  payload?: Record<string, unknown> | null;
  config?: ChartConfig;
  class?: string;
  hideLabel?: boolean;
  hideIndicator?: boolean;
  indicator?: "line" | "dot" | "dashed";
  labelFormatter?: (value: number | Date) => string;
  valueFormatter?: (value: number, key: string) => string;
  x?: number | Date;
}

const props = withDefaults(defineProps<ChartTooltipContentProps>(), {
  payload: null,
  config: undefined,
  class: undefined,
  hideLabel: false,
  hideIndicator: false,
  indicator: "dot",
  labelFormatter: undefined,
  valueFormatter: undefined,
  x: undefined,
});

const chartContext = inject(chartContextKey, null);

const resolvedConfig = computed(() => props.config ?? chartContext?.config.value ?? {});

const entries = computed<TooltipEntry[]>(() => {
  if (!props.payload) {
    return [];
  }

  return Object.entries(resolvedConfig.value)
    .map(([key, item]) => {
      const value = props.payload?.[key];
      if (typeof value !== "number") {
        return null;
      }

      return {
        key,
        label: item.label ?? key,
        value,
        color: `var(--color-${key})`,
      } satisfies TooltipEntry;
    })
    .filter((item): item is TooltipEntry => item !== null);
});

const total = computed(() => entries.value.reduce((sum, item) => sum + item.value, 0));

const label = computed(() => {
  if (props.hideLabel) {
    return null;
  }

  const sourceValue = typeof props.payload?.ts === "number" ? props.payload.ts : props.x;
  if (sourceValue === undefined) {
    return null;
  }

  if (props.labelFormatter) {
    return props.labelFormatter(sourceValue);
  }

  return sourceValue instanceof Date ? sourceValue.toLocaleString() : new Date(sourceValue).toLocaleString();
});

function formatValue(value: number, key: string) {
  return props.valueFormatter ? props.valueFormatter(value, key) : value.toLocaleString();
}
</script>

<template>
  <div :class="cn('chart-tooltip-content', props.class)">
    <div v-if="label" class="chart-tooltip-content__label">{{ label }}</div>
    <div v-if="entries.length > 1" class="chart-tooltip-content__total">
      <span>Total</span>
      <strong>{{ total.toLocaleString() }}</strong>
    </div>
    <div class="chart-tooltip-content__items">
      <div v-for="entry in entries" :key="entry.key" class="chart-tooltip-content__item">
        <div class="chart-tooltip-content__item-label">
          <span
            v-if="!props.hideIndicator"
            class="chart-tooltip-content__indicator"
            :class="`chart-tooltip-content__indicator--${props.indicator}`"
            :style="{ backgroundColor: entry.color, borderColor: entry.color }"
          />
          <span>{{ entry.label }}</span>
        </div>
        <strong class="chart-tooltip-content__item-value">{{ formatValue(entry.value, entry.key) }}</strong>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chart-tooltip-content {
  min-width: 180px;
  padding: 0.75rem 0.875rem;
  background: hsl(var(--popover));
  color: hsl(var(--popover-foreground));
}

.chart-tooltip-content__label {
  margin-bottom: 0.5rem;
  font-size: 0.75rem;
  font-weight: 600;
}

.chart-tooltip-content__total,
.chart-tooltip-content__item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.chart-tooltip-content__total {
  margin-bottom: 0.5rem;
  padding-bottom: 0.5rem;
  border-bottom: 1px solid hsl(var(--border));
  font-size: 0.75rem;
  color: hsl(var(--muted-foreground));
}

.chart-tooltip-content__items {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.chart-tooltip-content__item-label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}

.chart-tooltip-content__indicator {
  flex-shrink: 0;
}

.chart-tooltip-content__indicator--dot {
  width: 0.625rem;
  height: 0.625rem;
  border-radius: 9999px;
}

.chart-tooltip-content__indicator--line,
.chart-tooltip-content__indicator--dashed {
  width: 0.875rem;
  height: 0;
  border-top-width: 2px;
  border-top-style: solid;
  background-color: transparent !important;
}

.chart-tooltip-content__indicator--dashed {
  border-top-style: dashed;
}

.chart-tooltip-content__item-value {
  font-variant-numeric: tabular-nums;
}
</style>
