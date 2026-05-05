<script setup lang="ts">
import { Bookmark, Folder, Inbox, Layers, Pencil, Plus, Trash2 } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import type { QueryFolder } from "@/api/queryFolders";
import { folderDotClass } from "./folderColors";

export type FolderSystemView = "all" | "bookmarked" | "unfiled";

defineProps<{
  folders: QueryFolder[];
  activeView: FolderSystemView | "folder";
  activeFolderId: number | null;
  canManage: boolean;
  allCount: number;
  bookmarkedCount: number;
  unfiledCount: number;
}>();

const emit = defineEmits<{
  (e: "selectSystem", view: FolderSystemView): void;
  (e: "selectFolder", folderId: number): void;
  (e: "createFolder"): void;
  (e: "editFolder", folder: QueryFolder): void;
  (e: "deleteFolder", folder: QueryFolder): void;
}>();

const systemItems = [
  { id: "all", label: "All", icon: Layers },
  { id: "bookmarked", label: "Bookmarked", icon: Bookmark },
  { id: "unfiled", label: "Unfiled", icon: Inbox },
] as const;
</script>

<template>
  <aside class="rounded-md border bg-background p-2">
    <div class="mb-2 flex items-center justify-between px-2 py-1">
      <div class="text-xs font-medium uppercase text-muted-foreground">Folders</div>
      <Button v-if="canManage" variant="ghost" size="icon" class="h-7 w-7" @click="emit('createFolder')">
        <Plus class="h-4 w-4" />
      </Button>
    </div>

    <div class="space-y-1">
      <button
        v-for="item in systemItems"
        :key="item.id"
        type="button"
        class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
        :class="activeView === item.id ? 'bg-muted font-medium text-foreground' : 'text-muted-foreground'"
        @click="emit('selectSystem', item.id)"
      >
        <component :is="item.icon" class="h-4 w-4" />
        <span class="min-w-0 flex-1 truncate">{{ item.label }}</span>
        <span class="text-xs">{{ item.id === 'all' ? allCount : item.id === 'bookmarked' ? bookmarkedCount : unfiledCount }}</span>
      </button>
    </div>

    <div class="my-2 h-px bg-border" />

    <div v-if="folders.length" class="space-y-1">
      <div
        v-for="folder in folders"
        :key="folder.id"
        class="group flex items-center gap-1"
      >
        <button
          type="button"
          class="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
          :class="activeView === 'folder' && activeFolderId === folder.id ? 'bg-muted font-medium text-foreground' : 'text-muted-foreground'"
          @click="emit('selectFolder', folder.id)"
        >
          <span class="h-2.5 w-2.5 rounded-full" :class="folderDotClass[folder.color]" />
          <Folder class="h-4 w-4" />
          <span class="min-w-0 flex-1 truncate">{{ folder.name }}</span>
          <span class="text-xs">{{ folder.query_count }}</span>
        </button>
        <template v-if="canManage">
          <Button variant="ghost" size="icon" class="h-7 w-7 opacity-0 group-hover:opacity-100" @click="emit('editFolder', folder)">
            <Pencil class="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" size="icon" class="h-7 w-7 opacity-0 group-hover:opacity-100" @click="emit('deleteFolder', folder)">
            <Trash2 class="h-3.5 w-3.5 text-destructive" />
          </Button>
        </template>
      </div>
    </div>
    <div v-else class="px-2 py-3 text-sm text-muted-foreground">
      No folders yet.
    </div>
  </aside>
</template>
