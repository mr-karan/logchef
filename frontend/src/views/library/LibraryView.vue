<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Plus, Search, User, Users, Loader2 } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useCollectionsStore } from "@/stores/collections";
import type { Collection, CollectionRole } from "@/api/collections";
import CollectionDetailPane from "./CollectionDetailPane.vue";

const route = useRoute();
const router = useRouter();
const store = useCollectionsStore();

const personal = computed(() => store.personalCollection);
const shared = computed(() => store.sharedCollections);
const isLoading = computed(() => store.isLoadingOperation("listCollections"));

const search = ref("");
const filteredShared = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return shared.value;
  return shared.value.filter((c) => c.name.toLowerCase().includes(q));
});

// Selected collection = route param, falling back to the personal collection.
const selectedId = computed(() => {
  const param = Number(route.params.collectionID);
  if (param) return param;
  return personal.value?.id ?? null;
});

function select(id: number) {
  router.push({ path: `/logs/library/${id}`, query: {} });
}

// Role accent — owner=amber, editor=teal, member=muted. Encodes capability at a glance.
function roleClass(role?: CollectionRole): string {
  if (role === "owner") return "text-amber-500";
  if (role === "editor") return "text-teal-400";
  return "text-muted-foreground";
}

onMounted(async () => {
  await store.fetchCollections();
  // Land on the personal collection when no specific one is requested.
  if (!Number(route.params.collectionID) && personal.value) {
    router.replace({ path: `/logs/library/${personal.value.id}`, query: {} });
  }
});

// New collection
const showCreate = ref(false);
const createName = ref("");
const createDescription = ref("");
const isCreating = ref(false);

async function handleCreate() {
  if (!createName.value.trim()) return;
  isCreating.value = true;
  try {
    const result = await store.createCollection({
      name: createName.value.trim(),
      description: createDescription.value.trim(),
    });
    if (result.success && result.data) {
      showCreate.value = false;
      createName.value = "";
      createDescription.value = "";
      select((result.data as Collection).id);
    }
  } finally {
    isCreating.value = false;
  }
}

// When the open collection is deleted, drop back to the personal collection.
function onDeleted() {
  if (personal.value) router.replace({ path: `/logs/library/${personal.value.id}`, query: {} });
  else router.replace({ path: "/logs/library", query: {} });
}

// Keep landing logic correct if collections arrive after mount.
watch(personal, (p) => {
  if (p && !Number(route.params.collectionID)) {
    router.replace({ path: `/logs/library/${p.id}`, query: {} });
  }
});
</script>

<template>
  <div class="grid lg:grid-cols-[280px_minmax(0,1fr)] gap-5">
    <!-- Collections rail -->
    <aside class="lg:border-r lg:pr-5">
      <div class="flex items-center justify-between mb-3">
        <h1 class="text-lg font-semibold tracking-tight">Library</h1>
        <Button size="sm" variant="outline" @click="showCreate = true">
          <Plus class="mr-1.5 h-3.5 w-3.5" />
          New
        </Button>
      </div>

      <div class="relative mb-3">
        <Search class="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
        <Input v-model="search" placeholder="Search collections…" class="pl-8 h-9" />
      </div>

      <div v-if="isLoading" class="flex justify-center py-8">
        <Loader2 class="h-5 w-5 animate-spin text-muted-foreground" />
      </div>

      <nav v-else class="space-y-4">
        <!-- Personal -->
        <div v-if="personal">
          <p class="px-2 mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/70">Yours</p>
          <button
            type="button"
            class="flex w-full items-center gap-2.5 rounded-md border px-2.5 py-2 text-left transition-colors"
            :class="selectedId === personal.id ? 'border-primary/40 bg-primary/10' : 'border-transparent hover:bg-muted/60'"
            @click="select(personal.id)"
          >
            <User class="h-4 w-4 text-muted-foreground shrink-0" />
            <span class="flex-1 truncate text-sm font-medium">{{ personal.name }}</span>
            <span class="text-xs text-muted-foreground tabular-nums">{{ personal.item_count }}</span>
          </button>
        </div>

        <!-- Shared -->
        <div>
          <p class="px-2 mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/70">Shared</p>
          <div class="space-y-1">
            <button
              v-for="c in filteredShared"
              :key="c.id"
              type="button"
              class="flex w-full items-center gap-2.5 rounded-md border px-2.5 py-2 text-left transition-colors"
              :class="selectedId === c.id ? 'border-primary/40 bg-primary/10' : 'border-transparent hover:bg-muted/60'"
              @click="select(c.id)"
            >
              <Users class="h-4 w-4 text-muted-foreground shrink-0" />
              <span class="flex-1 truncate text-sm font-medium">{{ c.name }}</span>
              <span
                v-if="c.caller_role"
                class="text-[10px] font-semibold uppercase tracking-wider"
                :class="roleClass(c.caller_role)"
              >{{ c.caller_role }}</span>
            </button>
            <p
              v-if="filteredShared.length === 0"
              class="px-2.5 py-3 text-xs text-muted-foreground"
            >
              {{ search ? "No collections match." : "No shared collections yet. Create one and invite teammates." }}
            </p>
          </div>
        </div>
      </nav>
    </aside>

    <!-- Selected collection detail -->
    <section class="min-w-0">
      <CollectionDetailPane
        v-if="selectedId"
        :key="selectedId"
        :collection-id="selectedId"
        @deleted="onDeleted"
      />
    </section>

    <Dialog :open="showCreate" @update:open="(val) => !val && (showCreate = false)">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New collection</DialogTitle>
          <DialogDescription>
            Shared collections live alongside your personal one. You'll be the owner and can invite
            members and editors afterwards.
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
