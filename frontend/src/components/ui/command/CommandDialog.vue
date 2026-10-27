<script setup lang="ts">
import type { DialogRootEmits, DialogRootProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { useForwardPropsEmits } from "reka-ui"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { cn } from "@/lib/utils"
import Command from "./Command.vue"

defineOptions({
  inheritAttrs: false,
})

const props = defineProps<DialogRootProps & {
  title: string
  description: string
  class?: HTMLAttributes["class"]
}>()
const emits = defineEmits<DialogRootEmits>()

const delegatedProps = reactiveOmit(props, "title", "description", "class")

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <Dialog v-slot="slotProps" v-bind="forwarded">
    <DialogContent :show-close-button="false" :class="cn('overflow-hidden p-0', props.class)">
      <DialogHeader class="sr-only">
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription>{{ description }}</DialogDescription>
      </DialogHeader>
      <Command v-bind="$attrs">
        <slot v-bind="slotProps" />
      </Command>
    </DialogContent>
  </Dialog>
</template>
