<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { Bot, Search, User2, Users, X } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import {
  EmptyState,
  LoadingState,
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
import type { TeamMember } from "@/api/teams";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import { useTeamPermissions } from "@/composables/useTeamPermissions";
import {
  isTeamRole,
  usePendingConfirm,
  useTeamRoles,
  type TeamRole,
} from "@/composables/useTeamRoles";
import { useAuthStore } from "@/stores/auth";
import { useTeamsStore } from "@/stores/teams";
import { formatDate } from "@/utils/format";

const props = defineProps<{
  teamId: number;
  teamName: string;
  managed: boolean;
  members: TeamMember[];
  canManage: boolean;
}>();

const { t } = useI18n();
const router = useRouter();
const authStore = useAuthStore();
const teamsStore = useTeamsStore();
const { isGlobalAdmin, isTeamAdmin, isAnyTeamAdmin } = useTeamPermissions();
const { roleLabel, roleChangeConfirm, removeMemberConfirm } = useTeamRoles();
const {
  pending: confirmPending,
  open: confirmOpen,
  request: requestConfirm,
  confirm: handleConfirm,
  cancel: handleCancel,
  dismiss: dismissConfirm,
} = usePendingConfirm();

const roleFilter = ref<"all" | TeamRole>("all");
const selectResetKey = ref(0);

const teamAdminCount = computed(() => props.members.filter((m) => m.role === "admin").length);

const roleFilteredMembers = computed(() =>
  roleFilter.value === "all"
    ? props.members
    : props.members.filter((m) => m.role === roleFilter.value),
);

const {
  search: memberSearch,
  rows: sortedMembers,
  sortKey,
  sortDir,
  toggleSort,
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

const hasFilters = computed(() => memberSearch.value.trim() !== "" || roleFilter.value !== "all");

function clearFilters() {
  memberSearch.value = "";
  roleFilter.value = "all";
}

watch(
  () => props.teamId,
  () => {
    dismissConfirm();
    roleFilter.value = "all";
  },
);

function isRowBusy(userId: number): boolean {
  return (
    teamsStore.isLoadingOperation(`updateTeamMemberRole-${props.teamId}-${userId}`) ||
    teamsStore.isLoadingOperation(`removeTeamMember-${props.teamId}-${userId}`)
  );
}

function isSelf(member: TeamMember): boolean {
  return !!authStore.user && Number(member.user_id) === Number(authStore.user.id);
}

function isLastAdmin(member: TeamMember): boolean {
  return member.role === "admin" && teamAdminCount.value <= 1;
}

// After changing their own membership, a team admin may no longer manage this
// team; leave a page whose controls would now return 403.
async function leaveIfAccessLost(pinnedTeamId: number) {
  if (isTeamAdmin(pinnedTeamId)) return;
  await router.replace(isAnyTeamAdmin.value ? { name: "Teams" } : "/");
}

// Each action pins the team so a route change while the dialog is open can
// never act on the new team.
function onRoleChange(member: TeamMember, value: unknown) {
  if (!isTeamRole(value) || value === member.role) return;
  const pinnedTeamId = props.teamId;
  const userId = Number(member.user_id);
  const self = isSelf(member);

  const apply = async () => {
    const result = await teamsStore.updateTeamMemberRole(pinnedTeamId, userId, value);
    if (!result.success) {
      selectResetKey.value++;
      return;
    }
    if (self) await leaveIfAccessLost(pinnedTeamId);
  };

  const dialog = roleChangeConfirm({
    memberName: member.full_name || member.email,
    teamName: props.teamName,
    from: member.role,
    to: value,
    isLastAdmin: isLastAdmin(member),
    isSelf: self,
    managed: props.managed,
  });
  if (!dialog) {
    void apply();
    return;
  }
  requestConfirm({ ...dialog, onConfirm: apply, onCancel: () => selectResetKey.value++ });
}

function askRemove(member: TeamMember) {
  const pinnedTeamId = props.teamId;
  const userId = Number(member.user_id);
  const self = isSelf(member);
  requestConfirm({
    ...removeMemberConfirm({
      memberName: member.full_name || member.email,
      teamName: props.teamName,
      role: member.role,
      isLastAdmin: isLastAdmin(member),
      removesOwnAccess: self && !isGlobalAdmin.value,
      managed: props.managed,
    }),
    onConfirm: async () => {
      const result = await teamsStore.removeTeamMember(pinnedTeamId, userId);
      if (result.success && self) await leaveIfAccessLost(pinnedTeamId);
    },
  });
}

function viewInPeople(member: TeamMember) {
  void router.push({ name: "ManageUsers", query: { user: String(member.user_id) } });
}
</script>

<template>
  <LoadingState
    v-if="teamsStore.isLoadingTeamMembers(teamId)"
    :label="t('access.loadingMembers')"
  />
  <EmptyState v-else-if="members.length === 0" :icon="Users" :title="t('access.noMembersFound')" />
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
        v-if="hasFilters"
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
      v-if="sortedMembers.length === 0"
      :icon="Search"
      :title="t('access.noResults')"
      :description="t('access.noMembersMatch')"
      class="rounded-md border"
    >
      <template v-if="hasFilters" #action>
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
                {{ t("access.memberColumn") }}
              </SortHeader>
            </TableHead>
            <TableHead class="w-[160px]">
              <SortHeader column="role" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                {{ t("access.teamRole") }}
              </SortHeader>
            </TableHead>
            <TableHead class="w-[180px]">
              <SortHeader column="added" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
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
                  <Bot v-if="member.account_type === 'service'" class="h-4 w-4 text-muted-foreground" />
                  <span class="font-medium">{{ member.full_name || member.email }}</span>
                  <Badge v-if="member.account_type === 'service'" variant="secondary" class="text-xs">
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
                v-if="canManage"
                :key="`member-role-${member.user_id}-${selectResetKey}`"
                :model-value="member.role"
                :disabled="isRowBusy(member.user_id)"
                @update:model-value="(val) => onRoleChange(member, val)"
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
              <span v-else>{{ roleLabel(member.role) }}</span>
            </TableCell>
            <TableCell class="text-sm text-muted-foreground">
              {{ formatDate(member.created_at) }}
            </TableCell>
            <TableCell class="text-right">
              <RowActionsMenu
                v-if="canManage"
                :delete-label="t('access.removeFromTeam')"
                @delete="askRemove(member)"
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

  <ConfirmDialog
    v-model:open="confirmOpen"
    :title="confirmPending?.title ?? ''"
    :description="confirmPending?.description"
    :confirm-text="confirmPending?.confirmText"
    :destructive="confirmPending?.destructive"
    @confirm="handleConfirm"
    @cancel="handleCancel"
  />
</template>
