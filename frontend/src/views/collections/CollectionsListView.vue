<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import { useRouter } from "vue-router";
import { Plus, Trash2, FolderHeart, Folder, Users, Loader2, AlertCircle } from "lucide-vue-next";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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

async function handleDelete(collection: Collection) {
  if (collection.is_personal) {
    toast({
      title: "Personal collection",
      description: "Personal collections cannot be deleted.",
      variant: "destructive",
      duration: TOAST_DURATION.WARNING,
    });
    return;
  }
  if (!window.confirm(`Delete collection "${collection.name}"? Saved queries inside it will not be deleted.`)) return;
  await store.deleteCollection(collection.id);
}

function viewCollection(collection: Collection) {
  router.push(`/logs/collections/${collection.id}`);
}
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <div class="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <CardTitle>Collections</CardTitle>
            <CardDescription>
              Curated lists of saved queries. Each user has a personal collection plus any shared collections they own or have been invited to.
            </CardDescription>
          </div>
          <Button @click="showCreate = true">
            <Plus class="mr-2 h-4 w-4" />
            New Collection
          </Button>
        </div>
      </CardHeader>

      <CardContent class="space-y-6">
        <Alert v-if="store.error.value" variant="destructive">
          <AlertCircle class="h-4 w-4" />
          <AlertDescription>{{ store.error.value.message }}</AlertDescription>
        </Alert>

        <div v-if="isLoading" class="flex items-center justify-center py-10">
          <Loader2 class="h-6 w-6 animate-spin text-primary" />
          <p class="ml-2 text-muted-foreground">Loading collections…</p>
        </div>

        <template v-else>
          <section v-if="personal" class="space-y-3">
            <h3 class="text-sm font-semibold text-muted-foreground">Personal</h3>
            <button
              type="button"
              class="flex w-full items-center justify-between gap-4 rounded-md border bg-card p-4 text-left transition-colors hover:bg-muted"
              @click="viewCollection(personal)"
            >
              <div class="flex items-center gap-3">
                <FolderHeart class="h-5 w-5 text-amber-500" />
                <div>
                  <div class="font-medium">{{ personal.name }}</div>
                  <p class="text-sm text-muted-foreground">
                    {{ personal.item_count }} {{ personal.item_count === 1 ? "query" : "queries" }}
                  </p>
                </div>
              </div>
            </button>
          </section>

          <section class="space-y-3">
            <h3 class="text-sm font-semibold text-muted-foreground">Shared</h3>
            <div v-if="shared.length === 0" class="rounded-md border p-6 text-center">
              <Folder class="mx-auto mb-2 h-6 w-6 text-muted-foreground" />
              <p class="text-sm text-muted-foreground">
                You're not in any shared collections yet. Create one and invite teammates by user id.
              </p>
            </div>
            <div v-else class="space-y-2">
              <div
                v-for="c in shared"
                :key="c.id"
                class="flex items-center justify-between gap-4 rounded-md border bg-card p-4 transition-colors hover:bg-muted"
              >
                <button
                  type="button"
                  class="flex flex-1 items-center gap-3 text-left"
                  @click="viewCollection(c)"
                >
                  <Folder class="h-5 w-5 text-muted-foreground" />
                  <div>
                    <div class="font-medium">{{ c.name }}</div>
                    <p class="text-sm text-muted-foreground">
                      {{ c.item_count }} {{ c.item_count === 1 ? "query" : "queries" }}
                      · <Users class="inline h-3 w-3 align-middle" />
                      {{ c.member_count }} {{ c.member_count === 1 ? "member" : "members" }}
                      <span v-if="c.caller_role" class="ml-2 inline-flex items-center rounded bg-muted px-1.5 py-0.5 text-xs">{{ c.caller_role }}</span>
                    </p>
                  </div>
                </button>
                <Button
                  v-if="c.caller_role === 'owner'"
                  variant="ghost"
                  size="icon"
                  :disabled="store.isLoadingOperation(`deleteCollection-${c.id}`)"
                  @click.stop="handleDelete(c)"
                >
                  <Trash2 class="h-4 w-4 text-destructive" />
                </Button>
              </div>
            </div>
          </section>
        </template>
      </CardContent>
    </Card>

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
  </div>
</template>
