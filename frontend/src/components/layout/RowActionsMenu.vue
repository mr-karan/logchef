<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Copy, MoreHorizontal, Trash2 } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

// Overflow menu for a list row or card. Primary actions (edit, open) stay as
// inline icon buttons; this menu holds the secondary ones, with Delete last.
// Extra items go in the default slot, above Duplicate. An action shows only
// when the parent listens for it (`@duplicate`, `@delete`). Vue passes those
// listeners as the onDuplicate / onDelete props.
const props = defineProps<{
  onDuplicate?: () => void;
  onDelete?: () => void;
  deleteLabel?: string;
}>();

const { t } = useI18n();
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child @click.stop>
      <Button variant="ghost" size="icon" class="h-8 w-8" :aria-label="t('ui.actions')" :title="t('ui.actions')">
        <MoreHorizontal class="h-4 w-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="w-40" @click.stop>
      <slot />
      <DropdownMenuItem v-if="props.onDuplicate" @click="props.onDuplicate()">
        <Copy class="mr-2 h-4 w-4" />
        {{ t('ui.duplicate') }}
      </DropdownMenuItem>
      <template v-if="props.onDelete">
        <DropdownMenuSeparator v-if="props.onDuplicate || $slots.default" />
        <DropdownMenuItem class="text-destructive focus:text-destructive" @click="props.onDelete()">
          <Trash2 class="mr-2 h-4 w-4" />
          {{ deleteLabel ?? t('ui.delete') }}
        </DropdownMenuItem>
      </template>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
