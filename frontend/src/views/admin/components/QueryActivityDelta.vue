<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { changeOf } from "@/lib/queryActivity";

// Changes under this many percent, or rate points, read as flat.
const FLAT_PERCENT = 5;
const FLAT_POINTS = 0.1;

// count: a rise is good. latency: a rise is bad and reads as slower; compact
// leaves the word out. failures: a rise is bad. rate: current and prior are
// 0..1 and the change is in percentage points.
const props = defineProps<{ current: number | null; prior: number | null; kind: "count" | "latency" | "failures" | "rate"; compact?: boolean }>();
const { t, locale } = useI18n();

const good = "text-emerald-700 dark:text-emerald-400";
const muted = "text-muted-foreground";

const view = computed(() => {
  const signed = (value: number, digits: number) =>
    new Intl.NumberFormat(locale.value, { signDisplay: "exceptZero", minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value);
  if (props.kind === "rate") {
    if (props.current === null || props.prior === null) return null;
    const points = (props.current - props.prior) * 100;
    if (Math.abs(points) < FLAT_POINTS) return { text: `→ ${signed(0, 1)} pp`, tone: muted };
    return points > 0 ? { text: `↑ ${signed(points, 1)} pp`, tone: "text-destructive" } : { text: `↓ ${signed(points, 1)} pp`, tone: good };
  }
  const change = changeOf(props.current, props.prior);
  if (!change) return null;
  if (change.kind === "new") return { text: t("admin.deltaNew"), tone: muted };
  const up = change.kind === "multiple" || change.percent > 0;
  const amount = change.kind === "multiple"
    ? `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(change.factor)}×`
    : `${signed(change.percent, 0)}%`;
  if (change.kind === "percent" && Math.abs(change.percent) < FLAT_PERCENT) return { text: `→ ${amount}`, tone: muted };
  const arrow = up ? "↑" : "↓";
  if (props.kind === "count") return { text: `${arrow} ${amount}`, tone: up ? good : muted };
  const word = props.kind === "latency" && !props.compact ? ` ${up ? t("admin.slower") : t("admin.faster")}` : "";
  return { text: `${arrow} ${amount}${word}`, tone: up ? "text-destructive" : good };
});
</script>

<template>
  <span v-if="view" :class="['whitespace-nowrap text-xs tabular-nums', view.tone]">{{ view.text }}</span>
</template>
