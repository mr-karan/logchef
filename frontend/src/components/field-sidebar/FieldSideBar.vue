<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  ChevronRight,
  Search,
  Hash,
  Type,
  Calendar,
  Database,
  Plus,
  Minus,
  RefreshCw,
  Tag,
  X,
} from 'lucide-vue-next'
import { sourcesApi, type AllFieldValuesResult, type FieldValuesResult } from '@/api/sources'
import { useExploreStore } from '@/stores/explore'
import { getLocalTimeZone } from '@internationalized/date'
import { cn } from '@/lib/utils'
import { useVariables } from '@/composables/useVariables'

// Define field type for auto-completion
interface FieldInfo {
  name: string
  type: string
  isTimestamp?: boolean
  isSeverity?: boolean
}

// Props
const props = withDefaults(defineProps<{
  fields: FieldInfo[]
  expanded: boolean
  teamId?: number
  sourceId?: number
}>(), {
  expanded: false,
})

// Get time range and query from explore store
const exploreStore = useExploreStore()
const { convertVariables } = useVariables()

// Get the current LogchefQL query for filtering field values
const getCurrentLogchefQL = (): string => {
  // Only pass LogchefQL in logchefql mode - SQL mode doesn't filter sidebar
  if (exploreStore.activeMode !== 'logchefql') {
    return ''
  }
  const query = exploreStore.logchefqlCode || ''
  // Replace any variables before sending to backend
  return query ? convertVariables(query) : ''
}

// Emits
const emit = defineEmits<{
  (e: 'update:expanded', value: boolean): void
  (e: 'add-filter', field: string, value: string, operator: '=' | '!='): void
  (e: 'field-click', field: string): void
}>()

// Local state
const fieldSearch = ref('')
const expandedFields = ref<Set<string>>(new Set())
const fieldValues = ref<AllFieldValuesResult>({})
const loadingFieldValues = ref(false)
const loadingField = ref<string | null>(null)
const errorMessage = ref<string | null>(null)

// Check if a field type is filterable (can show distinct values)
// Matches backend logic: LowCardinality, String, Nullable(String), Enum
// Excludes complex types: Map, Array, Tuple, JSON
const isFilterableField = (type: string): boolean => {
  const lowerType = type.toLowerCase()
  
  // Exclude complex types that can't have simple distinct values
  if (lowerType.startsWith('map(') ||
      lowerType.startsWith('array(') ||
      lowerType.startsWith('tuple(') ||
      lowerType === 'json' ||
      lowerType.startsWith('json(')) {
    return false
  }
  
  // LowCardinality fields - always fast
  if (type.includes('LowCardinality')) {
    return true
  }
  // Regular String fields - included with timeout protection on backend
  if (type === 'String' || type === 'Nullable(String)') {
    return true
  }
  // Enum types - always fast, finite set
  if (type.startsWith('Enum')) {
    return true
  }
  return false
}

// Check if a field is LowCardinality (for styling purposes)
const isLowCardinality = (type: string): boolean => {
  return type.includes('LowCardinality')
}

// Get clean type name for display
const getCleanType = (type: string): string => {
  // Remove LowCardinality wrapper
  let clean = type.replace(/LowCardinality\(([^)]+)\)/g, '$1')
  // Remove Nullable wrapper
  clean = clean.replace(/Nullable\(([^)]+)\)/g, '$1')
  return clean
}

// Get type icon
const getTypeIcon = (type: string) => {
  const cleanType = getCleanType(type).toLowerCase()
  if (cleanType.includes('datetime') || cleanType.includes('date')) {
    return Calendar
  }
  if (cleanType.includes('int') || cleanType.includes('float') || cleanType.includes('decimal')) {
    return Hash
  }
  if (cleanType.includes('map')) {
    return Database
  }
  return Type
}

// Get type color class
const getTypeColorClass = (field: FieldInfo): string => {
  if (field.isTimestamp) return 'text-blue-500'
  if (field.isSeverity) return 'text-amber-500'
  if (isLowCardinality(field.type)) return 'text-emerald-500'
  if (isFilterableField(field.type)) return 'text-sky-500' // String fields
  return 'text-muted-foreground'
}

