<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { ref, computed, watch, onUnmounted } from 'vue'
import type { Source } from '@/api/sources'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  Search,
  Database,
  Plus,
  Minus,
  RefreshCw,
  Tag,
  X,
} from 'lucide-vue-next'
import { useExploreStore } from '@/stores/explore'
import { getLocalTimeZone } from '@internationalized/date'
import { cn } from '@/lib/utils'
import { useVariables } from '@/composables/useVariables'
import { useFieldValuesLoader } from '@/composables/useFieldValuesLoader'
import { getNativeQueryLanguageForSource, supportsQueryLanguage, type QueryLanguage } from '@/lib/queryMetadata'
import { buildSourceFieldGroups, type SourceFieldGroup } from '@/lib/sourceFields'
import FieldSidebarRow from './FieldSidebarRow.vue'
import { getCleanType, getTypeColorClass, getTypeIcon, type FieldInfo } from './fieldDisplay'

const { t } = useI18n();

// Props
const props = withDefaults(defineProps<{
  fields: FieldInfo[]
  expanded: boolean
  teamId?: number
  sourceId?: number
  source?: Pick<Source, 'source_type' | '_meta_ts_field' | '_meta_severity_field' | 'query_languages'> | null
}>(), {
  expanded: false,
  source: null,
})

// Get time range and query from explore store
const exploreStore = useExploreStore()
const { getVariablesForApi } = useVariables()

// Get the current datasource-native query for filtering field values.
const getCurrentFilterQuery = (): string => {
  if (exploreStore.activeMode === 'logchefql') {
    const query = exploreStore.logchefqlCode || ''
    return query
  }

  if (!supportsQueryLanguage(props.source, 'logchefql')) {
    const query = exploreStore.nativeQuery || ''
    return query
  }

  return ''
}

const getCurrentFilterVariables = () => {
  if (exploreStore.activeMode !== 'logchefql') {
    return []
  }
  return getVariablesForApi()
}

const getCurrentFilterQueryLanguage = (): QueryLanguage | undefined => {
  if (exploreStore.activeMode === 'logchefql') {
    return 'logchefql'
  }

  if (!supportsQueryLanguage(props.source, 'logchefql')) {
    return getNativeQueryLanguageForSource(props.source)
  }

  return undefined
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
  getFilterQuery: getCurrentFilterQuery,
  getFilterQueryLanguage: getCurrentFilterQueryLanguage,
  getFilterVariables: getCurrentFilterVariables,
  timezone: undefined,
  limit: 10
}))

const {
  fieldStates,
  fieldValues,
  isAnyLoading,
  getFieldState,
  loadField,
  loadPriorityFields,
  cancelAll,
  clearCache
} = useFieldValuesLoader(loaderOptions)

// Filtered fields based on search
const filteredFields = computed((): FieldInfo[] => {
  if (!fieldSearch.value) return props.fields
  const search = fieldSearch.value.toLowerCase()
  return props.fields.filter(field =>
    field.name.toLowerCase().includes(search) ||
    field.type.toLowerCase().includes(search)
  )
})

// A source schema can be far wider than any one result. Render the field list
// in bounded steps; search still covers every field.
const FIELD_RENDER_STEP = 100
const fieldRenderLimit = ref(FIELD_RENDER_STEP)

watch([fieldSearch, () => props.fields, () => props.sourceId], () => {
  fieldRenderLimit.value = FIELD_RENDER_STEP
})

const allFieldGroups = computed((): SourceFieldGroup<FieldInfo>[] => {
  return buildSourceFieldGroups<FieldInfo>(filteredFields.value, props.source)
})

// Group counts stay complete; only the rows rendered inside groups are capped.
const fieldGroups = computed((): SourceFieldGroup<FieldInfo>[] => {
  let budget = fieldRenderLimit.value
  const take = (fields: FieldInfo[]): FieldInfo[] => {
    const taken = fields.slice(0, Math.max(0, budget))
    budget -= taken.length
    return taken
  }
  return allFieldGroups.value
    .map(group => ({ ...group, filterableFields: take(group.filterableFields), plainFields: take(group.plainFields) }))
    .filter(group => group.filterableFields.length > 0 || group.plainFields.length > 0)
})

const renderedFields = computed((): FieldInfo[] => {
  return fieldGroups.value.flatMap(group => [...group.filterableFields, ...group.plainFields])
})

const hiddenFieldCount = computed(() => filteredFields.value.length - renderedFields.value.length)

const showMoreFields = () => {
  fieldRenderLimit.value += FIELD_RENDER_STEP
}

const getGroupHeaderIconClass = (groupId: SourceFieldGroup['id']): string => {
  switch (groupId) {
    case 'core':
      return 'text-blue-500'
    case 'context':
      return 'text-emerald-500'
    case 'filterable':
      return 'text-sky-500'
    case 'system':
      return 'text-amber-500'
    default:
      return 'text-muted-foreground'
  }
}

