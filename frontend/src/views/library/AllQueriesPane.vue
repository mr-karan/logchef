<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Lock, Search, Database, FileSearch, Shield } from "lucide-vue-next";
import { formatDate } from "@/utils/format";
import { PageSection, EmptyState, LoadingState } from "@/components/layout";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { useAuthStore } from "@/stores/auth";

const router = useRouter();
const store = useSavedQueriesStore();
const authStore = useAuthStore();

// Global admins get every saved query (scope=all, rows locked where they lack
// source access); everyone else gets the queries they have source access to.
// Both surfaces exist so unpinned queries have a home again after the refactor.
const isAdmin = computed(() => authStore.user?.role === "admin");
const rows = computed(() => (isAdmin.value ? store.allQueries : store.queries));
const isLoading = computed(() =>
  store.isLoadingOperation(isAdmin.value ? "listAllSavedQueries" : "listSavedQueries"),
);

const search = ref("");
const filtered = computed(() => {
  const q = search.value.trim().toLowerCase();
  const list = rows.value ?? [];
  if (!q) return list;
  return list.filter(
    (r) => r.name.toLowerCase().includes(q) || (r.source_name ?? "").toLowerCase().includes(q),
  );
});

// runnable is only set on the admin scope=all list; absent means the list was
// already source-access-gated, so treat absent as runnable.
function runnable(q: { runnable?: boolean }): boolean {
  return q.runnable !== false;
}

function openQuery(id: number) {
  router.push(`/logs/saved/${id}`);
}

onMounted(() => {
  if (isAdmin.value) store.listAll();
  else store.list();
});
</script>

<template>
  <div class="space-y-5">
    <div class="flex items-start gap-3">
      <div>
        <h2 class="text-lg font-semibold tracking-tight">All queries</h2>
        <p class="text-sm text-muted-foreground">
          Every saved query you can {{ isAdmin ? "see" : "access" }}, whether or not it's in a collection.
        </p>
      </div>
      <Badge v-if="isAdmin" variant="outline" class="ml-auto inline-flex items-center gap-1 font-medium">
        <Shield class="h-3 w-3" /> Admin · all sources
      </Badge>
    </div>

    <div class="relative max-w-sm">
      <Search class="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
      <Input v-model="search" placeholder="Search queries by name or source…" class="pl-8 h-9" />
    </div>

    <PageSection flush>
      <LoadingState v-if="isLoading" label="Loading queries…" />
      <EmptyState
        v-else-if="filtered.length === 0"
        :icon="FileSearch"
        :title="search ? 'No matches' : 'No saved queries'"
        :description="search ? `Nothing matches “${search}”.` : 'Save a query from the explorer to see it here.'"
      />
      <div v-else class="overflow-x-auto">
        <table class="w-full text-sm min-w-[640px]">
          <thead>
            <tr class="border-b bg-muted/30">
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5 w-[40px]">Type</th>
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5">Name</th>
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5 w-[160px]">Source</th>
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5 w-[140px]">Updated</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="q in filtered"
              :key="q.id"
              class="border-b last:border-0 hover:bg-muted/40 transition-colors"
              :class="!runnable(q) && 'opacity-60'"
            >
              <td class="px-4 py-3 align-middle">
                <Lock
                  v-if="!runnable(q)"
                  class="h-4 w-4 text-muted-foreground"
                  title="You don't have access to this query's source — can't run it."
                />
                <Search v-else-if="q.query_language === 'logchefql'" class="h-4 w-4 text-muted-foreground" title="LogchefQL" />
                <Database v-else class="h-4 w-4 text-muted-foreground" title="SQL" />
              </td>
              <td class="px-4 py-3 align-middle">
                <button
                  type="button"
                  class="font-medium text-foreground text-left hover:underline disabled:cursor-not-allowed disabled:hover:no-underline"
                  :class="!runnable(q) && 'text-muted-foreground'"
                  :disabled="!runnable(q)"
                  :title="runnable(q) ? 'Open in explorer' : 'No source access'"
                  @click="openQuery(q.id)"
                >
                  {{ q.name }}
                </button>
                <p v-if="q.description" class="text-xs text-muted-foreground mt-0.5 truncate max-w-[420px]">
                  {{ q.description }}
                </p>
              </td>
              <td class="px-4 py-3 align-middle text-muted-foreground text-xs">
                <span class="inline-block max-w-[140px] truncate align-bottom">
                  {{ q.source_name || `source ${q.source_id}` }}
                </span>
              </td>
              <td class="px-4 py-3 align-middle text-muted-foreground text-xs whitespace-nowrap tabular-nums">
                {{ formatDate(q.updated_at) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </PageSection>
  </div>
</template>