// Filtered fields based on search
const filteredFields = computed((): FieldInfo[] => {
  if (!fieldSearch.value) return props.fields
  const search = fieldSearch.value.toLowerCase()
  return props.fields.filter(field =>
    field.name.toLowerCase().includes(search) ||
    field.type.toLowerCase().includes(search)
  )
})

// Separate filterable fields (can show distinct values) from other fields
const filterableFields = computed(() => 
  filteredFields.value.filter(f => isFilterableField(f.type))
)

const otherFields = computed(() => 
  filteredFields.value.filter(f => !isFilterableField(f.type))
)

// Get field type by name from props
const getFieldType = (fieldName: string): string => {
  const field = props.fields.find(f => f.name === fieldName)
  return field?.type || ''
}

// Toggle field expansion
const toggleField = async (fieldName: string) => {
  if (expandedFields.value.has(fieldName)) {
    expandedFields.value.delete(fieldName)
    expandedFields.value = new Set(expandedFields.value)
  } else {
    expandedFields.value.add(fieldName)
    expandedFields.value = new Set(expandedFields.value)
    
    // Fetch values if not already loaded
    if (!fieldValues.value[fieldName] && props.teamId && props.sourceId) {
      await fetchFieldValues(fieldName)
    }
  }
}

// Get time range in ISO8601 format for API calls
const getTimeRangeForApi = () => {
  const timeRange = exploreStore.timeRange
  if (!timeRange) {
    return null
  }
  
  try {
    // Convert DateValue to JS Date using the local timezone, then to ISO8601
    const startDate = timeRange.start.toDate(getLocalTimeZone())
    const endDate = timeRange.end.toDate(getLocalTimeZone())
    
    return { 
      startTime: startDate.toISOString(),
      endTime: endDate.toISOString()
    }
  } catch (e) {
    console.error('Failed to convert time range:', e)
    return null
  }
}

// Fetch values for a specific field
const fetchFieldValues = async (fieldName: string) => {
  if (!props.teamId || !props.sourceId) return
  
  const fieldType = getFieldType(fieldName)
  if (!fieldType) {
    console.error(`Field type not found for ${fieldName}`)
    return
  }
  
  // Get time range - required for performance
  const timeRange = getTimeRangeForApi()
  if (!timeRange) {
    console.error('No time range available for field values query')
    return
  }
  
  loadingField.value = fieldName
  try {
    const response = await sourcesApi.getFieldValues(
      props.teamId,
      props.sourceId,
      fieldName,
      fieldType,
      timeRange.startTime,
      timeRange.endTime,
      undefined, // Use default timezone (UTC)
      10,
      getCurrentLogchefQL() // Pass current query to filter field values
    )
    if (response.data) {
      fieldValues.value = {
        ...fieldValues.value,
        [fieldName]: response.data
      }
    }
  } catch (error) {
    console.error(`Failed to fetch values for ${fieldName}:`, error)
  } finally {
    loadingField.value = null
  }
}

// Auto-expand threshold - fields with this many or fewer values are auto-expanded
const AUTO_EXPAND_THRESHOLD = 6

// Fetch all filterable field values
const fetchAllLowCardValues = async () => {
  if (!props.teamId || !props.sourceId) return
  
  // Get time range - required for performance
  const timeRange = getTimeRangeForApi()
  if (!timeRange) {
    console.error('No time range available for field values query')
    errorMessage.value = 'Select a time range to load field values'
    return
  }
  
  loadingFieldValues.value = true
  errorMessage.value = null
  try {
    const response = await sourcesApi.getAllFieldValues(
      props.teamId,
      props.sourceId,
      timeRange.startTime,
      timeRange.endTime,
      undefined, // Use default timezone (UTC)
      10,
      getCurrentLogchefQL() // Pass current query to filter field values
    )
    if (response.data) {
      fieldValues.value = response.data
      
      // Auto-expand fields with ≤6 distinct values
      // These are typically the most useful for quick filtering (e.g., log levels)
      const entries = Object.entries(response.data) as [string, FieldValuesResult][]
      const fieldsToExpand = entries
        .filter(([_, result]) => result.total_distinct <= AUTO_EXPAND_THRESHOLD)
        .map(([fieldName]) => fieldName)
      
      if (fieldsToExpand.length > 0) {
        expandedFields.value = new Set(fieldsToExpand)
      }
    }
  } catch (error: any) {
    console.error('Failed to fetch field values:', error)
    errorMessage.value = error.message || 'Failed to load field values'
  } finally {
    loadingFieldValues.value = false
  }
}

