<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

withDefaults(defineProps<{
  title?: string;
  description?: string;
  flush?: boolean;
  class?: HTMLAttributes["class"];
  contentClass?: HTMLAttributes["class"];
}>(), { flush: false });
</script>

<template>
  <section
    data-slot="page-section"
    :class="cn('rounded-md border bg-card', $props.class)"
  >
    <header
      v-if="title || description || $slots.header || $slots.actions"
      class="flex flex-col gap-2 border-b px-4 py-3 md:flex-row md:items-center md:justify-between"
    >
      <div class="space-y-0.5 min-w-0">
        <h2 v-if="title" class="text-base font-medium">{{ title }}</h2>
        <p v-if="description" class="text-sm text-muted-foreground">{{ description }}</p>
        <slot name="header" />
      </div>
      <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2 shrink-0">
        <slot name="actions" />
      </div>
    </header>
    <div :class="cn(flush ? '' : 'px-4 py-3', contentClass)">
      <slot />
    </div>
  </section>
</template>
