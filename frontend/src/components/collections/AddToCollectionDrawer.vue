<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { Loader2, Plus, ExternalLink, Check } from "lucide-vue-next";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { useCollectionsStore } from "@/stores/collections";
import { collectionsApi, type CollectionItem } from "@/api/collections";
import { useRouter } from "vue-router";

const props = defineProps<{
  open: boolean;
  queryId: number;
  queryName: string;
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
}>();

const router = useRouter();
const store = useCollectionsStore();

const isLoadingIndex = ref(false);
const isMutating = ref<number | null>(null);
const showInlineCreate = ref(false);
const newName = ref("");
const isCreating = ref(false);

// Map<collectionId, Set<queryId>> — built on open
const itemIndex = ref<Map<number, Set<number>>>(new Map());

const collections = computed(() => store.collections);
const isQueryInCollection = (collectionId: number) =>
  itemIndex.value.get(collectionId)?.has(props.queryId) ?? false;

watch(
  () => props.open,
  async (open) => {
    if (!open) return;
    // Load collections + build item index
    isLoadingIndex.value = true;
    try {
      await store.fetchCollections();
      const index = new Map<number, Set<number>>();
      // Load items for each collection in parallel
      const fetches = collections.value.map(async (c) => {
        try {
          const resp = await collectionsApi.listItems(c.id);
          const ids = new Set((resp.data as CollectionItem[] ?? []).map((i) => i.query.id));
          index.set(c.id, ids);
        } catch {
          index.set(c.id, new Set());
        }
      });
      await Promise.all(fetches);
      itemIndex.value = index;
    } finally {
      isLoadingIndex.value = false;
    }
  }
);

async function toggleItem(collectionId: number) {
  isMutating.value = collectionId;
  try {
    if (isQueryInCollection(collectionId)) {
      await collectionsApi.removeItem(collectionId, props.queryId);
      itemIndex.value.get(collectionId)?.delete(props.queryId);
    } else {
      await collectionsApi.addItem(collectionId, { saved_query_id: props.queryId });
      if (!itemIndex.value.has(collectionId)) {
        itemIndex.value.set(collectionId, new Set());
      }
      itemIndex.value.get(collectionId)!.add(props.queryId);
    }
    // Force reactivity on the index
    itemIndex.value = new Map(itemIndex.value);
    // Refresh collection list so item_count updates in the picker
    await store.fetchCollections();
  } finally {
    isMutating.value = null;
  }
}

async function handleCreate() {
  if (!newName.value.trim()) return;
  isCreating.value = true;
  try {
    const result = await store.createCollection({
      name: newName.value.trim(),
    });
    if (result.success && result.data) {
      // Auto-pin the query to the new collection
      await collectionsApi.addItem(result.data.id, { saved_query_id: props.queryId });
      itemIndex.value.set(result.data.id, new Set([props.queryId]));
      itemIndex.value = new Map(itemIndex.value);
      newName.value = "";
      showInlineCreate.value = false;
      // Refresh counts
      await store.fetchCollections();
    }
  } finally {
    isCreating.value = false;
  }
}

function navigateToCollection(collectionId: number) {
  emit("update:open", false);
  router.push({ path: `/logs/collections/${collectionId}`, query: {} });
}
</script>

<template>
  <Sheet :open="props.open" @update:open="emit('update:open', $event)">
    <SheetContent side="right" class="w-[380px] sm:w-[420px]">
      <SheetHeader>
        <SheetTitle>Add to Collection</SheetTitle>
        <SheetDescription class="truncate">
          {{ queryName }}
        </SheetDescription>
      </SheetHeader>

      <div class="mt-6 space-y-4">
        <div v-if="isLoadingIndex" class="flex items-center justify-center py-8">
          <Loader2 class="h-5 w-5 animate-spin text-muted-foreground" />
          <span class="ml-2 text-sm text-muted-foreground">Loading collections…</span>
        </div>

        <template v-else>
          <div class="space-y-1">
            <button
              v-for="c in collections"
              :key="c.id"
              type="button"
              class="flex w-full items-center justify-between gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors hover:bg-muted"
              :disabled="isMutating === c.id"
              @click="toggleItem(c.id)"
            >
              <div class="flex items-center gap-3 min-w-0">
                <div
                  class="flex h-5 w-5 shrink-0 items-center justify-center rounded border transition-colors"
                  :class="isQueryInCollection(c.id) ? 'bg-primary border-primary text-primary-foreground' : 'border-muted-foreground/30'"
                >
                  <Check v-if="isQueryInCollection(c.id)" class="h-3.5 w-3.5" />
                </div>
                <div class="min-w-0">
                  <div class="truncate font-medium">{{ c.name }}</div>
                  <div class="text-xs text-muted-foreground">
                    {{ c.item_count }} {{ c.item_count === 1 ? "item" : "items" }}
                    <span v-if="!c.is_personal"> · {{ c.member_count }} {{ c.member_count === 1 ? "member" : "members" }}</span>
                  </div>
                </div>
              </div>
              <div class="flex items-center gap-1 shrink-0">
                <Loader2 v-if="isMutating === c.id" class="h-4 w-4 animate-spin text-muted-foreground" />
                <button
                  v-if="!c.is_personal"
                  type="button"
                  class="p-1 rounded hover:bg-muted-foreground/10"
                  title="Manage collection"
                  @click.stop="navigateToCollection(c.id)"
                >
                  <ExternalLink class="h-3.5 w-3.5 text-muted-foreground" />
                </button>
              </div>
            </button>
          </div>

          <Separator />

          <div v-if="!showInlineCreate">
            <Button variant="ghost" size="sm" class="w-full justify-start" @click="showInlineCreate = true">
              <Plus class="mr-2 h-4 w-4" />
              New Collection
            </Button>
          </div>
          <div v-else class="space-y-2 px-1">
            <Label for="new-collection-name">Collection name</Label>
            <div class="flex gap-2">
              <Input
                id="new-collection-name"
                v-model="newName"
                placeholder="e.g. On-call runbook"
                class="h-8 text-sm"
                @keydown.enter="handleCreate"
                @keydown.escape="showInlineCreate = false"
              />
              <Button size="sm" class="h-8 shrink-0" :disabled="isCreating || !newName.trim()" @click="handleCreate">
                <Loader2 v-if="isCreating" class="h-3.5 w-3.5 animate-spin" />
                <span v-else>Add</span>
              </Button>
            </div>
          </div>
        </template>
      </div>
    </SheetContent>
  </Sheet>
</template>
