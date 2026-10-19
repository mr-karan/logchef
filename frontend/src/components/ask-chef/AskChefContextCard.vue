<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { AlertThresholdOperator } from "@/api/alerts";
import type { AskChefContext } from "@/stores/askChef";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import { epochMs, formatInZone, isSameZonedDay } from "@/utils/timeWindow";

const props = defineProps<{ context: AskChefContext }>();

const { t, locale } = useI18n();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();

const OPERATORS: Record<AlertThresholdOperator, string> = { gt: ">", gte: "≥", lt: "<", lte: "≤", eq: "=", neq: "≠" };

// The team and source names, or the source id until the stores load them.
const sourceLabel = computed(() => {
  const { teamId, sourceId } = props.context;
  const source = sourcesStore.getTeamSourceById(sourceId)?.name ?? `#${sourceId}`;
  const team = teamsStore.getTeamById(teamId)?.name;
  return team ? `${team} / ${source}` : source;
});

function formatTime(ms: number, withDate: boolean): string {
  const opts: Intl.DateTimeFormatOptions = withDate ? { dateStyle: "medium", timeStyle: "short" } : { timeStyle: "short" };
  return formatInZone(epochMs(ms), props.context.zone, opts, locale.value);
}

const windowLabel = computed(() => {
  const { start, end, zone } = props.context;
  const sameDay = isSameZonedDay(epochMs(start), epochMs(end), zone);
  return `${formatTime(start, true)} – ${formatTime(end, !sameDay)}`;
});

const details = computed(() => {
  const subject = props.context.subject;
  if (subject.kind === "explorer") return [t("askChef.window", { window: windowLabel.value })];
  const value = subject.value == null ? [] : [`${t("askChef.value", { value: subject.value })} ${OPERATORS[subject.thresholdOperator]} ${subject.thresholdValue}`];
  return [t("askChef.firedAt", { time: formatTime(Date.parse(subject.triggeredAt), true) }), ...value, t("askChef.window", { window: windowLabel.value }), sourceLabel.value];
});
</script>

<template>
  <div
    v-if="context.subject.kind === 'alert'"
    class="rounded-lg border border-red-200 bg-red-50 px-2.5 py-2 dark:border-red-900 dark:bg-red-950/40"
  >
    <div class="text-[11.5px] font-semibold text-red-800 dark:text-red-300">⚠ {{ t("askChef.alertContext", { name: context.subject.alertName }) }}</div>
    <div class="mt-0.5 text-[11px] text-red-900/80 dark:text-red-200/80">{{ details.join(" · ") }}</div>
  </div>
  <div v-else class="rounded-lg border bg-muted/40 px-2.5 py-2">
    <div class="text-[11.5px] font-semibold">{{ t("askChef.explorerContext", { source: sourceLabel }) }}</div>
    <div class="mt-0.5 text-[11px] text-muted-foreground">{{ details.join(" · ") }}</div>
    <div v-if="context.query" class="mt-1 truncate font-mono text-[10.5px] text-muted-foreground" :title="context.query">{{ context.query }}</div>
  </div>
</template>
