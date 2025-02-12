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
  editingFilter.value = null
  applyFilters()
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
  <div class="space-y-4">
    <!-- Filters Row -->
    <div class="flex flex-wrap items-center gap-2">
      <!-- Existing Filters -->
      <template v-for="(filter, idx) in filters" :key="filter.field">
        <!-- Collapsed View -->
        <Badge
          v-if="editingFilter !== idx"
          variant="secondary"
          class="h-8 pl-3 pr-2 flex items-center gap-1 bg-gray-100 hover:bg-gray-100 text-gray-900"
          @click="startEditing(idx)"
        >
          <span class="flex items-center gap-1">
            <span class="font-normal">{{ getColumnLabel(filter.field) }}</span>
            <span class="text-gray-500">{{ getOperatorLabel(filter.operator) }}</span>
            <span class="font-medium">"{{ filter.value }}"</span>
          </span>
          <Button
            variant="ghost"
            size="sm"
            @click.stop="removeFilter(idx)"
            class="h-4 w-4 p-0 hover:bg-transparent"
          >
            <X class="h-3 w-3" />
          </Button>
        </Badge>

        <!-- Expanded Edit View -->
        <Card v-else class="shadow-sm border mb-2 w-full">
          <CardContent class="p-4 space-y-4">
            <div class="grid grid-cols-[1fr,1fr,2fr] gap-4">
              <!-- Field Selector -->
              <Select v-model="filter.field">
                <SelectTrigger>
                  <SelectValue placeholder="Select field" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="column in columns" :key="column.id" :value="column.id">
                    {{ column.label }}
                  </SelectItem>
                </SelectContent>
              </Select>

              <!-- Operator Selector -->
              <Select v-model="filter.operator">
                <SelectTrigger>
                  <SelectValue placeholder="Select operator" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="op in operators" :key="op.value" :value="op.value">
                    {{ op.label }}
                  </SelectItem>
                </SelectContent>
              </Select>

              <!-- Value Input -->
              <Input v-model="filter.value" placeholder="Enter value" />
            </div>

            <!-- Action Buttons -->
            <div class="flex justify-end gap-2">
              <Button variant="ghost" size="sm" @click="cancelEditing">
                Cancel
              </Button>
              <Button variant="default" size="sm" @click="updateFilter(idx)">
                Update
              </Button>
            </div>
          </CardContent>
        </Card>
      </template>

      <!-- Add Filter Button -->
      <Button
        variant="outline"
        size="sm"
        @click="showAddFilter = true"
        class="gap-2 h-8"
        v-if="!showAddFilter"
      >
        <Plus class="h-4 w-4" />
        Add Filter
      </Button>
    </div>

    <!-- Add Filter Form -->
    <Card v-if="showAddFilter" class="shadow-sm border">
      <CardContent class="p-4 space-y-4">
        <div class="grid grid-cols-[1fr,1fr,2fr] gap-4">
          <!-- Field Selector -->
          <Select v-model="newFilter.field">
            <SelectTrigger>
              <SelectValue placeholder="Select field" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="column in columns" :key="column.id" :value="column.id">
                {{ column.label }}
              </SelectItem>
            </SelectContent>
          </Select>

          <!-- Operator Selector -->
          <Select v-model="newFilter.operator">
            <SelectTrigger>
              <SelectValue placeholder="Select operator" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="op in operators" :key="op.value" :value="op.value">
                {{ op.label }}
              </SelectItem>
            </SelectContent>
          </Select>

          <!-- Value Input -->
          <Input v-model="newFilter.value" placeholder="Enter value" />
        </div>

        <!-- Action Buttons -->
        <div class="flex justify-end gap-2">
          <Button variant="ghost" size="sm" @click="showAddFilter = false">
            Cancel
          </Button>
          <Button
            variant="default"
            size="sm"
            @click="addFilter"
            :disabled="!newFilter.field || !newFilter.operator || !newFilter.value"
          >
            Add
          </Button>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
