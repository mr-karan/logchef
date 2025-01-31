<template>
  <n-date-picker
    v-model:value="timeRange"
    type="datetimerange"
    :shortcuts="rangeShortcuts"
    clearable
    style="width: 100%"
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

const rangeShortcuts: DatePickerShortcut[] = [
  {
    label: 'Last 5 minutes',
    value: () => {
      const now = Date.now()
      return [now - 5 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 15 minutes',
    value: () => {
      const now = Date.now()
      return [now - 15 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 30 minutes',
    value: () => {
      const now = Date.now()
      return [now - 30 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 1 hour',
    value: () => {
      const now = Date.now()
      return [now - 60 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 3 hours',
    value: () => {
      const now = Date.now()
      return [now - 3 * 60 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 6 hours',
    value: () => {
      const now = Date.now()
      return [now - 6 * 60 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 12 hours',
    value: () => {
      const now = Date.now()
      return [now - 12 * 60 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 24 hours',
    value: () => {
      const now = Date.now()
      return [now - 24 * 60 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 2 days',
    value: () => {
      const now = Date.now()
      return [now - 2 * 24 * 60 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 7 days',
    value: () => {
      const now = Date.now()
      return [now - 7 * 24 * 60 * 60 * 1000, now]
    }
  },
  {
    label: 'Last 30 days',
    value: () => {
      const now = Date.now()
      return [now - 30 * 24 * 60 * 60 * 1000, now]
    }
  }
]
</script>
