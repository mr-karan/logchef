<script setup lang="ts">
import { ref } from 'vue'

const hovered = ref(false)
const focused = ref(false)

function onFocusOut(event: FocusEvent) {
  if (event.currentTarget instanceof HTMLElement) {
    focused.value = event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)
  }
}
</script>

<template>
  <div
    class="relative w-full"
    tabindex="0"
    @mouseenter="hovered = true"
    @mouseleave="hovered = false"
    @focusin="focused = true"
    @focusout="onFocusOut"
  >
    <slot />
    <slot v-if="hovered || focused" name="actions" />
  </div>
</template>
