<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { Search } from "lucide-vue-next";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

// Search box for list and table views. The width comes from `class`; the
// height and icon placement are the same in every list.
defineProps<{
  placeholder: string;
  class?: HTMLAttributes["class"];
}>();

const model = defineModel<string>({ required: true });
</script>

<template>
  <div data-slot="search-input" :class="cn('relative w-full max-w-sm', $props.class)">
    <Search class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
    <Input v-model="model" type="search" :placeholder="placeholder" :aria-label="placeholder" class="h-9 pl-8" />
  </div>
</template>
