<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { api } from '@/services/api'

interface SchemaField {
    name: string
    type: string
    path: string[]
    is_nested: boolean
    children?: SchemaField[]
    parent?: string
    sampleData?: any
}

const props = defineProps<{
    sourceId?: string
    startTime?: Date
    endTime?: Date
    schema: SchemaField[]
}>()

const emit = defineEmits(['field-toggle'])

const loading = ref(false)
const error = ref<string | null>(null)
const selectedFields = ref<Set<string>>(new Set(['timestamp', 'severity_text', 'body']))

const toggleField = (field: SchemaField) => {
    // For nested fields under log_attributes, construct the proper path
    const finalPath = field.parent === 'log_attributes'
        ? `log_attributes.${field.path[field.path.length - 1]}`  // Only take the last part of the path
        : field.path.join('.')

    if (selectedFields.value.has(finalPath)) {
        selectedFields.value.delete(finalPath)
    } else {
        selectedFields.value.add(finalPath)
    }

    emit('field-toggle', Array.from(selectedFields.value))
}

const getFieldDisplayName = (field: SchemaField) => {
    if (field.parent === 'log_attributes') {
        // For nested fields under log_attributes, show just the last part
        return field.path[field.path.length - 1]
    }
    return field.path[0]
}

const getIndentClass = (field: SchemaField) => {
    // Add indentation for nested fields
    if (field.parent) {
        return 'ml-4'
    }
    return ''
}

const getFieldTypeDisplay = (field: SchemaField) => {
    // Add null check and default value
    if (!field?.type) return 'Unknown'

    return field.type
        .replace('LowCardinality(', '')
        .replace(')', '')
        .replace('DateTime64(3)', 'DateTime')
}
</script>

<template>
    <div class="h-full flex flex-col bg-white">
        <div class="p-4 border-b border-gray-200">
            <h3 class="text-sm font-medium text-gray-700">Log Fields</h3>
            <p class="text-xs text-gray-500 mt-1">Select fields to display in the table</p>
        </div>

        <div v-if="loading" class="p-4">
            <div class="animate-pulse space-y-2">
                <div v-for="i in 5" :key="i" class="h-4 bg-gray-100 rounded"></div>
            </div>
        </div>

        <div v-else-if="error" class="p-4 text-sm text-red-600">
            {{ error }}
        </div>

        <div v-else-if="!props.schema?.length" class="p-4 text-sm text-gray-500">
            No fields available
        </div>

        <div v-else class="flex-1 overflow-y-auto">
            <div class="p-2">
                <div v-for="field in props.schema"
                     :key="field.path.join('.')"
                     :class="[
                         'relative flex items-center p-2 hover:bg-gray-50 rounded-md',
                         getIndentClass(field),
                         {
                             'cursor-pointer': field.name !== 'timestamp',
                             'opacity-50': field.name === 'timestamp'
                         }
                     ]"
                     @click="field.name !== 'timestamp' && toggleField(field)"
                >
                    <input type="checkbox"
                           :checked="selectedFields.has(field.path.join('.')) || field.name === 'timestamp'"
                           :disabled="field.name === 'timestamp'"
                           class="h-4 w-4 text-blue-600 rounded border-gray-300"
                    />
                    <div class="ml-3 flex-1">
                        <div class="text-sm font-medium text-gray-700 flex items-center gap-2">
                            {{ getFieldDisplayName(field) }}
                            <span v-if="field.name === 'timestamp'" class="text-xs text-gray-400">(required)</span>
                        </div>
                        <div class="text-xs text-gray-500 flex items-center gap-2">
                            <span>{{ field?.type ? getFieldTypeDisplay(field) : 'Unknown' }}</span>
                            <span v-if="field?.is_nested" class="text-blue-500">(nested)</span>
                        </div>
                    </div>
                </div>

                <!-- Render nested fields if present -->
                <template v-for="field in props.schema" :key="field.path.join('.')">
                    <div v-if="field.children"
                         v-for="child in field.children"
                         :key="child.path.join('.')"
                         :class="[
                             'relative flex items-center p-2 hover:bg-gray-50 rounded-md ml-4',
                             {'cursor-pointer': true}
                         ]"
                         @click="toggleField(child)"
                    >
                        <input type="checkbox"
                               :checked="selectedFields.has(child.path.join('.'))"
                               class="h-4 w-4 text-blue-600 rounded border-gray-300"
                        />
                        <div class="ml-3 flex-1">
                            <div class="text-sm font-medium text-gray-700">
                                {{ getFieldDisplayName(child) }}
                            </div>
                            <div class="text-xs text-gray-500">
                                {{ child?.type ? getFieldTypeDisplay(child) : 'Unknown' }}
                            </div>
                        </div>
                    </div>
                </template>
            </div>
        </div>
    </div>
</template>
