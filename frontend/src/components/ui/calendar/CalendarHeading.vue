<script lang="ts" setup>
import { cn } from '@/lib/utils'
import { CalendarHeading, type CalendarHeadingProps, useForwardProps } from 'radix-vue'
import { computed, type HTMLAttributes } from 'vue'

const props = defineProps<CalendarHeadingProps & { class?: HTMLAttributes['class'] }>()

const delegatedProps = computed(() => {
  const { class: _, ...delegated } = props

  return delegated
})

const forwardedProps = useForwardProps(delegatedProps)

interface SlotProps {
  headingValue: string
}
</script>

<template>
  <CalendarHeading v-slot="slotProps: SlotProps" :class="cn('text-sm font-medium', props.class)"
    v-bind="forwardedProps">
    <slot :heading-value="slotProps.headingValue">
      {{ slotProps.headingValue }}
    </slot>
  </CalendarHeading>
</template>