// Toggle field expansion
const toggleField = async (field: FieldInfo) => {
  if (expandedFields.value.has(field.name)) {
    expandedFields.value.delete(field.name)
    expandedFields.value = new Set(expandedFields.value)
  } else {
    expandedFields.value.add(field.name)
    expandedFields.value = new Set(expandedFields.value)

    // Load values if not already loaded (for click-to-load fields or fields that errored)
    const state = getFieldState(field.name)
    if ((state.status === 'click-to-load' || state.status === 'idle' || state.status === 'error')
        && props.teamId && props.sourceId) {
      await loadField(field.name, field.type)
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
  } catch {
    return null
  }
}

// Auto-expand threshold - fields with this many or fewer values are auto-expanded
const AUTO_EXPAND_THRESHOLD = 6

const resetFieldValues = () => {
  clearCache()
  expandedFields.value = new Set()
  if (props.expanded && props.teamId && props.sourceId) {
    loadPriorityFields(renderedFields.value)
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

// A new query, source, or reopened panel invalidates every loaded value.
watch(
  [() => props.expanded, () => props.teamId, () => props.sourceId, () => exploreStore.lastExecutionTimestamp],
  resetFieldValues,
  { immediate: true },
)

// Search and "show more" only add rows: load the new ones, keep the rest.
watch(renderedFields, (fields) => {
  if (props.expanded && props.teamId && props.sourceId) {
    loadPriorityFields(fields)
  }
})

// Expand a field with few distinct values once, when its values first arrive,
// so a field the user collapsed stays collapsed while other fields load.
watch(fieldValues, (values, previous) => {
  const arrived = Object.keys(values).filter(name =>
    !previous?.[name] && values[name].total_distinct <= AUTO_EXPAND_THRESHOLD
  )
  if (arrived.length > 0) {
    expandedFields.value = new Set([...expandedFields.value, ...arrived])
  }
})

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
          <span class='text-sm font-semibold text-foreground'>{{ t('ui.fields') }}</span>
          <div class="flex items-center gap-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="h-6 w-6 p-0"
                    :disabled="isAnyLoading"
                    @click="resetFieldValues"
                  >
                    <RefreshCw
                      :class="cn('h-3.5 w-3.5', isAnyLoading && 'animate-spin')"
                    />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  <p class='text-xs'>{{ t('ui.refreshFieldValues') }}</p>
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
            :placeholder="t('ui.searchFields')"
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

          <template v-for="group in fieldGroups" :key="group.id">
            <div class="mb-2">
              <div class="flex items-center gap-1.5 px-2 py-1 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                <component
                  :is="group.id === 'system' || group.id === 'other' ? Database : Tag"
                  :class="cn('h-3 w-3', getGroupHeaderIconClass(group.id))"
                />
                <span>{{ t(`fields.${group.id}.label`) }}</span>
                <Badge variant="secondary" class="ml-auto text-[9px] h-4 px-1">
                  {{ group.fields.length }}
                </Badge>
              </div>
              <p class="px-2 pb-1 text-[10px] leading-relaxed text-muted-foreground/80">
                {{ t(`fields.${group.id}.description`) }}
              </p>

              <div class="space-y-0.5">
                <FieldSidebarRow
                  v-for="field in group.filterableFields"
                  :key="`${group.id}-${field.name}`"
                  :field="field"
                  :state="fieldStates.get(field.name)"
                  :expanded="expandedFields.has(field.name)"
                  @toggle="toggleField"
                  @load="field => loadField(field.name, field.type)"
                  @add-filter="addFilter"
                />

                <div
                  v-for="field in group.plainFields"
                  :key="`${group.id}-${field.name}`"
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

          <div v-if="hiddenFieldCount > 0" class="px-2 pt-1 pb-2" data-field-render-hint>
            <p class="text-[10px] text-muted-foreground mb-1.5">
              {{ t('ui.showingFieldsOfTotal', { shown: renderedFields.length.toLocaleString(), total: filteredFields.length.toLocaleString() }) }}
            </p>
            <Button variant="outline" size="sm" class="w-full h-7 text-xs" @click="showMoreFields">
              {{ t('ui.showMore') }}
            </Button>
          </div>

          <!-- Empty State -->
          <div
            v-if="fieldGroups.length === 0"
            class="text-center py-8"
          >
            <Database class="h-8 w-8 mx-auto text-muted-foreground/40 mb-2" />
            <p class="text-sm text-muted-foreground">
              <template v-if="fieldSearch">
                {{ t('explore.noFieldMatches', { search: fieldSearch }) }}
              </template>
              <template v-else>
                {{ t('ui.noFieldsAvailable') }}
              </template>
            </p>
          </div>
        </div>
      </ScrollArea>

      <!-- Footer hint -->
      <div class="px-3 py-2 border-t bg-muted/20 text-[10px] text-muted-foreground">
        <div class="flex items-center gap-1">
          <Plus class="h-3 w-3" />
          <span>{{ t('ui.clickValueToAddFilter') }}</span>
        </div>
        <div class="flex items-center gap-1 mt-0.5">
          <Minus class="h-3 w-3" />
          <span>{{ t('ui.clickMinusToExclude') }}</span>
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
