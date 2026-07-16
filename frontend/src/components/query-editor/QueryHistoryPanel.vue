<template>
  <Sheet v-model:open="isOpen">
    <SheetTrigger asChild>
      <Button variant="outline" size="sm" class="h-7 gap-1.5">
        <History class="w-3.5 h-3.5" />
        <span class="text-xs font-medium hidden sm:inline">History</span>
      </Button>
    </SheetTrigger>
    <SheetContent side="right" class="w-[480px] max-w-[92vw] flex flex-col p-0">
      <SheetHeader class="p-4 pb-3 border-b">
        <SheetTitle class="text-sm font-medium">Query History</SheetTitle>
        <SheetDescription class="text-xs">
          Your recent queries across all sources. Click one to re-run it.
        </SheetDescription>
      </SheetHeader>

      <!-- Loading -->
      <div v-if="isLoading" class="flex items-center justify-center p-10">
        <div class="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 class="w-4 h-4 animate-spin" />
          Loading history…
        </div>
      </div>

      <!-- Error -->
      <div v-else-if="error" class="flex flex-col items-center justify-center p-10 text-center">
        <p class="text-sm text-muted-foreground mb-3">{{ error }}</p>
        <Button variant="outline" size="sm" @click="loadHistory">Retry</Button>
      </div>

      <!-- Empty -->
      <div v-else-if="entries.length === 0" class="flex flex-col items-center justify-center p-10 text-center">
        <History class="w-8 h-8 text-muted-foreground mb-2" />
        <p class="text-sm text-muted-foreground mb-1">No query history yet</p>
        <p class="text-xs text-muted-foreground">Execute queries to see them appear here.</p>
      </div>

      <!-- List -->
      <ScrollArea v-else class="flex-1 min-h-0">
        <div class="p-3 space-y-2">
          <button
            v-for="entry in entries"
            :key="entry.id"
            type="button"
            class="w-full text-left rounded-lg border bg-card hover:bg-muted/50 transition-colors p-3 focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            @click="rerun(entry)"
          >
            <div class="flex items-center justify-between gap-2 mb-1.5">
              <div class="flex items-center gap-2 min-w-0">
                <Badge :variant="entry.query_language === 'logchefql' ? 'default' : 'secondary'" class="text-[10px] px-1.5 py-0 h-4 shrink-0">
                  {{ getQueryLanguageLabel(entry.query_language) }}
                </Badge>
                <span class="text-xs font-medium text-foreground/80 truncate">
                  {{ sourceLabel(entry.source_id) }}
                </span>
              </div>
              <span class="text-[11px] text-muted-foreground shrink-0">
                {{ formatHistoryTimeAgo(entry.created_at, nowMs) }}
              </span>
            </div>

            <p class="text-xs font-mono text-foreground/70 leading-snug line-clamp-2 break-all">
              {{ entry.query_text?.trim() || "(default query)" }}
            </p>

            <div class="flex items-center gap-3 mt-1.5 text-[11px] text-muted-foreground">
              <span class="inline-flex items-center gap-1">
                <Clock class="w-3 h-3" />
                {{ formatHistoryDuration(entry.duration_ms) }}
              </span>
              <span class="inline-flex items-center gap-1">
                <Rows3 class="w-3 h-3" />
                {{ entry.row_count.toLocaleString() }} {{ entry.row_count === 1 ? "row" : "rows" }}
              </span>
            </div>
          </button>
        </div>
      </ScrollArea>

      <div v-if="entries.length > 0" class="p-3 border-t text-[11px] text-muted-foreground">
        {{ entries.length }} recent {{ entries.length === 1 ? "query" : "queries" }}
      </div>
    </SheetContent>
  </Sheet>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { History, Loader2, Clock, Rows3 } from "lucide-vue-next";
import { exploreApi, type QueryHistoryRecord } from "@/api/explore";
import { isSuccessResponse } from "@/api/types";
import { getQueryLanguageLabel } from "@/lib/queryMetadata";
import {
  buildHistoryRerunQuery,
  formatHistoryTimeAgo,
  formatHistoryDuration,
} from "@/lib/queryHistory";
import { useSourcesStore } from "@/stores/sources";

const HISTORY_LIMIT = 100;

const router = useRouter();
const sourcesStore = useSourcesStore();

const isOpen = ref(false);
const isLoading = ref(false);
const error = ref<string | null>(null);
const entries = ref<QueryHistoryRecord[]>([]);
// Snapshot "now" when the list loads so relative times render consistently.
const nowMs = ref(Date.now());

async function loadHistory() {
  isLoading.value = true;
  error.value = null;
  try {
    const response = await exploreApi.getMyQueryHistory(HISTORY_LIMIT);
    if (isSuccessResponse(response)) {
      entries.value = response.data ?? [];
      nowMs.value = Date.now();
    } else {
      error.value = response.message || "Failed to load query history.";
    }
  } catch (err: any) {
    error.value = err?.message || "Failed to load query history.";
  } finally {
    isLoading.value = false;
  }
}

// Best-effort source name resolution. Cross-team entries may reference sources
// not in the currently loaded lists, so fall back to a stable "Source #id".
function sourceLabel(sourceId: number): string {
  const source =
    sourcesStore.getSourceById(sourceId) || sourcesStore.getTeamSourceById(sourceId);
  return source?.name || `Source #${sourceId}`;
}

function rerun(entry: QueryHistoryRecord) {
  isOpen.value = false;
  const query = buildHistoryRerunQuery(entry);
  router.push({ path: "/logs/explore", query }).catch((err: any) => {
    // Re-running the query that's already loaded is a duplicate navigation;
    // that's harmless — the current results already reflect it.
    if (err?.name !== "NavigationDuplicated") {
      console.error("Failed to re-run query from history:", err);
    }
  });
}

// Fetch fresh each time the panel opens so it reflects server state (and any
// queries run on other devices).
watch(isOpen, (open) => {
  if (open) {
    loadHistory();
  }
});
</script>