// Add filter to query
const addFilter = (field: string, value: string, operator: '=' | '!=' = '=') => {
  emit('add-filter', field, value, operator)
}

// Handle field name click
const handleFieldClick = (fieldName: string) => {
  emit('field-click', fieldName)
}

// Format count with abbreviations
const formatCount = (count: number): string => {
  if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
  if (count >= 1000) return `${(count / 1000).toFixed(1)}K`
  return count.toString()
}

// Watch for source changes to refresh data
watch(
  () => [props.teamId, props.sourceId],
  () => {
    fieldValues.value = {}
    expandedFields.value = new Set()
    errorMessage.value = null
  }
)

// Watch for time range changes to refresh field values
// This ensures field values are always relevant to the current time window
watch(
  () => exploreStore.timeRange,
  (newTimeRange, oldTimeRange) => {
    // Only refresh if sidebar is expanded and time range actually changed
    if (props.expanded && newTimeRange && 
        (newTimeRange.start !== oldTimeRange?.start || newTimeRange.end !== oldTimeRange?.end)) {
      // Clear existing values and fetch fresh data for new time range
      fieldValues.value = {}
      expandedFields.value = new Set()
      fetchAllLowCardValues()
    }
  },
  { deep: true }
)

// Watch for query execution to auto-refresh field values
// This ensures sidebar reflects the current query filters
watch(
  () => exploreStore.lastExecutionTimestamp,
  (newTimestamp, oldTimestamp) => {
    // Refresh when a query is executed and sidebar is expanded
    if (props.expanded && newTimestamp && newTimestamp !== oldTimestamp) {
      // Clear existing values and fetch fresh data with new query filters
      fieldValues.value = {}
      expandedFields.value = new Set()
      fetchAllLowCardValues()
    }
  }
)

// Auto-fetch low cardinality values when sidebar expands
watch(
  () => props.expanded,
  (isExpanded) => {
    if (isExpanded && props.teamId && props.sourceId && Object.keys(fieldValues.value).length === 0) {
      fetchAllLowCardValues()
    }
  },
  { immediate: true }
)
</script>

