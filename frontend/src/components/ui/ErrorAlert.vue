<script setup lang="ts">
import { computed } from 'vue'
import { Button } from '@/components/ui/button'

interface Props {
  error: string | null | { message: string; operation?: string };
  title?: string;
  retryLabel?: string;
}

const props = withDefaults(defineProps<Props>(), {
  title: 'Error',
  retryLabel: 'Retry'
})

const emit = defineEmits<{
  (e: 'retry'): void
}>()

const errorMessage = computed(() => {
  if (!props.error) return null
  if (typeof props.error === 'string') return props.error
  return props.error.message
})
</script>

<template>
  <div v-if="errorMessage" class="rounded-lg border border-destructive p-4 text-center text-destructive">
    <p class="mb-2 font-medium">{{ title }}</p>
    <p class="text-sm">{{ errorMessage }}</p>
    <Button variant="outline" class="mt-4" @click="emit('retry')">
      {{ retryLabel }}
    </Button>
  </div>
</template>
