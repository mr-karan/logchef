<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import type { AcceptableValue } from "reka-ui";
import {
  Activity,
  Clock3,
  Database,
  RefreshCw,
  Search,
  ShieldCheck,
  Users,
} from "lucide-vue-next";
import { PageHeader, LoadingState, EmptyState } from "@/components/layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  adminApi,
  type QueryActivityResponse,
  type QueryStatsResponse,
} from "@/api/admin";
import { isSuccessResponse } from "@/api/types";
import { formatHistoryTimeAgo, formatHistoryDuration } from "@/lib/queryHistory";
import { useMetaStore } from "@/stores/meta";

const RECENT_LIMIT = 100;
const RECENT_PAGE_SIZE = 20;
const DAYS_OPTIONS = ["7", "30", "90"] as const;

const metaStore = useMetaStore();
const isDemo = computed(() => metaStore.demoReadOnly);

const isLoading = ref(true);
const error = ref<string | null>(null);
const activity = ref<QueryActivityResponse | null>(null);
const nowMs = ref(Date.now());
const recentSearch = ref("");
const visibleRecentCount = ref(RECENT_PAGE_SIZE);

const statsDays = ref<string>("30");
const statsLoading = ref(true);
const statsError = ref<string | null>(null);
const stats = ref<QueryStatsResponse | null>(null);

async function loadActivity() {
  error.value = null;

  if (isDemo.value) {
    activity.value = {
      total: 0,
      recent: [],
      by_language: [],
      by_source: [],
      slowest: [],
    };
    isLoading.value = false;
    return;
  }

  isLoading.value = true;
  try {
    const response = await adminApi.getQueryActivity(RECENT_LIMIT);
    if (isSuccessResponse(response)) {
      activity.value = response.data ?? null;
      nowMs.value = Date.now();
    } else {
      error.value = response.message || "Failed to load query activity.";
    }
  } catch (err: any) {
    error.value = err?.message || "Failed to load query activity.";
  } finally {
    isLoading.value = false;
  }
}

async function loadStats() {
  statsLoading.value = true;
  statsError.value = null;
  try {
    const response = await adminApi.getQueryStats(Number(statsDays.value));
    if (isSuccessResponse(response)) {
      stats.value = response.data ?? null;
    } else {
      statsError.value = response.message || "Failed to load usage stats.";
    }
  } catch (err: any) {
    statsError.value = err?.message || "Failed to load usage stats.";
  } finally {
    statsLoading.value = false;
  }
}

async function refreshAll() {
  await Promise.all([loadStats(), loadActivity()]);
}

function onDaysChange(value: AcceptableValue) {
  if (typeof value !== "string") return;
  statsDays.value = value;
  loadStats();
}

const isRefreshing = computed(() => statsLoading.value || isLoading.value);
const topSources = computed(() => stats.value?.top_sources ?? []);
const topUsers = computed(() => stats.value?.top_users ?? []);
const volumeByDay = computed(() => stats.value?.volume_by_day ?? []);
const total = computed(() => activity.value?.total ?? 0);
const byLanguage = computed(() => activity.value?.by_language ?? []);
const slowest = computed(() => activity.value?.slowest.slice(0, 5) ?? []);
const recent = computed(() => activity.value?.recent ?? []);

const statsTotal = computed(() =>
  volumeByDay.value.reduce((sum, day) => sum + day.query_count, 0)
);

const averageDuration = computed(() => {
  const count = topSources.value.reduce((sum, source) => sum + source.query_count, 0);
  if (!count) return 0;
  const totalDuration = topSources.value.reduce(
    (sum, source) => sum + source.query_count * source.avg_duration_ms,
    0
  );
  return Math.round(totalDuration / count);
});

const busiestDay = computed(() =>
  volumeByDay.value.reduce<(typeof volumeByDay.value)[number] | null>(
    (busiest, day) => (!busiest || day.query_count > busiest.query_count ? day : busiest),
    null
  )
);