<template>
  <!-- Sidebar Panel -->
  <Transition name="slide">
    <div v-if="expanded" 
      class="w-72 border-r h-full flex flex-col bg-background flex-shrink-0"
      style="max-width: 288px; min-width: 288px;">
      
      <!-- Header -->
      <div class="px-3 py-2 border-b bg-muted/30">
        <div class="flex items-center justify-between mb-2">
          <span class="text-sm font-semibold text-foreground">Fields</span>
          <div class="flex items-center gap-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="h-6 w-6 p-0"
                    :disabled="loadingFieldValues"
                    @click="fetchAllLowCardValues"
                  >
                    <RefreshCw 
                      :class="cn('h-3.5 w-3.5', loadingFieldValues && 'animate-spin')"
                    />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  <p class="text-xs">Refresh field values</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        </div>
        
        <!-- Search -->
        <div class="relative">
          <Search class="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
          <Input
            v-model="fieldSearch"
            placeholder="Search fields..."
            class="h-7 text-xs pl-7 pr-7"
          />
          <button
            v-if="fieldSearch"
            class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            @click="fieldSearch = ''"
          >
            <X class="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      <!-- Error Message -->
      <div v-if="errorMessage" class="px-3 py-2 bg-destructive/10 text-destructive text-xs">
        {{ errorMessage }}
      </div>

      <!-- Field List -->
      <ScrollArea class="flex-1">
        <div class="p-2 space-y-1">
          <!-- Loading skeleton -->
          <template v-if="loadingFieldValues && Object.keys(fieldValues).length === 0">
            <div v-for="i in 5" :key="i" class="space-y-1 p-2">
              <Skeleton class="h-4 w-full" />
              <Skeleton class="h-3 w-3/4" />
            </div>
          </template>

          <!-- Filterable Fields Section (LowCardinality, String, Enum) -->
          <template v-if="filterableFields.length > 0">
            <div class="mb-2">
              <div class="flex items-center gap-1.5 px-2 py-1 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                <Tag class="h-3 w-3" />
                <span>Filterable Fields</span>
                <Badge variant="secondary" class="ml-auto text-[9px] h-4 px-1">
                  {{ filterableFields.length }}
                </Badge>
              </div>
              
              <div class="space-y-0.5">
                <Collapsible
                  v-for="field in filterableFields"
                  :key="field.name"
                  :open="expandedFields.has(field.name)"
                  @update:open="() => toggleField(field.name)"
                >
                  <div class="rounded-md hover:bg-muted/50 transition-colors">
                    <CollapsibleTrigger class="w-full">
                      <div class="flex items-center gap-2 px-2 py-1.5 cursor-pointer group">
                        <ChevronRight 
                          :class="cn(
                            'h-3.5 w-3.5 text-muted-foreground transition-transform flex-shrink-0',
                            expandedFields.has(field.name) && 'rotate-90'
                          )"
                        />
                        <component 
                          :is="getTypeIcon(field.type)" 
                          :class="cn('h-3.5 w-3.5 flex-shrink-0', getTypeColorClass(field))"
                        />
                        <span 
                          class="text-sm font-medium text-foreground truncate flex-1 text-left"
                          :title="field.name"
                        >
                          {{ field.name }}
                        </span>
                        <!-- Value count badge - shows when collapsed and values loaded -->
                        <Badge 
                          v-if="!expandedFields.has(field.name) && fieldValues[field.name]?.total_distinct"
                          variant="secondary" 
                          class="text-[9px] h-4 px-1.5 font-normal flex-shrink-0 bg-primary/10 text-primary"
                          :title="`${fieldValues[field.name].total_distinct} unique values`"
                        >
                          {{ fieldValues[field.name].total_distinct }}
                        </Badge>
                        <Badge 
                          variant="outline" 
                          class="text-[9px] h-4 px-1 font-normal flex-shrink-0 opacity-60 group-hover:opacity-100"
                        >
                          {{ getCleanType(field.type) }}
                        </Badge>
                      </div>
                    </CollapsibleTrigger>

                    <CollapsibleContent>
                      <div class="pl-8 pr-2 pb-2">
                        <!-- Loading state for field values -->
                        <template v-if="loadingField === field.name">
                          <div class="space-y-1">
                            <Skeleton v-for="i in 3" :key="i" class="h-6 w-full" />
                          </div>
                        </template>

                        <!-- Field values -->
                        <template v-else-if="fieldValues[field.name]?.values?.length">
                          <div class="space-y-0.5">
                            <div
                              v-for="valueInfo in fieldValues[field.name].values"
                              :key="valueInfo.value"
                              class="flex items-center gap-1 group/value"
                            >
                              <TooltipProvider>
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      class="flex-1 flex items-center gap-2 px-2 py-1 rounded text-left hover:bg-primary/10 transition-colors min-w-0"
                                      @click="addFilter(field.name, valueInfo.value, '=')"
                                    >
                                      <span class="text-xs text-foreground truncate flex-1" :title="valueInfo.value">
                                        {{ valueInfo.value || '(empty)' }}
                                      </span>
                                      <span class="text-[10px] text-muted-foreground flex-shrink-0">
                                        {{ formatCount(valueInfo.count) }}
                                      </span>
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent side="right" class="text-xs">
                                    <p>Click to filter: {{ field.name }}="{{ valueInfo.value }}"</p>
                                  </TooltipContent>
                                </Tooltip>
                              </TooltipProvider>

                              <!-- Exclude button -->
                              <TooltipProvider>
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      class="h-5 w-5 flex items-center justify-center rounded opacity-0 group-hover/value:opacity-100 hover:bg-destructive/20 text-muted-foreground hover:text-destructive transition-all"
                                      @click="addFilter(field.name, valueInfo.value, '!=')"
                                    >
                                      <Minus class="h-3 w-3" />
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent side="right" class="text-xs">
                                    <p>Exclude: {{ field.name }}!="{{ valueInfo.value }}"</p>
                                  </TooltipContent>
                                </Tooltip>
                              </TooltipProvider>
                            </div>

                            <!-- Show total if more values exist -->
                            <div 
                              v-if="fieldValues[field.name].total_distinct > fieldValues[field.name].values.length"
                              class="text-[10px] text-muted-foreground px-2 pt-1"
                            >
                              +{{ fieldValues[field.name].total_distinct - fieldValues[field.name].values.length }} more values
                            </div>
                          </div>
                        </template>

                        <!-- Empty state -->
                        <template v-else>
                          <div class="text-xs text-muted-foreground italic py-1 px-2">
                            No values found
                          </div>
                        </template>
                      </div>
                    </CollapsibleContent>
                  </div>
                </Collapsible>
              </div>
            </div>
          </template>

          <!-- Other Fields Section -->
          <template v-if="otherFields.length > 0">
            <div>
              <div class="flex items-center gap-1.5 px-2 py-1 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                <Database class="h-3 w-3" />
                <span>Other Fields</span>
                <Badge variant="secondary" class="ml-auto text-[9px] h-4 px-1">
                  {{ otherFields.length }}
                </Badge>
              </div>

              <div class="space-y-0.5">
                <div
                  v-for="field in otherFields"
                  :key="field.name"
                  class="flex items-center gap-2 px-2 py-1.5 rounded-md hover:bg-muted/50 transition-colors cursor-pointer"
                  @click="handleFieldClick(field.name)"
                >
                  <component 
                    :is="getTypeIcon(field.type)" 
                    :class="cn('h-3.5 w-3.5 flex-shrink-0', getTypeColorClass(field))"
                  />
                  <span 
                    class="text-sm text-foreground truncate flex-1"
                    :title="field.name"
                  >
                    {{ field.name }}
                  </span>
                  <Badge 
                    variant="outline" 
                    class="text-[9px] h-4 px-1 font-normal flex-shrink-0 opacity-60"
                  >
                    {{ getCleanType(field.type) }}
                  </Badge>
                </div>
              </div>
            </div>
          </template>

          <!-- Empty State -->
          <div 
            v-if="!loadingFieldValues && filteredFields.length === 0" 
            class="text-center py-8"
          >
            <Database class="h-8 w-8 mx-auto text-muted-foreground/40 mb-2" />
            <p class="text-sm text-muted-foreground">
              <template v-if="fieldSearch">
                No fields match "{{ fieldSearch }}"
              </template>
              <template v-else>
                No fields available
              </template>
            </p>
          </div>
        </div>
      </ScrollArea>

      <!-- Footer hint -->
      <div class="px-3 py-2 border-t bg-muted/20 text-[10px] text-muted-foreground">
        <div class="flex items-center gap-1">
          <Plus class="h-3 w-3" />
          <span>Click value to add filter</span>
        </div>
        <div class="flex items-center gap-1 mt-0.5">
          <Minus class="h-3 w-3" />
          <span>Click minus to exclude</span>
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

/* Fix sidebar width issues */
div.w-72 {
  width: 18rem !important;
  max-width: 18rem !important;
  flex: 0 0 18rem !important;
}

/* Custom scrollbar */
:deep(.scroll-area-viewport) {
  scrollbar-width: thin;
  scrollbar-color: rgba(155, 155, 155, 0.4) transparent;
}

:deep(.scroll-area-viewport::-webkit-scrollbar) {
  width: 6px;
}

:deep(.scroll-area-viewport::-webkit-scrollbar-track) {
  background: transparent;
}

:deep(.scroll-area-viewport::-webkit-scrollbar-thumb) {
  background-color: rgba(155, 155, 155, 0.4);
  border-radius: 20px;
}

:deep(.scroll-area-viewport::-webkit-scrollbar-thumb:hover) {
  background-color: rgba(155, 155, 155, 0.6);
}
</style>
