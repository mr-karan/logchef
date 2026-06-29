<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { storeToRefs } from "pinia";
import {
  ArrowLeft,
  Lock,
  Pencil,
  Trash2,
  UserPlus,
  X,
  AlertCircle,
  FileSearch,
  Search,
  Database,
  Users,
} from "lucide-vue-next";
import { formatDate } from "@/utils/format";
import { PageHeader, PageSection, EmptyState, LoadingState } from "@/components/layout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { useToast } from "@/composables/useToast";
import { TOAST_DURATION } from "@/lib/constants";
import { useCollectionsStore } from "@/stores/collections";
import { useAuthStore } from "@/stores/auth";
import { useTeamPermissions } from "@/composables/useTeamPermissions";
import { useUsersStore } from "@/stores/users";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const route = useRoute();
const router = useRouter();
const { toast } = useToast();
const store = useCollectionsStore();
const authStore = useAuthStore();
const usersStore = useUsersStore();
const { data } = storeToRefs(store);
const { isAnyTeamAdmin, isGlobalAdmin, canManageCollection } = useTeamPermissions();

const collectionID = computed(() => Number(route.params.collectionID));
const collection = computed(() => store.collections.find((c) => c.id === collectionID.value) ?? null);
const items = computed(() => data.value.items[collectionID.value] ?? []);
const members = computed(() => data.value.members[collectionID.value] ?? []);

const itemCount = computed(() => items.value.length);
const memberCount = computed(() => members.value.length);

// Initials for the member avatar — first + last initial, falling back to the
// first character of whatever identifier we have.
function memberInitials(m: { full_name?: string | null; email?: string | null }): string {
  const source = (m.full_name || m.email || "").trim();
  if (!source) return "?";
  const parts = source.split(/\s+/);
  const first = parts[0]?.[0] ?? "";
  const last = parts.length > 1 ? parts[parts.length - 1][0] : "";
  return (first + last).toUpperCase() || source[0]!.toUpperCase();
}

// Member user_id is numeric while the auth store user id can be typed/serialized
// as a string — compare as strings so the "(you)" marker and self-removal guard
// work regardless.
function isCurrentUser(userId: number): boolean {
  const me = authStore.user?.id;
  return me != null && String(userId) === String(me);
}

const isOwner = computed(() => canManageCollection(collection.value));
// Listing users (`/api/v1/users`) requires admin or any-team-admin. Hide the
// invite UI from owners who lack that role since the dropdown can't be
// populated without it.
const canListUsers = computed(() => isGlobalAdmin.value || isAnyTeamAdmin.value);
const canInviteMembers = computed(() => isOwner.value && canListUsers.value && !collection.value?.is_personal);

const showAddMember = ref(false);
const newMemberId = ref("");
const newMemberRole = ref<"owner" | "member">("member");

// Users available for invite: all users minus current members
const availableUsers = computed(() => {
  const memberIds = new Set(members.value.map(m => String(m.user_id)));
  return (usersStore.users ?? []).filter(u => !memberIds.has(String(u.id)));
});

const showRename = ref(false);
const renameName = ref("");
const renameDescription = ref("");

const showDeleteDialog = ref(false);

// Confirm-dialog state — populated by handleRemove* and consumed by the
// ConfirmDialog instance at the bottom of the template.
const pendingMemberRemoval = ref<number | null>(null);
const pendingItemRemoval = ref<number | null>(null);

async function load() {
  if (!collectionID.value) return;
  if (!collection.value) {
    await store.fetchCollections();
  }
  const tasks: Promise<unknown>[] = [store.fetchItems(collectionID.value)];
  // Skip member fetch when the section won't render — personal collections
  // hide it, and non-team-admins can't see it (canListUsers is false).
  if (collection.value && !collection.value.is_personal && canListUsers.value) {
    tasks.push(store.fetchMembers(collectionID.value));
  }
  await Promise.all(tasks);
}

onMounted(load);
watch(collectionID, load);

