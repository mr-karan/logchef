<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { alertsApi, type BacktestAlertRequest, type BacktestResult, type BacktestStatus } from "@/api/alerts";
import { getErrorMessage } from "@/api/types";
import { formatAlertGroup } from "@/utils/alertGroups";
import { usePreferencesStore } from "@/stores/preferences";
import { epochMs, formatInZone } from "@/utils/timeWindow";

const { t, n, locale } = useI18n();
const preferences = usePreferencesStore();

function formatTime(iso: string): string {
  return formatInZone(epochMs(new Date(iso)), preferences.zone, { dateStyle: "medium", timeStyle: "short" }, locale.value);
}

const props = defineProps<{
  /** The rule to replay; null while it has no query. */
  request: Omit<BacktestAlertRequest, "range_seconds"> | null;
  disabled: boolean;
}>();

// For an lt or lte rule the lowest value is the one that breaches.
const showsMin = computed(() => props.request?.threshold_operator === "lt" || props.request?.threshold_operator === "lte");

const ranges = [
  { seconds: 3600, key: "alerts.backtestRange1h" },
  { seconds: 6 * 3600, key: "alerts.backtestRange6h" },
  { seconds: 24 * 3600, key: "alerts.backtestRange24h" },
  { seconds: 7 * 24 * 3600, key: "alerts.backtestRange7d" },
] as const;

// The server stops a backtest after 60 seconds; leave room for the response.
const BACKTEST_TIMEOUT_SECONDS = 75;

const rangeSeconds = ref<number>(24 * 3600);
const running = ref(false);
const result = ref<BacktestResult | null>(null);
const error = ref<string | null>(null);
let controller: AbortController | null = null;

function reset() {
  controller?.abort();
  controller = null;
  running.value = false;
  result.value = null;
  error.value = null;
}

// A result describes the rule it ran; any edit makes it stale.
watch(() => JSON.stringify([props.request, rangeSeconds.value]), reset);
onBeforeUnmount(() => controller?.abort());

async function run() {
  if (!props.request || running.value) return;
  reset();
  const current = new AbortController();
  controller = current;
  running.value = true;
  try {
    const response = await alertsApi.backtest(
      { ...props.request, range_seconds: rangeSeconds.value },
      { signal: current.signal, timeout: BACKTEST_TIMEOUT_SECONDS, suppressErrorToast: true },
    );
    if (controller === current) result.value = response.data ?? null;
  } catch (err) {
    if (controller === current) error.value = getErrorMessage(err);
  } finally {
    if (controller === current) {
      running.value = false;
      controller = null;
    }
  }
}

const ticks = computed(() =>
  props.request && props.request.frequency_seconds > 0 ? Math.floor(rangeSeconds.value / props.request.frequency_seconds) : 0,
);

function formatDuration(seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) return t("alerts.backtestHoursMinutes", { hours, minutes });
  return t("alerts.backtestMinutes", { minutes: Math.max(minutes, seconds > 0 ? 1 : 0) });
}

const statusClass: Record<BacktestStatus, string> = {
  ok: "bg-green-500/70",
  firing: "bg-red-500",
  no_data: "bg-amber-500",
  error: "bg-muted-foreground/40",
};

const statusLabel = computed<Record<BacktestStatus, string>>(() => ({
  ok: t("alerts.stateOk"),
  firing: t("alerts.stateFiring"),
  no_data: t("alerts.stateNoData"),
  error: t("alerts.backtestStatusError"),
}));

function pointTitle(point: BacktestResult["series"][number]): string {
  const parts = [formatTime(point.t), statusLabel.value[point.status]];
  if (point.value !== null) parts.push(String(point.value));
  if (point.firing_groups > 1) parts.push(t("alerts.groupsFiring", { count: point.firing_groups }));
  return parts.join(" · ");
}
</script>

