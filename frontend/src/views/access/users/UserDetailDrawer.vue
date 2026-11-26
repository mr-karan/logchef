<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { Check, Loader2, Pencil, Plus, Trash2, X } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SearchableSelect, type SearchableItem } from "@/components/ui/searchable-select";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import type { User } from "@/types";
import type { TeamMembershipSummary, TeamWithMemberCount } from "@/api/teams";
import {
  isTeamRole,
  usePendingConfirm,
  useTeamRoles,
  type TeamRole,
} from "@/composables/useTeamRoles";
import { useAuthStore } from "@/stores/auth";
import { useTeamsStore } from "@/stores/teams";
import { useUsersStore } from "@/stores/users";

type GlobalRole = "admin" | "member";
type UserStatus = "active" | "inactive";

const props = defineProps<{
  open: boolean;
  user: User | null;
  memberships: TeamMembershipSummary[];
  allMemberships: TeamMembershipSummary[];
  membershipsStatus: "loading" | "loaded" | "error";
  allTeams: TeamWithMemberCount[];
  activeGlobalAdminCount: number;
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "memberships-changed"): void;
  (e: "retry-memberships"): void;
  (e: "user-deleted"): void;
}>();

const { t } = useI18n();
const router = useRouter();
const authStore = useAuthStore();
const usersStore = useUsersStore();
const teamsStore = useTeamsStore();
const { roleChangeConfirm, removeMemberConfirm } = useTeamRoles();
const {
  pending: pendingConfirm,
  open: confirmOpen,
  request: openConfirm,
  confirm: handleConfirm,
  cancel: handleCancelConfirm,
} = usePendingConfirm();

const isEditingName = ref(false);
const draftName = ref("");
const addTeamId = ref("");
const addTeamRole = ref<TeamRole>("member");
const isAddingToTeam = ref(false);
const selectResetKey = ref(0);

watch(
  () => [props.open, props.user?.id] as const,
  () => {
    isEditingName.value = false;
    draftName.value = props.user?.full_name ?? "";
    addTeamId.value = "";
    addTeamRole.value = "member";
  },
);

const isSelf = computed(
  () => !!props.user && !!authStore.user && String(props.user.id) === String(authStore.user.id),
);

const isLastActiveGlobalAdmin = computed(
  () =>
    !!props.user &&
    props.user.role === "admin" &&
    props.user.status === "active" &&
    props.activeGlobalAdminCount <= 1,
);

const isSavingUser = computed(
  () => !!props.user && usersStore.isLoadingOperation(`updateUser-${props.user.id}`),
);

const isDeletingUser = computed(
  () => !!props.user && usersStore.isLoadingOperation(`deleteUser-${props.user.id}`),
);

const availableTeams = computed(() => {
  const existing = new Set(props.memberships.map((m) => m.team_id));
  return [...props.allTeams]
    .filter((team) => !existing.has(team.id))
    .sort((a, b) => a.name.localeCompare(b.name));
});

const availableTeamItems = computed<SearchableItem[]>(() =>
  availableTeams.value.map((team) => ({
    value: String(team.id),
    label: team.name,
    sublabel: team.managed ? t("access.provisionedFromConfig") : undefined,
  })),
);

function isLastTeamAdmin(teamId: number, userId: number): boolean {
  const admins = props.allMemberships.filter(
    (m) => m.team_id === teamId && m.role === "admin",
  );
  return admins.length <= 1 && admins.some((m) => Number(m.user_id) === Number(userId));
}

function isTeamRowBusy(teamId: number): boolean {
  if (!props.user) return false;
  const uid = Number(props.user.id);
  return (
    teamsStore.isLoadingOperation(`updateTeamMemberRole-${teamId}-${uid}`) ||
    teamsStore.isLoadingOperation(`removeTeamMember-${teamId}-${uid}`)
  );
}

function resetSelects() {
  selectResetKey.value++;
}

// Changing your own role or status can drop the global admin role that this
// page requires.
async function refreshSelf() {
  await authStore.refreshSession();
  if (authStore.user?.role !== "admin" || authStore.user.status !== "active") {
    await router.replace("/");
  }
}

function startEditName() {
  if (!props.user) return;
  draftName.value = props.user.full_name;
  isEditingName.value = true;
}

async function saveName() {
  if (!props.user) return;
  const trimmed = draftName.value.trim();
  if (!trimmed || trimmed === props.user.full_name) {
    isEditingName.value = false;
    return;
  }
  const result = await usersStore.updateUser(String(props.user.id), { full_name: trimmed });
  if (result.success) {
    isEditingName.value = false;
  }
}

async function applyGlobalRole(role: GlobalRole) {
  if (!props.user) return;
  const result = await usersStore.updateUser(String(props.user.id), { role });
  if (!result.success) {
    resetSelects();
  } else if (isSelf.value) {
    await refreshSelf();
  }
}

