<template>
  <n-date-picker
    v-model:value="timeRange"
    type="datetimerange"
    :shortcuts="rangeShortcuts"
    clearable
    style="width: 100%"
    :actions="['clear', 'confirm']"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { DatePickerShortcut } from 'naive-ui'

const props = defineProps<{
  modelValue: [number, number] | null
}>()

const emit = defineEmits(['update:modelValue'])

const timeRange = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const rangeShortcuts = {
  'Last 5m': () => {
    const now = Date.now()
    return [now - 5 * 60 * 1000, now]
  },
  'Last 15m': () => {
    const now = Date.now()
    return [now - 15 * 60 * 1000, now]
  },
  'Last 30m': () => {
    const now = Date.now()
    return [now - 30 * 60 * 1000, now]
  },
  'Last 1h': () => {
    const now = Date.now()
    return [now - 60 * 60 * 1000, now]
  },
  'Last 3h': () => {
    const now = Date.now()
    return [now - 3 * 60 * 60 * 1000, now]
  },
  'Last 6h': () => {
    const now = Date.now()
    return [now - 6 * 60 * 60 * 1000, now]
  },
  'Last 12h': () => {
    const now = Date.now()
    return [now - 12 * 60 * 60 * 1000, now]
  },
  'Last 24h': () => {
    const now = Date.now()
    return [now - 24 * 60 * 60 * 1000, now]
  },
  'Last 2d': () => {
    const now = Date.now()
    return [now - 2 * 24 * 60 * 60 * 1000, now]
  },
  'Last 7d': () => {
    const now = Date.now()
    return [now - 7 * 24 * 60 * 60 * 1000, now]
  },
  'Last 30d': () => {
    const now = Date.now()
    return [now - 30 * 24 * 60 * 60 * 1000, now]
  }
}
</script>
