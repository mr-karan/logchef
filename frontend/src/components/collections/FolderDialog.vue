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
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { QUERY_FOLDER_COLORS, type QueryFolder, type QueryFolderColor } from "@/api/queryFolders";
import { folderDotClass } from "./folderColors";

const props = defineProps<{
  open: boolean;
  folder?: QueryFolder | null;
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "submit", payload: { name: string; description: string; color: QueryFolderColor }): void;
}>();

const name = shallowRef("");
const description = shallowRef("");
const color = shallowRef<QueryFolderColor>("blue");

const isEditing = computed(() => !!props.folder);
const isValid = computed(() => name.value.trim().length > 0);

watch(
  () => [props.open, props.folder] as const,
  () => {
    if (!props.open) return;
    name.value = props.folder?.name ?? "";
    description.value = props.folder?.description ?? "";
    color.value = props.folder?.color ?? "blue";
  },
  { immediate: true }
);

function submit() {
  if (!isValid.value) return;
  emit("submit", {
    name: name.value.trim(),
    description: description.value.trim(),
    color: color.value,
  });
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-[460px]">
      <DialogHeader>
        <DialogTitle>{{ isEditing ? "Edit Folder" : "Create Folder" }}</DialogTitle>
        <DialogDescription>
          Group related saved queries for this team.
        </DialogDescription>
      </DialogHeader>

      <div class="space-y-4">
        <div class="grid gap-2">
          <Label for="folder-name">Name</Label>
          <Input id="folder-name" v-model="name" placeholder="Email Logs" @keydown.enter.prevent="submit" />
        </div>

        <div class="grid gap-2">
          <Label for="folder-description">Description</Label>
          <Textarea id="folder-description" v-model="description" rows="3" placeholder="Optional context for this folder" />
        </div>

        <div class="grid gap-2">
          <Label>Color</Label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="item in QUERY_FOLDER_COLORS"
              :key="item"
              type="button"
              class="h-6 w-6 rounded-full border transition-transform hover:scale-110"
              :class="[folderDotClass[item], color === item ? 'ring-2 ring-ring ring-offset-2' : '']"
              :title="item"
              @click="color = item"
            />
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('update:open', false)">Cancel</Button>
        <Button :disabled="!isValid" @click="submit">{{ isEditing ? "Update" : "Create" }}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
