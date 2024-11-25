<script setup>
import { cn } from '@/lib/utils';
import { ToastRoot, useForwardPropsEmits } from 'radix-vue';
import { computed } from 'vue';
import { toastVariants } from '.';

const props = defineProps({
  class: { type: null, required: false },
  variant: { type: null, required: false },
  onOpenChange: { type: Function, required: false, skipCheck: true },
  defaultOpen: { type: Boolean, required: false },
  forceMount: { type: Boolean, required: false },
  type: { type: String, required: false },
  open: { type: Boolean, required: false },
  duration: { type: Number, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
});

const emits = defineEmits([
  'escapeKeyDown',
  'pause',
  'resume',
  'swipeStart',
  'swipeMove',
  'swipeCancel',
  'swipeEnd',
  'update:open',
]);

const delegatedProps = computed(() => {
  const { class: _, ...delegated } = props;

  return delegated;
});

const forwarded = useForwardPropsEmits(delegatedProps, emits);
</script>

<template>
  <ToastRoot
    v-bind="forwarded"
    :class="cn(toastVariants({ variant }), props.class)"
    @update:open="onOpenChange"
  >
    <slot />
  </ToastRoot>
</template>
