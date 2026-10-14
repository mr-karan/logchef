<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, onMounted } from "vue";
import { useRouter } from "vue-router";
import { Lock, Search, Database, FileSearch, Shield } from "lucide-vue-next";
import { formatDate } from "@/utils/format";
import { PageHeader, PageSection, EmptyState, LoadingState, SortHeader, SearchInput } from "@/components/layout";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { useAuthStore } from "@/stores/auth";
import { useTableSearchSort } from "@/composables/useTableSearchSort";

const router = useRouter();
const { t } = useI18n();
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

const { search, rows: filtered, sortKey, sortDir, toggleSort } = useTableSearchSort(rows, {
  searchKeys: ["name", (r) => r.source_name],
  sortAccessors: {
    name: (r) => r.name.toLowerCase(),
    source: (r) => (r.source_name ?? "").toLowerCase(),
    updated: (r) => new Date(r.updated_at),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "all_queries",
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
    <PageHeader
      :title="t('library.allQueries')"
      :description="isAdmin ? t('library.allQueriesAdminDescription') : t('library.allQueriesUserDescription')"
    >
      <template v-if="isAdmin" #actions>
        <Badge variant="outline" class="inline-flex items-center gap-1 font-medium">
          <Shield class="h-3 w-3" /> {{ t('library.adminAllSources') }}
        </Badge>
      </template>
    </PageHeader>

    <SearchInput v-model="search" :placeholder="t('library.searchQueries')" />

    <PageSection flush>
      <LoadingState v-if="isLoading" :label="t('library.loadingQueries')" />
      <EmptyState
        v-else-if="filtered.length === 0"
        :icon="FileSearch"
        :title="search ? t('library.noMatches') : t('library.noSavedQueries')"
        :description="search ? t('library.nothingMatches', { query: search }) : t('library.saveQueryToSeeIt')"
      />
      <Table v-else class="min-w-[640px]">
        <TableHeader>
          <TableRow>
            <TableHead class="w-[40px] px-4">{{ t('library.type') }}</TableHead>
            <TableHead class="px-4">
              <SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.name') }}</SortHeader>
            </TableHead>
            <TableHead class="w-[160px] px-4">
              <SortHeader column="source" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.source') }}</SortHeader>
            </TableHead>
            <TableHead class="w-[140px] px-4">
              <SortHeader column="updated" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.updated2') }}</SortHeader>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="q in filtered"
            :key="q.id"
            :class="!runnable(q) && 'opacity-60'"
          >
            <TableCell class="px-4 py-3">
              <Lock
                v-if="!runnable(q)"
                class="h-4 w-4 text-muted-foreground"
                :title="t('library.noQuerySourceAccess')"
              />
              <Search v-else-if="q.query_language === 'logchefql'" class="h-4 w-4 text-muted-foreground" title="LogchefQL" />
              <Database v-else class="h-4 w-4 text-muted-foreground" title="SQL" />
            </TableCell>
            <TableCell class="px-4 py-3 whitespace-normal">
              <button
                type="button"
                class="font-medium text-foreground text-left hover:underline disabled:cursor-not-allowed disabled:hover:no-underline"
                :class="!runnable(q) && 'text-muted-foreground'"
                :disabled="!runnable(q)"
                :title="runnable(q) ? t('library.openInExplorer') : t('library.noSourceAccess')"
                @click="openQuery(q.id)"
              >
                {{ q.name }}
              </button>
              <p v-if="q.description" class="text-xs text-muted-foreground mt-0.5 truncate max-w-[420px]">
                {{ q.description }}
              </p>
            </TableCell>
            <TableCell class="px-4 py-3 text-muted-foreground text-xs">
              <span class="inline-block max-w-[140px] truncate align-bottom">
                {{ q.source_name || t('library.sourceNumber', { id: q.source_id }) }}
              </span>
            </TableCell>
            <TableCell class="px-4 py-3 text-muted-foreground text-xs tabular-nums">
              {{ formatDate(q.updated_at) }}
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </PageSection>
  </div>
</template>
