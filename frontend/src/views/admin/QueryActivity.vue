<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
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
import { adminApi, type QueryActivityResponse } from "@/api/admin";
import { isSuccessResponse } from "@/api/types";
import { formatHistoryTimeAgo, formatHistoryDuration } from "@/lib/queryHistory";

// Recent-feed length requested from the server (clamped 1..500 server-side).
const RECENT_LIMIT = 100;

const isLoading = ref(true);
const error = ref<string | null>(null);
const activity = ref<QueryActivityResponse | null>(null);
const nowMs = ref(Date.now());

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
});
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="Query Activity"
      description="Recent query activity across all users. This reflects the most recent query history and is not an all-time total."
    />

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
