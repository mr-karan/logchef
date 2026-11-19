<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toValue } from "vue";
import { useI18n } from "vue-i18n";
import { Activity, RefreshCw, ShieldCheck } from "lucide-vue-next";
import { PageHeader, LoadingState, EmptyState, SearchInput } from "@/components/layout";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { adminApi, type QueryActivityFilters, type QueryActivityResponse, type QueryStatsResponse } from "@/api/admin";
import { isSuccessResponse } from "@/api/types";
import { formatErrorMessage } from "@/api/error-handler";
import { formatHistoryTimeAgo, formatHistoryDuration } from "@/lib/queryHistory";
import { useMetaStore } from "@/stores/meta";
import { usePreferencesStore } from "@/stores/preferences";

const RECENT_LIMIT = 100;
const PAGE_SIZE = 20;
const { t, locale } = useI18n();
const metaStore = useMetaStore();
const preferencesStore = usePreferencesStore();
const isDemo = computed(() => metaStore.demoReadOnly);
const statsDays = ref("30");
const stats = ref<QueryStatsResponse | null>(null);
const statsLoading = ref(true);
const statsError = ref<string | null>(null);
const activity = ref<QueryActivityResponse | null>(null);
const activityLoading = ref(true);
const activityError = ref<string | null>(null);
const searchDraft = ref("");
const filters = ref<QueryActivityFilters>({});
const sourceScope = ref<string | null>(null);
const userScope = ref<string | null>(null);
const visibleCount = ref(PAGE_SIZE);
const investigation = ref<HTMLElement | null>(null);
const nowMs = ref(Date.now());
let statsRequest: AbortController | undefined;
let activityRequest: AbortController | undefined;
const hasFilters = computed(() => Object.values(filters.value).some(value => value !== undefined && value !== ""));
const isRefreshing = computed(() => statsLoading.value || activityLoading.value);
const dailySeries = computed(() => {
  if (!stats.value) return [];
  const counts = new Map(stats.value.volume_by_day.map(day => [day.date, day.query_count]));
  const start = new Date(`${stats.value.since}T00:00:00Z`);
  return Array.from({ length: stats.value.days }, (_, index) => {
    const date = new Date(start);
    date.setUTCDate(start.getUTCDate() + index);
    const key = date.toISOString().slice(0, 10);
    return { date: key, count: counts.get(key) ?? 0 };
  });
});
const maxDaily = computed(() => Math.max(1, ...dailySeries.value.map(day => day.count)));
const metrics = computed(() => stats.value ? [
  { label: t("admin.totalQueries"), value: stats.value.summary.query_count.toLocaleString(toValue(locale)) },
  { label: t("admin.activeSources"), value: stats.value.summary.active_sources.toLocaleString(toValue(locale)) },
  { label: t("admin.activeUsers"), value: stats.value.summary.active_users.toLocaleString(toValue(locale)) },
  { label: t("admin.averageLatency"), value: duration(stats.value.summary.avg_duration_ms) },
] : []);
const feeds = computed(() => [
  { key: "recent", title: t("admin.recentQueries"), queries: activity.value?.recent.slice(0, visibleCount.value) ?? [] },
  { key: "slowest", title: t("admin.slowestRecentQueries"), queries: activity.value?.slowest ?? [] },
]);

async function loadStats() {
  statsRequest?.abort();
  const request = new AbortController();
  statsRequest = request;
  statsLoading.value = true;
  statsError.value = null;
  try {
    const response = await adminApi.getQueryStats(Number(statsDays.value), { signal: request.signal });
    if (request.signal.aborted) return;
    if (isSuccessResponse(response)) stats.value = response.data ?? null;
    else statsError.value = response.message || t("admin.usageStatsLoadFailed");
  } catch (err: unknown) {
    if (!request.signal.aborted) statsError.value = formatErrorMessage(err, t("admin.usageStatsLoadFailed"));
  } finally {
    if (!request.signal.aborted) statsLoading.value = false;
  }
}

