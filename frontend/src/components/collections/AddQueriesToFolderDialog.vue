<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import type { SavedTeamQuery } from "@/api/savedQueries";
import type { QueryFolder } from "@/api/queryFolders";

const props = defineProps<{
  open: boolean;
  folder: QueryFolder | null;
  queries: SavedTeamQuery[];
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "submit", queryIds: number[]): void;
}>();

const search = shallowRef("");
const selectedIds = shallowRef<Set<number>>(new Set());

const availableQueries = computed(() => {
  const folderId = props.folder?.id;
  const term = search.value.trim().toLowerCase();
  return props.queries
    .filter((query) => !folderId || !(query.folders ?? []).some((folder) => folder.id === folderId))
    .filter((query) => {
      if (!term) return true;
      return query.name.toLowerCase().includes(term) || (query.description ?? "").toLowerCase().includes(term);
    });
});

watch(
  () => props.open,
  (open) => {
    if (!open) return;
    search.value = "";
    selectedIds.value = new Set();
  }
);

function toggle(queryId: number, checked: boolean) {
  const next = new Set(selectedIds.value);
  if (checked) {
    next.add(queryId);
  } else {
    next.delete(queryId);
  }
  selectedIds.value = next;
}

function submit() {
  emit("submit", Array.from(selectedIds.value));
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-[560px]">
      <DialogHeader>
        <DialogTitle>Add Queries</DialogTitle>
        <DialogDescription>
          Add existing collections to {{ folder?.name ?? "this folder" }}.
        </DialogDescription>
      </DialogHeader>

      <div class="space-y-3">
        <Input v-model="search" placeholder="Search collections..." />
        <div class="max-h-72 space-y-1 overflow-auto rounded-md border p-2">
          <label
            v-for="query in availableQueries"
            :key="query.id"
            class="flex cursor-pointer items-start gap-3 rounded-md p-2 hover:bg-muted"
          >
            <Checkbox
              class="mt-0.5"
              :checked="selectedIds.has(query.id)"
              @update:checked="toggle(query.id, Boolean($event))"
            />
            <span class="min-w-0">
              <span class="block truncate text-sm font-medium">{{ query.name }}</span>
              <span class="block truncate text-xs text-muted-foreground">{{ query.description || `Source ${query.source_id}` }}</span>
            </span>
          </label>
          <div v-if="!availableQueries.length" class="px-3 py-6 text-center text-sm text-muted-foreground">
            No matching unfiled queries for this folder.
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('update:open', false)">Cancel</Button>
        <Button :disabled="selectedIds.size === 0" @click="submit">
          Add {{ selectedIds.size || "" }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
