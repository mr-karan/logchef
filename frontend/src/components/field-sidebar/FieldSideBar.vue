<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
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
  AlertTriangle,
  Loader2,
} from 'lucide-vue-next'
import { useExploreStore } from '@/stores/explore'
import { getLocalTimeZone } from '@internationalized/date'
import { cn } from '@/lib/utils'
import { useVariables } from '@/composables/useVariables'
import {
  useFieldValuesLoader,
  isFilterableField as isFilterableFieldType
} from '@/composables/useFieldValuesLoader'

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

// Use the field values loader composable for progressive per-field loading
const loaderOptions = computed(() => ({
  teamId: props.teamId,
  sourceId: props.sourceId,
  getTimeRange: getTimeRangeForApi,
  getLogchefQL: getCurrentLogchefQL,
  timezone: undefined,
  limit: 10
}))

const {
  fieldValues,
  isAnyLoading,
  getFieldState,
  loadField,
  loadPriorityFields,
  cancelAll,
  clearCache
} = useFieldValuesLoader(loaderOptions)

// Use the imported isFilterableFieldType function
const isFilterableField = isFilterableFieldType

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

    // Load values if not already loaded (for click-to-load fields or fields that errored)
    const state = getFieldState(fieldName)
    const fieldType = getFieldType(fieldName)
    if ((state.status === 'click-to-load' || state.status === 'idle' || state.status === 'error')
        && props.teamId && props.sourceId && fieldType) {
      await loadField(fieldName, fieldType)
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

// Auto-expand threshold - fields with this many or fewer values are auto-expanded
const AUTO_EXPAND_THRESHOLD = 6

// Refresh all priority fields (called by refresh button)
const refreshAllFields = () => {
  clearCache()
  expandedFields.value = new Set()
  loadPriorityFields(props.fields)
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

// Watch for source changes to clear data
watch(
  () => [props.teamId, props.sourceId],
  () => {
    clearCache()
    expandedFields.value = new Set()
  }
)

// Watch for query execution to auto-refresh field values
// This ensures sidebar reflects the current query filters AND time range
// (Query execution already incorporates time range, so we only need this one watcher)
watch(
  () => exploreStore.lastExecutionTimestamp,
  (newTimestamp, oldTimestamp) => {
    // Refresh when a query is executed and sidebar is expanded
    if (props.expanded && newTimestamp && newTimestamp !== oldTimestamp) {
      // Clear and reload priority fields
      clearCache()
      expandedFields.value = new Set()
      loadPriorityFields(props.fields)
    }
  }
)

// Auto-load priority field values when sidebar expands
// Skip if already loading (prevents duplicate requests on initial page load)
watch(
  () => props.expanded,
  (isExpanded) => {
    if (isExpanded && props.teamId && props.sourceId && !isAnyLoading.value) {
      // Load priority fields (LowCardinality, Enum) - String fields will show "click to load"
      loadPriorityFields(props.fields)
    }
  },
  { immediate: true }
)

// Auto-expand fields with few values as they load
watch(
  () => fieldValues.value,
  (values) => {
    // Auto-expand fields with ≤6 distinct values
    const fieldsToExpand = Object.entries(values)
      .filter(([_, result]) => result.total_distinct <= AUTO_EXPAND_THRESHOLD)
      .map(([fieldName]) => fieldName)

    if (fieldsToExpand.length > 0) {
      fieldsToExpand.forEach(name => expandedFields.value.add(name))
      expandedFields.value = new Set(expandedFields.value)
    }
  },
  { deep: true }
)

// Cleanup on unmount
onUnmounted(() => {
  cancelAll()
})
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
                    :disabled="isAnyLoading"
                    @click="refreshAllFields"
                  >
                    <RefreshCw
                      :class="cn('h-3.5 w-3.5', isAnyLoading && 'animate-spin')"
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

      <!-- Field List -->
      <ScrollArea class="flex-1">
        <div class="p-2 space-y-1">

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
                        <!-- Per-field loading spinner -->
                        <Loader2
                          v-if="getFieldState(field.name).status === 'loading'"
                          class="h-3 w-3 text-muted-foreground animate-spin flex-shrink-0"
                        />
                        <!-- Value count badge - shows when collapsed and values loaded -->
                        <Badge
                          v-else-if="!expandedFields.has(field.name) && fieldValues[field.name]?.total_distinct"
                          variant="secondary"
                          class="text-[9px] h-4 px-1.5 font-normal flex-shrink-0 bg-primary/10 text-primary"
                          :title="`${fieldValues[field.name].total_distinct} unique values`"
                        >
                          {{ fieldValues[field.name].total_distinct }}
                        </Badge>
                        <!-- Click to load indicator for String fields -->
                        <Badge
                          v-else-if="getFieldState(field.name).status === 'click-to-load'"
                          variant="outline"
                          class="text-[9px] h-4 px-1 font-normal flex-shrink-0 text-muted-foreground"
                        >
                          click
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
                        <!-- Loading state -->
                        <template v-if="getFieldState(field.name).status === 'loading'">
                          <div class="space-y-1">
                            <Skeleton v-for="i in 3" :key="i" class="h-6 w-full" />
                          </div>
                        </template>

                        <!-- Error state with retry -->
                        <template v-else-if="getFieldState(field.name).status === 'error'">
                          <div class="flex items-center gap-2 text-xs text-amber-600 dark:text-amber-500 py-1 px-2">
                            <AlertTriangle class="h-3.5 w-3.5 flex-shrink-0" />
                            <span class="flex-1">Failed to load</span>
                            <Button
                              variant="ghost"
                              size="sm"
                              class="h-5 px-2 text-xs"
                              @click.stop="loadField(field.name, field.type)"
                            >
                              Retry
                            </Button>
                          </div>
                        </template>

                        <!-- Click-to-load state for String fields -->
                        <template v-else-if="getFieldState(field.name).status === 'click-to-load' || getFieldState(field.name).status === 'idle'">
                          <div class="py-2 px-2">
                            <Button
                              variant="outline"
                              size="sm"
                              class="w-full h-7 text-xs"
                              @click.stop="loadField(field.name, field.type)"
                            >
                              <RefreshCw class="h-3 w-3 mr-1.5" />
                              Load values
                            </Button>
                            <p class="text-[10px] text-muted-foreground mt-1.5 text-center">
                              May be slow for high-cardinality fields
                            </p>
                          </div>
                        </template>

                        <!-- Loaded values -->
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

                        <!-- Empty state (loaded but no values) -->
                        <template v-else-if="getFieldState(field.name).status === 'loaded'">
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
            v-if="filteredFields.length === 0"
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
