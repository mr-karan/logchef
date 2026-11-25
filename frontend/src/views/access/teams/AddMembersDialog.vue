<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Bot, Info, Loader2, Plus, User, UserPlus, X } from "lucide-vue-next";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SearchInput } from "@/components/layout";
import type { TeamMember } from "@/api/teams";
import type { TeamRole } from "@/composables/useTeamRoles";
import { useTeamsStore } from "@/stores/teams";
import { useUsersStore } from "@/stores/users";

const props = defineProps<{
  teamId: number;
  teamName: string;
  managed: boolean;
  members: TeamMember[];
}>();

const { t } = useI18n();
const usersStore = useUsersStore();
const teamsStore = useTeamsStore();

const open = ref(false);
const role = ref<TeamRole>("member");
const accountType = ref<"human" | "service">("human");
const search = ref("");
const selectedUserIds = ref<number[]>([]);

const isAdding = computed(() => teamsStore.isLoadingOperation(`addTeamMember-${props.teamId}`));

const availableUsers = computed(() => {
  const memberIds = props.members.map((m) => String(m.user_id));
  return usersStore
    .getUsersNotInTeam(memberIds)
    .sort((a, b) => (a.full_name || a.email).localeCompare(b.full_name || b.email));
});

const pickerUsers = computed(() => {
  const q = search.value.trim().toLowerCase();
  return availableUsers.value.filter(
    (u) =>
      (u.account_type ?? "human") === accountType.value &&
      (!q ||
        (u.full_name || "").toLowerCase().includes(q) ||
        (u.email || "").toLowerCase().includes(q)),
  );
});

const selectedUsers = computed(() => {
  const selected = new Set(selectedUserIds.value);
  return availableUsers.value.filter((u) => selected.has(Number(u.id)));
});

function isSelected(userId: string | number): boolean {
  return selectedUserIds.value.includes(Number(userId));
}

function toggle(userId: string | number) {
  const id = Number(userId);
  selectedUserIds.value = isSelected(id)
    ? selectedUserIds.value.filter((existing) => existing !== id)
    : [...selectedUserIds.value, id];
}

watch(open, async (isOpen) => {
  if (!isOpen) return;
  accountType.value = "human";
  role.value = "member";
  search.value = "";
  selectedUserIds.value = [];
  if (!usersStore.users.length) {
    await usersStore.loadUsers();
  }
});

async function addMembers() {
  if (selectedUserIds.value.length === 0) return;
  const result = await teamsStore.addTeamMembers(props.teamId, selectedUserIds.value, role.value);
  if (result.success) {
    open.value = false;
    return;
  }
  // Users that were added are members now; a retry must not send them again.
  selectedUserIds.value = result.failedIds;
}
</script>

<template>
  <Dialog v-model:open="open">
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
        <Alert v-if="managed" class="py-2.5">
          <Info class="h-4 w-4" />
          <AlertDescription class="text-xs">
            {{ t("access.managedTeamChangeWarning", { team: teamName }) }}
          </AlertDescription>
        </Alert>

        <div class="space-y-2">
          <Label>{{ t("access.accountType") }}</Label>
          <div class="grid grid-cols-2 gap-2">
            <Button
              type="button"
              size="sm"
              :variant="accountType === 'human' ? 'default' : 'outline'"
              class="justify-start gap-2"
              @click="accountType = 'human'"
            >
              <User class="h-4 w-4" />
              {{ t("access.humanUser") }}
            </Button>
            <Button
              type="button"
              size="sm"
              :variant="accountType === 'service' ? 'default' : 'outline'"
              class="justify-start gap-2"
              @click="accountType = 'service'"
            >
              <Bot class="h-4 w-4" />
              {{ t("access.serviceAccount") }}
            </Button>
          </div>
        </div>

        <div v-if="selectedUsers.length > 0" class="space-y-1.5">
          <div class="flex items-center justify-between text-xs text-muted-foreground">
            <span>{{ t("access.selectedCount", { count: selectedUsers.length }) }}</span>
            <button
              type="button"
              class="hover:text-foreground hover:underline"
              @click="selectedUserIds = []"
            >
              {{ t("ui.clear") }}
            </button>
          </div>
          <div class="flex flex-wrap gap-1.5">
            <Badge
              v-for="picked in selectedUsers"
              :key="picked.id"
              variant="secondary"
              class="gap-1 pr-1 font-normal"
            >
              <span>{{ picked.full_name || picked.email }}</span>
              <button
                type="button"
                class="rounded-sm p-0.5 hover:bg-muted-foreground/20"
                :aria-label="t('access.removeSelection')"
                @click="toggle(picked.id)"
              >
                <X class="h-3 w-3" />
              </button>
            </Badge>
          </div>
        </div>

        <div class="space-y-2">
          <SearchInput
            v-model="search"
            class="max-w-none"
            :placeholder="
              accountType === 'service' ? t('access.searchServiceAccounts') : t('access.searchUsers')
            "
          />
          <div
            class="max-h-56 divide-y overflow-y-auto rounded-md border"
            role="listbox"
            aria-multiselectable="true"
          >
            <div
              v-if="pickerUsers.length === 0"
              class="px-3 py-6 text-center text-sm text-muted-foreground"
            >
              {{
                accountType === "service"
                  ? t("access.noServiceAccountsAvailable")
                  : t("access.noUsersAvailable")
              }}
            </div>
            <button
              v-for="candidate in pickerUsers"
              :key="candidate.id"
              type="button"
              role="option"
              :aria-selected="isSelected(candidate.id)"
              :data-testid="`add-member-option-${candidate.id}`"
              class="flex w-full items-center gap-3 px-3 py-2 text-left text-sm transition-colors hover:bg-muted/50"
              :class="{ 'bg-accent/40': isSelected(candidate.id) }"
              @click="toggle(candidate.id)"
            >
              <Checkbox
                :model-value="isSelected(candidate.id)"
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
          <Select v-model="role">
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
        <Button variant="outline" @click="open = false">
          {{ t("ui.cancel") }}
        </Button>
        <Button
          :disabled="isAdding || selectedUserIds.length === 0"
          data-testid="add-members-submit"
          @click="addMembers"
        >
          <Loader2 v-if="isAdding" class="mr-2 h-4 w-4 animate-spin" />
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
