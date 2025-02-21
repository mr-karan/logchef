<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Plus, X } from 'lucide-vue-next'
import {
  Card,
  CardContent,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Input } from '@/components/ui/input'
import type { FilterCondition } from '@/api/explore'
import { Badge } from '@/components/ui/badge'

interface Props {
  columns?: { name: string; type: string }[]
}

const props = withDefaults(defineProps<Props>(), {
  columns: () => []
})

const emit = defineEmits<{
  (e: 'update:filters', value: FilterCondition[]): void
}>()

// Available operators
const operators = [
  { value: '=', label: 'equals' },
  { value: '!=', label: 'does not equal' },
  { value: 'contains', label: 'contains' },
  { value: 'startsWith', label: 'starts with' },
  { value: 'endsWith', label: 'ends with' },
  { value: '>', label: 'greater than' },
  { value: '<', label: 'less than' },
  { value: '>=', label: 'greater than or equal' },
  { value: '<=', label: 'less than or equal' },
] as const

// Active filters
const filters = ref<FilterCondition[]>([])
const editingFilter = ref<number | null>(null)
const showAddFilter = ref(false)
const newFilter = ref<FilterCondition>({
  field: '',
  operator: '=',
  value: ''
})

// Get available columns for filtering
const columns = computed(() => {
  return props.columns.map(column => ({
    id: column.name,
    label: column.name.charAt(0).toUpperCase() + column.name.slice(1).replace(/_/g, ' ')
  }))
})

// Get operator label
const getOperatorLabel = (operator: string) => {
  return operators.find(op => op.value === operator)?.label || operator
}

// Get column label
const getColumnLabel = (field: string) => {
  return columns.value.find(col => col.id === field)?.label || field
}


// Add a new filter
const addFilter = () => {
  if (newFilter.value.field && newFilter.value.operator && newFilter.value.value) {
    filters.value.push({ ...newFilter.value })
    newFilter.value = { field: '', operator: '=', value: '' }
    showAddFilter.value = false
    applyFilters()
  }
}

// Remove a filter
const removeFilter = (index: number) => {
  filters.value.splice(index, 1)
  editingFilter.value = null
  applyFilters()
}

// Start editing a filter
const startEditing = (index: number) => {
  editingFilter.value = index
}

// Update an existing filter
const updateFilter = (index: number) => {
  if (editingFilter.value === index) {
    editingFilter.value = null
    applyFilters()
  }
}

// Cancel editing
const cancelEditing = () => {
  editingFilter.value = null
}

// Apply filters to the table
const applyFilters = () => {
  // Only emit filters that have all fields filled
  const validFilters = filters.value.filter(f => f.field && f.operator && f.value)
  emit('update:filters', validFilters)
}

// Watch for changes and apply filters
watch(filters, () => {
  applyFilters()
}, { deep: true })
</script>

<template>
  <div class="space-y-2">
    <!-- Filters Row -->
    <div class="flex flex-wrap items-center gap-2">
      <!-- Existing Filters -->
      <template v-for="(filter, idx) in filters" :key="filter.field">
        <!-- Collapsed View -->
        <Badge v-if="editingFilter !== idx" variant="secondary"
          class="h-7 pl-2 pr-1.5 flex items-center gap-1 hover:bg-muted/50" @click="startEditing(idx)">
          <span class="flex items-center gap-1 text-xs">
            <span class="font-normal">{{ getColumnLabel(filter.field) }}</span>
            <span class="text-muted-foreground">{{ getOperatorLabel(filter.operator) }}</span>
            <span class="font-medium">"{{ filter.value }}"</span>
          </span>
          <Button variant="ghost" size="sm" @click.stop="removeFilter(idx)" class="h-4 w-4 p-0 hover:bg-transparent">
            <X class="h-3 w-3" />
          </Button>
        </Badge>

        <!-- Expanded Edit View -->
        <div v-else class="flex items-center gap-2 w-full">
          <!-- Field Selector -->
          <Select v-model="filter.field" class="w-[180px]">
            <SelectTrigger class="h-8">
              <SelectValue placeholder="Select field" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="column in columns" :key="column.id" :value="column.id">
                {{ column.label }}
              </SelectItem>
            </SelectContent>
          </Select>

          <!-- Operator Selector -->
          <Select v-model="filter.operator" class="w-[140px]">
            <SelectTrigger class="h-8">
              <SelectValue placeholder="Select operator" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="op in operators" :key="op.value" :value="op.value">
                {{ op.label }}
              </SelectItem>
            </SelectContent>
          </Select>

          <!-- Value Input -->
          <Input v-model="filter.value" placeholder="Enter value" class="h-8 flex-1" />

          <!-- Action Buttons -->
          <div class="flex gap-1">
            <Button variant="ghost" size="sm" @click="cancelEditing" class="h-8 px-2">
              Cancel
            </Button>
            <Button variant="default" size="sm" @click="updateFilter(idx)" class="h-8 px-2">
              Update
            </Button>
          </div>
        </div>
      </template>

      <!-- Add Filter Button -->
      <Button variant="outline" size="sm" @click="showAddFilter = true" class="gap-1.5 h-7" v-if="!showAddFilter">
        <Plus class="h-3.5 w-3.5" />
        Add Filter
      </Button>
    </div>

    <!-- Add Filter Form -->
    <div v-if="showAddFilter" class="flex items-center gap-2 w-full border rounded-md bg-muted/20 p-2">
      <!-- Field Selector -->
      <Select v-model="newFilter.field" class="w-[180px]">
        <SelectTrigger class="h-8">
          <SelectValue placeholder="Select field" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="column in columns" :key="column.id" :value="column.id">
            {{ column.label }}
          </SelectItem>
        </SelectContent>
      </Select>

      <!-- Operator Selector -->
      <Select v-model="newFilter.operator" class="w-[140px]">
        <SelectTrigger class="h-8">
          <SelectValue placeholder="Select operator" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="op in operators" :key="op.value" :value="op.value">
            {{ op.label }}
          </SelectItem>
        </SelectContent>
      </Select>

      <!-- Value Input -->
      <Input v-model="newFilter.value" placeholder="Enter value" class="h-8 flex-1" />

      <!-- Action Buttons -->
      <div class="flex gap-1">
        <Button variant="ghost" size="sm" @click="showAddFilter = false" class="h-8 px-2">
          Cancel
        </Button>
        <Button variant="default" size="sm" @click="addFilter" class="h-8 px-2"
          :disabled="!newFilter.field || !newFilter.operator || !newFilter.value">
          Add
        </Button>
      </div>
    </div>
  </div>
</template>
