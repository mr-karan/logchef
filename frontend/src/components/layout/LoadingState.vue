<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { Loader2 } from "lucide-vue-next";
import { cn } from "@/lib/utils";

defineProps<{
  label?: string;
  class?: HTMLAttributes["class"];
}>();
</script>

<template>
  <div
    data-slot="loading-state"
    :class="cn('flex flex-col items-center justify-center gap-2 py-10 text-sm text-muted-foreground', $props.class)"
  >
    <Loader2 class="h-5 w-5 animate-spin" />
    <p v-if="label">{{ label }}</p>
  </div>
</template>
