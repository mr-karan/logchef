<script setup lang="ts">
import { ref, onMounted, computed, watch } from "vue";
import { useRouter, useRoute } from "vue-router";
import {
  Eye,
  Pencil,
  Trash2,
  Loader2,
  Search,
  FolderMinus,
  Link,
  FolderPlus,
  FolderHeart,
  Folder,
  FolderOpen,
  MoreHorizontal,
  FileSearch,
  FileCode,
} from "lucide-vue-next";
import { PageHeader } from "@/components/layout";
import { formatDate } from "@/utils/format";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useToast } from "@/composables/useToast";
import { TOAST_DURATION } from "@/lib/constants";
import { getErrorMessage } from "@/api/types";
import type { SavedQuery } from "@/api/savedQueries";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { useCollectionsStore } from "@/stores/collections";
import { collectionsApi, type CollectionItem } from "@/api/collections";
import { useSavedQueries } from "@/composables/useSavedQueries";
import AddToCollectionDrawer from "@/components/collections/AddToCollectionDrawer.vue";

const router = useRouter();
const route = useRoute();
const { toast } = useToast();

const savedQueriesStore = useSavedQueriesStore();
const collectionsStore = useCollectionsStore();

const localQueries = ref<SavedQuery[]>([]);
const {
  showSaveQueryModal,
  editingQuery,
  isLoading,
  openingQueryId,
  searchQuery,
  getQueryUrl,
  openQuery,
  editQuery,
  deleteQuery,
  clearSearch,
  canManageCollections,
} = useSavedQueries(localQueries);

// Collection picker state: "all" or a numeric collection id
const selectedCollection = ref<string>("all");
const isLoadingItems = ref(false);

// "Add to Collection" drawer state
const showCollectionDrawer = ref(false);
const drawerQueryId = ref(0);
const drawerQueryName = ref("");

const collections = computed(() => collectionsStore.collections);

// Queries to display — either all or filtered by collection
const displayQueries = computed(() => {
  const base = localQueries.value ?? [];
  if (!searchQuery.value.trim()) return base;
  const s = searchQuery.value.toLowerCase();
  return base.filter(
    (q) =>
      q.name.toLowerCase().includes(s) ||
      (q.description && q.description.toLowerCase().includes(s))
  );
});

const queryCount = computed(() => displayQueries.value.length);

const selectedCollectionName = computed(() => {
  if (selectedCollection.value === "all") return "All Queries";
  const c = collections.value.find((x) => x.id === Number(selectedCollection.value));
  return c?.name ?? "Collection";
});

onMounted(async () => {
  await collectionsStore.fetchCollections();
  await loadQueries();
});

watch(selectedCollection, async () => {
  await loadQueries();
});