const dailySeries = computed(() => {
  if (!stats.value) return [];
  const byDate = new Map(volumeByDay.value.map((day) => [day.date, day.query_count]));
  const start = new Date(`${stats.value.since}T00:00:00Z`);
  return Array.from({ length: stats.value.days }, (_, index) => {
    const date = new Date(start);
    date.setUTCDate(start.getUTCDate() + index);
    const key = date.toISOString().slice(0, 10);
    return { date: key, query_count: byDate.get(key) ?? 0 };
  });
});

const maxDailyVolume = computed(() =>
  dailySeries.value.reduce((max, day) => Math.max(max, day.query_count), 0)
);

const maxTopSourceCount = computed(() =>
  topSources.value.reduce((max, source) => Math.max(max, source.query_count), 0)
);

function dailyBarHeight(count: number): string {
  if (!count || !maxDailyVolume.value) return "2px";
  return `${Math.max(8, Math.round((count / maxDailyVolume.value) * 100))}%`;
}

function topSourceWidth(count: number): string {
  if (!maxTopSourceCount.value) return "0%";
  return `${Math.max(4, Math.round((count / maxTopSourceCount.value) * 100))}%`;
}

function sourceLabel(name: string, sourceId: number): string {
  return name || `Source #${sourceId}`;
}

function formatDay(date: string): string {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(new Date(`${date}T00:00:00Z`));
}

function timeAgo(createdAt: string): string {
  return formatHistoryTimeAgo(createdAt, nowMs.value);
}

function duration(ms: number): string {
  return formatHistoryDuration(ms);
}

function languageLabel(language: string): string {
  if (language === "logchefql") return "LogChefQL";
  if (language === "clickhouse-sql") return "ClickHouse SQL";
  if (language === "logsql") return "LogsQL";
  return language;
}

function queryLabel(queryText: string, language: string): string {
  const trimmed = queryText?.trim();
  return trimmed || `${languageLabel(language)} query · text unavailable`;
}

const filteredRecent = computed(() => {
  const search = recentSearch.value.trim().toLowerCase();
  if (!search) return recent.value;
  return recent.value.filter((query) =>
    [query.query_text, query.source_name, query.user_email, query.query_language]
      .some((value) => String(value ?? "").toLowerCase().includes(search))
  );
});

const visibleRecent = computed(() =>
  filteredRecent.value.slice(0, visibleRecentCount.value)
);

watch(recentSearch, () => {
  visibleRecentCount.value = RECENT_PAGE_SIZE;
});

