<script setup lang="ts">
import { computed } from 'vue'
import Tag from 'primevue/tag'

const props = defineProps<{
    field: string
    value: any
}>()

const isJSON = (value: any) => {
    return typeof value === 'object' && value !== null
}

const formatValue = (value: any) => {
    if (value === undefined || value === null || value === '') {
        return '-'
    }

    if (isJSON(value)) {
        return JSON.stringify(value, null, 2)
    }

    return String(value)
}

// Special formatting for specific fields
const getFieldStyle = (field: string, value: any) => {
    if (field === 'severity_text') {
        const severity = value?.toLowerCase()
        const colors = {
            error: 'bg-red-50 text-red-700',
            warn: 'bg-yellow-50 text-yellow-700',
            info: 'bg-blue-50 text-blue-700',
            debug: 'bg-green-50 text-green-700'
        }
        return `inline-block px-2 py-0.5 rounded-full text-xs font-medium ${colors[severity] || ''}`
    }
    return ''
}

const isNestedField = computed(() => props.field.includes('.'))

// Add memoization for expensive operations
const formattedValue = computed(() => formatValue(props.value))

const fieldStyle = computed(() => getFieldStyle(props.field, props.value))

// Optimize JSON detection
const isJSONValue = computed(() => {
  if (!props.value) return false
  return typeof props.value === 'object' && props.value !== null
})

// Add type checking for better performance
const valueType = computed(() => {
  if (props.field === 'timestamp') return 'timestamp'
  if (props.field === 'severity_text') return 'severity'
  if (isJSONValue.value) return 'json'
  return 'text'
})
</script>

<template>
    <div class="min-w-0 py-1">
        <template v-if="isJSON(value)">
            <pre class="text-xs whitespace-pre-wrap bg-gray-50 p-1 rounded">{{ formatValue(value) }}</pre>
        </template>
        <template v-else-if="field === 'severity_text'">
            <span :class="getFieldStyle(field, value)">{{ formatValue(value) }}</span>
        </template>
        <template v-else-if="isNestedField">
            <span
                class="block truncate"
                :title="formatValue(value)"
            >
                {{ formatValue(value) }}
            </span>
        </template>
        <template v-else>
            <span
                class="block truncate"
                :class="{ 'font-mono': field === 'timestamp' }"
                :title="formatValue(value)"
            >
                {{ formatValue(value) }}
            </span>
        </template>
    </div>
</template>

<style scoped>
.field-value {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
}

:deep(.p-tag) {
    font-size: 0.75rem;
    padding: 0.15rem 0.4rem;
}
</style>