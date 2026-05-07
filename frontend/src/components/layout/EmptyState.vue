<script setup lang="ts">
import type { Component, HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

defineProps<{
  icon?: Component;
  title?: string;
  description?: string;
  class?: HTMLAttributes["class"];
}>();
</script>

<template>
  <div
    data-slot="empty-state"
    :class="cn('flex flex-col items-center justify-center gap-3 py-10 px-4 text-center', $props.class)"
  >
    <component v-if="icon" :is="icon" class="h-8 w-8 text-muted-foreground/60" />
    <div class="space-y-1">
      <p v-if="title" class="text-sm font-medium">{{ title }}</p>
      <p v-if="description" class="text-sm text-muted-foreground">{{ description }}</p>
    </div>
    <div v-if="$slots.action" class="pt-1">
      <slot name="action" />
    </div>
  </div>
</template>
