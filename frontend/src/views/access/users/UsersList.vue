<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { storeToRefs } from "pinia";
import { Pencil, Plus, Search, Shield, User2, Users, X } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import {
  EmptyState,
  LoadingState,
  PageHeader,
  RowActionsMenu,
  SearchInput,
  SortHeader,
} from "@/components/layout";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { teamsApi, type TeamMembershipSummary } from "@/api/teams";
import { isSuccessResponse } from "@/api/types";
import type { User } from "@/types";
import { useAuthStore } from "@/stores/auth";
import { useTeamsStore } from "@/stores/teams";
import { useUsersStore } from "@/stores/users";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import { useTeamRoles } from "@/composables/useTeamRoles";
import { formatDate } from "@/utils/format";
import AddUser from "./AddUser.vue";
import UserDetailDrawer from "./UserDetailDrawer.vue";

const MAX_VISIBLE_TEAM_CHIPS = 2;

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const authStore = useAuthStore();
const usersStore = useUsersStore();
const teamsStore = useTeamsStore();
const { isLoading } = storeToRefs(usersStore);
const { roleLabel } = useTeamRoles();

const allMemberships = ref<TeamMembershipSummary[]>([]);
// Until memberships load, every user would look teamless: views that depend
// on them show this status instead of an empty list.
const membershipsStatus = ref<"loading" | "loaded" | "error">("loading");
const roleFilter = ref<"all" | "admin" | "member">("all");
const teamFilter = ref<string>("all");
const statusFilter = ref<"all" | "active" | "inactive">("all");

const selectedUserId = ref<string | null>(null);
const drawerOpen = ref(false);
const showDeleteDialog = ref(false);
const userToDelete = ref<User | null>(null);

const humanUsers = computed(() =>
  usersStore.users.filter((user) => user.account_type !== "service"),
);

const activeGlobalAdminCount = computed(
  () =>
    humanUsers.value.filter((user) => user.role === "admin" && user.status === "active").length,
);

const membershipsByUserId = computed(() => {
  const map = new Map<string, TeamMembershipSummary[]>();
  for (const membership of allMemberships.value) {
    const key = String(membership.user_id);
    const list = map.get(key) ?? [];
    list.push(membership);
    map.set(key, list);
  }
  return map;
});

function userMemberships(userId: string | number): TeamMembershipSummary[] {
  return membershipsByUserId.value.get(String(userId)) ?? [];
}

const sortedTeamsForFilter = computed(() =>
  [...teamsStore.adminTeams].sort((a, b) => a.name.localeCompare(b.name)),
);

const dropdownFilteredUsers = computed(() =>
  humanUsers.value.filter((user) => {
    if (roleFilter.value !== "all" && user.role !== roleFilter.value) return false;
    if (statusFilter.value !== "all" && user.status !== statusFilter.value) return false;
    if (teamFilter.value !== "all") {
      const memberships = userMemberships(user.id);
      if (teamFilter.value === "__none__") {
        if (memberships.length > 0) return false;
      } else {
        const wantedTeamId = Number(teamFilter.value);
        if (!memberships.some((m) => m.team_id === wantedTeamId)) return false;
      }
    }
    return true;
  }),
);