<template>
  <div class="rounded-lg border bg-background p-4 space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h4 class="text-sm font-medium">{{ t("alerts.backtest") }}</h4>
      <div class="flex items-center gap-2">
        <Select :model-value="String(rangeSeconds)" @update:model-value="(v: any) => (rangeSeconds = Number(v))">
          <SelectTrigger class="h-8 w-40 text-xs" :aria-label="t('alerts.backtestRange')">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="range in ranges" :key="range.seconds" :value="String(range.seconds)">
              {{ t(range.key) }}
            </SelectItem>
          </SelectContent>
        </Select>
        <Button type="button" variant="outline" size="sm" :disabled="!request || disabled || running" @click="run">
          {{ running ? t("alerts.backtestRunning") : t("alerts.backtestRun") }}
        </Button>
      </div>
    </div>

    <div v-if="result" class="space-y-3" data-testid="backtest-result">
      <p class="text-sm">
        {{ t("alerts.backtestEpisodes", { count: result.summary.episodes }, result.summary.episodes) }}
        · {{ t("alerts.backtestNotifications", { count: result.summary.notifications }, result.summary.notifications) }}
        <template v-if="result.summary.firing_seconds > 0">
          · {{ t("alerts.backtestFiringFor", { duration: formatDuration(result.summary.firing_seconds) }) }}
        </template>
        <template v-if="result.summary.no_data_evaluations > 0">
          · {{ t("alerts.backtestNoDataEvaluations", { count: result.summary.no_data_evaluations }, result.summary.no_data_evaluations) }}
        </template>
      </p>
      <p class="text-xs text-muted-foreground">
        {{
          t("alerts.backtestEvaluated", {
            evaluations: n(result.evaluations),
            ticks: n(ticks),
            coverage: n(Math.round(result.coverage * 100)),
          })
        }}
      </p>

      <div
        class="flex h-5 w-full gap-px overflow-hidden rounded"
        role="img"
        :aria-label="t('alerts.backtestStrip')"
      >
        <div
          v-for="point in result.series"
          :key="point.t"
          class="h-full min-w-px flex-1"
          :class="statusClass[point.status]"
          :title="pointTitle(point)"
        />
      </div>
      <div class="flex justify-between text-xs text-muted-foreground tabular-nums">
        <span>{{ formatTime(result.start) }}</span>
        <span>{{ formatTime(result.end) }}</span>
      </div>

      <p v-if="result.partial" class="text-xs text-amber-700 dark:text-amber-400">{{ t("alerts.backtestPartial") }}</p>
      <p v-if="result.summary.errors > 0" class="text-xs text-destructive">
        {{ t("alerts.backtestErrors", { count: result.summary.errors }, result.summary.errors) }}<template v-if="result.first_error">: {{ result.first_error }}</template>
      </p>

      <table v-if="result.groups.length > 0" class="w-full text-sm">
        <thead>
          <tr class="text-left text-xs text-muted-foreground">
            <th class="py-1 pr-3 font-normal">{{ t("alerts.group") }}</th>
            <th class="py-1 pr-3 text-right font-normal">{{ t("alerts.backtestGroupEpisodes") }}</th>
            <th class="py-1 pr-3 text-right font-normal">{{ t("alerts.backtestGroupBreaching") }}</th>
            <th class="py-1 text-right font-normal">{{ t(showsMin ? "alerts.backtestGroupMin" : "alerts.backtestGroupMax") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="group in result.groups" :key="formatAlertGroup(group.group)" class="border-t">
            <td class="py-1 pr-3 font-mono text-xs">{{ formatAlertGroup(group.group) }}</td>
            <td class="py-1 pr-3 text-right tabular-nums">{{ n(group.episodes) }}</td>
            <td class="py-1 pr-3 text-right tabular-nums">{{ n(group.breaching_evaluations) }}</td>
            <td class="py-1 text-right tabular-nums">{{ (showsMin ? group.min_value : group.max_value) ?? "–" }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="result.groups_omitted > 0" class="text-xs text-muted-foreground">
        {{ t("alerts.backtestGroupsOmitted", { count: result.groups_omitted }, result.groups_omitted) }}
      </p>
    </div>

    <div v-if="error" class="flex gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm dark:border-red-800 dark:bg-red-950/20">
      <span class="flex-shrink-0 text-red-600 dark:text-red-500">✗</span>
      <span class="text-red-900 dark:text-red-100">{{ error }}</span>
    </div>
  </div>
</template>
