<script setup lang="ts">
import { computed, ref, toValue } from "vue";
import { useI18n } from "vue-i18n";
import type { DailyQueryStat } from "@/api/admin";

// Two single-series bar charts on one day axis: queries and active users
// differ in scale, so they never share a y-axis. Hovering a day in either
// chart highlights it in both and shows its numbers in the readout; with no
// day hovered, the readout shows the latest day.
const props = defineProps<{ days: DailyQueryStat[]; priorAvgQueries: number; priorAvgUsers: number }>();
const { t, locale } = useI18n();

const hovered = ref<number | null>(null);

const panels = computed(() => [
  { key: "queries", title: t("admin.dailyQueries"), value: (d: DailyQueryStat) => d.query_count, prior: props.priorAvgQueries, height: "h-36" },
  { key: "users", title: t("admin.dailyActiveUsers"), value: (d: DailyQueryStat) => d.active_users, prior: props.priorAvgUsers, height: "h-20" },
].map(panel => {
  const top = Math.max(1, panel.prior, ...props.days.map(panel.value));
  return {
    ...panel,
    priorPct: (panel.prior / top) * 100,
    bars: props.days.map(day => ({ day, value: panel.value(day), pct: (panel.value(day) / top) * 100 })),
  };
}));
const labelEvery = computed(() => Math.max(1, Math.ceil(props.days.length / 7)));

const readout = computed(() => {
  const index = hovered.value ?? props.days.length - 1;
  const day = props.days[index];
  if (!day) return null;
  return t("admin.dailyChartReadout", {
    date: formatDay(day.date, true),
    queries: formatCount(day.query_count),
    users: formatCount(day.active_users),
  });
});

function formatDay(date: string, withWeekday = false) {
  return new Intl.DateTimeFormat(toValue(locale), {
    weekday: withWeekday ? "short" : undefined,
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(new Date(`${date}T00:00:00Z`));
}
function formatCount(value: number) {
  return value.toLocaleString(toValue(locale), { maximumFractionDigits: 1 });
}
</script>

<template>
  <div class="space-y-4" @mouseleave="hovered = null">
    <p class="min-h-4 text-xs tabular-nums" data-chart-readout>
      <span :class="hovered === null ? 'text-muted-foreground' : 'font-medium'">{{ readout }}</span>
    </p>
    <figure v-for="panel in panels" :key="panel.key" :data-chart="panel.key">
      <figcaption class="mb-1 flex flex-wrap items-baseline justify-between gap-2 text-xs">
        <span class="font-medium">{{ panel.title }}</span>
        <span class="flex items-center gap-1.5 text-muted-foreground">
          <span class="inline-block w-4 border-t border-dashed border-foreground/60" aria-hidden="true" />
          {{ t('admin.priorAverage', { value: formatCount(panel.prior) }) }}
        </span>
      </figcaption>
      <div :class="['relative flex items-end gap-0.5 border-b', panel.height]" aria-hidden="true">
        <div
          v-for="(bar, index) in panel.bars"
          :key="bar.day.date"
          class="flex h-full min-w-1 flex-1 cursor-default items-end"
          :class="hovered === index ? 'bg-foreground/5' : ''"
          :data-day="bar.day.date"
          @mouseenter="hovered = index"
        >
          <div
            class="w-full rounded-t-sm bg-[var(--panel-chart-1)] transition-opacity"
            :class="hovered !== null && hovered !== index ? 'opacity-40' : ''"
            :style="{ height: bar.value ? `${Math.max(2, bar.pct)}%` : '0' }"
          />
        </div>
        <div class="pointer-events-none absolute inset-x-0 border-t border-dashed border-foreground/60" :style="{ bottom: `${panel.priorPct}%` }" />
      </div>
    </figure>
    <div class="flex text-[10px] text-muted-foreground" aria-hidden="true">
      <span v-for="(day, index) in days" :key="day.date" class="min-w-1 flex-1 whitespace-nowrap">
        {{ index % labelEvery === 0 ? formatDay(day.date) : '' }}
      </span>
    </div>
  </div>
</template>