onMounted(refreshAll);
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="Query Activity"
      description="Understand query volume, latency, sources, and users without leaving LogChef."
    >
      <template #actions>
        <Button variant="outline" size="sm" :disabled="isRefreshing" @click="refreshAll">
          <RefreshCw :class="['mr-2 h-3.5 w-3.5', isRefreshing && 'animate-spin']" />
          Refresh
        </Button>
      </template>
    </PageHeader>

    <section class="overflow-hidden rounded-lg border bg-card shadow-sm">
      <div class="flex flex-wrap items-start justify-between gap-4 border-b px-5 py-4">
        <div class="flex items-start gap-3">
          <div class="mt-0.5 rounded-md bg-sky-500/10 p-2 text-sky-600 dark:bg-sky-400/10 dark:text-sky-300">
            <Activity class="h-4 w-4" />
          </div>
          <div>
            <div class="flex items-center gap-2">
              <h2 class="font-semibold">Usage rollup</h2>
              <Badge variant="secondary" class="font-normal">Durable totals</Badge>
            </div>
            <p class="mt-1 text-sm text-muted-foreground">
              Aggregate query telemetry for capacity planning and adoption.
              <span v-if="stats" class="tabular-nums">Window starts {{ formatDay(stats.since) }}.</span>
            </p>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <label for="stats-days" class="text-xs font-medium text-muted-foreground">Window</label>
          <Select :model-value="statsDays" @update:model-value="onDaysChange">
            <SelectTrigger id="stats-days" class="h-8 w-[126px] text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="days in DAYS_OPTIONS" :key="days" :value="days">
                Last {{ days }} days
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <LoadingState v-if="statsLoading" class="py-16" label="Loading usage stats…" />

      <div
        v-else-if="statsError"
        class="m-5 rounded-md border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
      >
        {{ statsError }}
      </div>

      <template v-else-if="stats">
        <div class="grid border-b sm:grid-cols-2 xl:grid-cols-4">
          <div class="border-b p-5 sm:border-r xl:border-b-0">
            <div class="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <Activity class="h-3.5 w-3.5" /> Total queries
            </div>
            <p class="mt-2 text-3xl font-semibold tracking-tight tabular-nums">{{ statsTotal.toLocaleString() }}</p>
            <p class="mt-1 text-xs text-muted-foreground">in the selected window</p>
          </div>
          <div class="border-b p-5 xl:border-b-0 xl:border-r">
            <div class="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <Database class="h-3.5 w-3.5" /> Active sources
            </div>
            <p class="mt-2 text-3xl font-semibold tracking-tight tabular-nums">{{ topSources.length }}</p>
            <p class="mt-1 text-xs text-muted-foreground">sources receiving queries</p>
          </div>
          <div class="border-b p-5 sm:border-r sm:border-b-0">
            <div class="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <Clock3 class="h-3.5 w-3.5" /> Average latency
            </div>
            <p class="mt-2 text-3xl font-semibold tracking-tight tabular-nums">{{ duration(averageDuration) }}</p>
            <p class="mt-1 text-xs text-muted-foreground">weighted across top sources</p>
          </div>
          <div class="p-5">
            <div class="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <Users class="h-3.5 w-3.5" /> Top user
            </div>
            <p class="mt-2 truncate text-sm font-semibold" :title="topUsers[0]?.user_email">
              {{ topUsers[0]?.user_email || "No activity yet" }}
            </p>
            <p class="mt-1 text-xs text-muted-foreground tabular-nums">
              {{ topUsers[0] ? `${topUsers[0].query_count.toLocaleString()} queries` : "—" }}
            </p>
          </div>
        </div>

        <div class="grid gap-0 xl:grid-cols-[minmax(0,2fr)_minmax(300px,1fr)]">
          <div class="border-b p-5 xl:border-r xl:border-b-0">
            <div class="mb-4 flex items-start justify-between gap-4">
              <div>
                <h3 class="text-sm font-semibold">Daily volume</h3>
                <p class="mt-1 text-xs text-muted-foreground">Queries completed successfully, grouped by UTC day.</p>
              </div>
              <div v-if="busiestDay" class="text-right text-xs text-muted-foreground">
                Peak <span class="font-medium text-foreground tabular-nums">{{ busiestDay.query_count.toLocaleString() }}</span>
                <span class="ml-1">on {{ formatDay(busiestDay.date) }}</span>
              </div>
            </div>
            <div class="overflow-x-auto pb-1">
              <div
                class="flex h-36 items-end gap-1 border-b border-border/70"
                :class="dailySeries.length > 30 && 'min-w-[720px]'"
              >
                <div
                  v-for="day in dailySeries"
                  :key="day.date"
                  class="group relative flex h-full min-w-[5px] flex-1 items-end"
                  :title="`${formatDay(day.date)}: ${day.query_count.toLocaleString()} queries`"
                >
                  <div
                    class="w-full rounded-t-sm bg-sky-500/75 transition-colors group-hover:bg-sky-400 dark:bg-sky-400/70 dark:group-hover:bg-sky-300"
                    :class="!day.query_count && 'bg-muted dark:bg-muted'"
                    :style="{ height: dailyBarHeight(day.query_count) }"
                  />
                </div>
              </div>
            </div>
            <div v-if="dailySeries.length" class="mt-2 flex justify-between text-[10px] text-muted-foreground tabular-nums">
              <span>{{ formatDay(dailySeries[0].date) }}</span>
              <span>{{ formatDay(dailySeries[dailySeries.length - 1].date) }}</span>
            </div>
          </div>

          <div class="p-5">
            <h3 class="text-sm font-semibold">Top sources</h3>
            <p class="mt-1 text-xs text-muted-foreground">Ranked by completed queries.</p>
            <div v-if="topSources.length" class="mt-4 space-y-4">
              <div v-for="source in topSources.slice(0, 6)" :key="source.source_id" class="space-y-1.5">
                <div class="flex items-center justify-between gap-3 text-xs">
                  <span class="truncate font-medium">{{ sourceLabel(source.source_name, source.source_id) }}</span>
                  <span class="shrink-0 text-muted-foreground tabular-nums">{{ source.query_count.toLocaleString() }}</span>
                </div>
                <div class="h-1.5 overflow-hidden rounded-full bg-muted">
                  <div class="h-full rounded-full bg-sky-500/75 dark:bg-sky-400/70" :style="{ width: topSourceWidth(source.query_count) }" />
                </div>
                <p class="text-[10px] text-muted-foreground">{{ duration(source.avg_duration_ms) }} average</p>
              </div>
            </div>
            <p v-else class="mt-8 text-center text-sm text-muted-foreground">No usage in this window.</p>
          </div>
        </div>
      </template>
    </section>

    <section
      v-if="isDemo"
      class="flex items-start gap-3 rounded-lg border border-sky-500/25 bg-sky-500/[0.05] p-4"
    >
      <div class="rounded-md bg-sky-500/10 p-2 text-sky-600 dark:text-sky-300">
        <ShieldCheck class="h-4 w-4" />
      </div>
      <div>
        <h2 class="text-sm font-semibold">Aggregate-only on the public demo</h2>
        <p class="mt-1 max-w-3xl text-sm leading-relaxed text-muted-foreground">
          Volume and latency counters update normally, but individual query text is never retained.
          This keeps the shared demo useful without exposing one visitor's searches to another.
        </p>
      </div>
    </section>

    <template v-else>
      <section class="space-y-4">
        <div>
          <h2 class="text-base font-semibold">Recent query sample</h2>
          <p class="mt-1 text-sm text-muted-foreground">
            A bounded operational feed for debugging recent usage. Durable totals remain in the rollup above.
          </p>
        </div>

        <LoadingState v-if="isLoading" label="Loading recent query activity…" />

        <div
          v-else-if="error"
          class="rounded-md border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
        >
          {{ error }}
        </div>

        <template v-else-if="activity">
          <div class="flex flex-wrap items-center gap-3 rounded-lg border bg-card px-5 py-4 shadow-sm">
            <div>
              <span class="text-2xl font-semibold tabular-nums">{{ total.toLocaleString() }}</span>
              <span class="ml-2 text-sm text-muted-foreground">retained records in the recent window</span>
            </div>
            <div class="ml-auto flex flex-wrap gap-2">
              <Badge v-for="language in byLanguage" :key="language.language" variant="secondary" class="font-normal">
                {{ languageLabel(language.language) }}
                <span class="ml-1 tabular-nums text-muted-foreground">{{ language.count }}</span>
              </Badge>
            </div>
          </div>

          <div class="overflow-hidden rounded-lg border bg-card shadow-sm">
            <div class="border-b px-5 py-4">
              <h3 class="text-sm font-semibold">Slowest recent queries</h3>
              <p class="mt-1 text-xs text-muted-foreground">Highest execution time within the retained sample.</p>
            </div>
            <div class="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Query</TableHead>
                    <TableHead>Source</TableHead>
                    <TableHead>User</TableHead>
                    <TableHead class="text-right">Duration</TableHead>
                    <TableHead class="text-right">Time</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-if="!slowest.length">
                    <TableCell colspan="5" class="py-10 text-center text-sm text-muted-foreground">No recent queries.</TableCell>
                  </TableRow>
                  <TableRow v-for="query in slowest" :key="query.id">
                    <TableCell class="max-w-lg">
                      <code class="block truncate text-xs" :class="!query.query_text?.trim() && 'text-muted-foreground italic'" :title="queryLabel(query.query_text, query.query_language)">
                        {{ queryLabel(query.query_text, query.query_language) }}
                      </code>
                    </TableCell>
                    <TableCell class="text-sm">{{ sourceLabel(query.source_name, query.source_id) }}</TableCell>
                    <TableCell class="text-sm text-muted-foreground">{{ query.user_email }}</TableCell>
                    <TableCell class="text-right text-sm font-medium tabular-nums">{{ duration(query.duration_ms) }}</TableCell>
                    <TableCell class="text-right text-sm text-muted-foreground">{{ timeAgo(query.created_at) }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </div>

          <div class="overflow-hidden rounded-lg border bg-card shadow-sm">
            <div class="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
              <div>
                <h3 class="text-sm font-semibold">Recent queries</h3>
                <p class="mt-1 text-xs text-muted-foreground">
                  Showing {{ Math.min(visibleRecentCount, filteredRecent.length) }} of {{ filteredRecent.length }} loaded records.
                </p>
              </div>
              <div class="relative w-full sm:w-72">
                <Search class="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input v-model="recentSearch" class="h-8 pl-8 text-xs" placeholder="Filter query, source, user…" />
              </div>
            </div>
            <div class="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Time</TableHead>
                    <TableHead>Source</TableHead>
                    <TableHead>Language</TableHead>
                    <TableHead class="text-right">Duration</TableHead>
                    <TableHead class="text-right">Rows</TableHead>
                    <TableHead>Query</TableHead>
                    <TableHead>User</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-if="!visibleRecent.length">
                    <TableCell colspan="7" class="py-10 text-center text-sm text-muted-foreground">
                      {{ recentSearch ? "No queries match this filter." : "No recent queries." }}
                    </TableCell>
                  </TableRow>
                  <TableRow v-for="query in visibleRecent" :key="query.id">
                    <TableCell class="whitespace-nowrap text-xs text-muted-foreground">{{ timeAgo(query.created_at) }}</TableCell>
                    <TableCell class="whitespace-nowrap text-sm font-medium">{{ sourceLabel(query.source_name, query.source_id) }}</TableCell>
                    <TableCell>
                      <Badge variant="outline" class="font-normal">{{ languageLabel(query.query_language) }}</Badge>
                    </TableCell>
                    <TableCell class="text-right text-sm tabular-nums">{{ duration(query.duration_ms) }}</TableCell>
                    <TableCell class="text-right text-sm tabular-nums">{{ query.row_count.toLocaleString() }}</TableCell>
                    <TableCell class="max-w-md">
                      <code class="block truncate text-xs" :class="!query.query_text?.trim() && 'text-muted-foreground italic'" :title="queryLabel(query.query_text, query.query_language)">
                        {{ queryLabel(query.query_text, query.query_language) }}
                      </code>
                    </TableCell>
                    <TableCell class="whitespace-nowrap text-xs text-muted-foreground">{{ query.user_email }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
            <div v-if="visibleRecentCount < filteredRecent.length" class="border-t p-3 text-center">
              <Button variant="ghost" size="sm" @click="visibleRecentCount += RECENT_PAGE_SIZE">
                Show {{ Math.min(RECENT_PAGE_SIZE, filteredRecent.length - visibleRecentCount) }} more
              </Button>
            </div>
          </div>
        </template>

        <EmptyState v-else title="No recent activity" description="No retained query activity is available yet." />
      </section>
    </template>
  </div>
</template>