async function loadQueries() {
  isLoadingItems.value = true;
  try {
    if (selectedCollection.value === "all") {
      const result = await savedQueriesStore.list();
      localQueries.value = result.data ?? [];
    } else {
      const collectionId = Number(selectedCollection.value);
      const resp = await collectionsApi.listItems(collectionId);
      const items: CollectionItem[] = resp.data ?? [];
      localQueries.value = items.map((item) => item.query);
    }
  } catch (error) {
    toast({
      title: "Error",
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
    localQueries.value = [];
  } finally {
    isLoadingItems.value = false;
  }
}

function openCollectionDrawer(query: SavedQuery) {
  drawerQueryId.value = query.id;
  drawerQueryName.value = query.name;
  showCollectionDrawer.value = true;
}

async function handleDeleteQuery(query: SavedQuery) {
  const result = await deleteQuery(query);
  if (result.success) {
    await loadQueries();
  }
}

async function removeFromCurrentCollection(query: SavedQuery) {
  if (selectedCollection.value === "all") return;
  const collectionId = Number(selectedCollection.value);
  await collectionsApi.removeItem(collectionId, query.id);
  await collectionsStore.fetchCollections();
  await loadQueries();
}

function copyShareUrl(query: SavedQuery) {
  const url = `${window.location.origin}/logs/saved/${query.id}`;
  navigator.clipboard.writeText(url).then(() => {
    toast({ title: "Link copied", duration: TOAST_DURATION.SUCCESS });
  });
}

function manageCollection() {
  if (selectedCollection.value !== "all") {
    router.push({ path: `/logs/collections/${selectedCollection.value}`, query: {} });
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader title="Saved Queries" description="Search and run queries you've saved across collections.">
      <template #actions>
        <Button variant="outline" size="sm" @click="router.push({ path: '/logs/collections', query: {} })">
          <FolderOpen class="mr-2 h-4 w-4" />
          Manage collections
        </Button>
      </template>
    </PageHeader>

    <!-- Filter row: collection picker + search -->
    <div class="flex items-center gap-3">
      <Select v-model="selectedCollection">
        <SelectTrigger class="w-[220px] h-9">
          <div class="flex items-center gap-2">
            <FolderHeart v-if="selectedCollection !== 'all' && collections.find(c => c.id === Number(selectedCollection))?.is_personal" class="h-3.5 w-3.5 text-amber-500 shrink-0" />
            <Folder v-else-if="selectedCollection !== 'all'" class="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <SelectValue>{{ selectedCollectionName }}</SelectValue>
          </div>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All Queries</SelectItem>
          <SelectItem
            v-for="c in collections"
            :key="c.id"
            :value="String(c.id)"
          >
            <span class="flex items-center gap-2">
              <FolderHeart v-if="c.is_personal" class="h-3.5 w-3.5 text-amber-500" />
              <Folder v-else class="h-3.5 w-3.5 text-muted-foreground" />
              {{ c.name }}
              <span class="text-xs text-muted-foreground ml-1">({{ c.item_count }})</span>
            </span>
          </SelectItem>
        </SelectContent>
      </Select>

      <div class="relative flex-1 max-w-sm">
        <Search class="absolute left-2.5 top-2 h-4 w-4 text-muted-foreground" />
        <Input
          v-model="searchQuery"
          type="search"
          placeholder="Search by name or description…"
          class="pl-8 h-9"
        />
      </div>

      <Button
        v-if="selectedCollection !== 'all'"
        variant="outline"
        size="sm"
        class="h-9"
        @click="manageCollection"
      >
        Manage
      </Button>
    </div>

    <!-- Query count -->
    <p class="text-xs text-muted-foreground">
      {{ queryCount }} {{ queryCount === 1 ? "query" : "queries" }}
    </p>

    <!-- Loading -->
    <div v-if="isLoadingItems || isLoading" class="flex items-center justify-center py-12">
      <Loader2 class="h-5 w-5 animate-spin text-muted-foreground" />
    </div>

    <!-- Empty state -->
    <div v-else-if="queryCount === 0" class="flex flex-col items-center justify-center py-16 gap-3">
      <Search class="h-8 w-8 text-muted-foreground/50" />
      <p class="text-sm text-muted-foreground">
        {{ searchQuery ? "No queries match your search." : selectedCollection === "all" ? "No saved queries yet. Save a query from the Explorer." : "This collection is empty. Pin queries from All Queries." }}
      </p>
      <Button v-if="searchQuery" variant="outline" size="sm" @click="clearSearch">Clear search</Button>
    </div>

    <!-- Metabase-style flat table -->
    <div v-else class="rounded-md border">
      <table class="w-full text-sm">
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
            v-for="query in displayQueries"
            :key="query.id"
            class="border-b last:border-0 hover:bg-muted/40 transition-colors group"
          >
            <td class="px-4 py-3">
              <FileSearch v-if="query.query_type === 'logchefql'" class="h-4 w-4 text-muted-foreground" title="LogchefQL" />
              <FileCode v-else class="h-4 w-4 text-muted-foreground" title="SQL" />
            </td>
            <td class="px-4 py-3">
              <a
                :href="getQueryUrl(query)"
                class="font-medium text-foreground hover:underline cursor-pointer"
                @click.prevent="openingQueryId === null && openQuery(query)"
              >
                <span class="flex items-center gap-2">
                  <Loader2 v-if="openingQueryId === query.id" class="h-3.5 w-3.5 animate-spin" />
                  {{ query.name }}
                </span>
              </a>
              <p v-if="query.description" class="text-xs text-muted-foreground mt-0.5 truncate max-w-[400px]">
                {{ query.description }}
              </p>
            </td>
            <td class="px-4 py-3 text-muted-foreground text-xs">
              {{ query.source_name || '-' }}
            </td>
            <td class="px-4 py-3 text-muted-foreground text-xs">
              {{ formatDate(query.updated_at) }}
            </td>
            <td class="px-4 py-3">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" class="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity">
                    <MoreHorizontal class="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem @click="openQuery(query)">
                    <Eye class="mr-2 h-4 w-4" /> Open
                  </DropdownMenuItem>
                  <DropdownMenuItem @click="openCollectionDrawer(query)">
                    <FolderPlus class="mr-2 h-4 w-4" /> Add to Collection
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    v-if="selectedCollection !== 'all'"
                    @click="removeFromCurrentCollection(query)"
                    class="text-destructive"
                  >
                    <FolderMinus class="mr-2 h-4 w-4" /> Remove from {{ selectedCollectionName }}
                  </DropdownMenuItem>
                  <DropdownMenuItem @click="copyShareUrl(query)">
                    <Link class="mr-2 h-4 w-4" /> Copy Link
                  </DropdownMenuItem>
                  <DropdownMenuItem v-if="canManageCollections" @click="editQuery(query)">
                    <Pencil class="mr-2 h-4 w-4" /> Edit
                  </DropdownMenuItem>
                  <DropdownMenuItem v-if="canManageCollections" @click="handleDeleteQuery(query)" class="text-destructive">
                    <Trash2 class="mr-2 h-4 w-4" /> Delete
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AddToCollectionDrawer
      :open="showCollectionDrawer"
      :query-id="drawerQueryId"
      :query-name="drawerQueryName"
      @update:open="showCollectionDrawer = $event"
    />
  </div>
</template>