function onGlobalRoleChange(value: unknown) {
  if (!props.user || (value !== "admin" && value !== "member")) return;
  const nextRole: GlobalRole = value;
  if (nextRole === props.user.role) return;

  if (isSelf.value && nextRole !== "admin") {
    openConfirm({
      title: t("access.demoteSelfTitle"),
      description: t("access.demoteSelfDescription"),
      confirmText: t("access.demoteSelfConfirm"),
      destructive: true,
      onConfirm: () => applyGlobalRole(nextRole),
      onCancel: resetSelects,
    });
    return;
  }

  void applyGlobalRole(nextRole);
}

async function applyStatus(status: UserStatus) {
  if (!props.user) return;
  const result = await usersStore.updateUser(String(props.user.id), { status });
  if (!result.success) {
    resetSelects();
  } else if (isSelf.value) {
    await refreshSelf();
  }
}

function onStatusChange(value: unknown) {
  if (!props.user || (value !== "active" && value !== "inactive")) return;
  const nextStatus: UserStatus = value;
  if (nextStatus === props.user.status) return;

  if (isSelf.value && nextStatus === "inactive") {
    openConfirm({
      title: t("access.deactivateSelfTitle"),
      description: t("access.deactivateSelfDescription"),
      confirmText: t("access.deactivateSelfConfirm"),
      destructive: true,
      onConfirm: () => applyStatus(nextStatus),
      onCancel: resetSelects,
    });
    return;
  }

  void applyStatus(nextStatus);
}

async function applyTeamRole(teamId: number, role: TeamRole) {
  if (!props.user) return;
  const result = await teamsStore.updateTeamMemberRole(teamId, Number(props.user.id), role);
  if (result.success) {
    emit("memberships-changed");
  } else {
    resetSelects();
  }
}

function onTeamRoleChange(membership: TeamMembershipSummary, value: unknown) {
  if (!props.user || !isTeamRole(value) || value === membership.role) return;
  const apply = () => applyTeamRole(membership.team_id, value);
  const dialog = roleChangeConfirm({
    memberName: props.user.full_name || props.user.email,
    teamName: membership.team_name,
    from: membership.role,
    to: value,
    isLastAdmin: isLastTeamAdmin(membership.team_id, Number(props.user.id)),
    isSelf: isSelf.value,
    managed: membership.team_managed,
  });
  if (!dialog) {
    void apply();
    return;
  }
  openConfirm({ ...dialog, onConfirm: apply, onCancel: resetSelects });
}

function askRemoveFromTeam(membership: TeamMembershipSummary) {
  if (!props.user) return;
  const userId = Number(props.user.id);
  openConfirm({
    ...removeMemberConfirm({
      memberName: props.user.full_name || props.user.email,
      teamName: membership.team_name,
      role: membership.role,
      isLastAdmin: isLastTeamAdmin(membership.team_id, userId),
      // A global admin keeps access to every team.
      removesOwnAccess: false,
      managed: membership.team_managed,
    }),
    onConfirm: async () => {
      const result = await teamsStore.removeTeamMember(membership.team_id, userId);
      if (result.success) {
        emit("memberships-changed");
      }
    },
  });
}

async function performAddToTeam(teamId: number, role: TeamRole) {
  if (!props.user) return;
  isAddingToTeam.value = true;
  try {
    const result = await teamsStore.addTeamMember(teamId, {
      user_id: Number(props.user.id),
      role,
    });
    if (result.success) {
      addTeamId.value = "";
      addTeamRole.value = "member";
      emit("memberships-changed");
    }
  } finally {
    isAddingToTeam.value = false;
  }
}

function handleAddToTeam() {
  if (!props.user || !addTeamId.value) return;
  const teamId = Number(addTeamId.value);
  if (!Number.isFinite(teamId) || teamId <= 0) return;

  const selectedTeam = props.allTeams.find((team) => team.id === teamId);
  const role = addTeamRole.value;
  if (selectedTeam?.managed) {
    openConfirm({
      title: t("access.addToManagedTeamTitle"),
      description: t("access.managedTeamChangeWarning", { team: selectedTeam.name }),
      confirmText: t("access.add"),
      destructive: false,
      onConfirm: () => performAddToTeam(teamId, role),
    });
    return;
  }

  void performAddToTeam(teamId, role);
}

function askDeleteUser() {
  if (!props.user) return;
  const target = props.user;
  openConfirm({
    title: t("access.deleteUserTitle"),
    description: t("access.deleteUserConfirmation", { name: target.full_name || target.email }),
    confirmText: t("ui.delete"),
    destructive: true,
    onConfirm: async () => {
      const result = await usersStore.deleteUser(String(target.id));
      if (result.success) {
        emit("user-deleted");
        emit("update:open", false);
      }
    },
  });
}
</script>

