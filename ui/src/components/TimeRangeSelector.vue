<template>
    <n-space vertical>
        <n-date-picker
            v-model:value="timeRange"
            type="datetimerange"
            :shortcuts="rangeShortcuts"
            clearable
            style="width: 100%"
        />
    </n-space>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const props = defineProps<{
  modelValue: [number, number] | null
}>()

const emit = defineEmits(['update:modelValue'])

// Default to last 3 hours
const timeRange = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

// Time ranges similar to Grafana
const rangeShortcuts = {
    'Last 5 minutes': () => {
        const now = new Date().getTime()
        return [now - 5 * 60 * 1000, now]
    },
    'Last 15 minutes': () => {
        const now = new Date().getTime()
        return [now - 15 * 60 * 1000, now]
    },
    'Last 30 minutes': () => {
        const now = new Date().getTime()
        return [now - 30 * 60 * 1000, now]
    },
    'Last 1 hour': () => {
        const now = new Date().getTime()
        return [now - 60 * 60 * 1000, now]
    },
    'Last 3 hours': () => {
        const now = new Date().getTime()
        return [now - 3 * 60 * 60 * 1000, now]
    },
    'Last 6 hours': () => {
        const now = new Date().getTime()
        return [now - 6 * 60 * 60 * 1000, now]
    },
    'Last 12 hours': () => {
        const now = new Date().getTime()
        return [now - 12 * 60 * 60 * 1000, now]
    },
    'Last 24 hours': () => {
        const now = new Date().getTime()
        return [now - 24 * 60 * 60 * 1000, now]
    },
    'Last 2 days': () => {
        const now = new Date().getTime()
        return [now - 2 * 24 * 60 * 60 * 1000, now]
    },
    'Last 7 days': () => {
        const now = new Date().getTime()
        return [now - 7 * 24 * 60 * 60 * 1000, now]
    },
    'Last 30 days': () => {
        const now = new Date().getTime()
        return [now - 30 * 24 * 60 * 60 * 1000, now]
    }
}

</script>
