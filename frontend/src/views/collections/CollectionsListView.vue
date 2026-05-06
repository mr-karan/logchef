<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import { useRouter } from "vue-router";
import { Plus, Trash2, FolderHeart, Folder, Users, Loader2, AlertCircle } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import type { Collection } from "@/api/collections";

const router = useRouter();
const { toast } = useToast();
const store = useCollectionsStore();

const showCreate = ref(false);
const createName = ref("");
const createDescription = ref("");
const isCreating = ref(false);

const showDeleteDialog = ref(false);
const pendingDelete = ref<Collection | null>(null);

const personal = computed(() => store.personalCollection);
const shared = computed(() => store.sharedCollections);
const isLoading = computed(() => store.isLoadingOperation("listCollections"));

onMounted(async () => {
  await store.fetchCollections();
});

async function handleCreate() {
  if (!createName.value.trim()) return;
  isCreating.value = true;
  try {
    const result = await store.createCollection({
      name: createName.value.trim(),
      description: createDescription.value.trim(),
    });
    if (result.success) {
      showCreate.value = false;
      createName.value = "";
      createDescription.value = "";
    }
  } finally {
    isCreating.value = false;
  }
}

function handleDelete(collection: Collection) {
  if (collection.is_personal) {
    toast({
      title: "Personal collection",
      description: "Personal collections cannot be deleted.",
      variant: "destructive",
      duration: TOAST_DURATION.WARNING,
    });
    return;
  }
  pendingDelete.value = collection;
  showDeleteDialog.value = true;
}

async function confirmDelete() {
  if (!pendingDelete.value) return;
  await store.deleteCollection(pendingDelete.value.id);
  showDeleteDialog.value = false;
  pendingDelete.value = null;
}

function cancelDelete() {
  showDeleteDialog.value = false;
  pendingDelete.value = null;
}

function viewCollection(collection: Collection) {
  router.push({ path: `/logs/collections/${collection.id}`, query: {} });
}
</script>

<template>
  <div class="space-y-5">
    <!-- Page header — tight, no card wrapper -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-lg font-semibold tracking-tight">Collections</h1>
        <p class="text-sm text-muted-foreground">Organize saved queries into shareable lists.</p>
      </div>
      <Button size="sm" @click="showCreate = true">
        <Plus class="mr-1.5 h-3.5 w-3.5" />
        New
      </Button>
    </div>

    <Alert v-if="store.error" variant="destructive">
      <AlertCircle class="h-4 w-4" />
      <AlertDescription>{{ store.error.message }}</AlertDescription>
    </Alert>

    <div v-if="isLoading" class="flex items-center justify-center py-10">
      <Loader2 class="h-5 w-5 animate-spin text-muted-foreground" />
    </div>

    <!-- Grid of collection cards -->
    <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <!-- Personal collection -->
      <button
        v-if="personal"
        type="button"
        class="group flex flex-col gap-1 rounded-lg border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-muted/50"
        @click="viewCollection(personal)"
      >
        <div class="flex items-center gap-2">
          <FolderHeart class="h-4 w-4 text-amber-500 shrink-0" />
          <span class="font-medium text-sm truncate">{{ personal.name }}</span>
        </div>
        <span class="text-xs text-muted-foreground pl-6">
          {{ personal.item_count }} {{ personal.item_count === 1 ? "query" : "queries" }}
        </span>
      </button>

      <!-- Shared collections -->
      <div
        v-for="c in shared"
        :key="c.id"
        class="group relative flex flex-col gap-1 rounded-lg border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-muted/50"
      >
        <button type="button" class="flex flex-col gap-1 text-left" @click="viewCollection(c)">
          <div class="flex items-center gap-2">
            <Folder class="h-4 w-4 text-muted-foreground shrink-0" />
            <span class="font-medium text-sm truncate">{{ c.name }}</span>
            <span v-if="c.caller_role" class="ml-auto text-[10px] uppercase tracking-wider text-muted-foreground font-medium">{{ c.caller_role }}</span>
          </div>
          <span class="text-xs text-muted-foreground pl-6">
            {{ c.item_count }} {{ c.item_count === 1 ? "query" : "queries" }}
            · {{ c.member_count }} {{ c.member_count === 1 ? "member" : "members" }}
          </span>
        </button>
        <Button
          v-if="c.caller_role === 'owner'"
          variant="ghost"
          size="icon"
          class="absolute top-2 right-2 h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
          :disabled="store.isLoadingOperation(`deleteCollection-${c.id}`)"
          @click.stop="handleDelete(c)"
        >
          <Trash2 class="h-3.5 w-3.5 text-destructive" />
        </Button>
      </div>

      <!-- Empty state for shared -->
      <div
        v-if="shared.length === 0"
        class="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-6 text-center col-span-full sm:col-span-1"
      >
        <Folder class="h-5 w-5 text-muted-foreground" />
        <p class="text-xs text-muted-foreground max-w-[200px]">
          No shared collections yet. Create one and invite teammates.
        </p>
      </div>
    </div>

    <Dialog :open="showCreate" @update:open="(val) => !val && (showCreate = false)">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New Collection</DialogTitle>
          <DialogDescription>
            Shared collections live alongside your personal one. You'll be the owner and can invite members later.
          </DialogDescription>
        </DialogHeader>
        <form @submit.prevent="handleCreate" class="space-y-4">
          <div class="grid gap-2">
            <Label for="collection-name">Name</Label>
            <Input id="collection-name" v-model="createName" placeholder="Incident on-call dashboard" required />
          </div>
          <div class="grid gap-2">
            <Label for="collection-description">Description (optional)</Label>
            <Textarea id="collection-description" v-model="createDescription" rows="3" />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" @click="showCreate = false">Cancel</Button>
            <Button type="submit" :disabled="isCreating || !createName.trim()">
              <Loader2 v-if="isCreating" class="mr-2 h-4 w-4 animate-spin" />
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = $event">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete "{{ pendingDelete?.name }}"?</AlertDialogTitle>
          <AlertDialogDescription>Saved queries inside this collection are not deleted.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel @click="cancelDelete">Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="confirmDelete">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
