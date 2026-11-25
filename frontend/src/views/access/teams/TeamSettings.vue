<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useToast } from "@/composables/useToast";
import { useTeamPermissions } from "@/composables/useTeamPermissions";
import { usePendingConfirm } from "@/composables/useTeamRoles";
import { type Source } from "@/api/sources";
import { Database, Info, Loader2, Plus, Search, Trash2, Users } from "lucide-vue-next";
import {
  EmptyState,
  LoadingState,
  PageHeader,
  SearchInput,
  SortHeader,
} from "@/components/layout";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import { formatDate, formatSourceName } from "@/utils/format";
import AddMembersDialog from "./AddMembersDialog.vue";
import TeamMembersTab from "./TeamMembersTab.vue";

const route = useRoute();
const router = useRouter();
const { toast } = useToast();
const { t } = useI18n();

const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();
const { isGlobalAdmin, isTeamAdmin } = useTeamPermissions();

const teamId = computed(() => Number(route.params.id));

const isLoading = ref(true);

const team = computed(() => teamsStore.getTeamById(teamId.value));
const isTeamManaged = computed(() => Boolean(team.value && "managed" in team.value && team.value.managed));
const members = computed(() => teamsStore.getTeamMembersByTeamId(teamId.value) || []);
const teamSources = computed(() => teamsStore.getTeamSourcesByTeamId(teamId.value) || []);
const canManageTeam = computed(() => isTeamAdmin(teamId.value));

const teamHeaderSummary = computed(() => {
  if (!team.value) return "";
  return t("access.teamHeaderSummary", {
    admin: t("ui.admin"),
    teams: t("routes.teams"),
    name: team.value.name,
    members: t("access.memberCount", { count: members.value.length }, members.value.length),
    sources: t("access.sourceCount", { count: teamSources.value.length }, teamSources.value.length),
  });
});

const isSavingSettings = computed(() => teamsStore.isLoadingOperation(`updateTeam-${teamId.value}`));
const isAddingSource = computed(() =>
  teamsStore.isLoadingOperation(`addTeamSource-${teamId.value}-${selectedSourceId.value}`),
);

function isRemovingSource(sourceId: number): boolean {
  return teamsStore.isLoadingOperation(`removeTeamSource-${teamId.value}-${sourceId}`);
}

// Form state
const name = ref("");
const description = ref("");

const activeTab = ref("members");

// Add Source dialog state
const showAddSourceDialog = ref(false);
const selectedSourceId = ref("");

watch(
  () => team.value,
  (newTeam) => {
    if (newTeam) {
      name.value = newTeam.name || "";
      description.value = newTeam.description || "";
    }
  },
  { immediate: true },
);

const availableSources = computed(() => {
  const teamSourceIds = teamSources.value.map((s: Source) => s.id);
  const sources = sourcesStore.getSourcesNotInTeam(teamSourceIds);
  return sources.sort((a, b) => a.name.localeCompare(b.name));
});

const sourceItems = computed(() =>
  availableSources.value.map((s) => ({ value: String(s.id), label: formatSourceName(s) })),
);

