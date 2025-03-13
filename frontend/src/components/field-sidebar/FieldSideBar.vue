<script setup lang="ts">
import { ref, computed } from 'vue'
import { Button } from '@/components/ui/button'

// Define field type for auto-completion
interface FieldInfo {
    name: string
    type: string
    isTimestamp?: boolean
    isSeverity?: boolean
}

// Define emits - remove fieldClick event
const emit = defineEmits(['update:expanded'])

// Define props with defaults
const props = withDefaults(defineProps<{
    fields: FieldInfo[]
    expanded: boolean
}>(), {
    expanded: true,
})

// Local state
const fieldSearch = ref('')

// Filtered fields based on search
const filteredFields = computed((): FieldInfo[] => {
    if (!fieldSearch.value) return props.fields

    const search = fieldSearch.value.toLowerCase()
    return props.fields.filter(field =>
        field.name.toLowerCase().includes(search) ||
        field.type.toLowerCase().includes(search)
    )
})
</script>

<template>
    <!-- Sidebar Panel - without integrated toggle button -->
    <Transition name="slide">
        <div v-if="expanded" class="w-64 border-r h-full flex flex-col bg-background">
            <div class="p-2 border-b bg-muted/10 flex items-center justify-between">
                <span class="text-sm font-medium">Fields</span>
                <!-- Search box for fields -->
                <div class="relative flex-1 px-2">
                    <input v-model="fieldSearch" placeholder="Search fields..."
                        class="w-full h-7 text-xs px-2 py-1 rounded border border-input bg-background" />
                </div>
            </div>

            <div class="overflow-y-auto flex-grow p-2">
                <div v-if="filteredFields.length" class="space-y-1 text-sm">
                    <div v-for="field in filteredFields" :key="field.name"
                        class="flex items-center justify-between py-1 px-2 rounded hover:bg-muted group">
                        <div class="flex items-center">
                            <span :class="{
                                'text-blue-500': field.isTimestamp,
                                'text-amber-500': field.isSeverity
                            }">{{ field.name }}</span>
                        </div>
                        <span
                            class="text-[10px] text-muted-foreground/70 group-hover:text-muted-foreground transition-colors font-light">
                            {{ field.type }}
                        </span>
                    </div>
                </div>
                <div v-else-if="props.fields.length && fieldSearch" class="text-sm text-muted-foreground py-2">
                    No fields match "{{ fieldSearch }}".
                </div>
                <div v-else-if="!props.fields.length" class="text-sm text-muted-foreground py-2">
                    No fields available. Select a source to view fields.
                </div>
            </div>
        </div>
    </Transition>
</template>

<style scoped>
/* Panel transitions */
.slide-enter-active,
.slide-leave-active {
    transition: transform 0.2s ease, opacity 0.2s ease;
}

.slide-enter-from,
.slide-leave-to {
    transform: translateX(-100%);
    opacity: 0;
}

/* Add scrollbar styling for field list */
.overflow-y-auto::-webkit-scrollbar {
    width: 4px;
}

.overflow-y-auto::-webkit-scrollbar-track {
    background: transparent;
}

.overflow-y-auto::-webkit-scrollbar-thumb {
    background-color: rgba(155, 155, 155, 0.4);
    border-radius: 20px;
}

.overflow-y-auto::-webkit-scrollbar-thumb:hover {
    background-color: rgba(155, 155, 155, 0.6);
}
</style>