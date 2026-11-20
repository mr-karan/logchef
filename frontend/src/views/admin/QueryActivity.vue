<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toValue, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter, type LocationQueryRaw } from "vue-router";
import { Activity, CircleCheck, FileSearch, RefreshCw, Settings2, Telescope, X } from "lucide-vue-next";
import { PageHeader, LoadingState, EmptyState } from "@/components/layout";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { adminApi, type FailurePair, type QueryAccount, type QueryStatsResponse, type RunHealth, type UserQueryStat } from "@/api/admin";
import { isSuccessResponse } from "@/api/types";
import { formatErrorMessage } from "@/api/error-handler";
import { formatHistoryDuration } from "@/lib/queryHistory";
import { advisorLink, auditLink, averageMs, formatBytes, formatLookback, formatPercentile, formatRate, formatTotalTime, shapeColumn, userName } from "@/lib/queryActivity";
import QueryActivityDelta from "./components/QueryActivityDelta.vue";
import QueryActivityDailyChart from "./components/QueryActivityDailyChart.vue";
import QueryActivitySparkline from "./components/QueryActivitySparkline.vue";

const WINDOWS = [7, 30, 90] as const;
const ACCOUNTS: QueryAccount[] = ["human", "service", "all"];
// Languages keep their color whatever their rank.
const LANGUAGES = ["logchefql", "clickhouse-sql", "logsql", "logql"];

