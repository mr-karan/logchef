<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, onMounted } from "vue";
import { useRouter } from "vue-router";
import { Lock, Search, Database, FileSearch, Shield, ShieldCheck } from "lucide-vue-next";
import { formatDate } from "@/utils/format";
import { PageHeader, PageSection, EmptyState, LoadingState, SortHeader, SearchInput } from "@/components/layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { useCollectionsStore } from "@/stores/collections";
import { useAuthStore } from "@/stores/auth";
import type { CollectionLinkReportRow } from "@/api/collections";
import { useTableSearchSort } from "@/composables/useTableSearchSort";

const router = useRouter();
const { t } = useI18n();
const store = useSavedQueriesStore();
const collectionsStore = useCollectionsStore();
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

// Pins in shared collections, for admins. Right after the link-kind upgrade
// these are exactly the links that lost delegation; converting one delegates
// edit on the query to the collection's owners and editors.
const linkReport = computed(() => collectionsStore.data.linkReport);
const isLoadingReport = computed(() => collectionsStore.isLoadingOperation("linkReport"));

function reportEditors(row: CollectionLinkReportRow): string {
  return row.editors.map((m) => m.full_name || m.email || String(m.user_id)).join(", ");
}

async function convertReportLink(row: CollectionLinkReportRow) {
  const result = await collectionsStore.updateItem(row.collection_id, row.saved_query_id, { kind: "managed" });
  if (result.success) await collectionsStore.fetchLinkReport();
}

onMounted(() => {
  if (isAdmin.value) {
    store.listAll();
    collectionsStore.fetchLinkReport();
  } else {
    store.list();
  }
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

    <PageSection
      v-if="isAdmin"
      :title="t('library.linkReportTitle')"
      :description="t('library.linkReportDescription')"
      flush
    >
      <LoadingState v-if="isLoadingReport" />
      <EmptyState
        v-else-if="linkReport.length === 0"
        class="py-6"
        :icon="ShieldCheck"
        :description="t('library.linkReportEmpty')"
      />
      <Table v-else class="min-w-[720px]">
        <TableHeader>
          <TableRow>
            <TableHead class="px-4">{{ t('library.linkReportCollection') }}</TableHead>
            <TableHead class="px-4">{{ t('library.linkReportQuery') }}</TableHead>
            <TableHead class="px-4">{{ t('library.linkReportCreator') }}</TableHead>
            <TableHead class="px-4">{{ t('library.linkReportAddedBy') }}</TableHead>
            <TableHead class="w-[140px] px-4">{{ t('library.linkReportAddedAt') }}</TableHead>
            <TableHead class="px-4">{{ t('library.linkReportEditors') }}</TableHead>
            <TableHead class="w-[200px] px-4" />
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="row in linkReport" :key="`${row.collection_id}-${row.saved_query_id}`">
            <TableCell class="px-4 py-3 font-medium">{{ row.collection_name }}</TableCell>
            <TableCell class="px-4 py-3">{{ row.saved_query_name }}</TableCell>
            <TableCell class="px-4 py-3 text-xs text-muted-foreground">{{ row.query_creator_email || t('common.unknown') }}</TableCell>
            <TableCell class="px-4 py-3 text-xs text-muted-foreground">{{ row.added_by_email || t('common.unknown') }}</TableCell>
            <TableCell class="px-4 py-3 text-xs text-muted-foreground tabular-nums">{{ formatDate(row.created_at) }}</TableCell>
            <TableCell class="px-4 py-3 text-xs text-muted-foreground">{{ reportEditors(row) || t('library.linkReportNoEditors') }}</TableCell>
            <TableCell class="px-4 py-3 text-right">
              <Button size="sm" variant="outline" @click="convertReportLink(row)">
                <ShieldCheck class="mr-2 h-4 w-4" />
                {{ t('library.delegateEdit') }}
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </PageSection>
  </div>
</template>
