<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { CalendarClock, History, Pause, Pencil, Play, Plus, Search, UserCheck } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import { EmptyState, LoadingState, PageHeader, RowActionsMenu, SearchInput, SortHeader } from "@/components/layout";
import ReportEditorDialog from "./ReportEditorDialog.vue";
import ReportHistoryDrawer from "./ReportHistoryDrawer.vue";
import { useReportsStore } from "@/stores/reports";
import { useAuthStore } from "@/stores/auth";
import { useMetaStore } from "@/stores/meta";
import { usePreferencesStore } from "@/stores/preferences";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import type { Report, ReportScope } from "@/api/reports";
import { describeSchedule, runStatusVariant } from "@/utils/reportSchedule";
import { epochMs, formatInZone } from "@/utils/timeWindow";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const store = useReportsStore();
const authStore = useAuthStore();
const metaStore = useMetaStore();
const preferences = usePreferencesStore();

const isAdmin = computed(() => authStore.user?.role === "admin");
const demoReadOnly = computed(() => metaStore.demoReadOnly);
const isLoading = computed(() => store.isLoadingOperation("fetchReports"));

function ownerName(r: Report): string {
  return r.owner ? r.owner.name || r.owner.email : t("reports.noOwner");
}

const { search, rows, sortKey, sortDir, toggleSort } = useTableSearchSort(() => store.reports, {
  searchKeys: ["name", (r) => r.source.name, (r) => ownerName(r)],
  sortAccessors: {
    name: (r) => r.name.toLowerCase(),
    source: (r) => r.source.name.toLowerCase(),
    lastRun: (r) => (r.last_run ? new Date(r.last_run.started_at) : new Date(0)),
    owner: (r) => ownerName(r).toLowerCase(),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "reports-list",
});

function setScope(value: string | number) {
  void store.fetchReports(value as ReportScope);
}

function when(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : formatInZone(epochMs(date), preferences.zone, { dateStyle: "medium", timeStyle: "short" }, locale.value);
}

// --- Editor -----------------------------------------------------------------
// /reports/:id opens a report (the link in every report email), and
// ?saved_query=<id> starts a new report on that saved query.
const editorOpen = ref(false);
const editing = ref<Report | null>(null);
const copyOf = ref<Report | null>(null);
const presetQuery = ref<number | null>(null);

function openNew(savedQueryId: number | null = null) {
  editing.value = null;
  copyOf.value = null;
  presetQuery.value = savedQueryId;
  editorOpen.value = true;
}

function openEdit(r: Report) {
  editing.value = r;
  copyOf.value = null;
  editorOpen.value = true;
}

function openDuplicate(r: Report) {
  editing.value = null;
  copyOf.value = r;
  editorOpen.value = true;
}

async function openFromRoute() {
  const id = Number(route.params.id);
  if (Number.isInteger(id) && id > 0) {
    const listed = store.reports.find((r) => r.id === id);
    if (listed) return openEdit(listed);
    const result = await store.getReport(id);
    if (result.success && result.data) openEdit(result.data);
    return;
  }
  const savedQuery = Number(route.query.saved_query);
  if (Number.isInteger(savedQuery) && savedQuery > 0) openNew(savedQuery);
}

// Closing the editor leaves the deep link, so a reload does not reopen it.
watch(editorOpen, (isOpen) => {
  if (!isOpen && (route.params.id || route.query.saved_query)) void router.replace({ name: "Reports" });
});

// --- History ----------------------------------------------------------------
const historyOpen = ref(false);
const historyReport = ref<Report | null>(null);
function openHistory(r: Report) {
  historyReport.value = r;
  historyOpen.value = true;
}

// --- Delete -----------------------------------------------------------------
const deleteTarget = ref<Report | null>(null);
const deleteOpen = ref(false);
function confirmDelete(r: Report) {
  deleteTarget.value = r;
  deleteOpen.value = true;
}
async function doDelete() {
  if (deleteTarget.value) await store.deleteReport(deleteTarget.value.id);
}

function claimable(r: Report): boolean {
  return !r.owner || r.pause_reason === "owner_inactive";
}

onMounted(async () => {
  await store.fetchReports(isAdmin.value ? store.scope : "mine");
  await openFromRoute();
});
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('reports.title')" :description="t('reports.description')">
      <template #actions>
        <Button v-if="!demoReadOnly" size="sm" class="gap-1.5" @click="openNew()">
          <Plus class="h-4 w-4" />{{ t("reports.newReport") }}
        </Button>
      </template>
    </PageHeader>

    <div class="flex flex-wrap items-center gap-3">
      <Tabs v-if="isAdmin" :model-value="store.scope" @update:model-value="setScope">
        <TabsList>
          <TabsTrigger value="mine">{{ t("reports.tabMine") }}</TabsTrigger>
          <TabsTrigger value="all">{{ t("reports.tabAll") }}</TabsTrigger>
        </TabsList>
      </Tabs>
      <SearchInput v-model="search" class="flex-1" :placeholder="t('reports.search')" />
    </div>

    <LoadingState v-if="isLoading && !store.reports.length" :label="t('reports.loading')" />
    <EmptyState
      v-else-if="!store.reports.length"
      :icon="CalendarClock"
      :title="t('reports.empty')"
      :description="t('reports.emptyDescription')"
    >
      <template v-if="!demoReadOnly" #action>
        <Button @click="openNew()"><Plus class="-ml-1 mr-2 h-4 w-4" />{{ t("reports.newReport") }}</Button>
      </template>
    </EmptyState>
    <EmptyState
      v-else-if="!rows.length"
      :icon="Search"
      :title="t('reports.noMatches')"
      :description="t('ui.tryADifferentNameOrClearTheSearch')"
    />
    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead><SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t("ui.name") }}</SortHeader></TableHead>
          <TableHead><SortHeader column="source" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t("reports.source") }}</SortHeader></TableHead>
          <TableHead>{{ t("reports.schedule") }}</TableHead>
          <TableHead><SortHeader column="lastRun" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t("reports.lastRun") }}</SortHeader></TableHead>
          <TableHead><SortHeader column="owner" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t("reports.owner") }}</SortHeader></TableHead>
          <TableHead class="w-32 text-right">{{ t("ui.actions") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="r in rows" :key="r.id" :data-report-id="r.id">
          <TableCell class="py-3">
            <div class="flex items-center gap-2">
              <button type="button" class="font-medium text-left hover:underline" @click="openEdit(r)">{{ r.name }}</button>
              <Badge v-if="r.state === 'paused'" variant="outline">{{ t(`reports.paused_${r.pause_reason || 'user'}`) }}</Badge>
            </div>
          </TableCell>
          <TableCell class="py-3 text-sm">{{ r.source.name }}</TableCell>
          <TableCell class="py-3 text-sm">
            <div>{{ describeSchedule(r.schedule, t, locale) }}</div>
            <div class="text-xs text-muted-foreground">{{ r.timezone }}</div>
          </TableCell>
          <TableCell class="py-3 text-sm">
            <template v-if="r.last_run">
              <Badge :variant="runStatusVariant(r.last_run.status)">{{ t(`reports.status_${r.last_run.status}`) }}</Badge>
              <div class="text-xs text-muted-foreground mt-0.5">{{ when(r.last_run.started_at) }}</div>
            </template>
            <span v-else class="text-muted-foreground">{{ t("reports.never") }}</span>
          </TableCell>
          <TableCell class="py-3 text-sm" :class="!r.owner && 'text-muted-foreground'">{{ ownerName(r) }}</TableCell>
          <TableCell class="py-3 text-right">
            <div class="inline-flex items-center gap-0.5">
              <Button variant="ghost" size="icon" class="h-8 w-8" :title="t('ui.edit')" :aria-label="t('ui.edit')" @click="openEdit(r)">
                <Pencil class="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" class="h-8 w-8" :title="t('reports.history')" :aria-label="t('reports.history')" @click="openHistory(r)">
                <History class="h-4 w-4" />
              </Button>
              <RowActionsMenu
                v-if="!demoReadOnly"
                :on-duplicate="() => openDuplicate(r)"
                :on-delete="r.can_edit ? () => confirmDelete(r) : undefined"
              >
                <DropdownMenuItem v-if="r.can_edit && r.state === 'active'" @click="store.setPaused(r.id, true)">
                  <Pause class="mr-2 h-4 w-4" />{{ t("reports.pause") }}
                </DropdownMenuItem>
                <DropdownMenuItem v-if="r.can_edit && r.state === 'paused'" @click="store.setPaused(r.id, false)">
                  <Play class="mr-2 h-4 w-4" />{{ t("reports.resume") }}
                </DropdownMenuItem>
                <DropdownMenuItem v-if="claimable(r)" @click="store.claimReport(r.id)">
                  <UserCheck class="mr-2 h-4 w-4" />{{ t("reports.claim") }}
                </DropdownMenuItem>
              </RowActionsMenu>
            </div>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <ReportEditorDialog
      v-model:open="editorOpen"
      :report="editing"
      :copy-of="copyOf"
      :saved-query-id="presetQuery"
      @history="openHistory"
    />
    <ReportHistoryDrawer v-model:open="historyOpen" :report="historyReport" />
    <ConfirmDialog
      v-model:open="deleteOpen"
      :title="t('reports.deleteTitle')"
      :description="t('reports.deleteDescription', { name: deleteTarget?.name ?? '' })"
      :confirm-text="t('ui.delete')"
      destructive
      @confirm="doDelete"
      @cancel="deleteTarget = null"
    />
  </div>
</template>
