<script setup lang="ts">
import type { ListboxGroupProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { ListboxGroup, ListboxGroupLabel } from "reka-ui"
import { cn } from "@/lib/utils"

const props = defineProps<ListboxGroupProps & {
  class?: HTMLAttributes["class"]
  heading?: string
}>()

const delegatedProps = reactiveOmit(props, "class", "heading")
</script>

<template>
  <ListboxGroup
    v-bind="delegatedProps"
    data-slot="command-group"
    :class="cn('text-foreground overflow-hidden p-1', props.class)"
  >
    <ListboxGroupLabel
      v-if="heading || $slots.heading"
      data-slot="command-group-heading"
      class="text-muted-foreground px-2 py-1.5 text-xs font-medium"
    >
      <slot name="heading">
        {{ heading }}
      </slot>
    </ListboxGroupLabel>
    <slot />
  </ListboxGroup>
</template>
