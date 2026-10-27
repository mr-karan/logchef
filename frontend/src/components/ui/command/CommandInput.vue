<script setup lang="ts">
import type { ListboxFilterEmits, ListboxFilterProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { Search } from "lucide-vue-next"
import { ListboxFilter, useForwardPropsEmits } from "reka-ui"
import { cn } from "@/lib/utils"

defineOptions({
  inheritAttrs: false,
})

const props = defineProps<ListboxFilterProps & {
  class?: HTMLAttributes["class"]
  wrapperClass?: HTMLAttributes["class"]
}>()
const emits = defineEmits<ListboxFilterEmits>()

const delegatedProps = reactiveOmit(props, "class", "wrapperClass")

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <div
    data-slot="command-input-wrapper"
    :class="cn('flex h-9 items-center gap-2 border-b px-3', props.wrapperClass)"
  >
    <slot name="leading">
      <Search class="size-4 shrink-0 opacity-50" />
    </slot>
    <div class="relative flex min-w-0 flex-1 items-center">
      <slot />
      <ListboxFilter
        v-bind="{ ...forwarded, ...$attrs }"
        data-slot="command-input"
        auto-focus
        :class="cn('placeholder:text-muted-foreground flex h-10 w-full rounded-md bg-transparent py-3 text-sm outline-hidden disabled:cursor-not-allowed disabled:opacity-50', props.class)"
      />
    </div>
    <slot name="trailing" />
  </div>
</template>
