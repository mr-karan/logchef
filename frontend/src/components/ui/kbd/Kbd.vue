<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"

const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>

<template>
  <kbd
    data-slot="kbd"
    :class="cn('bg-background text-muted-foreground dark:bg-muted pointer-events-none inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-[3px] border border-b-2 px-1 font-sans text-[11px] leading-4 select-none', props.class)"
  >
    <slot />
  </kbd>
</template>
