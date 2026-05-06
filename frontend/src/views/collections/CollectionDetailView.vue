<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { storeToRefs } from "pinia";
import {
  ArrowLeft,
  Loader2,
  Lock,
  Pencil,
  Trash2,
  UserPlus,
  Users,
  X,
  AlertCircle,
} from "lucide-vue-next";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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

const collectionID = computed(() => Number(route.params.collectionID));
const collection = computed(() => store.collections.find((c) => c.id === collectionID.value) ?? null);
const items = computed(() => data.value.items[collectionID.value] ?? []);
const members = computed(() => data.value.members[collectionID.value] ?? []);

const isOwner = computed(() => collection.value?.caller_role === "owner" || authStore.user?.role === "admin");

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
  await Promise.all([store.fetchItems(collectionID.value), store.fetchMembers(collectionID.value)]);
}

onMounted(async () => {
  await load();
  // Load users list for the invite dropdown
  await usersStore.loadUsers();
});
watch(collectionID, load);

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
    <div class="flex items-center gap-2">
      <Button variant="ghost" size="sm" @click="router.push({ path: '/logs/collections', query: {} })">
        <ArrowLeft class="mr-2 h-4 w-4" />
        Back to Collections
      </Button>
    </div>

    <Card v-if="!collection && !store.isLoading">
      <CardHeader>
        <CardTitle>Collection not found</CardTitle>
        <CardDescription>It may have been deleted or you may not be a member.</CardDescription>
      </CardHeader>
    </Card>

    <template v-else-if="collection">
      <Card>
        <CardHeader>
          <div class="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
            <div class="space-y-1">
              <CardTitle class="flex items-center gap-2">
                {{ collection.name }}
                <Badge v-if="collection.is_personal" variant="secondary">personal</Badge>
                <Badge v-else-if="collection.caller_role" variant="outline">{{ collection.caller_role }}</Badge>
              </CardTitle>
              <CardDescription v-if="collection.description">{{ collection.description }}</CardDescription>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <Button v-if="isOwner && !collection.is_personal" variant="outline" size="sm" @click="openRename">
                <Pencil class="mr-2 h-4 w-4" />
                Rename
              </Button>
              <Button v-if="isOwner && !collection.is_personal" variant="outline" size="sm" @click="showAddMember = true">
                <UserPlus class="mr-2 h-4 w-4" />
                Invite member
              </Button>
              <Button v-if="isOwner && !collection.is_personal" variant="destructive" size="sm" @click="showDeleteDialog = true">
                <Trash2 class="mr-2 h-4 w-4" />
                Delete
              </Button>
            </div>
          </div>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Items</CardTitle>
          <CardDescription>
            Saved queries pinned to this collection. Items you can't run for this source show with a lock icon.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Alert v-if="store.error" variant="destructive">
            <AlertCircle class="h-4 w-4" />
            <AlertDescription>{{ store.error.message }}</AlertDescription>
          </Alert>
          <div v-if="store.isLoadingOperation(`listItems-${collectionID}`)" class="flex items-center justify-center py-6">
            <Loader2 class="h-5 w-5 animate-spin text-primary" />
          </div>
          <div v-else-if="items.length === 0" class="rounded-md border p-6 text-center text-sm text-muted-foreground">
            No queries pinned yet. From a saved query you can edit, add it here.
          </div>
          <ul v-else class="divide-y rounded-md border">
            <li
              v-for="item in items"
              :key="item.query.id"
              class="flex items-center gap-3 px-4 py-3"
            >
              <Lock v-if="!item.runnable" class="h-4 w-4 text-muted-foreground" :title="'You cannot run this query (no source access).'" />
              <div class="min-w-0 flex-1">
                <button
                  type="button"
                  class="block truncate text-left text-sm font-medium hover:underline disabled:cursor-not-allowed disabled:hover:no-underline"
                  :class="!item.runnable && 'text-muted-foreground'"
                  :disabled="!item.runnable"
                  @click="openQuery(item.query.id)"
                >
                  {{ item.query.name }}
                </button>
                <p class="truncate text-xs text-muted-foreground">
                  {{ item.query.source_name || `source ${item.query.source_id}` }} ·
                  {{ item.query.query_type === "logchefql" ? "Search" : "SQL" }}
                </p>
              </div>
              <Button
                v-if="isOwner"
                variant="ghost"
                size="icon"
                @click="handleRemoveItem(item.query.id)"
              >
                <Trash2 class="h-4 w-4 text-destructive" />
              </Button>
            </li>
          </ul>
        </CardContent>
      </Card>

      <Card v-if="!collection.is_personal">
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <Users class="h-4 w-4" /> Members
          </CardTitle>
          <CardDescription>
            Owners can invite new members and adjust roles. Members can read items they have source access to.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div v-if="store.isLoadingOperation(`listMembers-${collectionID}`)" class="flex items-center justify-center py-6">
            <Loader2 class="h-5 w-5 animate-spin text-primary" />
          </div>
          <ul v-else class="divide-y rounded-md border">
            <li v-for="m in members" :key="m.user_id" class="flex items-center justify-between gap-3 px-4 py-3">
              <div class="min-w-0">
                <p class="truncate text-sm font-medium">{{ m.full_name || m.email || `User ${m.user_id}` }}</p>
                <p class="truncate text-xs text-muted-foreground">{{ m.email }}</p>
              </div>
              <div class="flex items-center gap-2">
                <Badge variant="outline">{{ m.role }}</Badge>
                <Button
                  v-if="isOwner && m.user_id !== authStore.user?.id"
                  variant="ghost"
                  size="icon"
                  @click="handleRemoveMember(m.user_id)"
                >
                  <X class="h-4 w-4 text-destructive" />
                </Button>
              </div>
            </li>
          </ul>
        </CardContent>
      </Card>
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