<template>
  <Sheet :open="open" @update:open="emit('update:open', $event)">
    <SheetContent
      side="right"
      class="flex w-full flex-col gap-0 overflow-y-auto p-0 sm:max-w-md"
      data-testid="user-detail-drawer"
    >
      <template v-if="user">
        <SheetHeader class="border-b px-6 py-5 pr-12">
          <div v-if="!isEditingName" class="flex items-center gap-2">
            <SheetTitle class="truncate text-lg">
              {{ user.full_name || user.email }}
            </SheetTitle>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              class="h-7 w-7 shrink-0 text-muted-foreground"
              :title="t('access.editName')"
              @click="startEditName"
            >
              <Pencil class="h-3.5 w-3.5" />
            </Button>
          </div>
          <form v-else class="flex items-center gap-1.5" @submit.prevent="saveName">
            <Input
              v-model="draftName"
              class="h-8 text-sm"
              :aria-label="t('ui.fullName')"
              :disabled="isSavingUser"
            />
            <Button type="submit" size="icon" variant="ghost" class="h-8 w-8 shrink-0" :disabled="isSavingUser">
              <Check class="h-4 w-4" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              class="h-8 w-8 shrink-0"
              @click="isEditingName = false"
            >
              <X class="h-4 w-4" />
            </Button>
          </form>

          <SheetDescription class="flex flex-wrap items-center gap-2">
            <span class="truncate">{{ user.email }}</span>
            <Badge
              v-if="user.managed"
              variant="outline"
              class="text-[11px] font-normal"
              :title="t('access.provisionedFromConfig')"
            >
              {{ t("access.managed") }}
            </Badge>
          </SheetDescription>
        </SheetHeader>

        <div class="flex-1 space-y-6 px-6 py-5">
          <!-- Account Section -->
          <section class="space-y-3">
            <h4 class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ t("access.accountSection") }}
            </h4>

            <div class="divide-y rounded-md border">
              <div class="flex items-center justify-between gap-4 px-3 py-2.5">
                <span class="text-sm font-medium">{{ t("access.globalRole") }}</span>
                <Select
                  :key="`global-role-${user.id}-${selectResetKey}`"
                  :model-value="user.role"
                  :disabled="isSavingUser || isLastActiveGlobalAdmin"
                  @update:model-value="onGlobalRoleChange"
                >
                  <SelectTrigger
                    size="sm"
                    class="w-[130px]"
                    :aria-label="t('access.globalRole')"
                    data-testid="drawer-global-role"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="admin">{{ t("access.roleAdmin") }}</SelectItem>
                    <SelectItem value="member">{{ t("access.roleMember") }}</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div class="flex items-center justify-between gap-4 px-3 py-2.5">
                <span class="text-sm font-medium">{{ t("ui.status") }}</span>
                <Select
                  :key="`status-${user.id}-${selectResetKey}`"
                  :model-value="user.status"
                  :disabled="isSavingUser || isLastActiveGlobalAdmin"
                  @update:model-value="onStatusChange"
                >
                  <SelectTrigger
                    size="sm"
                    class="w-[130px]"
                    :aria-label="t('ui.status')"
                    data-testid="drawer-status"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="active">{{ t("access.statusActive") }}</SelectItem>
                    <SelectItem value="inactive">{{ t("access.statusInactive") }}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <p v-if="isLastActiveGlobalAdmin" class="text-xs text-muted-foreground">
              {{ t("access.lastAdminRequiredHint") }}
            </p>
          </section>

          <!-- Teams Section -->
          <section class="space-y-3">
            <h4 class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ t("routes.teams") }}
            </h4>

            <div
              v-if="membershipsStatus === 'error'"
              class="flex items-center justify-between gap-2 rounded-md border border-destructive/40 px-3 py-3 text-sm text-destructive"
              data-testid="drawer-teams-error"
            >
              <span>{{ t("access.teamMembershipsLoadFailed") }}</span>
              <Button type="button" variant="outline" size="sm" class="h-7" @click="emit('retry-memberships')">
                {{ t("ui.retry") }}
              </Button>
            </div>

            <p v-else-if="membershipsStatus === 'loading'" class="px-1 text-sm text-muted-foreground">
              {{ t("access.loadingTeams") }}
            </p>

            <p v-else-if="memberships.length === 0" class="rounded-md border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">
              {{ t("access.notInAnyTeamsYet") }}
            </p>

            <div v-else class="divide-y rounded-md border">
              <div
                v-for="membership in memberships"
                :key="membership.team_id"
                class="flex items-center justify-between gap-2 px-3 py-2"
                :data-testid="`drawer-team-row-${membership.team_id}`"
              >
                <div class="flex min-w-0 items-center gap-1.5">
                  <router-link
                    :to="{ name: 'TeamSettings', params: { id: String(membership.team_id) } }"
                    class="truncate text-sm font-medium hover:underline"
                    @click="emit('update:open', false)"
                  >
                    {{ membership.team_name }}
                  </router-link>
                  <Badge
                    v-if="membership.team_managed"
                    variant="outline"
                    class="shrink-0 text-[11px] font-normal"
                    :title="t('access.provisionedFromConfig')"
                  >
                    {{ t("access.managed") }}
                  </Badge>
                </div>

                <div class="flex shrink-0 items-center gap-1.5">
                  <Select
                    :key="`team-role-${membership.team_id}-${selectResetKey}`"
                    :model-value="membership.role"
                    :disabled="isTeamRowBusy(membership.team_id)"
                    @update:model-value="(val) => onTeamRoleChange(membership, val)"
                  >
                    <SelectTrigger
                      size="sm"
                      class="w-[104px]"
                      :aria-label="`${membership.team_name} ${t('ui.role')}`"
                      :data-testid="`drawer-team-role-${membership.team_id}`"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="admin">{{ t("access.roleAdmin") }}</SelectItem>
                      <SelectItem value="editor">{{ t("access.roleEditor") }}</SelectItem>
                      <SelectItem value="member">{{ t("access.roleMember") }}</SelectItem>
                    </SelectContent>
                  </Select>

                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8 text-muted-foreground hover:text-destructive"
                    :title="t('access.removeFromTeam')"
                    :aria-label="`${t('access.removeFromTeam')}: ${membership.team_name}`"
                    :disabled="isTeamRowBusy(membership.team_id)"
                    :data-testid="`drawer-team-remove-${membership.team_id}`"
                    @click="askRemoveFromTeam(membership)"
                  >
                    <Loader2 v-if="isTeamRowBusy(membership.team_id)" class="h-3.5 w-3.5 animate-spin" />
                    <X v-else class="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </div>

            <div
              v-if="membershipsStatus === 'loaded' && availableTeamItems.length > 0"
              class="flex items-center gap-2 pt-1"
            >
              <div class="min-w-0 flex-1">
                <SearchableSelect
                  v-model="addTeamId"
                  :items="availableTeamItems"
                  :placeholder="t('access.addToTeamPlaceholder')"
                  :search-placeholder="t('access.searchTeams')"
                  :empty-text="t('access.noTeamsAvailable')"
                  trigger-class="h-8 text-xs"
                  :disabled="isAddingToTeam"
                />
              </div>

              <Select v-model="addTeamRole" :disabled="isAddingToTeam">
                <SelectTrigger size="sm" class="w-[100px]" :aria-label="t('ui.role')">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">{{ t("access.roleMember") }}</SelectItem>
                  <SelectItem value="editor">{{ t("access.roleEditor") }}</SelectItem>
                  <SelectItem value="admin">{{ t("access.roleAdmin") }}</SelectItem>
                </SelectContent>
              </Select>

              <Button
                type="button"
                variant="outline"
                size="sm"
                class="h-8 shrink-0"
                :disabled="!addTeamId || isAddingToTeam"
                data-testid="drawer-add-team-submit"
                @click="handleAddToTeam"
              >
                <Loader2 v-if="isAddingToTeam" class="h-3.5 w-3.5 animate-spin" />
                <Plus v-else class="mr-1 h-3.5 w-3.5" />
                {{ t("access.add") }}
              </Button>
            </div>
          </section>

          <!-- Danger Zone -->
          <section class="space-y-2 border-t pt-5">
            <h4 class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ t("access.dangerZone") }}
            </h4>
            <div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                class="border-destructive/40 text-destructive hover:bg-destructive/10 hover:text-destructive"
                :disabled="isLastActiveGlobalAdmin || isSelf || isDeletingUser"
                data-testid="drawer-delete-user"
                @click="askDeleteUser"
              >
                <Trash2 class="mr-2 h-4 w-4" />
                {{ t("access.deleteUser") }}
              </Button>
              <p v-if="isSelf" class="mt-1.5 text-xs text-muted-foreground">
                {{ t("access.cannotDeleteSelfHint") }}
              </p>
            </div>
          </section>
        </div>
      </template>
    </SheetContent>
  </Sheet>

  <ConfirmDialog
    v-model:open="confirmOpen"
    :title="pendingConfirm?.title ?? ''"
    :description="pendingConfirm?.description"
    :confirm-text="pendingConfirm?.confirmText"
    :destructive="pendingConfirm?.destructive"
    @confirm="handleConfirm"
    @cancel="handleCancelConfirm"
  />
</template>
