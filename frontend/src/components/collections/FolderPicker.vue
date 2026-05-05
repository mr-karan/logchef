<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { FolderPlus, Loader2, Plus } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { QUERY_FOLDER_COLORS, type QueryFolder, type QueryFolderColor } from "@/api/queryFolders";

const props = defineProps<{
  folders: QueryFolder[];
  modelValue: number[];
  canManage: boolean;
  isCreating?: boolean;
}>();

const emit = defineEmits<{
  (e: "update:modelValue", value: number[]): void;
  (e: "createFolder", payload: { name: string; color: QueryFolderColor }): void;
}>();

const newFolderName = shallowRef("");
const newFolderColor = shallowRef<QueryFolderColor>("blue");

const selectedIds = computed(() => new Set(props.modelValue));

const colorClasses: Record<QueryFolderColor, string> = {
  gray: "bg-gray-500",
  red: "bg-red-500",
  orange: "bg-orange-500",
  amber: "bg-amber-500",
  yellow: "bg-yellow-500",
  green: "bg-green-500",
  teal: "bg-teal-500",
  cyan: "bg-cyan-500",
  blue: "bg-blue-500",
  indigo: "bg-indigo-500",
  violet: "bg-violet-500",
  pink: "bg-pink-500",
};

function toggleFolder(folderId: number, checked: boolean) {
  if (checked) {
    emit("update:modelValue", [...selectedIds.value, folderId]);
    return;
  }
  emit("update:modelValue", props.modelValue.filter((id) => id !== folderId));
}

function createFolder() {
  const name = newFolderName.value.trim();
  if (!name) return;
  emit("createFolder", { name, color: newFolderColor.value });
  newFolderName.value = "";
}
</script>

<template>
  <div class="space-y-3 rounded-md border p-3">
    <div class="flex items-center justify-between gap-2">
      <div>
        <Label>Folders</Label>
        <p class="text-xs text-muted-foreground">Optional. A query can be in multiple folders.</p>
      </div>
      <FolderPlus class="h-4 w-4 text-muted-foreground" />
    </div>

    <div v-if="folders.length" class="grid max-h-40 gap-2 overflow-auto pr-1">
      <label
        v-for="folder in folders"
        :key="folder.id"
        class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted/60"
      >
        <Checkbox
          :checked="selectedIds.has(folder.id)"
          @update:checked="toggleFolder(folder.id, Boolean($event))"
        />
        <span class="h-2.5 w-2.5 rounded-full" :class="colorClasses[folder.color]" />
        <span class="min-w-0 flex-1 truncate text-sm">{{ folder.name }}</span>
      </label>
    </div>
    <div v-else class="rounded-md bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
      No folders yet.
    </div>

    <div v-if="canManage" class="space-y-2 border-t pt-3">
      <div class="flex gap-2">
        <Input
          v-model="newFolderName"
          placeholder="Create folder"
          class="h-8"
          @keydown.enter.prevent="createFolder"
        />
        <Button type="button" size="sm" :disabled="isCreating || !newFolderName.trim()" @click="createFolder">
          <Loader2 v-if="isCreating" class="mr-1 h-3.5 w-3.5 animate-spin" />
          <Plus v-else class="mr-1 h-3.5 w-3.5" />
          Add
        </Button>
      </div>
      <div class="flex flex-wrap gap-1.5">
        <button
          v-for="color in QUERY_FOLDER_COLORS"
          :key="color"
          type="button"
          class="h-5 w-5 rounded-full border transition-transform hover:scale-110"
          :class="[colorClasses[color], newFolderColor === color ? 'ring-2 ring-ring ring-offset-2' : '']"
          :title="color"
          @click="newFolderColor = color"
        />
      </div>
    </div>
  </div>
</template>
