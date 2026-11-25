<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useToast } from "@/composables/useToast";
import { useTeamPermissions } from "@/composables/useTeamPermissions";
import { type Source } from "@/api/sources";
import { type TeamMember } from "@/api/teams";
import {
  Bot,
  Database,
  Info,
  Loader2,
  Plus,
  Search,
  Trash2,
  User,
  User2,
  UserPlus,
  Users,
  X,
} from "lucide-vue-next";
import {
  EmptyState,
  LoadingState,
  PageHeader,
  RowActionsMenu,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import { useUsersStore } from "@/stores/users";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import { useAuthStore } from "@/stores/auth";
import { formatDate, formatSourceName } from "@/utils/format";

type TeamRole = "admin" | "editor" | "member";

const route = useRoute();
const router = useRouter();
const { toast } = useToast();
const { t } = useI18n();

const usersStore = useUsersStore();
const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();
const authStore = useAuthStore();
const { isGlobalAdmin, isTeamAdmin } = useTeamPermissions();

const teamId = computed(() => Number(route.params.id));

function translateRole(role: string) {
  switch (role) {
    case "admin":
      return t("access.roleAdmin");
    case "editor":
      return t("access.roleEditor");
    case "member":
      return t("access.roleMember");
    default:
      return role;
  }
}

const isLoading = ref(true);

const team = computed(() => teamsStore.getTeamById(teamId.value));
const isTeamManaged = computed(() => Boolean(team.value && "managed" in team.value && team.value.managed));
const members = computed(() => teamsStore.getTeamMembersByTeamId(teamId.value) || []);
const teamSources = computed(() => teamsStore.getTeamSourcesByTeamId(teamId.value) || []);
const canManageTeam = computed(() => isTeamAdmin(teamId.value));

const teamAdminCount = computed(
  () => members.value.filter((m) => m.role === "admin").length,
);

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

const isSaving = computed(() => {
  return (
    teamsStore.isLoadingOperation("updateTeam-" + teamId.value) ||
    teamsStore.isLoadingOperation("addTeamMember-" + teamId.value) ||
    teamsStore.isLoadingOperation("removeTeamMember-" + teamId.value) ||
    teamsStore.isLoadingOperation("addTeamSource-" + teamId.value) ||
    teamsStore.isLoadingOperation("removeTeamSource-" + teamId.value)
  );
});

function isMemberRoleBusy(userId: number): boolean {
  return (
    teamsStore.isLoadingOperation(`updateTeamMemberRole-${teamId.value}-${userId}`) ||
    teamsStore.isLoadingOperation(`removeTeamMember-${teamId.value}-${userId}`)
  );
}

// Form state
const name = ref("");
const description = ref("");

// UI state
const activeTab = ref("members");
const roleFilter = ref<"all" | TeamRole>("all");
const selectResetKey = ref(0);

// Multi-select Add Members dialog state
const showAddMemberDialog = ref(false);
const newMemberRole = ref<TeamRole>("member");
const newMemberType = ref<"human" | "service">("human");
const memberPickerSearch = ref("");
const selectedUserIds = ref<number[]>([]);

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

const allAvailableUsers = computed(() => {
  const teamMemberIds = members.value.map((m) => String(m.user_id));
  return usersStore
    .getUsersNotInTeam(teamMemberIds)
    .sort((a, b) => (a.full_name || a.email).localeCompare(b.full_name || b.email));
});

const availableUsersByType = computed(() => {
  const wantedType = newMemberType.value;
  return allAvailableUsers.value.filter((user) => (user.account_type ?? "human") === wantedType);
});

const filteredPickerUsers = computed(() => {
  const q = memberPickerSearch.value.trim().toLowerCase();
  if (!q) return availableUsersByType.value;
  return availableUsersByType.value.filter(
    (u) =>
      (u.full_name || "").toLowerCase().includes(q) ||
      (u.email || "").toLowerCase().includes(q),
  );
});

const selectedUsersList = computed(() => {
  const selected = new Set(selectedUserIds.value);
  return allAvailableUsers.value.filter((u) => selected.has(Number(u.id)));
});

function isUserSelected(userId: string | number): boolean {
  return selectedUserIds.value.includes(Number(userId));
}

function toggleUserSelection(userId: string | number) {
  const id = Number(userId);
  if (selectedUserIds.value.includes(id)) {
    selectedUserIds.value = selectedUserIds.value.filter((existing) => existing !== id);
  } else {
    selectedUserIds.value = [...selectedUserIds.value, id];
  }
}

function clearSelectedUsers() {
  selectedUserIds.value = [];
}

const availableSources = computed(() => {
  const teamSourceIds = teamSources.value.map((s: Source) => s.id);
  const sources = sourcesStore.getSourcesNotInTeam(teamSourceIds);
  return sources.sort((a, b) => a.name.localeCompare(b.name));
});

const sourceItems = computed(() =>
  availableSources.value.map((s) => ({ value: String(s.id), label: formatSourceName(s) })),
);

const roleFilteredMembers = computed(() => {
  if (roleFilter.value === "all") return members.value;
  return members.value.filter((m) => m.role === roleFilter.value);
});

const {
  search: memberSearch,
  rows: sortedMembers,
  sortKey: memberSortKey,
  sortDir: memberSortDir,
  toggleSort: toggleMemberSort,
} = useTableSearchSort(roleFilteredMembers, {
  searchKeys: ["email", (m) => m.full_name, (m) => m.role],
  sortAccessors: {
    name: (m) => (m.full_name || m.email || "").toLowerCase(),
    role: (m) => m.role || "",
    added: (m) => new Date(m.created_at),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "team_members",
});

const hasMemberFilters = computed(
  () => memberSearch.value.trim() !== "" || roleFilter.value !== "all",
);

function clearMemberFilters() {
  memberSearch.value = "";
  roleFilter.value = "all";
}

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

// Unified ConfirmDialog for removals and guarded role changes.
// Pins teamId so switching routes while open can never act on the new team.
interface PendingAction {
  title: string;
  description: string;
  confirmText: string;
  destructive?: boolean;
  onConfirm: () => Promise<void>;
  onCancel?: () => void;
}

const pendingAction = ref<PendingAction | null>(null);
const confirmOpen = ref(false);

function openConfirm(action: PendingAction) {
  pendingAction.value = action;
  confirmOpen.value = true;
}

async function handleConfirm() {
  const action = pendingAction.value;
  pendingAction.value = null;
  if (action) await action.onConfirm();
}

function handleCancelConfirm() {
  const action = pendingAction.value;
  pendingAction.value = null;
  action?.onCancel?.();
}

function isSelfMember(member: TeamMember): boolean {
  return !!authStore.user && Number(member.user_id) === Number(authStore.user.id);
}

function isLastTeamAdmin(member: TeamMember): boolean {
  return member.role === "admin" && teamAdminCount.value <= 1;
}

async function applyMemberRoleChange(pinnedTeamId: number, userId: number, nextRole: TeamRole) {
  const result = await teamsStore.updateTeamMemberRole(pinnedTeamId, userId, nextRole);
  if (!result.success) {
    selectResetKey.value++;
  }
}

function onMemberRoleChange(member: TeamMember, value: unknown) {
  if (!team.value || (value !== "admin" && value !== "editor" && value !== "member")) return;
  const nextRole: TeamRole = value;
  if (nextRole === member.role) return;

  const pinnedTeamId = team.value.id;
  const pinnedTeamName = team.value.name;
  const memberName = member.full_name || member.email;
  const demotingFromAdmin = member.role === "admin" && nextRole !== "admin";

  const warnings: string[] = [];
  if (demotingFromAdmin && isLastTeamAdmin(member)) {
    warnings.push(
      t("access.lastTeamAdminDemoteWarning", {
        name: memberName,
        team: pinnedTeamName,
      }),
    );
  }
  if (demotingFromAdmin && isSelfMember(member)) {
    warnings.push(t("access.selfTeamDemoteWarning", { team: pinnedTeamName }));
  }
  if (isTeamManaged.value) {
    warnings.push(t("access.managedTeamChangeWarning", { team: pinnedTeamName }));
  }

  if (warnings.length > 0) {
    openConfirm({
      title: t("access.changeTeamRoleTitle"),
      description: warnings.join(" "),
      confirmText: t("access.changeTeamRoleConfirm"),
      destructive: demotingFromAdmin,
      onConfirm: () => applyMemberRoleChange(pinnedTeamId, Number(member.user_id), nextRole),
      onCancel: () => {
        selectResetKey.value++;
      },
    });
    return;
  }

  void applyMemberRoleChange(pinnedTeamId, Number(member.user_id), nextRole);
}

function askRemoveMember(member: TeamMember) {
  if (!team.value) return;
  const pinnedTeamId = team.value.id;
  const pinnedTeamName = team.value.name;
  const memberName = member.full_name || member.email;
  const parts = [
    t("access.removeMemberConfirmation", { name: memberName, team: pinnedTeamName }),
  ];

  if (isLastTeamAdmin(member)) {
    parts.push(
      t("access.lastTeamAdminRemoveWarning", { name: memberName, team: pinnedTeamName }),
    );
  }
  if (isSelfMember(member) && !isGlobalAdmin.value) {
    parts.push(t("access.selfTeamRemoveWarning", { team: pinnedTeamName }));
  }
  if (isTeamManaged.value) {
    parts.push(t("access.managedTeamChangeWarning", { team: pinnedTeamName }));
  }

  openConfirm({
    title: t("access.removeMemberTitle"),
    description: parts.join(" "),
    confirmText: t("access.removeFromTeam"),
    destructive: true,
    onConfirm: async () => {
      await teamsStore.removeTeamMember(pinnedTeamId, Number(member.user_id));
    },
  });
}

function askRemoveSource(source: Source) {
  if (!team.value) return;
  const pinnedTeamId = team.value.id;
  const pinnedTeamName = team.value.name;
  openConfirm({
    title: t("access.removeSourceTitle"),
    description: t("access.removeSourceConfirmation", {
      name: formatSourceName(source),
      team: pinnedTeamName,
    }),
    confirmText: t("access.removeSource"),
    destructive: true,
    onConfirm: async () => {
      activeTab.value = "sources";
      await teamsStore.removeTeamSource(pinnedTeamId, Number(source.id));
    },
  });
}

function viewInPeople(member: TeamMember) {
  void router.push({ name: "ManageUsers", query: { user: String(member.user_id) } });
}

watch(teamId, () => {
  confirmOpen.value = false;
  pendingAction.value = null;
  roleFilter.value = "all";
});

watch(showAddMemberDialog, async (isOpen) => {
  if (isOpen) {
    newMemberType.value = "human";
    newMemberRole.value = "member";
    memberPickerSearch.value = "";
    selectedUserIds.value = [];
    if (!usersStore.users.length) {
      await usersStore.loadUsers();
    }
  }
});

watch(showAddSourceDialog, async (isOpen) => {
  if (isOpen && !sourcesStore.sources.length) {
    await sourcesStore.loadSources();
  }
});

const handleSubmit = async () => {
  if (!team.value || isTeamManaged.value) return;

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

const handleAddMembers = async () => {
  if (!team.value || selectedUserIds.value.length === 0) return;

  const result = await teamsStore.addTeamMembers(
    team.value.id,
    selectedUserIds.value,
    newMemberRole.value,
  );

  if (result.success) {
    selectedUserIds.value = [];
    newMemberRole.value = "member";
    memberPickerSearch.value = "";
    showAddMemberDialog.value = false;
  }
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
          <Dialog v-model:open="showAddMemberDialog">
            <DialogTrigger asChild>
              <Button data-testid="add-members-trigger">
                <UserPlus class="mr-2 h-4 w-4" />
                {{ t("access.addMembers") }}
              </Button>
            </DialogTrigger>
            <DialogContent class="sm:max-w-[500px]">
              <DialogHeader>
                <DialogTitle>{{ t("access.addTeamMembers") }}</DialogTitle>
                <DialogDescription>
                  {{ t("access.addTeamMembersDescription") }}
                </DialogDescription>
              </DialogHeader>

              <div class="space-y-4 py-2">
                <Alert v-if="isTeamManaged" class="py-2.5">
                  <Info class="h-4 w-4" />
                  <AlertDescription class="text-xs">
                    {{ t("access.managedTeamChangeWarning", { team: team.name }) }}
                  </AlertDescription>
                </Alert>

                <div class="space-y-2">
                  <Label>{{ t("access.accountType") }}</Label>
                  <div class="grid grid-cols-2 gap-2">
                    <Button
                      type="button"
                      size="sm"
                      :variant="newMemberType === 'human' ? 'default' : 'outline'"
                      class="justify-start gap-2"
                      @click="newMemberType = 'human'"
                    >
                      <User class="h-4 w-4" />
                      {{ t("access.humanUser") }}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      :variant="newMemberType === 'service' ? 'default' : 'outline'"
                      class="justify-start gap-2"
                      @click="newMemberType = 'service'"
                    >
                      <Bot class="h-4 w-4" />
                      {{ t("access.serviceAccount") }}
                    </Button>
                  </div>
                </div>

                <div v-if="selectedUsersList.length > 0" class="space-y-1.5">
                  <div class="flex items-center justify-between text-xs text-muted-foreground">
                    <span>{{ t("access.selectedCount", { count: selectedUsersList.length }) }}</span>
                    <button
                      type="button"
                      class="hover:text-foreground hover:underline"
                      @click="clearSelectedUsers"
                    >
                      {{ t("ui.clear") }}
                    </button>
                  </div>
                  <div class="flex flex-wrap gap-1.5">
                    <Badge
                      v-for="picked in selectedUsersList"
                      :key="picked.id"
                      variant="secondary"
                      class="gap-1 pr-1 font-normal"
                    >
                      <span>{{ picked.full_name || picked.email }}</span>
                      <button
                        type="button"
                        class="rounded-sm p-0.5 hover:bg-muted-foreground/20"
                        :aria-label="t('access.removeSelection')"
                        @click="toggleUserSelection(picked.id)"
                      >
                        <X class="h-3 w-3" />
                      </button>
                    </Badge>
                  </div>
                </div>

                <div class="space-y-2">
                  <SearchInput
                    v-model="memberPickerSearch"
                    class="max-w-none"
                    :placeholder="
                      newMemberType === 'service'
                        ? t('access.searchServiceAccounts')
                        : t('access.searchUsers')
                    "
                  />
                  <div
                    class="max-h-56 divide-y overflow-y-auto rounded-md border"
                    role="listbox"
                    aria-multiselectable="true"
                  >
                    <div
                      v-if="filteredPickerUsers.length === 0"
                      class="px-3 py-6 text-center text-sm text-muted-foreground"
                    >
                      {{
                        newMemberType === "service"
                          ? t("access.noServiceAccountsAvailable")
                          : t("access.noUsersAvailable")
                      }}
                    </div>
                    <button
                      v-for="candidate in filteredPickerUsers"
                      :key="candidate.id"
                      type="button"
                      role="option"
                      :aria-selected="isUserSelected(candidate.id)"
                      :data-testid="`add-member-option-${candidate.id}`"
                      class="flex w-full items-center gap-3 px-3 py-2 text-left text-sm transition-colors hover:bg-muted/50"
                      :class="{ 'bg-accent/40': isUserSelected(candidate.id) }"
                      @click="toggleUserSelection(candidate.id)"
                    >
                      <Checkbox
                        :model-value="isUserSelected(candidate.id)"
                        class="pointer-events-none"
                        tabindex="-1"
                      />
                      <Bot
                        v-if="candidate.account_type === 'service'"
                        class="h-4 w-4 shrink-0 text-muted-foreground"
                      />
                      <span class="flex min-w-0 flex-1 flex-col">
                        <span class="truncate font-medium">{{ candidate.full_name || candidate.email }}</span>
                        <span
                          v-if="candidate.full_name && candidate.account_type !== 'service'"
                          class="truncate text-xs text-muted-foreground"
                        >
                          {{ candidate.email }}
                        </span>
                      </span>
                    </button>
                  </div>
                </div>

                <div class="space-y-2">
                  <Label>{{ t("access.teamRole") }}</Label>
                  <Select v-model="newMemberRole">
                    <SelectTrigger data-testid="add-members-role">
                      <SelectValue :placeholder="t('access.selectRole')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="member">{{ t("access.roleMember") }}</SelectItem>
                      <SelectItem value="editor">{{ t("access.roleEditor") }}</SelectItem>
                      <SelectItem value="admin">{{ t("access.roleAdmin") }}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <DialogFooter>
                <Button variant="outline" @click="showAddMemberDialog = false">
                  {{ t("ui.cancel") }}
                </Button>
                <Button
                  :disabled="isSaving || selectedUserIds.length === 0"
                  data-testid="add-members-submit"
                  @click="handleAddMembers"
                >
                  <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
                  <Plus v-else class="mr-2 h-4 w-4" />
                  {{
                    selectedUserIds.length > 0
                      ? t("access.addMembersCount", { count: selectedUserIds.length }, selectedUserIds.length)
                      : t("access.addMembers")
                  }}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
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
          <LoadingState
            v-if="teamsStore.isLoadingTeamMembers(teamId)"
            :label="t('access.loadingMembers')"
          />
          <EmptyState
            v-else-if="members.length === 0"
            :icon="Users"
            :title="t('access.noMembersFound')"
          />
          <div v-else class="space-y-3">
            <div class="flex flex-wrap items-center gap-2">
              <SearchInput v-model="memberSearch" :placeholder="t('access.searchMembers')" />

              <Select v-model="roleFilter">
                <SelectTrigger
                  class="w-[145px]"
                  :aria-label="t('access.filterRole')"
                  data-testid="team-members-filter-role"
                >
                  <span class="truncate">
                    <span class="text-muted-foreground">{{ t("access.filterRole") }}: </span>
                    <SelectValue />
                  </span>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{{ t("access.filterAny") }}</SelectItem>
                  <SelectItem value="admin">{{ t("access.roleAdmin") }}</SelectItem>
                  <SelectItem value="editor">{{ t("access.roleEditor") }}</SelectItem>
                  <SelectItem value="member">{{ t("access.roleMember") }}</SelectItem>
                </SelectContent>
              </Select>

              <Button
                v-if="hasMemberFilters"
                type="button"
                variant="ghost"
                size="sm"
                class="h-9 text-muted-foreground"
                @click="clearMemberFilters"
              >
                <X class="mr-1 h-3.5 w-3.5" />
                {{ t("access.clearFilters") }}
              </Button>
            </div>

            <EmptyState
              v-if="sortedMembers.length === 0"
              :icon="Search"
              :title="t('access.noResults')"
              :description="t('access.noMembersMatch')"
              class="rounded-md border"
            >
              <template v-if="hasMemberFilters" #action>
                <Button variant="outline" size="sm" @click="clearMemberFilters">
                  {{ t("access.clearFilters") }}
                </Button>
              </template>
            </EmptyState>

            <div v-else class="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      <SortHeader
                        column="name"
                        :sort-key="memberSortKey"
                        :sort-dir="memberSortDir"
                        @toggle="toggleMemberSort"
                      >
                        {{ t("access.memberColumn") }}
                      </SortHeader>
                    </TableHead>
                    <TableHead class="w-[160px]">
                      <SortHeader
                        column="role"
                        :sort-key="memberSortKey"
                        :sort-dir="memberSortDir"
                        @toggle="toggleMemberSort"
                      >
                        {{ t("access.teamRole") }}
                      </SortHeader>
                    </TableHead>
                    <TableHead class="w-[180px]">
                      <SortHeader
                        column="added"
                        :sort-key="memberSortKey"
                        :sort-dir="memberSortDir"
                        @toggle="toggleMemberSort"
                      >
                        {{ t("access.added") }}
                      </SortHeader>
                    </TableHead>
                    <TableHead class="w-[60px] text-right">{{ t("ui.actions") }}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow
                    v-for="member in sortedMembers"
                    :key="member.user_id"
                    :data-testid="`team-member-row-${member.user_id}`"
                  >
                    <TableCell>
                      <div class="flex flex-col gap-0.5">
                        <div class="flex items-center gap-2">
                          <Bot
                            v-if="member.account_type === 'service'"
                            class="h-4 w-4 text-muted-foreground"
                          />
                          <span class="font-medium">{{ member.full_name || member.email }}</span>
                          <Badge
                            v-if="member.account_type === 'service'"
                            variant="secondary"
                            class="text-xs"
                          >
                            {{ t("access.serviceAccount") }}
                          </Badge>
                        </div>
                        <span
                          v-if="member.account_type !== 'service' && member.full_name"
                          class="text-xs text-muted-foreground"
                        >
                          {{ member.email }}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Select
                        v-if="canManageTeam"
                        :key="`member-role-${member.user_id}-${selectResetKey}`"
                        :model-value="member.role"
                        :disabled="isMemberRoleBusy(member.user_id)"
                        @update:model-value="(val) => onMemberRoleChange(member, val)"
                      >
                        <SelectTrigger
                          size="sm"
                          class="w-[115px]"
                          :aria-label="`${member.full_name || member.email} ${t('access.teamRole')}`"
                          :data-testid="`member-role-select-${member.user_id}`"
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="admin">{{ t("access.roleAdmin") }}</SelectItem>
                          <SelectItem value="editor">{{ t("access.roleEditor") }}</SelectItem>
                          <SelectItem value="member">{{ t("access.roleMember") }}</SelectItem>
                        </SelectContent>
                      </Select>
                      <span v-else>{{ translateRole(member.role) }}</span>
                    </TableCell>
                    <TableCell class="text-sm text-muted-foreground">
                      {{ formatDate(member.created_at) }}
                    </TableCell>
                    <TableCell class="text-right">
                      <RowActionsMenu
                        v-if="canManageTeam"
                        :delete-label="t('access.removeFromTeam')"
                        @delete="askRemoveMember(member)"
                      >
                        <DropdownMenuItem
                          v-if="isGlobalAdmin && member.account_type !== 'service'"
                          @click="viewInPeople(member)"
                        >
                          <User2 class="mr-2 h-4 w-4" />
                          {{ t("access.viewInPeople") }}
                        </DropdownMenuItem>
                      </RowActionsMenu>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>

            <!-- One-line role legend based on real permission checks -->
            <p class="text-xs text-muted-foreground" data-testid="team-role-legend">
              {{ t("access.roleLegendPrefix") }}
              <strong class="font-medium text-foreground">{{ t("access.roleAdmin") }}</strong>
              {{ t("access.roleLegendAdmin") }}
              ·
              <strong class="font-medium text-foreground">{{ t("access.roleEditor") }}</strong>
              {{ t("access.roleLegendEditor") }}
              ·
              <strong class="font-medium text-foreground">{{ t("access.roleMember") }}</strong>
              {{ t("access.roleLegendMember") }}
            </p>
          </div>
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
                <Dialog v-model:open="showAddSourceDialog">
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
                      <Button :disabled="isSaving" @click="handleAddSource">
                        <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
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
                      <TableHead class="text-right">{{ t("ui.actions") }}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TableRow v-for="source in sortedTeamSources" :key="source.id">
                      <TableCell>{{ formatSourceName(source) }}</TableCell>
                      <TableCell>{{ source.description }}</TableCell>
                      <TableCell>{{ formatDate(source.created_at) }}</TableCell>
                      <TableCell class="text-right">
                        <Button
                          variant="ghost"
                          size="icon"
                          class="h-8 w-8 text-muted-foreground hover:text-destructive"
                          :title="t('access.removeSource')"
                          :disabled="isSaving"
                          @click="askRemoveSource(source)"
                        >
                          <Loader2 v-if="isSaving" class="h-4 w-4 animate-spin" />
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
                    <Input id="name" v-model="name" required :disabled="isTeamManaged" />
                  </div>

                  <div class="grid gap-2">
                    <Label for="description">{{ t("ui.description") }}</Label>
                    <Textarea
                      id="description"
                      v-model="description"
                      :placeholder="t('access.teamDescription')"
                      :disabled="isTeamManaged"
                      rows="3"
                    />
                  </div>
                </div>

                <div class="flex justify-end">
                  <Button type="submit" :disabled="isSaving || isTeamManaged">
                    <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
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
        :title="pendingAction?.title ?? ''"
        :description="pendingAction?.description"
        :confirm-text="pendingAction?.confirmText"
        :destructive="pendingAction?.destructive"
        @confirm="handleConfirm"
        @cancel="handleCancelConfirm"
      />
    </template>
  </div>
</template>