const {
  search: searchQuery,
  rows: filteredUsers,
  sortKey,
  sortDir,
  toggleSort,
} = useTableSearchSort(dropdownFilteredUsers, {
  searchKeys: [
    "full_name",
    "email",
    (u) => userMemberships(u.id).map((m) => `${m.team_name} ${m.role}`).join(" "),
  ],
  sortAccessors: {
    name: (u) => (u.full_name || u.email || "").toLowerCase(),
    role: (u) => u.role,
    status: (u) => u.status,
    lastLogin: (u) => (u.last_login_at ? new Date(u.last_login_at) : new Date(0)),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "users",
});

const hasActiveFilters = computed(
  () =>
    searchQuery.value.trim() !== "" ||
    roleFilter.value !== "all" ||
    teamFilter.value !== "all" ||
    statusFilter.value !== "all",
);

function clearFilters() {
  searchQuery.value = "";
  roleFilter.value = "all";
  teamFilter.value = "all";
  statusFilter.value = "all";
}

const selectedUser = computed(
  () => humanUsers.value.find((u) => String(u.id) === String(selectedUserId.value)) ?? null,
);

const selectedUserMemberships = computed(() =>
  selectedUser.value ? userMemberships(selectedUser.value.id) : [],
);

function openUserDrawer(user: User) {
  selectedUserId.value = String(user.id);
  drawerOpen.value = true;
}

function handleDrawerOpenChange(isOpen: boolean) {
  drawerOpen.value = isOpen;
  if (!isOpen) {
    selectedUserId.value = null;
    if (route.query.user !== undefined) {
      const nextQuery = { ...route.query };
      delete nextQuery.user;
      void router.replace({ query: nextQuery });
    }
  }
}

function handleUserDeleted() {
  selectedUserId.value = null;
  drawerOpen.value = false;
  void loadMemberships();
}

function canDeleteUser(user: User): boolean {
  if (authStore.user && String(user.id) === String(authStore.user.id)) return false;
  if (user.role === "admin" && user.status === "active" && activeGlobalAdminCount.value <= 1) {
    return false;
  }
  return true;
}

function handleDelete(user: User) {
  if (!canDeleteUser(user)) return;
  userToDelete.value = user;
  showDeleteDialog.value = true;
}

async function confirmDelete() {
  if (!userToDelete.value) return;
  const deletedId = String(userToDelete.value.id);
  const result = await usersStore.deleteUser(deletedId);
  showDeleteDialog.value = false;
  userToDelete.value = null;
  if (result.success) {
    if (selectedUserId.value === deletedId) {
      handleDrawerOpenChange(false);
    }
    await loadMemberships();
  }
}

async function loadMemberships() {
  try {
    const response = await teamsApi.listAllMemberships();
    if (isSuccessResponse(response) && Array.isArray(response.data)) {
      allMemberships.value = response.data;
      membershipsStatus.value = "loaded";
      return;
    }
    console.error("Failed to load team memberships:", response);
  } catch (error) {
    console.error("Failed to load team memberships:", error);
  }
  membershipsStatus.value = "error";
}

async function handleMembershipsChanged() {
  await Promise.all([loadMemberships(), teamsStore.loadAdminTeams(true)]);
}

function syncUserFromQuery() {
  const queryUser = route.query.user;
  if (typeof queryUser !== "string" || !queryUser) return;
  const match = humanUsers.value.find((u) => String(u.id) === queryUser);
  if (match) {
    openUserDrawer(match);
  }
}

watch(
  () => [route.query.user, humanUsers.value.length] as const,
  () => {
    syncUserFromQuery();
  },
);

onMounted(async () => {
  await Promise.all([
    usersStore.loadUsers(true),
    teamsStore.loadAdminTeams(true),
    loadMemberships(),
  ]);
  syncUserFromQuery();
});
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('admin.navPeople')" :description="t('access.peopleDescription')">
      <template #actions>
        <AddUser />
      </template>
    </PageHeader>

    <LoadingState v-if="isLoading && humanUsers.length === 0" :label="t('access.loadingUsers')" />

    <EmptyState
      v-else-if="humanUsers.length === 0"
      :icon="Users"
      :title="t('access.noUsersYet')"
      :description="t('access.addFirstUser')"
    >
      <template #action>
        <AddUser>
          <Button size="sm">
            <Plus class="mr-2 h-4 w-4" />
            {{ t("access.addUser") }}
          </Button>
        </AddUser>
      </template>
    </EmptyState>

    <div v-else class="space-y-4">
      <div class="flex flex-wrap items-center gap-2">
        <SearchInput v-model="searchQuery" :placeholder="t('access.searchPeople')" />

        <Select v-model="roleFilter">
          <SelectTrigger class="w-[140px]" :aria-label="t('access.filterRole')" data-testid="people-filter-role">
            <span class="truncate">
              <span class="text-muted-foreground">{{ t("access.filterRole") }}: </span>
              <SelectValue />
            </span>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{{ t("access.filterAny") }}</SelectItem>
            <SelectItem value="admin">{{ t("access.roleAdmin") }}</SelectItem>
            <SelectItem value="member">{{ t("access.roleMember") }}</SelectItem>
          </SelectContent>
        </Select>

        <Select v-model="teamFilter" :disabled="membershipsStatus !== 'loaded'">
          <SelectTrigger class="w-[175px]" :aria-label="t('access.filterTeam')" data-testid="people-filter-team">
            <span class="truncate">
              <span class="text-muted-foreground">{{ t("access.filterTeam") }}: </span>
              <SelectValue />
            </span>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{{ t("access.filterAny") }}</SelectItem>
            <SelectItem value="__none__">{{ t("access.noTeams") }}</SelectItem>
            <SelectItem
              v-for="team in sortedTeamsForFilter"
              :key="team.id"
              :value="String(team.id)"
            >
              {{ team.name }}
            </SelectItem>
          </SelectContent>
        </Select>

        <Select v-model="statusFilter">
          <SelectTrigger class="w-[150px]" :aria-label="t('access.filterStatus')" data-testid="people-filter-status">
            <span class="truncate">
              <span class="text-muted-foreground">{{ t("access.filterStatus") }}: </span>
              <SelectValue />
            </span>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{{ t("access.filterAny") }}</SelectItem>
            <SelectItem value="active">{{ t("access.statusActive") }}</SelectItem>
            <SelectItem value="inactive">{{ t("access.statusInactive") }}</SelectItem>
          </SelectContent>
        </Select>

        <Button
          v-if="hasActiveFilters"
          type="button"
          variant="ghost"
          size="sm"
          class="h-9 text-muted-foreground"
          @click="clearFilters"
        >
          <X class="mr-1 h-3.5 w-3.5" />
          {{ t("access.clearFilters") }}
        </Button>
      </div>

      <EmptyState
        v-if="filteredUsers.length === 0"
        :icon="Search"
        :title="t('access.noResults')"
        :description="t('access.noPeopleMatchFilters')"
        class="rounded-md border"
      >
        <template v-if="hasActiveFilters" #action>
          <Button variant="outline" size="sm" @click="clearFilters">
            {{ t("access.clearFilters") }}
          </Button>
        </template>
      </EmptyState>

      <div v-else class="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>
                <SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                  {{ t("ui.name") }}
                </SortHeader>
              </TableHead>
              <TableHead>
                <SortHeader column="role" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                  {{ t("access.globalRole") }}
                </SortHeader>
              </TableHead>
              <TableHead>{{ t("routes.teams") }}</TableHead>
              <TableHead>
                <SortHeader column="status" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                  {{ t("ui.status") }}
                </SortHeader>
              </TableHead>
              <TableHead>
                <SortHeader column="lastLogin" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                  {{ t("access.lastLogin") }}
                </SortHeader>
              </TableHead>
              <TableHead class="w-[88px] text-right">{{ t("ui.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="user in filteredUsers"
              :key="user.id"
              class="cursor-pointer"
              :class="{ 'bg-accent/50': drawerOpen && String(selectedUserId) === String(user.id) }"
              :data-testid="`people-row-${user.id}`"
              @click="openUserDrawer(user)"
            >
              <TableCell>
                <div class="flex flex-col">
                  <span class="font-medium">{{ user.full_name || user.email }}</span>
                  <span class="text-xs text-muted-foreground">{{ user.email }}</span>
                </div>
              </TableCell>
              <TableCell>
                <Badge
                  :variant="user.role === 'admin' ? 'destructive' : 'outline'"
                  class="inline-flex items-center gap-1 font-medium"
                >
                  <Shield v-if="user.role === 'admin'" class="h-3 w-3" />
                  <User2 v-else class="h-3 w-3" />
                  {{ user.role === "admin" ? t("access.roleAdmin") : t("access.roleMember") }}
                </Badge>
              </TableCell>
              <TableCell>
                <span
                  v-if="membershipsStatus === 'error'"
                  class="text-sm text-destructive"
                  data-testid="people-teams-error"
                >
                  {{ t("access.teamMembershipsLoadFailed") }}
                </span>
                <span v-else-if="membershipsStatus === 'loading'" class="text-sm text-muted-foreground">
                  …
                </span>
                <div
                  v-else-if="userMemberships(user.id).length > 0"
                  class="flex flex-wrap items-center gap-1"
                >
                  <Badge
                    v-for="membership in userMemberships(user.id).slice(0, MAX_VISIBLE_TEAM_CHIPS)"
                    :key="membership.team_id"
                    variant="secondary"
                    class="font-normal"
                  >
                    {{ membership.team_name }} · {{ roleLabel(membership.role).toLowerCase() }}
                  </Badge>
                  <Badge
                    v-if="userMemberships(user.id).length > MAX_VISIBLE_TEAM_CHIPS"
                    variant="secondary"
                    class="font-normal"
                    :title="
                      userMemberships(user.id)
                        .slice(MAX_VISIBLE_TEAM_CHIPS)
                        .map((m) => `${m.team_name} · ${roleLabel(m.role).toLowerCase()}`)
                        .join(', ')
                    "
                  >
                    +{{ userMemberships(user.id).length - MAX_VISIBLE_TEAM_CHIPS }}
                  </Badge>
                </div>
                <span v-else class="text-sm text-muted-foreground">
                  {{ t("access.noTeams") }}
                </span>
              </TableCell>
              <TableCell>
                <span class="inline-flex items-center gap-2 text-sm">
                  <span
                    class="h-2 w-2 rounded-full"
                    :class="user.status === 'active' ? 'bg-emerald-600' : 'bg-muted-foreground/50'"
                  />
                  {{ user.status === "active" ? t("access.statusActive") : t("access.statusInactive") }}
                </span>
              </TableCell>
              <TableCell class="text-sm text-muted-foreground">
                {{ formatDate(user.last_login_at) }}
              </TableCell>
              <TableCell class="text-right" @click.stop>
                <div class="flex items-center justify-end gap-1">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    :title="t('ui.edit')"
                    :aria-label="`${t('ui.edit')}: ${user.full_name || user.email}`"
                    :data-testid="`people-edit-${user.id}`"
                    @click="openUserDrawer(user)"
                  >
                    <Pencil class="h-4 w-4" />
                  </Button>
                  <RowActionsMenu
                    v-if="canDeleteUser(user)"
                    @delete="handleDelete(user)"
                  />
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </div>

    <UserDetailDrawer
      :open="drawerOpen"
      :user="selectedUser"
      :memberships="selectedUserMemberships"
      :all-memberships="allMemberships"
      :memberships-status="membershipsStatus"
      :all-teams="teamsStore.adminTeams"
      :active-global-admin-count="activeGlobalAdminCount"
      @update:open="handleDrawerOpenChange"
      @memberships-changed="handleMembershipsChanged"
      @retry-memberships="loadMemberships"
      @user-deleted="handleUserDeleted"
    />

    <ConfirmDialog
      :open="showDeleteDialog"
      :title="t('access.deleteUserTitle')"
      :description="
        userToDelete
          ? t('access.deleteUserConfirmation', { name: userToDelete.full_name || userToDelete.email })
          : undefined
      "
      :confirm-text="t('ui.delete')"
      destructive
      @update:open="(v) => { if (!v) { showDeleteDialog = false; userToDelete = null } }"
      @confirm="confirmDelete"
    />
  </div>
</template>
