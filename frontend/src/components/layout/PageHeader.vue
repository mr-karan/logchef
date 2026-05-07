<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

defineProps<{
  title: string;
  description?: string;
  class?: HTMLAttributes["class"];
}>();
</script>

<template>
  <div
    data-slot="page-header"
    :class="cn('flex flex-col gap-2 md:flex-row md:items-start md:justify-between', $props.class)"
  >
    <div class="space-y-1 min-w-0">
      <h1 class="text-xl font-semibold tracking-tight truncate">{{ title }}</h1>
      <p v-if="description" class="text-sm text-muted-foreground">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2 shrink-0">
      <slot name="actions" />
    </div>
  </div>
</template>
