<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useLocalStorage } from "@vueuse/core";
import {
  Plus,
  LayoutGrid,
  List,
  Search,
  BarChart3,
  Hash,
  Table2,
  ListFilter,
  User,
  LockKeyhole,
  ArrowUpDown,
  Check,
} from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Toggle } from "@/components/ui/toggle";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import {
  EmptyState,
  LoadingState,
  PageHeader,
  RowActionsMenu,
  SearchInput,
  SortHeader,
} from "@/components/layout";
import { useDashboardsStore } from "@/stores/dashboards";
import { useMetaStore } from "@/stores/meta";
import { useAuthStore } from "@/stores/auth";
import { useTeamsStore } from "@/stores/teams";
import { useTableSearchSort, type SortDir } from "@/composables/useTableSearchSort";
import { copyableDashboardPanels } from "@/utils/dashboardPanels";
import type { Dashboard } from "@/api/dashboards";

const { t, locale } = useI18n();

const router = useRouter();
const store = useDashboardsStore();
const metaStore = useMetaStore();
const authStore = useAuthStore();
const teamsStore = useTeamsStore();

const isLoading = computed(() => store.isLoadingOperation("fetchDashboards"));
const dashboards = computed(() => store.dashboards);
const demoReadOnly = computed(() => metaStore.demoReadOnly);

function ownerName(d: Dashboard): string {
  return d.created_by_name || d.created_by_email || t("common.unknown");
}

