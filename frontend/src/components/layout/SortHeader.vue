<script setup lang="ts">
import { computed } from "vue";
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-vue-next";
import type { SortDir } from "@/composables/useTableSearchSort";

// Clickable column header for tables sorted with useTableSearchSort.
const props = defineProps<{
  column: string;
  sortKey: string;
  sortDir: SortDir;
}>();

const emit = defineEmits<{ toggle: [column: string] }>();

const icon = computed(() => {
  if (props.sortKey !== props.column) return ArrowUpDown;
  return props.sortDir === "asc" ? ArrowUp : ArrowDown;
});
</script>

<template>
  <button
    type="button"
    class="inline-flex items-center gap-1 hover:text-foreground"
    @click="emit('toggle', column)"
  >
    <slot />
    <component :is="icon" class="size-3.5 opacity-60" />
  </button>
</template>
