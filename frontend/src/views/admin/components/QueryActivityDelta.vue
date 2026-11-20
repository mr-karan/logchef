<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { changeOf } from "@/lib/queryActivity";

// Changes under this many percent read as flat.
const FLAT_PERCENT = 5;

const props = defineProps<{ current: number | null; prior: number | null; kind: "count" | "latency" }>();
const { t, locale } = useI18n();

const view = computed(() => {
  const change = changeOf(props.current, props.prior);
  if (!change) return null;
  if (change.kind === "new") return { text: t("admin.deltaNew"), tone: "text-muted-foreground" };
  const { percent } = change;
  const value = new Intl.NumberFormat(locale.value, { signDisplay: "exceptZero", maximumFractionDigits: 0 }).format(percent);
  if (Math.abs(percent) < FLAT_PERCENT) return { text: `→ ${value}%`, tone: "text-muted-foreground" };
  const up = percent > 0;
  const arrow = up ? "↑" : "↓";
  if (props.kind === "latency") {
    return {
      text: `${arrow} ${value}% ${up ? t("admin.slower") : t("admin.faster")}`,
      tone: up ? "text-destructive" : "text-emerald-700 dark:text-emerald-400",
    };
  }
  return { text: `${arrow} ${value}%`, tone: up ? "text-emerald-700 dark:text-emerald-400" : "text-muted-foreground" };
});
</script>

<template>
  <span v-if="view" :class="['whitespace-nowrap text-xs tabular-nums', view.tone]">{{ view.text }}</span>
</template>