const { t, te, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const stats = ref<QueryStatsResponse | null>(null);
const loading = ref(true);
const error = ref<string | null>(null);
let request: AbortController | undefined;

function queryNumber(key: string): number | undefined {
  const raw = route.query[key];
  const value = typeof raw === "string" ? Number(raw) : NaN;
  return Number.isInteger(value) && value > 0 ? value : undefined;
}
const days = computed(() => {
  const value = queryNumber("days");
  return value && (WINDOWS as readonly number[]).includes(value) ? value : 30;
});
const account = computed<QueryAccount>(() => {
  const raw = route.query.account;
  return ACCOUNTS.includes(raw as QueryAccount) ? raw as QueryAccount : "human";
});
const sourceId = computed(() => queryNumber("source"));
const userId = computed(() => queryNumber("user"));

function setQuery(changes: Record<string, string | null>) {
  const query: LocationQueryRaw = { ...route.query };
  for (const [key, value] of Object.entries(changes)) {
    if (value === null) delete query[key];
    else query[key] = value;
  }
  void router.replace({ query });
}

async function load() {
  request?.abort();
  const controller = new AbortController();
  request = controller;
  loading.value = true;
  error.value = null;
  try {
    const response = await adminApi.getQueryStats(
      { days: days.value, account: account.value, source_id: sourceId.value, user_id: userId.value },
      { signal: controller.signal },
    );
    if (controller.signal.aborted) return;
    if (isSuccessResponse(response)) stats.value = response.data ?? null;
    else error.value = response.message || t("admin.usageStatsLoadFailed");
  } catch (err: unknown) {
    if (!controller.signal.aborted) error.value = formatErrorMessage(err, t("admin.usageStatsLoadFailed"));
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}
watch([days, account, sourceId, userId], load, { immediate: true });
onBeforeUnmount(() => request?.abort());

const number = (value: number) => value.toLocaleString(toValue(locale), { maximumFractionDigits: 1 });
const compact = (value: number) => new Intl.NumberFormat(toValue(locale), { notation: "compact", maximumFractionDigits: 1 }).format(value);
const duration = (ms: number | null) => (ms === null ? "—" : formatHistoryDuration(ms, toValue(locale)));
const formatDay = (date: string) =>
  new Intl.DateTimeFormat(toValue(locale), { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(`${date}T00:00:00Z`));
const languageLabel = (language: string) =>
  ({ logchefql: "LogchefQL", "clickhouse-sql": "ClickHouse SQL", logsql: "LogsQL", logql: "LogQL" })[language] ?? language;
const sourceLabel = (name: string, id: number) => name || t("admin.sourceFallback", { id });
const userLabel = (user: Pick<UserQueryStat, "user_id" | "full_name" | "email" | "account_type">) => userName(user) || t("admin.userFallback", { id: user.user_id });
const rate = (health: RunHealth) => (health.failure_rate === null ? "—" : formatRate(health.failure_rate, toValue(locale)));
const bytes = (value: number) => formatBytes(value, toValue(locale));
const reasonLabel = (reason: string) => (te(`admin.failureReason.${reason}`) ? t(`admin.failureReason.${reason}`) : reason);
// Why a latency, scan or failure value is missing: it was not recorded yet.
const notCollected = computed(() => {
  const since = stats.value?.collected_since;
  return since ? t("admin.collectedFrom", { date: formatDay(since) }) : t("admin.notCollectedYet");
});
// The window starts before latency, scan and failure data does.
const partlyCollected = computed(() => {
  const since = stats.value?.collected_since;
  return since && stats.value && since > stats.value.window.since ? t("admin.collectedPartly", { date: formatDay(since) }) : undefined;
});

const accountTabs = computed(() => ACCOUNTS.map(key => ({
  key,
  label: t({ human: "admin.accountHumans", service: "admin.accountServices", all: "admin.accountAll" }[key]),
  count: stats.value?.account_totals[key],
})));

const chips = computed(() => {
  const filters = stats.value?.filters;
  const out: { key: "source" | "user"; label: string }[] = [];
  if (sourceId.value) out.push({ key: "source", label: `${t("ui.source")}: ${filters?.source_name || t("admin.sourceFallback", { id: sourceId.value })}` });
  if (userId.value) out.push({ key: "user", label: `${t("ui.user")}: ${filters?.user_name || t("admin.userFallback", { id: userId.value })}` });
  return out;
});

const headline = computed(() => {
  if (!stats.value) return [];
  const { current, prior } = stats.value.summary;
  return [
    {
      key: "users", label: t("admin.activeUsers"), value: number(current.active_users), title: undefined,
      aside: t("admin.ofUsers", { count: number(stats.value.total_users) }),
      detail: t("admin.avgDailyActive", { count: number(current.avg_daily_active_users) }),
      current: current.active_users, prior: prior.active_users, priorText: number(prior.active_users), kind: "count" as const,
    },
    {
      key: "queries", label: t("admin.queries"), value: number(current.query_count), title: undefined,
      aside: t("admin.sourceCount", { count: number(current.active_sources) }, current.active_sources),
      detail: account.value === "human" && stats.value.account_totals.service
        ? t("admin.serviceQueriesHidden", { count: compact(stats.value.account_totals.service) })
        : "",
      current: current.query_count, prior: prior.query_count, priorText: number(prior.query_count), kind: "count" as const,
    },
    latencyMetric(),
    {
      key: "failures", label: t("admin.failureRate"), value: rate(current),
      title: current.failure_rate === null ? notCollected.value : partlyCollected.value,
      aside: current.failure_rate === null ? "" : t("admin.failedCount", { count: number(current.failure_count) }),
      detail: stats.value.failures.by_reason.slice(0, 2).map(r => `${number(r.count)} ${reasonLabel(r.reason).toLowerCase()}`).join(" · "),
      current: current.failure_rate, prior: prior.failure_rate, priorText: rate(prior), kind: "rate" as const,
    },
  ];
});

// p50 and p95 when the window has latency data, else the average alone. The
// delta follows p95 when both windows have a bounded one: an open p95 says
// only "past the last bucket", so the delta follows the average instead.
function latencyMetric() {
  const { current, prior } = stats.value!.summary;
  const avg = averageMs(current);
  const base = { key: "latency", label: t("admin.latencyPercentiles"), detail: avg === null ? "" : t("admin.averageValue", { value: duration(avg) }) };
  if (!current.cost) {
    return {
      ...base, value: "—", title: notCollected.value, aside: "",
      current: avg, prior: averageMs(prior), priorText: duration(averageMs(prior)), kind: "latency" as const,
    };
  }
  const locale_ = toValue(locale);
  const byP95 = prior.cost && !prior.cost.p95.open && !current.cost.p95.open;
  return {
    ...base,
    value: formatPercentile(current.cost.p50, locale_),
    title: partlyCollected.value,
    aside: `p95 ${formatPercentile(current.cost.p95, locale_)}`,
    current: byP95 ? current.cost.p95.ms : avg,
    prior: byP95 ? prior.cost!.p95.ms : averageMs(prior),
    priorText: prior.cost ? `p95 ${formatPercentile(prior.cost.p95, locale_)}` : duration(averageMs(prior)),
    kind: "latency" as const,
  };
}

const failureLink = (pair?: FailurePair) => auditLink(stats.value!.window, {
  outcome: "failure",
  source_id: pair?.source_id ?? sourceId.value,
  actor_id: pair?.user_id ?? userId.value,
});

const todayInWindow = computed(() => stats.value?.window.until);
const priorAvgQueries = computed(() => stats.value ? stats.value.summary.prior.query_count / stats.value.prior_window.days : 0);

const languageMix = computed(() => {
  const mix = stats.value?.language_mix ?? [];
  const total = mix.reduce((sum, item) => sum + item.query_count, 0);
  if (!total) return [];
  return mix.map(item => {
    const slot = LANGUAGES.indexOf(item.language);
    return {
      language: item.language,
      label: languageLabel(item.language),
      percent: (item.query_count / total) * 100,
      color: slot === -1 ? "var(--panel-chart-other)" : `var(--panel-chart-${slot + 1})`,
    };
  });
});
const percent = (value: number) => `${value < 1 ? value.toFixed(1) : Math.round(value)}%`;
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('routes.queryActivity')">
      <template #actions>
        <template v-if="stats">
          <Button variant="ghost" size="sm" as-child>
            <router-link :to="auditLink(stats.window, { outcome: 'failure' })">{{ t('admin.failedQueries') }}</router-link>
          </Button>
          <Button variant="ghost" size="sm" as-child>
            <router-link :to="auditLink(stats.window)">{{ t('routes.auditLog') }}</router-link>
          </Button>
        </template>
        <Button variant="outline" size="sm" :disabled="loading" @click="load">
          <RefreshCw :class="['mr-2 h-3.5 w-3.5', loading && 'animate-spin']" />{{ t('ui.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <div role="tablist" :aria-label="t('admin.accountType')" class="inline-flex rounded-md border bg-muted/40 p-0.5">
          <button
            v-for="tab in accountTabs"
            :key="tab.key"
            type="button"
            role="tab"
            :aria-selected="account === tab.key"
            :class="['rounded px-3 py-1 text-sm', account === tab.key ? 'bg-background font-medium shadow-sm' : 'text-muted-foreground hover:text-foreground']"
            @click="setQuery({ account: tab.key === 'human' ? null : tab.key })"
          >
            {{ tab.label }}<span v-if="tab.count !== undefined" class="ml-1.5 text-xs tabular-nums text-muted-foreground">{{ compact(tab.count) }}</span>
          </button>
        </div>
        <Button
          v-for="chip in chips"
          :key="chip.key"
          variant="secondary"
          size="sm"
          :aria-label="t('admin.removeFilter', { name: chip.label })"
          @click="setQuery({ [chip.key]: null })"
        >
          {{ chip.label }}<X class="ml-1 h-3.5 w-3.5" />
        </Button>
      </div>
      <div class="flex flex-wrap items-center gap-3">
        <span v-if="stats" class="text-xs text-muted-foreground tabular-nums">
          {{ t('admin.windowVsPrior', { since: formatDay(stats.window.since), until: formatDay(stats.window.until), days: stats.prior_window.days }) }}
        </span>
        <div role="group" :aria-label="t('admin.window')" class="inline-flex rounded-md border p-0.5">
          <button
            v-for="option in WINDOWS"
            :key="option"
            type="button"
            :aria-pressed="days === option"
            :class="['rounded px-2.5 py-1 text-sm tabular-nums', days === option ? 'bg-muted font-medium' : 'text-muted-foreground hover:text-foreground']"
            @click="setQuery({ days: option === 30 ? null : String(option) })"
          >
            {{ t('settings.days', { count: option }, option) }}
          </button>
        </div>
      </div>
    </div>

    <LoadingState v-if="loading && !stats" class="py-16" :label="t('admin.loadingUsageStats')" />
    <div v-else-if="error" role="alert" class="rounded-md border border-destructive/50 p-4 text-sm text-destructive">
      {{ error }} <Button variant="outline" size="sm" class="ml-3" @click="load">{{ t('admin.retry') }}</Button>
    </div>
    <template v-else-if="stats">
      <dl class="grid overflow-hidden rounded-lg border bg-card sm:grid-cols-2 xl:grid-cols-4" data-section="headline">
        <div v-for="metric in headline" :key="metric.key" :data-metric="metric.key" class="space-y-1 border-border p-4 sm:odd:border-r xl:border-r xl:last:border-r-0">
          <div class="flex items-center justify-between gap-2">
            <dt class="text-xs font-medium text-muted-foreground">{{ metric.label }}</dt>
            <QueryActivityDelta :current="metric.current" :prior="metric.prior" :kind="metric.kind" />
          </div>
          <dd class="flex items-baseline gap-2">
            <span class="text-2xl font-semibold tracking-tight tabular-nums" :title="metric.title">{{ metric.value }}</span>
            <span v-if="metric.aside" class="text-xs text-muted-foreground">{{ metric.aside }}</span>
          </dd>
          <dd class="flex flex-wrap justify-between gap-x-3 text-xs text-muted-foreground">
            <span>{{ metric.detail }}</span>
            <span class="tabular-nums">{{ t('admin.priorValue', { value: metric.priorText }) }}</span>
          </dd>
        </div>
      </dl>

      <section class="rounded-lg border bg-card" data-section="adoption">
        <header class="border-b px-4 py-3">
          <h2 class="text-base font-medium">{{ t('admin.adoptionTitle') }}</h2>
        </header>
        <EmptyState v-if="!stats.summary.current.query_count" :icon="Activity" :title="t('admin.noUsageInWindow')" class="py-10" />
        <div v-else class="grid gap-6 p-4 lg:grid-cols-5">
          <div class="min-w-0 lg:col-span-3">
            <QueryActivityDailyChart :days="stats.volume_by_day" :prior-avg-queries="priorAvgQueries" :prior-avg-users="stats.summary.prior.avg_daily_active_users" />
            <p class="mt-2 text-xs text-muted-foreground">{{ t('admin.utcDayInProgress', { date: formatDay(todayInWindow ?? '') }) }}</p>
            <details class="mt-2 text-sm">
              <summary class="w-fit cursor-pointer text-muted-foreground hover:text-foreground">{{ t('admin.viewDailyValues') }}</summary>
              <div class="mt-2 max-h-60 overflow-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{{ t('admin.utcDate') }}</TableHead>
                      <TableHead class="text-right">{{ t('admin.queries') }}</TableHead>
                      <TableHead class="text-right">{{ t('admin.activeUsers') }}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TableRow v-for="day in stats.volume_by_day" :key="day.date">
                      <TableCell>{{ day.date }}</TableCell>
                      <TableCell class="text-right tabular-nums">{{ number(day.query_count) }}</TableCell>
                      <TableCell class="text-right tabular-nums">{{ number(day.active_users) }}</TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </div>
            </details>
          </div>

          <div class="min-w-0 space-y-4 lg:col-span-2">
            <div>
              <h3 class="text-sm font-medium">{{ t('admin.topUsers') }}</h3>
              <!-- Fixed columns keep the table inside its column: names truncate and headers wrap. -->
              <Table class="mt-2 table-fixed" data-table="users">
                <colgroup>
                  <col>
                  <col class="w-[4.5rem]">
                  <col class="w-[3.75rem]">
                  <col class="w-[4.75rem]">
                  <col class="w-8">
                </colgroup>
                <TableHeader>
                  <TableRow>
                    <TableHead>{{ t('ui.user') }}</TableHead>
                    <TableHead class="whitespace-normal text-right leading-tight">{{ t('admin.queries') }}</TableHead>
                    <TableHead class="whitespace-normal text-right leading-tight">{{ t('admin.activeDays') }}</TableHead>
                    <TableHead class="whitespace-normal text-right leading-tight">{{ t('admin.avgLatency') }}</TableHead>
                    <TableHead><span class="sr-only">{{ t('routes.auditLog') }}</span></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="user in stats.top_users" :key="user.user_id">
                    <TableCell class="truncate" data-cell="user">
                      <button type="button" class="block max-w-full truncate text-left font-medium hover:underline focus-visible:underline" @click="setQuery({ user: String(user.user_id) })">
                        {{ userLabel(user) }}
                      </button>
                      <span v-if="user.account_type === 'service'" class="text-xs text-muted-foreground">{{ t('admin.serviceAccount') }}</span>
                      <span v-else-if="user.full_name && user.email" class="block truncate text-xs text-muted-foreground">{{ user.email }}</span>
                    </TableCell>
                    <TableCell class="text-right">
                      <div class="tabular-nums">{{ number(user.current.query_count) }}</div>
                      <QueryActivityDelta :current="user.current.query_count" :prior="user.prior.query_count" kind="count" />
                    </TableCell>
                    <TableCell class="whitespace-nowrap text-right text-xs tabular-nums text-muted-foreground">
                      {{ t('admin.activeDaysValue', { count: user.active_days, days: stats.window.days }) }}
                    </TableCell>
                    <TableCell class="text-right">
                      <div class="whitespace-nowrap text-xs tabular-nums">{{ duration(averageMs(user.current)) }}</div>
                      <QueryActivityDelta :current="averageMs(user.current)" :prior="averageMs(user.prior)" kind="latency" compact />
                    </TableCell>
                    <TableCell>
                      <router-link :to="auditLink(stats.window, { actor_id: user.user_id })" :title="t('admin.viewQueryRuns')" :aria-label="t('admin.viewQueryRuns')" class="text-muted-foreground hover:text-foreground">
                        <FileSearch class="h-4 w-4" />
                      </router-link>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>

            <div v-if="stats.service_summary" class="flex items-start justify-between gap-3 rounded-md border bg-muted/30 p-3 text-sm" data-section="service-summary">
              <div class="min-w-0">
                <div class="font-medium">
                  {{ t('admin.serviceAccountCount', { count: stats.service_summary.account_count }, stats.service_summary.account_count) }}
                  · <span class="tabular-nums">{{ t('admin.queryCount', { count: number(stats.service_summary.query_count) }) }}</span>
                </div>
                <div class="truncate text-xs text-muted-foreground">
                  {{ stats.service_summary.top.map(user => `${userLabel(user)} (${compact(user.current.query_count)})`).join(' · ') }}
                </div>
              </div>
              <Button variant="link" size="sm" class="h-auto shrink-0 p-0" @click="setQuery({ account: 'service' })">{{ t('admin.showServiceAccounts') }}</Button>
            </div>

            <div v-if="languageMix.length" data-section="languages">
              <div class="mb-1 text-xs font-medium">{{ t('admin.language') }}</div>
              <div class="flex h-2 gap-0.5 overflow-hidden rounded-full" aria-hidden="true">
                <div v-for="item in languageMix" :key="item.language" :style="{ width: `${item.percent}%`, background: item.color }" />
              </div>
              <ul class="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <li v-for="item in languageMix" :key="item.language" class="flex items-center gap-1">
                  <span class="inline-block h-2 w-2 rounded-full" :style="{ background: item.color }" aria-hidden="true" />
                  <span class="font-medium text-foreground tabular-nums">{{ percent(item.percent) }}</span> {{ item.label }}
                </li>
              </ul>
            </div>
          </div>
        </div>
      </section>

      <section class="rounded-lg border bg-card" data-section="sources">
        <header class="border-b px-4 py-3">
          <h2 class="text-base font-medium">{{ t('admin.sourcesTitle') }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ t('admin.sourcesScope') }}</p>
        </header>
        <EmptyState v-if="!stats.sources.length" :title="t('admin.noUsageInWindow')" class="py-10" />
        <div v-else class="overflow-x-auto">
          <Table class="min-w-[960px]">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t('ui.source') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.queries') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.latencyPercentiles') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.avgScanned') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.failureRate') }}</TableHead>
                <TableHead>{{ t('admin.schemaAdvisor') }}</TableHead>
                <TableHead>{{ t('admin.openIn') }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="source in stats.sources" :key="source.source_id" :data-source="source.source_id">
                <TableCell class="max-w-64">
                  <button type="button" class="block max-w-full truncate text-left font-medium hover:underline focus-visible:underline" @click="setQuery({ source: String(source.source_id) })">
                    {{ sourceLabel(source.source_name, source.source_id) }}
                  </button>
                  <span v-if="account === 'human' && source.service_query_count" class="text-xs text-muted-foreground">
                    {{ t('admin.serviceQueriesExtra', { count: compact(source.service_query_count) }) }}
                  </span>
                  <span v-else-if="account === 'all' && source.service_query_count" class="text-xs text-muted-foreground">
                    {{ t('admin.serviceQueriesIncluded', { count: compact(source.service_query_count) }) }}
                  </span>
                </TableCell>
                <TableCell class="text-right">
                  <div class="tabular-nums">{{ number(source.current.query_count) }}</div>
                  <QueryActivityDelta :current="source.current.query_count" :prior="source.prior.query_count" kind="count" />
                </TableCell>
                <TableCell class="text-right" data-cell="latency">
                  <div v-if="source.cost" class="whitespace-nowrap tabular-nums">
                    {{ formatPercentile(source.cost.p50, locale) }} <span class="text-muted-foreground">/</span> {{ formatPercentile(source.cost.p95, locale) }}
                  </div>
                  <div v-else class="tabular-nums text-muted-foreground" :title="notCollected">—</div>
                  <div class="flex items-center justify-end gap-2">
                    <QueryActivitySparkline :values="source.p95_by_day" :label="t('admin.p95ByDay')" />
                    <span class="whitespace-nowrap text-xs tabular-nums text-muted-foreground">{{ t('admin.averageValue', { value: duration(averageMs(source.current)) }) }}</span>
                  </div>
                  <QueryActivityDelta :current="averageMs(source.current)" :prior="averageMs(source.prior)" kind="latency" />
                </TableCell>
                <TableCell class="text-right" data-cell="scanned">
                  <template v-if="source.cost && (source.cost.avg_bytes_read || source.source_type === 'clickhouse')">
                    <div class="whitespace-nowrap tabular-nums">{{ bytes(source.cost.avg_bytes_read) }}</div>
                    <div class="whitespace-nowrap text-xs tabular-nums text-muted-foreground">{{ t('admin.rowsValue', { count: compact(source.cost.avg_rows_read) }) }}</div>
                  </template>
                  <span v-else-if="source.cost" class="text-muted-foreground" :title="t('admin.scanClickHouseOnly')">—</span>
                  <span v-else class="text-muted-foreground" :title="notCollected">—</span>
                </TableCell>
                <TableCell class="text-right" data-cell="failures">
                  <div class="tabular-nums" :title="source.failure_rate === null ? notCollected : undefined">{{ rate(source) }}</div>
                  <div v-if="source.failure_count" class="whitespace-nowrap text-xs tabular-nums text-muted-foreground">{{ t('admin.failedCount', { count: number(source.failure_count) }) }}</div>
                </TableCell>
                <TableCell class="text-xs">
                  <router-link v-if="source.advisor" :to="advisorLink(source.source_id)" class="hover:underline" data-link="advisor">
                    <template v-if="!source.advisor.analyzed">{{ t('admin.advisorNotAnalyzed') }}</template>
                    <template v-else-if="!source.advisor.high && !source.advisor.medium && !source.advisor.low">{{ t('admin.advisorClean') }}</template>
                    <template v-else>
                      {{ [
                        source.advisor.high && t('admin.findingsHigh', { count: source.advisor.high }),
                        source.advisor.medium && t('admin.findingsMedium', { count: source.advisor.medium }),
                        source.advisor.low && t('admin.findingsLow', { count: source.advisor.low }),
                      ].filter(Boolean).join(' · ') }}
                    </template>
                  </router-link>
                  <span v-else class="text-muted-foreground">—</span>
                </TableCell>
                <TableCell>
                  <div class="flex items-center gap-2 text-muted-foreground">
                    <router-link v-if="source.team_id" :to="{ name: 'LogExplorer', query: { team: String(source.team_id), source: String(source.source_id) } }" :title="t('admin.openInExplorer')" :aria-label="t('admin.openInExplorer')" class="hover:text-foreground">
                      <Telescope class="h-4 w-4" />
                    </router-link>
                    <span v-else role="link" aria-disabled="true" :title="t('admin.noTeamLinksSource')" :aria-label="t('admin.openInExplorer')" class="opacity-40">
                      <Telescope class="h-4 w-4" />
                    </span>
                    <router-link :to="{ name: 'EditSource', params: { sourceId: String(source.source_id) } }" :title="t('admin.sourceSettings')" :aria-label="t('admin.sourceSettings')" class="hover:text-foreground">
                      <Settings2 class="h-4 w-4" />
                    </router-link>
                    <router-link :to="auditLink(stats.window, { source_id: source.source_id })" :title="t('admin.viewQueryRuns')" :aria-label="t('admin.viewQueryRuns')" class="hover:text-foreground">
                      <FileSearch class="h-4 w-4" />
                    </router-link>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </section>

      <section class="rounded-lg border bg-card" data-section="patterns">
        <header class="border-b px-4 py-3">
          <h2 class="text-base font-medium">{{ t('admin.patternsTitle') }}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ t('admin.patternsScope') }}</p>
        </header>
        <EmptyState v-if="!stats.shapes.rows.length" :title="t('admin.noShapes')" class="py-10" />
        <div v-else class="overflow-x-auto">
          <Table class="min-w-[760px]">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t('admin.filterShape') }}</TableHead>
                <TableHead>{{ t('ui.source') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.runs') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.totalTime') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.avgLatency') }}</TableHead>
                <TableHead class="text-right">{{ t('admin.scannedPerRun') }}</TableHead>
                <TableHead>{{ t('admin.schemaAdvisor') }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="row in stats.shapes.rows" :key="`${row.source_id}-${row.query_language}-${row.shape_hash}`">
                <TableCell class="max-w-80">
                  <div class="flex flex-wrap gap-1">
                    <code v-for="item in row.shape" :key="`${shapeColumn(item)}-${item.kind}`" class="rounded bg-muted px-1.5 py-0.5 text-xs">
                      {{ shapeColumn(item) }} <span class="text-muted-foreground">{{ item.kind }}</span>
                    </code>
                  </div>
                  <div class="mt-1 text-xs text-muted-foreground">
                    {{ languageLabel(row.query_language) }}<template v-if="row.max_lookback_s !== null"> · {{ t('admin.maxLookback', { duration: formatLookback(row.max_lookback_s) }) }}</template>
                  </div>
                </TableCell>
                <TableCell class="max-w-40">
                  <button type="button" class="block max-w-full truncate text-left text-sm hover:underline focus-visible:underline" @click="setQuery({ source: String(row.source_id) })">
                    {{ sourceLabel(row.source_name, row.source_id) }}
                  </button>
                </TableCell>
                <TableCell class="text-right">
                  <div class="tabular-nums">{{ number(row.current.query_count) }}</div>
                  <QueryActivityDelta :current="row.current.query_count" :prior="row.prior?.query_count ?? null" kind="count" />
                </TableCell>
                <TableCell class="whitespace-nowrap text-right tabular-nums">{{ formatTotalTime(row.current.total_duration_ms, locale) ?? duration(row.current.total_duration_ms) }}</TableCell>
                <TableCell class="text-right">
                  <div class="whitespace-nowrap tabular-nums">{{ duration(averageMs(row.current)) }}</div>
                  <QueryActivityDelta :current="averageMs(row.current)" :prior="row.prior ? averageMs(row.prior) : null" kind="latency" />
                </TableCell>
                <TableCell class="whitespace-nowrap text-right tabular-nums" data-cell="scanned">
                  <template v-if="row.avg_bytes_read !== null">{{ bytes(row.avg_bytes_read) }}</template>
                  <span v-else class="text-muted-foreground" :title="notCollected">—</span>
                </TableCell>
                <TableCell class="max-w-64 text-xs">
                  <router-link v-if="row.source_type === 'clickhouse'" :to="advisorLink(row.source_id)" class="hover:underline" data-link="advisor">
                    {{ row.finding ? row.finding.title : t('admin.openAdvisor') }}
                  </router-link>
                  <span v-else class="text-muted-foreground">—</span>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
        <footer v-if="stats.shapes.unanalyzed_query_count || stats.shapes.since !== stats.window.since" class="space-y-1 border-t px-4 py-2 text-xs text-muted-foreground">
          <p v-if="stats.shapes.unanalyzed_query_count">{{ t('admin.unanalyzedFootnote', { count: number(stats.shapes.unanalyzed_query_count) }) }}</p>
          <p v-if="stats.shapes.since !== stats.window.since">{{ t('admin.shapesSince', { date: formatDay(stats.shapes.since) }) }}</p>
        </footer>
      </section>

      <section class="rounded-lg border bg-card" data-section="failures">
        <header class="flex flex-wrap items-start justify-between gap-3 border-b px-4 py-3">
          <div>
            <h2 class="text-base font-medium">{{ t('admin.failuresTitle') }}</h2>
            <p v-if="stats.summary.current.failure_count" class="mt-0.5 text-xs text-muted-foreground">
              {{ t('admin.failuresScope', { count: number(stats.summary.current.failure_count), rate: rate(stats.summary.current) }) }}
            </p>
          </div>
          <Button v-if="stats.summary.current.failure_count" variant="link" size="sm" class="h-auto p-0" as-child>
            <router-link :to="failureLink()" data-link="all-failures">{{ t('admin.viewAllFailures') }}</router-link>
          </Button>
        </header>
        <EmptyState
          v-if="!stats.summary.current.failure_count"
          :icon="CircleCheck"
          :title="stats.collected_since ? t('admin.noFailures') : t('admin.notCollectedYet')"
          class="py-10"
        />
        <div v-else class="grid gap-6 p-4 lg:grid-cols-2">
          <div class="min-w-0">
            <h3 class="text-sm font-medium">{{ t('admin.failuresByReason') }}</h3>
            <ul class="mt-2 divide-y rounded-md border text-sm" data-list="reasons">
              <li v-for="item in stats.failures.by_reason" :key="item.reason" class="flex items-center justify-between gap-3 px-3 py-2">
                <router-link :to="failureLink()" class="min-w-0 truncate hover:underline">{{ reasonLabel(item.reason) }}</router-link>
                <span class="flex shrink-0 items-baseline gap-2">
                  <span class="font-medium tabular-nums">{{ number(item.count) }}</span>
                  <QueryActivityDelta :current="item.count" :prior="item.prior" kind="failures" />
                </span>
              </li>
            </ul>
          </div>
          <div class="min-w-0">
            <h3 class="text-sm font-medium">{{ t('admin.failuresWhere') }}</h3>
            <ul class="mt-2 divide-y rounded-md border text-sm" data-list="failure-pairs">
              <li v-for="pair in stats.failures.top" :key="`${pair.source_id}-${pair.user_id}`" class="flex items-center justify-between gap-3 px-3 py-2">
                <div class="min-w-0">
                  <div class="truncate font-medium">
                    {{ sourceLabel(pair.source_name, pair.source_id) }} · {{ t('admin.failedCount', { count: number(pair.count) }) }}
                  </div>
                  <div class="truncate text-xs text-muted-foreground">
                    {{ userLabel(pair) }}<template v-if="pair.account_type === 'service'"> · {{ t('admin.serviceAccount') }}</template>
                  </div>
                </div>
                <router-link :to="failureLink(pair)" class="shrink-0 text-xs text-muted-foreground hover:text-foreground" :aria-label="t('admin.viewFailedRuns')">
                  {{ t('routes.auditLog') }} →
                </router-link>
              </li>
            </ul>
          </div>
        </div>
        <footer v-if="partlyCollected" class="border-t px-4 py-2 text-xs text-muted-foreground">
          {{ partlyCollected }}
        </footer>
      </section>
    </template>
  </div>
</template>