const {
  search: sourceSearch,
  rows: sortedTeamSources,
  sortKey: sourceSortKey,
  sortDir: sourceSortDir,
  toggleSort: toggleSourceSort,
} = useTableSearchSort(teamSources, {
  searchKeys: [(s) => formatSourceName(s), (s) => s.description],
  sortAccessors: {
    name: (s) => formatSourceName(s).toLowerCase(),
    added: (s) => new Date(s.created_at),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "team_sources",
});

const {
  pending: confirmPending,
  open: confirmOpen,
  request: requestConfirm,
  confirm: handleConfirm,
  cancel: handleCancelConfirm,
  dismiss: dismissConfirm,
} = usePendingConfirm();

// Pins teamId so switching routes while the dialog is open can never act on
// the new team.
function askRemoveSource(source: Source) {
  if (!team.value) return;
  const pinnedTeamId = team.value.id;
  requestConfirm({
    title: t("access.removeSourceTitle"),
    description: t("access.removeSourceConfirmation", {
      name: formatSourceName(source),
      team: team.value.name,
    }),
    confirmText: t("access.removeSource"),
    destructive: true,
    onConfirm: async () => {
      activeTab.value = "sources";
      await teamsStore.removeTeamSource(pinnedTeamId, Number(source.id));
    },
  });
}

watch(teamId, dismissConfirm);

watch(showAddSourceDialog, async (isOpen) => {
  if (isOpen && !sourcesStore.sources.length) {
    await sourcesStore.loadSources();
  }
});

const handleSubmit = async () => {
  if (!team.value || isTeamManaged.value || !canManageTeam.value) return;

  if (!name.value) {
    toast({
      title: t("ui.error"),
      description: t("access.teamNameRequired"),
      variant: "destructive",
    });
    return;
  }

  await teamsStore.updateTeam(team.value.id, {
    name: name.value,
    description: description.value || "",
  });
};

const handleAddSource = async () => {
  if (!team.value || !selectedSourceId.value) return;

  activeTab.value = "sources";

  const result = await teamsStore.addTeamSource(team.value.id, Number(selectedSourceId.value));

  if (result.success) {
    selectedSourceId.value = "";
    showAddSourceDialog.value = false;
  }
};

watch(
  teamId,
  async (id, _previousId, onCleanup) => {
    const teamRequests = new AbortController();
    onCleanup(() => teamRequests.abort());

    if (isNaN(id) || id <= 0) {
      toast({
        title: t("ui.error"),
        description: t("access.invalidTeamID", { id: route.params.id }),
        variant: "destructive",
      });
      isLoading.value = false;
      return;
    }

    try {
      isLoading.value = true;

      if (isGlobalAdmin.value) {
        await teamsStore.loadAdminTeams();
      } else {
        await teamsStore.loadUserTeams();
      }

      if (teamRequests.signal.aborted) return;
      await teamsStore.getTeam(id, teamRequests.signal);

      if (teamRequests.signal.aborted) return;
      await Promise.all([
        teamsStore.listTeamMembers(id, teamRequests.signal),
        teamsStore.listTeamSources(id, teamRequests.signal),
      ]);
    } catch (error) {
      console.error("Error loading team settings:", error);
      toast({
        title: t("ui.error"),
        description: t("access.loadTeamFailed"),
        variant: "destructive",
      });
    } finally {
      if (!teamRequests.signal.aborted) isLoading.value = false;
    }
  },
  { immediate: true },
);
</script>

<template>
  <div class="space-y-6">
    <LoadingState v-if="isLoading" :label="t('access.loadingTeamSettings')" />
    <EmptyState
      v-else-if="!team"
      :icon="Users"
      :title="t('access.teamNotFound')"
      :description="t('access.teamNotFoundDescription')"
    >
      <template #action>
        <Button variant="outline" size="sm" @click="router.push({ name: 'Teams' })">
          {{ t("access.backToTeams") }}
        </Button>
      </template>
    </EmptyState>

    <template v-else>
      <PageHeader :title="team.name" :description="teamHeaderSummary">
        <template v-if="isTeamManaged" #badge>
          <Badge
            variant="outline"
            class="text-xs font-normal"
            :title="t('access.provisionedFromConfig')"
          >
            {{ t("access.managed") }}
          </Badge>
        </template>
        <template v-if="activeTab === 'members' && canManageTeam" #actions>
          <AddMembersDialog
            :team-id="team.id"
            :team-name="team.name"
            :managed="isTeamManaged"
            :members="members"
          />
        </template>
      </PageHeader>

      <!-- Tabs -->
      <Tabs v-model="activeTab" class="space-y-6">
        <TabsList>
          <TabsTrigger value="members">{{ t("access.members") }}</TabsTrigger>
          <TabsTrigger value="sources">{{ t("ui.sources") }}</TabsTrigger>
          <TabsTrigger value="settings">{{ t("access.settings") }}</TabsTrigger>
        </TabsList>

        <!-- Members Tab -->
        <TabsContent value="members" class="space-y-4">
          <TeamMembersTab
            :team-id="team.id"
            :team-name="team.name"
            :managed="isTeamManaged"
            :members="members"
            :can-manage="canManageTeam"
          />
        </TabsContent>

        <!-- Sources Tab -->
        <TabsContent value="sources">
          <Card>
            <CardHeader>
              <div class="flex items-center justify-between">
                <div>
                  <CardTitle>{{ t("access.teamSources") }}</CardTitle>
                  <CardDescription>
                    {{ t("access.manageTeamSourcesDescription") }}
                  </CardDescription>
                </div>
                <Dialog v-if="canManageTeam" v-model:open="showAddSourceDialog">
                  <DialogTrigger asChild>
                    <Button @click="activeTab = 'sources'">
                      <Database class="mr-2 h-4 w-4" />
                      {{ t("sources.addSource") }}
                    </Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>{{ t("access.addDataSource") }}</DialogTitle>
                      <DialogDescription>
                        {{ t("access.addDataSourceDescription") }}
                      </DialogDescription>
                    </DialogHeader>
                    <div class="space-y-4 py-4">
                      <div class="space-y-2">
                        <Label>{{ t("ui.source") }}</Label>
                        <SearchableSelect
                          v-model="selectedSourceId"
                          :items="sourceItems"
                          :placeholder="t('sources.selectSource')"
                          :search-placeholder="t('access.searchSources')"
                          :empty-text="t('access.noSourcesAvailable')"
                        />
                      </div>
                    </div>
                    <DialogFooter>
                      <Button variant="outline" @click="showAddSourceDialog = false">
                        {{ t("ui.cancel") }}
                      </Button>
                      <Button :disabled="isAddingSource || !selectedSourceId" @click="handleAddSource">
                        <Loader2 v-if="isAddingSource" class="mr-2 h-4 w-4 animate-spin" />
                        <Plus v-else class="mr-2 h-4 w-4" />
                        {{ t("sources.addSource") }}
                      </Button>
                    </DialogFooter>
                  </DialogContent>
                </Dialog>
              </div>
            </CardHeader>
            <CardContent>
              <LoadingState
                v-if="teamsStore.isLoadingTeamSources(teamId)"
                :label="t('access.loadingSources')"
              />
              <EmptyState
                v-else-if="teamSources.length === 0"
                :icon="Database"
                :title="t('access.noSourcesFound')"
              />
              <div v-else class="space-y-3">
                <SearchInput v-model="sourceSearch" :placeholder="t('access.searchSources')" />
                <EmptyState
                  v-if="sortedTeamSources.length === 0"
                  :icon="Search"
                  :title="t('access.noResults')"
                />
                <Table v-else>
                  <TableHeader>
                    <TableRow>
                      <TableHead>
                        <SortHeader
                          column="name"
                          :sort-key="sourceSortKey"
                          :sort-dir="sourceSortDir"
                          @toggle="toggleSourceSort"
                        >
                          {{ t("ui.source") }}
                        </SortHeader>
                      </TableHead>
                      <TableHead>{{ t("ui.description") }}</TableHead>
                      <TableHead>
                        <SortHeader
                          column="added"
                          :sort-key="sourceSortKey"
                          :sort-dir="sourceSortDir"
                          @toggle="toggleSourceSort"
                        >
                          {{ t("access.added") }}
                        </SortHeader>
                      </TableHead>
                      <TableHead v-if="canManageTeam" class="text-right">{{ t("ui.actions") }}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TableRow v-for="source in sortedTeamSources" :key="source.id">
                      <TableCell>{{ formatSourceName(source) }}</TableCell>
                      <TableCell>{{ source.description }}</TableCell>
                      <TableCell>{{ formatDate(source.created_at) }}</TableCell>
                      <TableCell v-if="canManageTeam" class="text-right">
                        <Button
                          variant="ghost"
                          size="icon"
                          class="h-8 w-8 text-muted-foreground hover:text-destructive"
                          :title="t('access.removeSource')"
                          :aria-label="`${t('access.removeSource')}: ${formatSourceName(source)}`"
                          :disabled="isRemovingSource(source.id)"
                          :data-testid="`remove-source-${source.id}`"
                          @click="askRemoveSource(source)"
                        >
                          <Loader2 v-if="isRemovingSource(source.id)" class="h-4 w-4 animate-spin" />
                          <Trash2 v-else class="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <!-- Settings Tab -->
        <TabsContent value="settings">
          <Card>
            <CardHeader>
              <CardTitle>{{ t("access.settings") }}</CardTitle>
              <CardDescription>
                {{ t("access.updateTeamDescription") }}
              </CardDescription>
            </CardHeader>
            <CardContent class="space-y-4">
              <Alert v-if="isTeamManaged">
                <Info class="h-4 w-4" />
                <AlertDescription>
                  {{ t("access.managedTeamSettingsNotice") }}
                </AlertDescription>
              </Alert>

              <form class="space-y-6" @submit.prevent="handleSubmit">
                <div class="space-y-4">
                  <div class="grid gap-2">
                    <Label for="name">{{ t("access.teamName") }}</Label>
                    <Input id="name" v-model="name" required :disabled="isTeamManaged || !canManageTeam" />
                  </div>

                  <div class="grid gap-2">
                    <Label for="description">{{ t("ui.description") }}</Label>
                    <Textarea
                      id="description"
                      v-model="description"
                      :placeholder="t('access.teamDescription')"
                      :disabled="isTeamManaged || !canManageTeam"
                      rows="3"
                    />
                  </div>
                </div>

                <div v-if="canManageTeam" class="flex justify-end">
                  <Button type="submit" :disabled="isSavingSettings || isTeamManaged" data-testid="team-settings-save">
                    <Loader2 v-if="isSavingSettings" class="mr-2 h-4 w-4 animate-spin" />
                    {{ t("ui.saveChanges2") }}
                  </Button>
                </div>
              </form>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <ConfirmDialog
        v-model:open="confirmOpen"
        :title="confirmPending?.title ?? ''"
        :description="confirmPending?.description"
        :confirm-text="confirmPending?.confirmText"
        :destructive="confirmPending?.destructive"
        @confirm="handleConfirm"
        @cancel="handleCancelConfirm"
      />
    </template>
  </div>
</template>