async function loadActivity() {
  activityRequest?.abort();
  const request = new AbortController();
  activityRequest = request;
  activityError.value = null;
  visibleCount.value = PAGE_SIZE;
  if (isDemo.value) {
    activity.value = null;
    activityLoading.value = false;
    return;
  }
  activityLoading.value = true;
  try {
    const response = await adminApi.getQueryActivity(RECENT_LIMIT, filters.value, { signal: request.signal });
    if (request.signal.aborted) return;
    if (isSuccessResponse(response)) {
      activity.value = response.data ?? null;
      nowMs.value = Date.now();
    } else activityError.value = response.message || t("admin.queryActivityLoadFailed");
  } catch (err: unknown) {
    if (!request.signal.aborted) activityError.value = formatErrorMessage(err, t("admin.queryActivityLoadFailed"));
  } finally {
    if (!request.signal.aborted) activityLoading.value = false;
  }
}

async function refreshAll() {
  await Promise.all([loadStats(), loadActivity()]);
}
function search() {
  filters.value = { ...filters.value, q: searchDraft.value.trim() || undefined };
  loadActivity();
}
async function scopeSource(id: number, label: string) {
  sourceScope.value = label;
  filters.value = { ...filters.value, source_id: id };
  loadActivity();
  await nextTick();
  investigation.value?.focus();
  investigation.value?.scrollIntoView?.({ behavior: "smooth", block: "start" });
}
async function scopeUser(id: number, label: string) {
  userScope.value = label;
  filters.value = { ...filters.value, user_id: id };
  loadActivity();
  await nextTick();
  investigation.value?.focus();
  investigation.value?.scrollIntoView?.({ behavior: "smooth", block: "start" });
}
function clearFilter(key?: keyof QueryActivityFilters) {
  if (!key || key === "source_id") sourceScope.value = null;
  if (!key || key === "user_id") userScope.value = null;
  if (!key || key === "q") searchDraft.value = "";
  if (key) {
    const next = { ...filters.value };
    delete next[key];
    filters.value = next;
  } else filters.value = {};
  loadActivity();
}
function sourceLabel(name: string, id: number) {
  return name || t("admin.sourceFallback", { id });
}
function userLabel(email: string, id: number) {
  return email || t("admin.userFallback", { id });
}
function formatDay(date: string) {
  return new Intl.DateTimeFormat(toValue(locale), { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(`${date}T00:00:00Z`));
}
function timeAgo(date: string) {
  return formatHistoryTimeAgo(date, preferencesStore.zone, nowMs.value, toValue(locale));
}
function duration(ms: number) {
  return formatHistoryDuration(ms, toValue(locale));
}
function languageLabel(language: string) {
  return ({ logchefql: "LogChefQL", "clickhouse-sql": "ClickHouse SQL", logsql: "LogsQL", logql: "LogQL" })[language] ?? language;
}
onMounted(refreshAll);
onBeforeUnmount(() => {
  statsRequest?.abort();
  activityRequest?.abort();
});
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('routes.queryActivity')">
      <template #actions>
        <Button variant="ghost" size="sm" as-child><router-link :to="{ name: 'AdminAuditLog' }">{{ t('routes.auditLog') }}</router-link></Button>
        <Button variant="outline" size="sm" :disabled="isRefreshing" @click="refreshAll">
          <RefreshCw :class="['mr-2 h-3.5 w-3.5', isRefreshing && 'animate-spin']" />{{ t('ui.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <section class="overflow-hidden rounded-lg border bg-card">
      <div class="flex flex-wrap items-center justify-between gap-4 border-b px-5 py-4">
        <div>
          <h2 class="font-semibold">{{ t('admin.recordedUsage') }}</h2>
          <p v-if="stats && !statsLoading" class="mt-1 text-xs text-muted-foreground">{{ t('admin.recordedUsageScope', { date: formatDay(stats.since) }) }}</p>
        </div>
        <div class="flex items-center gap-2">
          <label for="stats-days" class="text-sm text-muted-foreground">{{ t('admin.window') }}</label>
          <select id="stats-days" v-model="statsDays" class="h-9 rounded-md border bg-background px-3 text-sm" @change="loadStats">
            <option v-for="days in [7, 30, 90]" :key="days" :value="String(days)">{{ t('settings.days', { count: days }, days) }}</option>
          </select>
        </div>
      </div>
      <LoadingState v-if="statsLoading" class="py-16" :label="t('admin.loadingUsageStats')" />
      <div v-else-if="statsError" role="alert" class="p-5 text-sm text-destructive">
        {{ statsError }} <Button variant="outline" size="sm" class="ml-3" @click="loadStats">{{ t('admin.retry') }}</Button>
      </div>
      <template v-else-if="stats">
        <dl class="grid border-b sm:grid-cols-2 xl:grid-cols-4">
          <div v-for="metric in metrics" :key="metric.label" class="border-border p-5 sm:odd:border-r xl:border-r xl:last:border-0">
            <dt class="text-xs font-medium text-muted-foreground">{{ metric.label }}</dt>
            <dd class="mt-2 text-3xl font-semibold tracking-tight tabular-nums">{{ metric.value }}</dd>
          </div>
        </dl>
        <div class="p-5">
          <h3 class="text-sm font-semibold">{{ t('admin.dailyVolume') }}</h3>
          <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.dailyVolumeDescription') }}</p>
          <EmptyState v-if="!stats.summary.query_count" :icon="Activity" :title="t('admin.noUsageInWindow')" />
          <template v-else>
            <div class="mt-5 overflow-x-auto pb-7">
              <div class="flex h-44 items-end gap-1 border-b" :style="{ minWidth: `${dailySeries.length * 18}px` }" aria-hidden="true">
                <div v-for="(day, index) in dailySeries" :key="day.date" class="relative flex h-full min-w-3 flex-1 items-end justify-center">
                  <div class="w-full rounded-t-sm bg-primary/65" :style="{ height: day.count ? `${Math.max(2, day.count / maxDaily * 85)}%` : '1px' }" :title="`${formatDay(day.date)}: ${day.count}`" />
                  <span v-if="index % Math.max(1, Math.ceil(dailySeries.length / 8)) === 0" class="absolute bottom-0 left-0 translate-y-5 whitespace-nowrap text-[10px] text-muted-foreground">{{ formatDay(day.date) }}</span>
                </div>
              </div>
            </div>
            <details class="mt-3 text-sm">
              <summary class="w-fit cursor-pointer text-muted-foreground hover:text-foreground">{{ t('admin.viewDailyValues') }}</summary>
              <div class="mt-2 max-h-60 overflow-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{{ t('admin.utcDate') }}</TableHead>
                      <TableHead class="text-right">{{ t('admin.totalQueries') }}</TableHead></TableRow>
                    </TableHeader>
                    <TableBody>
                      <TableRow v-for="day in dailySeries" :key="day.date"><TableCell>{{ day.date }}</TableCell>
                      <TableCell class="text-right tabular-nums">{{ day.count.toLocaleString() }}</TableCell></TableRow>
                    </TableBody>
                  </Table>
              </div>
            </details>
          </template>
        </div>
        <div class="grid border-t lg:grid-cols-2">
          <section class="min-w-0 border-b p-5 lg:border-b-0 lg:border-r">
            <h3 class="text-sm font-semibold">{{ t('admin.topSources') }}</h3>
            <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.rankingScope') }}</p>
            <Table v-if="stats.top_sources.length" class="mt-3">
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t('ui.source') }}</TableHead>
                  <TableHead class="text-right">{{ t('admin.totalQueries') }}</TableHead>
                  <TableHead class="text-right">{{ t('admin.averageLatency') }}</TableHead></TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="source in stats.top_sources" :key="source.source_id">
                    <TableCell class="max-w-48 truncate"><Button v-if="!isDemo" variant="link" class="h-auto max-w-full p-0 text-left" @click="scopeSource(source.source_id, sourceLabel(source.source_name, source.source_id))"><span class="truncate">{{ sourceLabel(source.source_name, source.source_id) }}</span></Button><span v-else>{{ sourceLabel(source.source_name, source.source_id) }}</span></TableCell>
                    <TableCell class="text-right tabular-nums">{{ source.query_count.toLocaleString() }}</TableCell>
                    <TableCell class="whitespace-nowrap text-right tabular-nums">{{ duration(source.avg_duration_ms) }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            <EmptyState v-else :title="t('admin.noUsageInWindow')" />
          </section>
          <section class="min-w-0 p-5">
            <h3 class="text-sm font-semibold">{{ t('admin.topUsers') }}</h3>
            <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.rankingScope') }}</p>
            <Table v-if="stats.top_users.length" class="mt-3">
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t('ui.user') }}</TableHead>
                  <TableHead class="text-right">{{ t('admin.totalQueries') }}</TableHead></TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="user in stats.top_users" :key="user.user_id">
                    <TableCell class="max-w-48 truncate"><Button v-if="!isDemo" variant="link" class="h-auto max-w-full p-0 text-left" @click="scopeUser(user.user_id, userLabel(user.user_email, user.user_id))"><span class="truncate">{{ userLabel(user.user_email, user.user_id) }}</span></Button><span v-else>{{ userLabel(user.user_email, user.user_id) }}</span></TableCell>
                    <TableCell class="text-right tabular-nums">{{ user.query_count.toLocaleString() }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            <EmptyState v-else :title="t('admin.noUsageInWindow')" />
          </section>
        </div>
      </template>
    </section>

    <section v-if="isDemo" class="flex gap-3 rounded-lg border p-5">
      <ShieldCheck class="h-5 w-5 shrink-0 text-muted-foreground" />
      <div><h2 class="text-sm font-semibold">{{ t('admin.publicDemoAggregateOnly') }}</h2><p class="mt-1 text-sm text-muted-foreground">{{ t('admin.publicDemoAggregateDescription') }}</p></div>
    </section>
    <section v-else ref="investigation" tabindex="-1" class="scroll-mt-5 space-y-4 rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <div>
        <h2 class="text-base font-semibold">{{ t('admin.recentQuerySample') }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('admin.retainedScope') }}</p>
      </div>
      <div class="space-y-3 rounded-lg border bg-card p-4">
        <form class="flex flex-wrap items-center gap-2" @submit.prevent="search">
          <SearchInput v-model="searchDraft" :placeholder="t('admin.searchRetained')" class="sm:max-w-md" />
          <Button type="submit" variant="outline" size="sm">{{ t('ui.search') }}</Button>
          <label for="activity-language" class="sr-only">{{ t('admin.language') }}</label>
          <select id="activity-language" v-model="filters.language" class="h-9 rounded-md border bg-background px-3 text-sm" @change="loadActivity">
            <option :value="undefined">{{ t('admin.allLanguages') }}</option>
            <option v-for="language in ['logchefql', 'clickhouse-sql', 'logsql', 'logql']" :key="language" :value="language">{{ languageLabel(language) }}</option>
          </select>
          <Button v-if="hasFilters" type="button" variant="ghost" size="sm" @click="clearFilter()">{{ t('admin.clearFilters') }}</Button>
        </form>
        <div v-if="hasFilters" class="flex flex-wrap gap-2 text-xs">
          <Button v-if="sourceScope" variant="secondary" size="sm" @click="clearFilter('source_id')">{{ t('ui.source') }}: {{ sourceScope }} ×</Button>
          <Button v-if="userScope" variant="secondary" size="sm" @click="clearFilter('user_id')">{{ t('ui.user') }}: {{ userScope }} ×</Button>
          <Button v-if="filters.q" variant="secondary" size="sm" @click="clearFilter('q')">{{ t('ui.search') }}: {{ filters.q }} ×</Button>
        </div>
        <p v-if="activity && !activityLoading && !activityError" class="text-xs text-muted-foreground tabular-nums" role="status">{{ t('admin.retainedMatches', { matches: activity.total.toLocaleString(), scanned: activity.window_size.toLocaleString(), limit: activity.window_limit.toLocaleString(), loaded: activity.recent.length }) }}</p>
      </div>
      <LoadingState v-if="activityLoading" :label="t('admin.loadingRecentQueryActivity')" />
      <div v-else-if="activityError" role="alert" class="rounded-md border border-destructive/50 p-4 text-sm text-destructive">{{ activityError }} <Button variant="outline" size="sm" class="ml-3" @click="loadActivity">{{ t('admin.retry') }}</Button></div>
      <EmptyState v-else-if="!activity?.total" :icon="Activity" :title="hasFilters ? t('admin.noQueriesMatchFilter') : t('admin.noRecentQueries')" />
      <template v-else>
        <section v-for="feed in feeds" :key="feed.key" class="overflow-hidden rounded-lg border bg-card">
          <div class="border-b px-5 py-4"><h3 class="text-sm font-semibold">{{ feed.title }}</h3><p class="mt-1 text-xs text-muted-foreground">{{ t(feed.key === 'recent' ? 'admin.recentFeedScope' : 'admin.slowFeedScope', { count: feed.queries.length, limit: RECENT_LIMIT }) }}</p></div>
          <div class="overflow-x-auto">
            <Table class="min-w-[850px] table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead class="w-28">{{ t('admin.time') }}</TableHead>
                  <TableHead class="w-36">{{ t('ui.source') }}</TableHead>
                  <TableHead class="w-44">{{ t('ui.user') }}</TableHead>
                  <TableHead class="w-28">{{ t('admin.language') }}</TableHead>
                  <TableHead class="w-24 text-right">{{ t('admin.duration') }}</TableHead>
                  <TableHead class="w-20 text-right">{{ t('admin.rows') }}</TableHead>
                  <TableHead class="w-72">{{ t('admin.query') }}</TableHead></TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="query in feed.queries" :key="`${feed.key}-${query.id}`">
                    <TableCell class="text-xs text-muted-foreground"><time :datetime="query.created_at" :title="query.created_at">{{ timeAgo(query.created_at) }}</time></TableCell>
                    <TableCell><button class="block max-w-full truncate text-left text-xs hover:underline focus-visible:underline" :title="sourceLabel(query.source_name, query.source_id)" @click="scopeSource(query.source_id, sourceLabel(query.source_name, query.source_id))">{{ sourceLabel(query.source_name, query.source_id) }}</button></TableCell>
                    <TableCell><button class="block max-w-full truncate text-left text-xs hover:underline focus-visible:underline" :title="userLabel(query.user_email, query.user_id)" @click="scopeUser(query.user_id, userLabel(query.user_email, query.user_id))">{{ userLabel(query.user_email, query.user_id) }}</button></TableCell>
                    <TableCell class="text-xs">{{ languageLabel(query.query_language) }}</TableCell>
                    <TableCell class="text-right text-xs tabular-nums">{{ duration(query.duration_ms) }}</TableCell>
                    <TableCell class="text-right text-xs tabular-nums">{{ query.row_count.toLocaleString() }}</TableCell>
                    <TableCell class="align-top"><details v-if="query.query_text?.trim()" class="group"><summary class="cursor-pointer text-xs"><span class="inline-block max-w-56 truncate align-bottom font-mono group-open:hidden">{{ query.query_text }}</span><span class="hidden group-open:inline">{{ t('admin.hideQuery') }}</span></summary><pre class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-3 text-xs">{{ query.query_text }}</pre></details><span v-else class="text-xs italic text-muted-foreground">{{ t('admin.queryTextUnavailable', { language: languageLabel(query.query_language) }) }}</span></TableCell>
                  </TableRow>
                </TableBody>
              </Table>
          </div>
          <div v-if="feed.key === 'recent' && visibleCount < activity.recent.length" class="border-t p-3 text-center"><Button variant="ghost" size="sm" @click="visibleCount += PAGE_SIZE">{{ t('admin.showMoreRecords', { count: Math.min(PAGE_SIZE, activity.recent.length - visibleCount) }) }}</Button></div>
        </section>
      </template>
    </section>
  </div>
</template>