// Load users only when the invite dialog opens, and only if the caller can
// list users. Guards against 403 spam on collection detail pages when a
// non-team-admin owner views the page.
watch(showAddMember, async (isOpen) => {
  if (!isOpen || !canListUsers.value) return;
  if (!usersStore.users.length) {
    await usersStore.loadUsers();
  }
});

async function handleAddMember() {
  const idNum = Number(newMemberId.value);
  if (!idNum) {
    toast({ title: "Invalid user id", variant: "destructive", duration: TOAST_DURATION.WARNING });
    return;
  }
  const result = await store.addMember(collectionID.value, { user_id: idNum, role: newMemberRole.value });
  if (result.success) {
    showAddMember.value = false;
    newMemberId.value = "";
    newMemberRole.value = "member";
  }
}

function handleRemoveMember(userId: number) {
  pendingMemberRemoval.value = userId;
}

async function confirmMemberRemoval() {
  const userId = pendingMemberRemoval.value;
  pendingMemberRemoval.value = null;
  if (userId == null) return;
  await store.removeMember(collectionID.value, userId);
}

function openRename() {
  if (!collection.value) return;
  renameName.value = collection.value.name;
  renameDescription.value = collection.value.description ?? "";
  showRename.value = true;
}

async function handleRename() {
  if (!renameName.value.trim()) return;
  const result = await store.updateCollection(collectionID.value, {
    name: renameName.value.trim(),
    description: renameDescription.value.trim(),
  });
  if (result.success) showRename.value = false;
}

function handleRemoveItem(queryId: number) {
  pendingItemRemoval.value = queryId;
}

async function confirmItemRemoval() {
  const queryId = pendingItemRemoval.value;
  pendingItemRemoval.value = null;
  if (queryId == null) return;
  await store.removeItem(collectionID.value, queryId);
}

function openQuery(queryId: number) {
  router.push(`/logs/saved/${queryId}`);
}

async function handleDeleteCollection() {
  if (!collection.value) return;
  await store.deleteCollection(collection.value.id);
  showDeleteDialog.value = false;
  router.push({ path: '/logs/collections', query: {} });
}
</script>