const { search, rows: filteredDashboards, sortKey, sortDir, toggleSort } = useTableSearchSort(dashboards, {
  searchKeys: ["name", "description"],
  sortAccessors: {
    name: (d) => d.name.toLowerCase(),
    panels: (d) => panelCount(d),
    owner: (d) => ownerName(d).toLowerCase(),
    updated: (d) => new Date(d.updated_at),
    created: (d) => new Date(d.created_at),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "dashboards",
});

// Grid view sorts from a menu; list view sorts from the column headers. Both
// drive the same sort state, so switching views keeps the order.
const SORT_OPTIONS: { key: string; dir: SortDir; label: () => string }[] = [
  { key: "name", dir: "asc", label: () => t("dashboards.sortNameAsc") },
  { key: "name", dir: "desc", label: () => t("dashboards.sortNameDesc") },
  { key: "updated", dir: "desc", label: () => t("dashboards.sortRecentlyUpdated") },
  { key: "created", dir: "desc", label: () => t("dashboards.sortRecentlyCreated") },
];
const activeSort = computed(() => SORT_OPTIONS.find((o) => o.key === sortKey.value && o.dir === sortDir.value));
function selectSort(opt: (typeof SORT_OPTIONS)[number]) {
  sortKey.value = opt.key;
  sortDir.value = opt.dir;
}

type ViewMode = "grid" | "list";
const storedViewMode = useLocalStorage<string>("logchef_list_view_dashboards", "grid");
const viewMode = computed<ViewMode>({
  get: () => (storedViewMode.value === "list" ? "list" : "grid"),
  set: (mode) => {
    storedViewMode.value = mode;
  },
});

onMounted(() => {
  void store.fetchDashboards();
});

function openDashboard(id: number) {
  router.push(`/dashboards/${id}`);
}

function panelCount(d: Dashboard): number {
  return d.panels?.panels?.length ?? 0;
}

// Distinct panel types on a dashboard, for the small type-icon row on each
// card — a cheap stand-in for a real thumbnail that still hints at content.
const TYPE_ICONS = { timeseries: BarChart3, stat: Hash, breakdown: ListFilter, table: Table2 } as const;
function panelTypeIcons(d: Dashboard) {
  const types = new Set((d.panels?.panels ?? []).map((p) => p.type));
  return Array.from(types)
    .filter((t): t is keyof typeof TYPE_ICONS => t in TYPE_ICONS)
    .map((t) => TYPE_ICONS[t]);
}

function updatedLabel(d: Dashboard): string {
  const date = new Date(d.updated_at);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleDateString(locale.value);
}

// --- Create / duplicate dialog -----------------------------------------------
// Duplicate opens the create dialog prefilled from the source dashboard, the
// same flow alerts and sources use. The copy belongs to the caller.
const createOpen = ref(false);
const newName = ref("");
const newDescription = ref("");
const duplicateSource = ref<Dashboard | null>(null);
const isCreating = computed(() => store.isLoadingOperation("createDashboard"));
// Global admins may use any team; everyone else only the teams they belong to.
const duplicateCopy = computed(() => {
  if (!duplicateSource.value) return null;
  const isAdmin = authStore.user?.role === "admin";
  const memberTeamIds = new Set(teamsStore.userTeams.map((team) => team.id));
  return copyableDashboardPanels(duplicateSource.value.panels, (teamId) => isAdmin || memberTeamIds.has(teamId));
});

// Bumped by every open request, so a slow duplicate request cannot take over a
// dialog that the user has since opened for something else.
let openRequest = 0;

function openCreate() {
  openRequest++;
  duplicateSource.value = null;
  newName.value = "";
  newDescription.value = "";
  createOpen.value = true;
}

async function openDuplicate(d: Dashboard) {
  const request = ++openRequest;
  // Without the caller's teams, panels would be wrongly left out of the copy.
  // A failed load already shows an error toast.
  if (authStore.user?.role !== "admin") {
    const teams = await teamsStore.loadUserTeams();
    if (!teams.success || request !== openRequest) return;
  }
  duplicateSource.value = d;
  newName.value = t("common.copyName", { name: d.name });
  newDescription.value = d.description;
  createOpen.value = true;
}

async function submitCreate() {
  const name = newName.value.trim();
  if (!name) return;
  const result = await store.createDashboard({
    name,
    description: newDescription.value.trim(),
    // A duplicate carries the source's panels; a new dashboard starts empty.
    panels: duplicateCopy.value?.panels ?? { version: 1, layout: [], panels: [] },
  });
  if (result.success && result.data) {
    createOpen.value = false;
    router.push(`/dashboards/${result.data.id}`);
  }
}

// --- Delete -----------------------------------------------------------------
// The target outlives the dialog's close animation, so its text stays put.
const deleteTarget = ref<Dashboard | null>(null);
const deleteOpen = ref(false);

function confirmDelete(d: Dashboard) {
  deleteTarget.value = d;
  deleteOpen.value = true;
}

async function doDelete() {
  if (!deleteTarget.value) return;
  await store.deleteDashboard(deleteTarget.value.id);
}

// Row actions are hidden in the read-only demo. Anyone who can see a dashboard
// may duplicate it, unless its stored panels are unreadable; only editors may
// delete it.
function duplicateAction(d: Dashboard) {
  return demoReadOnly.value || d.panels_corrupt ? undefined : () => void openDuplicate(d);
}
function deleteAction(d: Dashboard) {
  return d.can_edit && !demoReadOnly.value ? () => confirmDelete(d) : undefined;
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('ui.dashboards')">
      <template #actions>
        <Button v-if="!demoReadOnly" size="sm" class="gap-1.5" @click="openCreate">
          <Plus class="h-4 w-4" />
          {{ t('ui.newDashboard') }}
        </Button>
        <Button v-else size="sm" variant="outline" class="gap-1.5" disabled>
          <LockKeyhole class="h-4 w-4" />
          {{ t('ui.readOnlyDemo') }}
        </Button>
      </template>
    </PageHeader>

    <div v-if="dashboards.length > 0" class="flex flex-wrap items-center gap-2">
      <SearchInput v-model="search" :placeholder="t('ui.searchDashboards')" class="w-64" />
      <div class="ml-auto flex items-center gap-2">
        <DropdownMenu v-if="viewMode === 'grid'">
          <DropdownMenuTrigger as-child>
            <Button variant="outline" size="sm" class="h-9 gap-1.5" :title="t('dashboards.sortBy')">
              <ArrowUpDown class="h-3.5 w-3.5" />
              {{ activeSort?.label() ?? t('dashboards.sortBy') }}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" class="w-48">
            <DropdownMenuItem
              v-for="opt in SORT_OPTIONS"
              :key="`${opt.key}-${opt.dir}`"
              class="text-xs"
              @click="selectSort(opt)"
            >
              <Check class="mr-2 h-3.5 w-3.5" :class="opt === activeSort ? 'opacity-100' : 'opacity-0'" />
              {{ opt.label() }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <div class="flex items-center rounded-md border p-0.5">
          <Toggle
            size="sm"
            class="h-7 w-7 p-0"
            :model-value="viewMode === 'grid'"
            :aria-label="t('ui.gridView')"
            :title="t('ui.gridView')"
            @update:model-value="viewMode = 'grid'"
          >
            <LayoutGrid class="h-3.5 w-3.5" />
          </Toggle>
          <Toggle
            size="sm"
            class="h-7 w-7 p-0"
            :model-value="viewMode === 'list'"
            :aria-label="t('ui.listView')"
            :title="t('ui.listView')"
            @update:model-value="viewMode = 'list'"
          >
            <List class="h-3.5 w-3.5" />
          </Toggle>
        </div>
      </div>
    </div>

    <LoadingState v-if="isLoading && dashboards.length === 0" />

    <EmptyState
      v-else-if="dashboards.length === 0"
      class="rounded-lg border border-dashed py-14"
      :icon="LayoutGrid"
      :title="t('ui.noDashboardsYet')"
      :description="t('ui.buildADashboardToGroupSavedQueriesIntoASharedViewErrors')"
    >
      <template v-if="!demoReadOnly" #action>
        <Button size="sm" class="gap-1.5" @click="openCreate">
          <Plus class="h-4 w-4" />
          {{ t('ui.createYourFirstDashboard') }}
        </Button>
      </template>
    </EmptyState>

    <EmptyState
      v-else-if="filteredDashboards.length === 0"
      class="rounded-lg border border-dashed py-14"
      :icon="Search"
      :title="t('dashboards.noMatches', { search })"
      :description="t('ui.tryADifferentNameOrClearTheSearch')"
    />

    <!-- Grid -->
    <div v-else-if="viewMode === 'grid'" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="d in filteredDashboards"
        :key="d.id"
        class="group relative flex cursor-pointer flex-col rounded-lg border bg-card p-4 transition-all hover:-translate-y-0.5 hover:border-primary/50 hover:shadow-md"
        @click="openDashboard(d.id)"
      >
        <div class="flex items-start justify-between gap-2">
          <h2 class="font-medium leading-tight truncate">{{ d.name }}</h2>
          <span
            v-if="duplicateAction(d) || deleteAction(d)"
            class="-my-1 -mr-2 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100 has-[[data-state=open]]:opacity-100"
          >
            <RowActionsMenu :on-duplicate="duplicateAction(d)" :on-delete="deleteAction(d)" />
          </span>
        </div>

        <p class="mt-1 line-clamp-2 min-h-[2.5rem] text-sm text-muted-foreground">
          {{ d.description || t('pages.dashboardNoDescription') }}
        </p>

        <div class="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
          <span class="rounded bg-muted px-1.5 py-0.5 font-medium">
            {{ t('dashboards.panelCount', { count: panelCount(d) }, panelCount(d)) }}
          </span>
          <span v-if="panelTypeIcons(d).length" class="flex items-center gap-1 text-muted-foreground/70">
            <component :is="icon" v-for="(icon, i) in panelTypeIcons(d)" :key="i" class="h-3 w-3" />
          </span>
          <span class="ml-auto flex items-center gap-1 truncate">
            <User class="h-3 w-3 shrink-0" />
            <span class="truncate">{{ ownerName(d) }}</span>
          </span>
        </div>
        <p v-if="updatedLabel(d)" class="mt-1 text-[11px] text-muted-foreground/70">
          {{ t('ui.updated2') }} {{ updatedLabel(d) }}
        </p>
      </div>
    </div>

    <!-- List -->
    <div v-else class="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>
              <SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.name') }}</SortHeader>
            </TableHead>
            <TableHead class="w-40">
              <SortHeader column="panels" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('dashboards.panels') }}</SortHeader>
            </TableHead>
            <TableHead class="w-48">
              <SortHeader column="owner" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.createdBy') }}</SortHeader>
            </TableHead>
            <TableHead class="w-32">
              <SortHeader column="updated" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.updated2') }}</SortHeader>
            </TableHead>
            <TableHead class="w-16 text-right">{{ t('ui.actions') }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="d in filteredDashboards" :key="d.id">
            <TableCell class="max-w-0 py-3">
              <RouterLink :to="`/dashboards/${d.id}`" class="block truncate font-medium hover:underline">
                {{ d.name }}
              </RouterLink>
              <p v-if="d.description" class="truncate text-xs text-muted-foreground">{{ d.description }}</p>
            </TableCell>
            <TableCell class="py-3 text-sm text-muted-foreground">
              <span class="flex items-center gap-2">
                {{ t('dashboards.panelCount', { count: panelCount(d) }, panelCount(d)) }}
                <span v-if="panelTypeIcons(d).length" class="flex items-center gap-1 text-muted-foreground/70">
                  <component :is="icon" v-for="(icon, i) in panelTypeIcons(d)" :key="i" class="h-3 w-3" />
                </span>
              </span>
            </TableCell>
            <TableCell class="max-w-0 truncate py-3 text-sm text-muted-foreground">{{ ownerName(d) }}</TableCell>
            <TableCell class="py-3 text-sm text-muted-foreground">{{ updatedLabel(d) }}</TableCell>
            <TableCell class="py-3 text-right">
              <RowActionsMenu
                v-if="duplicateAction(d) || deleteAction(d)"
                :on-duplicate="duplicateAction(d)"
                :on-delete="deleteAction(d)"
              />
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>

    <!-- Create / duplicate dialog -->
    <Dialog v-model:open="createOpen">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{{ duplicateSource ? t('dashboards.duplicateTitle') : t('ui.newDashboard') }}</DialogTitle>
          <DialogDescription>
            {{
              duplicateSource
                ? t('dashboards.duplicateDescription')
                : t('ui.giveYourDashboardANameAddPanelsAfterCreatingIt')
            }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-3 py-1">
          <div class="space-y-1.5">
            <Label for="dash-name">{{ t('ui.name') }}</Label>
            <Input
              id="dash-name"
              v-model="newName"
              :placeholder="t('ui.eGHTTPErrorOverview')"
              autofocus
              @keydown.enter.prevent="submitCreate"
            />
          </div>
          <div class="space-y-1.5">
            <Label for="dash-desc">{{ t('ui.description') }}</Label>
            <Textarea
              id="dash-desc"
              v-model="newDescription"
              :placeholder="t('ui.optional')"
              rows="2"
            />
          </div>
          <p
            v-if="duplicateCopy && duplicateCopy.skipped > 0"
            class="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs"
          >
            <LockKeyhole class="mt-0.5 h-3.5 w-3.5 shrink-0" />
            {{ t('dashboards.duplicateSkipsLockedPanels', { count: duplicateCopy.skipped }, duplicateCopy.skipped) }}
          </p>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="createOpen = false">{{ t('ui.cancel') }}</Button>
          <Button :disabled="!newName.trim() || isCreating" @click="submitCreate">
            {{ isCreating ? t('ui.creating') : t('ui.create') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Delete confirm -->
    <ConfirmDialog
      v-model:open="deleteOpen"
      :title="t('ui.deleteDashboard')"
      :description="t('pages.deleteDashboardConfirmation', { name: deleteTarget?.name ?? '' })"
      :confirm-text="t('ui.delete')"
      destructive
      @confirm="doDelete"
    />
  </div>
</template>
