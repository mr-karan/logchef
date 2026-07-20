<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { AcceptableValue } from "reka-ui";
import { PageHeader, LoadingState, EmptyState } from "@/components/layout";
import { Badge } from "@/components/ui/badge";
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

// Recent-feed length requested from the server (clamped 1..500 server-side).
const RECENT_LIMIT = 100;

const isLoading = ref(true);
const error = ref<string | null>(null);
const activity = ref<QueryActivityResponse | null>(null);
const nowMs = ref(Date.now());

// --- All-time usage (#127): authoritative rollup, independent from recent ---
const DAYS_OPTIONS = ["7", "30", "90"] as const;
const statsDays = ref<string>("30");
const statsLoading = ref(true);
const statsError = ref<string | null>(null);
const stats = ref<QueryStatsResponse | null>(null);

async function loadActivity() {
  isLoading.value = true;
  error.value = null;
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

function onDaysChange(value: AcceptableValue) {
  if (typeof value !== "string") return;
  statsDays.value = value;
  loadStats();
}

const topSources = computed(() => stats.value?.top_sources ?? []);
const topUsers = computed(() => stats.value?.top_users ?? []);
const volumeByDay = computed(() => stats.value?.volume_by_day ?? []);

// Largest daily volume drives the relative height of the volume bars.
const maxDailyVolume = computed(() =>
  volumeByDay.value.reduce((max, d) => Math.max(max, d.query_count), 0)
);

function volumeBarHeight(count: number): string {
  if (maxDailyVolume.value <= 0) return "0%";
  // Floor at a sliver so non-zero days stay visible.
  return `${Math.max(2, Math.round((count / maxDailyVolume.value) * 100))}%`;
}

const total = computed(() => activity.value?.total ?? 0);
const byLanguage = computed(() => activity.value?.by_language ?? []);
const bySource = computed(() => activity.value?.by_source ?? []);
const slowest = computed(() => activity.value?.slowest ?? []);
const recent = computed(() => activity.value?.recent ?? []);

// Largest source count drives the relative width of the inline bars.
const maxSourceCount = computed(() =>
  bySource.value.reduce((max, s) => Math.max(max, s.count), 0)
);

function sourceBarWidth(count: number): string {
  if (maxSourceCount.value <= 0) return "0%";
  return `${Math.round((count / maxSourceCount.value) * 100)}%`;
}

function sourceLabel(name: string, sourceId: number): string {
  return name || `Source #${sourceId}`;
}

function timeAgo(createdAt: string): string {
  return formatHistoryTimeAgo(createdAt, nowMs.value);
}

function duration(ms: number): string {
  return formatHistoryDuration(ms);
}

onMounted(() => {
  loadActivity();
  loadStats();
});
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="Query Activity"
      description="Recent query activity across all users. This reflects the most recent query history and is not an all-time total."
    />

    <!-- ===================================================================
         All-time usage (#127) — AUTHORITATIVE analytics from the non-pruned
         daily rollup. Distinct from the "Recent activity" section further
         down, which is only the capped 200-rows/user window.
         =================================================================== -->
    <section class="space-y-4 rounded-md border border-primary/30 bg-primary/[0.03] p-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="space-y-1">
          <div class="flex items-center gap-2">
            <h2 class="text-base font-semibold">All-time usage</h2>
            <Badge variant="default" class="font-normal">Authoritative</Badge>
          </div>
          <p class="text-sm text-muted-foreground">
            Complete totals from the daily rollup — not capped like the recent
            feed below.
            <span v-if="stats" class="tabular-nums">Since {{ stats.since }}.</span>
          </p>
        </div>
        <div class="flex items-center gap-2">
          <label for="stats-days" class="text-sm text-muted-foreground">Window</label>
          <Select :model-value="statsDays" @update:model-value="onDaysChange">
            <SelectTrigger id="stats-days" class="w-[130px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="d in DAYS_OPTIONS" :key="d" :value="d">
                Last {{ d }} days
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <LoadingState v-if="statsLoading" label="Loading usage stats…" />

      <div
        v-else-if="statsError"
        class="rounded-md border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
      >
        {{ statsError }}
      </div>

      <template v-else-if="stats">
        <div class="grid gap-4 lg:grid-cols-2">
          <!-- Top sources -->
          <div class="space-y-2">
            <h3 class="text-sm font-medium">Top sources</h3>
            <div class="rounded-md border bg-background">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Source</TableHead>
                    <TableHead class="text-right">Queries</TableHead>
                    <TableHead class="text-right">Avg duration</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-if="!topSources.length">
                    <TableCell colspan="3" class="text-center text-sm text-muted-foreground">
                      No usage recorded in this window.
                    </TableCell>
                  </TableRow>
                  <TableRow v-for="src in topSources" :key="src.source_id">
                    <TableCell class="font-medium">{{ sourceLabel(src.source_name, src.source_id) }}</TableCell>
                    <TableCell class="text-right tabular-nums">{{ src.query_count }}</TableCell>
                    <TableCell class="text-right text-sm text-muted-foreground tabular-nums">{{ duration(src.avg_duration_ms) }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </div>

          <!-- Top users -->
          <div class="space-y-2">
            <h3 class="text-sm font-medium">Top users</h3>
            <div class="rounded-md border bg-background">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>User</TableHead>
                    <TableHead class="text-right">Queries</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-if="!topUsers.length">
                    <TableCell colspan="2" class="text-center text-sm text-muted-foreground">
                      No usage recorded in this window.
                    </TableCell>
                  </TableRow>
                  <TableRow v-for="u in topUsers" :key="u.user_id">
                    <TableCell class="font-medium">{{ u.user_email || `User #${u.user_id}` }}</TableCell>
                    <TableCell class="text-right tabular-nums">{{ u.query_count }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </div>
        </div>

        <!-- Volume by day -->
        <div class="space-y-2">
          <h3 class="text-sm font-medium">Volume by day</h3>
          <div class="rounded-md border bg-background p-4">
            <div v-if="!volumeByDay.length" class="py-6 text-center text-sm text-muted-foreground">
              No usage recorded in this window.
            </div>
            <div v-else class="flex h-40 items-end gap-1">
              <div
                v-for="d in volumeByDay"
                :key="d.date"
                class="flex flex-1 flex-col items-center justify-end gap-1"
                :title="`${d.date}: ${d.query_count} queries`"
              >
                <div class="flex w-full flex-1 items-end">
                  <div
                    class="w-full rounded-t bg-primary"
                    :style="{ height: volumeBarHeight(d.query_count) }"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </section>

    <h2 class="border-t pt-6 text-base font-semibold">Recent activity</h2>
    <p class="-mt-4 text-sm text-muted-foreground">
      The most recent queries only — capped at 200 rows per user, so these
      figures are not all-time totals.
    </p>

    <LoadingState v-if="isLoading" label="Loading query activity…" />

    <div
      v-else-if="error"
      class="rounded-md border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
    >
      {{ error }}
    </div>

    <template v-else-if="activity">
      <!-- Summary: total + compact by_language breakdown -->
      <div class="rounded-md border p-4 space-y-3">
        <div class="flex items-baseline gap-2">
          <span class="text-2xl font-semibold tabular-nums">{{ total }}</span>
          <span class="text-sm text-muted-foreground">queries in the recent window</span>
        </div>
        <div v-if="byLanguage.length" class="flex flex-wrap items-center gap-2">
          <Badge
            v-for="lang in byLanguage"
            :key="lang.language"
            variant="secondary"
            class="font-normal"
          >
            <span class="font-mono">{{ lang.language }}</span>
            <span class="ml-1.5 tabular-nums text-muted-foreground">{{ lang.count }}</span>
          </Badge>
        </div>
      </div>

      <!-- By source -->
      <div class="space-y-2">
        <h2 class="text-sm font-medium">By source</h2>
        <div class="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Source</TableHead>
                <TableHead class="w-1/2">Queries</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-if="!bySource.length">
                <TableCell colspan="2" class="text-center text-sm text-muted-foreground">
                  No source activity in the recent window.
                </TableCell>
              </TableRow>
              <TableRow v-for="src in bySource" :key="src.source_id">
                <TableCell class="font-medium">{{ sourceLabel(src.source_name, src.source_id) }}</TableCell>
                <TableCell>
                  <div class="flex items-center gap-2">
                    <div class="h-2 flex-1 rounded-full bg-muted">
                      <div
                        class="h-2 rounded-full bg-primary"
                        :style="{ width: sourceBarWidth(src.count) }"
                      />
                    </div>
                    <span class="w-10 shrink-0 text-right text-sm tabular-nums">{{ src.count }}</span>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </div>

      <!-- Slowest queries -->
      <div class="space-y-2">
        <h2 class="text-sm font-medium">Slowest queries</h2>
        <div class="rounded-md border">
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
                <TableCell colspan="5" class="text-center text-sm text-muted-foreground">
                  No queries in the recent window.
                </TableCell>
              </TableRow>
              <TableRow v-for="q in slowest" :key="q.id">
                <TableCell class="max-w-md">
                  <span class="block truncate font-mono text-xs" :title="q.query_text">{{ q.query_text }}</span>
                </TableCell>
                <TableCell class="text-sm">{{ sourceLabel(q.source_name, q.source_id) }}</TableCell>
                <TableCell class="text-sm text-muted-foreground">{{ q.user_email }}</TableCell>
                <TableCell class="text-right text-sm tabular-nums">{{ duration(q.duration_ms) }}</TableCell>
                <TableCell class="text-right text-sm text-muted-foreground">{{ timeAgo(q.created_at) }}</TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </div>

      <!-- Recent queries feed -->
      <div class="space-y-2">
        <h2 class="text-sm font-medium">Recent queries</h2>
        <div class="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Time</TableHead>
                <TableHead>User</TableHead>
                <TableHead>Source</TableHead>
                <TableHead>Language</TableHead>
                <TableHead class="text-right">Duration</TableHead>
                <TableHead class="text-right">Rows</TableHead>
                <TableHead>Query</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-if="!recent.length">
                <TableCell colspan="7" class="text-center text-sm text-muted-foreground">
                  No recent queries.
                </TableCell>
              </TableRow>
              <TableRow v-for="q in recent" :key="q.id">
                <TableCell class="whitespace-nowrap text-sm text-muted-foreground">{{ timeAgo(q.created_at) }}</TableCell>
                <TableCell class="whitespace-nowrap text-sm">{{ q.user_email }}</TableCell>
                <TableCell class="whitespace-nowrap text-sm">{{ sourceLabel(q.source_name, q.source_id) }}</TableCell>
                <TableCell>
                  <code class="rounded bg-muted px-1 py-0.5 text-xs">{{ q.query_language }}</code>
                </TableCell>
                <TableCell class="text-right text-sm tabular-nums">{{ duration(q.duration_ms) }}</TableCell>
                <TableCell class="text-right text-sm tabular-nums">{{ q.row_count }}</TableCell>
                <TableCell class="max-w-xs">
                  <span class="block truncate font-mono text-xs" :title="q.query_text">{{ q.query_text }}</span>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </div>
    </template>

    <EmptyState v-else title="No activity" description="No query activity is available yet." />
  </div>
</template>