<template>
  <div class="space-y-6">
    <Button
      variant="ghost"
      size="sm"
      class="-ml-2"
      @click="router.push({ path: '/logs/collections', query: {} })"
    >
      <ArrowLeft class="mr-2 h-4 w-4" />
      Back to Collections
    </Button>

    <EmptyState
      v-if="!collection && !store.isLoading"
      title="Collection not found"
      description="It may have been deleted or you may not be a member."
    />

    <template v-else-if="collection">
      <PageHeader :title="collection.name" :description="collection.description || undefined">
        <template #actions>
          <Button v-if="isOwner && !collection.is_personal" variant="outline" size="sm" @click="openRename">
            <Pencil class="mr-2 h-4 w-4" />
            Rename
          </Button>
          <Button v-if="canInviteMembers" variant="outline" size="sm" @click="showAddMember = true">
            <UserPlus class="mr-2 h-4 w-4" />
            Invite member
          </Button>
          <Button v-if="isOwner && !collection.is_personal" variant="destructive" size="sm" @click="showDeleteDialog = true">
            <Trash2 class="mr-2 h-4 w-4" />
            Delete
          </Button>
        </template>
      </PageHeader>

      <!-- Metadata strip: visibility, caller's role, and at-a-glance counts. -->
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm text-muted-foreground">
        <Badge
          :variant="collection.is_personal ? 'secondary' : 'outline'"
          class="inline-flex items-center gap-1 font-medium"
        >
          <Lock v-if="collection.is_personal" class="h-3 w-3" />
          <Users v-else class="h-3 w-3" />
          {{ collection.is_personal ? "Personal" : "Shared" }}
        </Badge>
        <template v-if="collection.caller_role">
          <span class="text-muted-foreground/40">•</span>
          <span>
            Your role
            <span class="font-medium text-foreground capitalize">{{ collection.caller_role }}</span>
          </span>
        </template>
        <span class="text-muted-foreground/40">•</span>
        <span>
          <span class="font-medium text-foreground tabular-nums">{{ itemCount }}</span>
          {{ itemCount === 1 ? "query" : "queries" }}
        </span>
        <template v-if="!collection.is_personal && canListUsers">
          <span class="text-muted-foreground/40">•</span>
          <span>
            <span class="font-medium text-foreground tabular-nums">{{ memberCount }}</span>
            {{ memberCount === 1 ? "member" : "members" }}
          </span>
        </template>
        <template v-if="collection.created_at">
          <span class="text-muted-foreground/40">•</span>
          <span>Created {{ formatDate(collection.created_at) }}</span>
        </template>
      </div>

      <Alert v-if="store.error" variant="destructive">
        <AlertCircle class="h-4 w-4" />
        <AlertDescription>{{ store.error.message }}</AlertDescription>
      </Alert>

      <PageSection
        title="Items"
        description="Saved queries pinned to this collection. Items you can't run for this source show with a lock icon."
        flush
      >
        <LoadingState v-if="store.isLoadingOperation(`listItems-${collectionID}`)" />
        <EmptyState
          v-else-if="items.length === 0"
          :icon="FileSearch"
          title="No queries pinned"
          description="From a saved query you can edit, add it to this collection."
        />
        <table v-else class="w-full text-sm">
          <thead>
            <tr class="border-b bg-muted/30">
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5 w-[40px]">Type</th>
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5">Name</th>
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5 w-[140px]">Source</th>
              <th class="text-left font-medium text-muted-foreground px-4 py-2.5 w-[150px]">Updated</th>
              <th class="w-[40px]"></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="item in items"
              :key="item.query.id"
              class="border-b last:border-0 hover:bg-muted/40 transition-colors group"
              :class="!item.runnable && 'opacity-60'"
            >
              <td class="px-4 py-3 align-middle">
                <Lock
                  v-if="!item.runnable"
                  class="h-4 w-4 text-muted-foreground"
                  title="You cannot run this query (no source access)."
                />
                <Search
                  v-else-if="item.query.query_type === 'logchefql'"
                  class="h-4 w-4 text-muted-foreground"
                  title="LogchefQL"
                />
                <Database v-else class="h-4 w-4 text-muted-foreground" title="SQL" />
              </td>
              <td class="px-4 py-3 align-middle">
                <button
                  type="button"
                  class="font-medium text-foreground text-left hover:underline disabled:cursor-not-allowed disabled:hover:no-underline"
                  :class="!item.runnable && 'text-muted-foreground'"
                  :disabled="!item.runnable"
                  @click="openQuery(item.query.id)"
                >
                  {{ item.query.name }}
                </button>
                <p v-if="item.query.description" class="text-xs text-muted-foreground mt-0.5 truncate max-w-[400px]">
                  {{ item.query.description }}
                </p>
              </td>
              <td class="px-4 py-3 align-middle text-muted-foreground text-xs">
                <span class="inline-block max-w-[130px] truncate align-bottom">
                  {{ item.query.source_name || `source ${item.query.source_id}` }}
                </span>
              </td>
              <td class="px-4 py-3 align-middle text-muted-foreground text-xs whitespace-nowrap tabular-nums">
                {{ formatDate(item.query.updated_at) }}
              </td>
              <td class="px-4 py-3 align-middle">
                <Button
                  v-if="isOwner"
                  variant="ghost"
                  size="icon"
                  class="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                  @click="handleRemoveItem(item.query.id)"
                  title="Remove from collection"
                >
                  <Trash2 class="h-4 w-4 text-destructive" />
                </Button>
              </td>
            </tr>
          </tbody>
        </table>
      </PageSection>

      <PageSection
        v-if="!collection.is_personal && canListUsers"
        title="Members"
        description="Owners can invite new members and adjust roles. Members can read items they have source access to."
        flush
      >
        <LoadingState v-if="store.isLoadingOperation(`listMembers-${collectionID}`)" />
        <ul v-else class="divide-y">
          <li v-for="m in members" :key="m.user_id" class="flex items-center gap-3 px-4 py-3">
            <div
              class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground"
              aria-hidden="true"
            >
              {{ memberInitials(m) }}
            </div>
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium">
                {{ m.full_name || m.email || `User ${m.user_id}` }}
                <span v-if="isCurrentUser(m.user_id)" class="ml-1 text-xs font-normal text-muted-foreground">(you)</span>
              </p>
              <p class="truncate text-xs text-muted-foreground">{{ m.email }}</p>
            </div>
            <Badge
              :variant="m.role === 'owner' ? 'secondary' : 'outline'"
              class="w-16 shrink-0 justify-center capitalize"
            >
              {{ m.role }}
            </Badge>
            <div class="flex w-8 shrink-0 justify-center">
              <Button
                v-if="isOwner && !isCurrentUser(m.user_id)"
                variant="ghost"
                size="icon"
                class="h-7 w-7"
                title="Remove member"
                @click="handleRemoveMember(m.user_id)"
              >
                <X class="h-4 w-4 text-destructive" />
              </Button>
            </div>
          </li>
        </ul>
      </PageSection>
    </template>

    <Dialog :open="showAddMember" @update:open="(val) => !val && (showAddMember = false)">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Invite member</DialogTitle>
          <DialogDescription>
            Select a user by email. Owners can fully manage the collection; members can read and run items they have source access to.
          </DialogDescription>
        </DialogHeader>
        <form @submit.prevent="handleAddMember" class="space-y-4">
          <div class="grid gap-2">
            <Label>User</Label>
            <Select v-model="newMemberId">
              <SelectTrigger>
                <SelectValue placeholder="Select a user to invite" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="user in availableUsers"
                  :key="user.id"
                  :value="String(user.id)"
                  :text-value="user.email"
                >
                  {{ user.email }}
                  <span v-if="user.full_name" class="ml-2 text-muted-foreground text-xs">({{ user.full_name }})</span>
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="grid gap-2">
            <Label>Role</Label>
            <Select v-model="newMemberRole">
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="member">Member</SelectItem>
                <SelectItem value="owner">Owner</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" @click="showAddMember = false">Cancel</Button>
            <Button type="submit" :disabled="!newMemberId">Invite</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <Dialog :open="showRename" @update:open="(val) => !val && (showRename = false)">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Rename collection</DialogTitle>
        </DialogHeader>
        <form @submit.prevent="handleRename" class="space-y-4">
          <div class="grid gap-2">
            <Label for="rename-name">Name</Label>
            <Input id="rename-name" v-model="renameName" required />
          </div>
          <div class="grid gap-2">
            <Label for="rename-description">Description (optional)</Label>
            <Textarea id="rename-description" v-model="renameDescription" rows="3" />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" @click="showRename = false">Cancel</Button>
            <Button type="submit" :disabled="!renameName.trim()">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <ConfirmDialog
      :open="pendingMemberRemoval !== null"
      title="Remove member?"
      description="They will no longer see this collection."
      confirm-text="Remove"
      destructive
      @update:open="(v) => { if (!v) pendingMemberRemoval = null }"
      @confirm="confirmMemberRemoval"
    />
    <ConfirmDialog
      :open="pendingItemRemoval !== null"
      title="Remove query from collection?"
      description="The saved query itself is not deleted."
      confirm-text="Remove"
      destructive
      @update:open="(v) => { if (!v) pendingItemRemoval = null }"
      @confirm="confirmItemRemoval"
    />

    <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = $event">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete "{{ collection?.name }}"?</AlertDialogTitle>
          <AlertDialogDescription>This will remove the collection and all membership data. Saved queries inside are not deleted.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel @click="showDeleteDialog = false">Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="handleDeleteCollection">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
