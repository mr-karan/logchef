<script setup lang="ts">
import { ref, onMounted, watch, computed, nextTick } from 'vue'
import type { WritableComputedRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SelectGroup } from '@/components/ui/select'
import {
  NumberField,
  NumberFieldContent,
  NumberFieldDecrement,
  NumberFieldIncrement,
  NumberFieldInput,
} from '@/components/ui/number-field'
import { useToast } from '@/components/ui/toast'
import type { FilterCondition } from '@/api/explore'
import { getErrorMessage } from '@/api/types'
import { TOAST_DURATION } from '@/lib/constants'
import { DateTimePicker } from '@/components/date-time-picker'
import { getLocalTimeZone, now, CalendarDateTime, type DateValue } from '@internationalized/date'
import type { DateRange } from 'radix-vue'
import { Search, Plus } from 'lucide-vue-next'
import { createColumns } from './table/columns'
import DataTable from './table/data-table.vue'
import EmptyState from './EmptyState.vue'
import type { ColumnDef } from '@tanstack/vue-table'
import DataTableFilters from './table/data-table-filters.vue'
import SQLEditor from '@/components/sql-editor/SQLEditor.vue'
import { formatSourceName } from '@/utils/format'
import { useSourcesStore } from '@/stores/sources'
import { useExploreStore } from '@/stores/explore'
import { useTeamsStore } from '@/stores/teams'
import { useSqlGenerator } from '@/composables/useSqlGenerator'
import SqlPreview from '@/components/sql-preview/SqlPreview.vue'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import { ChevronRight } from 'lucide-vue-next'
import SmartFilterBar from '@/components/smart-filter/SmartFilterBar.vue'
import SavedQueriesDropdown from '@/components/saved-queries/SavedQueriesDropdown.vue'
import SaveQueryModal from '@/components/saved-queries/SaveQueryModal.vue'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { serializeQueryState, deserializeQueryContent } from '@/utils/querySerializer'
import type { SavedTeamQuery } from '@/api/types'
import type { CreateTeamQueryRequest } from '@/api/sources'

const router = useRouter()
const sourcesStore = useSourcesStore()
const exploreStore = useExploreStore()
const savedQueriesStore = useSavedQueriesStore()
const teamsStore = useTeamsStore()
const { toast } = useToast()
const route = useRoute()

// Saved queries state
const showSaveQueryModal = ref(false)
const queryToSave = ref('')

// Local UI state
const isLoading = computed(() => exploreStore.isLoading || sourcesStore.isLoading || teamsStore.isLoading)
const previewOpen = ref(false)

// Flag to prevent unnecessary URL updates on initial load
const isInitialLoad = ref(true)

// Date state
const currentTime = now(getLocalTimeZone())

// Computed DateRange for v-model binding
const dateRange = computed({
  get: () => ({
    start: exploreStore.data.timeRange?.start || currentTime.subtract({ hours: 3 }),
    end: exploreStore.data.timeRange?.end || currentTime
  }),
  set: (newValue: DateRange | null) => {
    if (newValue?.start && newValue?.end) {
      // Create a properly typed range object
      const range: { start: DateValue; end: DateValue } = {
        start: newValue.start as DateValue,
        end: newValue.end as DateValue
      }
      exploreStore.setTimeRange(range)
      // Update SQL generator when time range changes
      updateSqlGenerator()
    }
  }
}) as unknown as WritableComputedRef<DateRange>

// Add loading state handling
const showLoadingState = computed(() => {
  return sourcesStore.isLoading
})

// Fix the empty state computed to handle null sources from API
const showEmptyState = computed(() => {
  return !sourcesStore.isLoading &&
    (!sourcesStore.deduplicatedSources || sourcesStore.deduplicatedSources.length === 0)
})

// Get the teams for the selected source
const sourceTeams = computed(() => {
  if (!exploreStore.data.sourceId) return []
  return sourcesStore.getTeamsForSource(exploreStore.data.sourceId)
})

// Fix the selected source name computed to handle null sources
const selectedSourceName = computed(() => {
  const source = sourcesStore.sourcesWithTeams.find(s => s.id === exploreStore.data.sourceId)
  return source ? formatSourceName(source) : 'Select a source'
})

// Fix the selected source details computed to handle null sources
const selectedSourceDetails = computed(() => {
  const source = sourcesStore.sourcesWithTeams.find(s => s.id === exploreStore.data.sourceId)
  if (!source) return { database: '', table: '' }

  return {
    database: source.connection.database,
    table: source.connection.table_name
  }
})

// Add team selection name computed
const selectedTeamName = computed(() => {
  return teamsStore.currentTeam?.name || 'Select a team'
})

// Add a computed property for the selected team ID that handles undefined correctly
const selectedTeamIdForDropdown = computed(() => {
  return teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : undefined;
});

// Function to get timestamps from store's time range
const getTimestamps = () => {
  const timeRange = exploreStore.data.timeRange
  if (!timeRange?.start || !timeRange?.end) {
    return {
      start: 0,
      end: 0
    }
  }

  const startDate = new Date(
    timeRange.start.year,
    timeRange.start.month - 1,
    timeRange.start.day,
    'hour' in timeRange.start ? timeRange.start.hour : 0,
    'minute' in timeRange.start ? timeRange.start.minute : 0,
    'second' in timeRange.start ? timeRange.start.second : 0
  )

  const endDate = new Date(
    timeRange.end.year,
    timeRange.end.month - 1,
    timeRange.end.day,
    'hour' in timeRange.end ? timeRange.end.hour : 0,
    'minute' in timeRange.end ? timeRange.end.minute : 0,
    'second' in timeRange.end ? timeRange.end.second : 0
  )

  return {
    start: startDate.getTime(),
    end: endDate.getTime()
  }
}

// Initialize SQL generator with options
const sqlGenerator = ref(useSqlGenerator({
  database: selectedSourceDetails.value.database,
  table: selectedSourceDetails.value.table,
  start_timestamp: getTimestamps().start,
  end_timestamp: getTimestamps().end,
  limit: exploreStore.data.limit,
}))

// Add computed for SQL preview state
const sqlPreviewState = computed(() => {
  if (!sqlGenerator.value?.previewSql) {
    return {
      sql: '',
      isValid: true,
      error: undefined
    }
  }
  const state = sqlGenerator.value.previewSql
  return {
    sql: state.sql ?? '',
    isValid: state.isValid ?? true,
    error: state.error
  }
})

// Function to initialize or update SQL generator
function updateSqlGenerator() {
  // Only update if we have a valid source with database and table
  if (selectedSourceDetails.value.database && selectedSourceDetails.value.table) {
    const timestamps = getTimestamps()

    sqlGenerator.value = useSqlGenerator({
      database: selectedSourceDetails.value.database,
      table: selectedSourceDetails.value.table,
      start_timestamp: timestamps.start,
      end_timestamp: timestamps.end,
      limit: exploreStore.data.limit
    })

    // Only generate SQL if we have filters
    const queryState = sqlGenerator.value.generateQuerySql(exploreStore.data.filterConditions)
    // Update rawSQL only if valid
    if (queryState.isValid) {
      exploreStore.setRawSql(queryState.sql)
    }
  }
}

// Initialize state from URL parameters
function initializeFromURL() {
  // Set team from URL
  if (route.query.team && typeof route.query.team === 'string') {
    teamsStore.setCurrentTeam(parseInt(route.query.team));
  }

  // Set source from URL
  if (route.query.source && typeof route.query.source === 'string') {
    exploreStore.setSource(parseInt(route.query.source));
  }

  // Set limit from URL
  if (route.query.limit && typeof route.query.limit === 'string') {
    exploreStore.setLimit(parseInt(route.query.limit))
  }

  // Set time range from URL
  if (
    route.query.start_time &&
    route.query.end_time &&
    typeof route.query.start_time === 'string' &&
    typeof route.query.end_time === 'string'
  ) {
    try {
      const startValue = parseInt(route.query.start_time)
      const endValue = parseInt(route.query.end_time)

      const startDate = new Date(startValue)
      const endDate = new Date(endValue)

      const start = new CalendarDateTime(
        startDate.getFullYear(),
        startDate.getMonth() + 1,
        startDate.getDate(),
        startDate.getHours(),
        startDate.getMinutes(),
        startDate.getSeconds()
      )

      const end = new CalendarDateTime(
        endDate.getFullYear(),
        endDate.getMonth() + 1,
        endDate.getDate(),
        endDate.getHours(),
        endDate.getMinutes(),
        endDate.getSeconds()
      )

      exploreStore.setTimeRange({
        start,
        end
      })
    } catch (e) {
      console.error('Failed to parse time range from URL', e)
    }
  }

  // Set raw SQL from URL
  if (route.query.query && typeof route.query.query === 'string') {
    exploreStore.setRawSql(decodeURIComponent(route.query.query))
  }
}

// Update URL with current state
function updateURL() {
  if (isInitialLoad.value) {
    isInitialLoad.value = false
    return
  }

  const timestamps = getTimestamps()
  const query: Record<string, string> = {
    team: teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : '',
    source: exploreStore.data.sourceId ? exploreStore.data.sourceId.toString() : '',
    limit: exploreStore.data.limit.toString(),
    start_time: timestamps.start.toString(),
    end_time: timestamps.end.toString()
  }

  if (exploreStore.data.rawSql.trim()) {
    query.query = encodeURIComponent(exploreStore.data.rawSql.trim())
  }

  router.replace({ query })
}

// Add activeTab ref for tab management
const activeTab = ref<string | number>('filters')

// Update onMounted
onMounted(async () => {
  try {
    // First initialize from URL
    initializeFromURL()

    // Load teams first in the team-first approach
    await teamsStore.loadTeams()

    // Auto-select first team if none is selected
    if (!teamsStore.currentTeamId && teamsStore.teams.length > 0) {
      teamsStore.currentTeamId = teamsStore.teams[0].id
      await nextTick()
    }

    // If team is set from URL, use that
    if (route.query.team && typeof route.query.team === "string") {
      teamsStore.currentTeamId = parseInt(route.query.team);
      await nextTick();
    }

    // Then load sources with team information
    if (teamsStore.currentTeamId) {
      await sourcesStore.loadUserSources()

      // Auto-select first source if none is selected and we have sources
      if (!exploreStore.data.sourceId && sourcesStore.teamSources.length > 0) {
        exploreStore.setSource(sourcesStore.teamSources[0].id)
        await nextTick()
      }

      // If source is set from URL, use that
      if (route.query.source && typeof route.query.source === "string") {
        exploreStore.setSource(parseInt(route.query.source));
        await nextTick();
      }

      // If we have a source selected, load its saved queries for the current team
      if (exploreStore.data.sourceId && teamsStore.currentTeamId) {
        await savedQueriesStore.fetchSourceQueries(
          exploreStore.data.sourceId,
          teamsStore.currentTeamId
        )
      }
    }

    // Check if we have a query_id in the URL
    if (route.query.query_id && typeof route.query.query_id === "string") {
      // Try to fetch user teams first to establish team context
      if (!teamsStore.currentTeamId) {
        await savedQueriesStore.fetchUserTeams()
      }

      if (teamsStore.currentTeamId || savedQueriesStore.data.selectedTeamId) {
        // Then try to load the saved query
        await loadSavedQuery(route.query.query_id)
      } else {
        toast({
          title: 'Warning',
          description: 'Could not load saved query: No team context available',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
    }

    // If we have all the necessary parameters, execute the query
    if (exploreStore.canExecuteQuery && !isInitialLoad.value) {
      await executeQuery()
    }
  } catch (error) {
    console.error('Error initializing LogExplorer:', error)
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
})

// Watch for changes that should trigger SQL updates
watch(
  () => exploreStore.data.filterConditions,
  (newFilters, oldFilters) => {
    // Only update if we have a valid source
    if (selectedSourceDetails.value.database && selectedSourceDetails.value.table) {
      updateSqlGenerator();
    }
  },
  { deep: true, immediate: true }
)

// Watch for other changes that should trigger SQL updates
watch(
  () => ({
    sourceId: exploreStore.data.sourceId,
    sourceDetails: selectedSourceDetails.value,
    timeRange: exploreStore.data.timeRange,
    limit: exploreStore.data.limit
  }),
  () => {
    // Only update if we have a valid source
    if (selectedSourceDetails.value.database && selectedSourceDetails.value.table) {
      updateSqlGenerator();
    }
  },
  { deep: true }
)

// Update query execution to always use SQL
async function executeQuery() {
  if (!exploreStore.data.sourceId || !selectedSourceDetails.value.database || !selectedSourceDetails.value.table) {
    toast({
      title: 'Error',
      description: 'Please select a valid source before executing query',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
    return
  }

  try {
    let rawSql: string
    if (activeTab.value === 'filters') {
      // Generate SQL from filters
      const queryState = sqlGenerator.value?.generateQuerySql(exploreStore.data.filterConditions)
      if (!queryState?.isValid) {
        throw new Error(queryState?.error || 'Invalid SQL query')
      }
      rawSql = queryState.sql
    } else {
      // Use raw SQL directly
      rawSql = exploreStore.data.rawSql
    }

    // Execute query through store
    await exploreStore.executeQuery(rawSql)

    // Update URL only after successful search
    if (!isInitialLoad.value) {
      updateURL()
    }
    isInitialLoad.value = false
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}

const tableColumns = ref<ColumnDef<Record<string, any>>[]>([])

watch(
  () => exploreStore.data.columns,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(newColumns)
    }
  },
  { immediate: true }
)

// Handle filter updates
const handleFiltersUpdate = (filters: FilterCondition[]) => {
  exploreStore.setFilterConditions(filters)
}

// Handle loading saved query
async function loadSavedQuery(queryId: string) {
  try {
    // First get team context
    const teamId = savedQueriesStore.data.selectedTeamId
    if (!teamId) {
      // Try to load teams first
      await savedQueriesStore.fetchUserTeams()
      if (!savedQueriesStore.data.selectedTeamId) {
        toast({
          title: 'Error',
          description: 'No team context available',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
        return
      }
    }

    // Fetch the query
    const queryResponse = await savedQueriesStore.fetchQuery(
      savedQueriesStore.data.selectedTeamId || 0,
      queryId
    )

    if (queryResponse.success && queryResponse.data) {
      try {
        const query = queryResponse.data
        // Parse the query content
        const content = JSON.parse(query.query_content)

        // Load the query state into the store
        if (content.sourceId) exploreStore.setSource(content.sourceId)

        // Load time range
        if (content.timeRange) {
          const startDate = new Date(content.timeRange.absolute.start)
          const endDate = new Date(content.timeRange.absolute.end)

          const start = new CalendarDateTime(
            startDate.getFullYear(),
            startDate.getMonth() + 1,
            startDate.getDate(),
            startDate.getHours(),
            startDate.getMinutes(),
            startDate.getSeconds()
          )

          const end = new CalendarDateTime(
            endDate.getFullYear(),
            endDate.getMonth() + 1,
            endDate.getDate(),
            endDate.getHours(),
            endDate.getMinutes(),
            endDate.getSeconds()
          )

          exploreStore.setTimeRange({
            start,
            end
          })
        }

        // Load limit
        if (content.limit) exploreStore.setLimit(content.limit)

        // Load raw SQL
        if (content.rawSql) exploreStore.setRawSql(content.rawSql)

        // Set active tab
        activeTab.value = content.activeTab || 'filters'

        // Update URL
        updateURL()

        // Execute the query
        await executeQuery()

        toast({
          title: 'Success',
          description: `Loaded query "${query.name}"`,
          duration: TOAST_DURATION.SUCCESS,
        })
      } catch (parseError) {
        toast({
          title: 'Error',
          description: `Failed to parse query content: ${getErrorMessage(parseError)}`,
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
    }
  } catch (error) {
    toast({
      title: 'Error',
      description: `Failed to load saved query: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}

// Handle showing save query modal
function showSaveQueryForm() {
  try {
    // Serialize the current query state
    const serialized = serializeQueryState(exploreStore.data)
    queryToSave.value = JSON.stringify(serialized)
    showSaveQueryModal.value = true
  } catch (error) {
    toast({
      title: 'Error',
      description: `Failed to prepare query for saving: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}

// Handle team selection change
async function handleTeamChange(teamId: string) {
  try {
    // Set the current team directly
    teamsStore.currentTeamId = parseInt(teamId);

    // Wait for reactivity to update teamSources
    await nextTick();

    // Check if sources are available for this team
    if (sourcesStore.teamSources && sourcesStore.teamSources.length > 0) {
      // Reset source selection if current source doesn't belong to new team
      const sourceExists = sourcesStore.teamSources.some(s => s.id === exploreStore.data.sourceId);
      if (!sourceExists) {
        // Auto-select first source if current source not in team
        exploreStore.setSource(sourcesStore.teamSources[0].id);
      }
    } else {
      // No sources for this team
      exploreStore.setSource(0);
      console.log(`No sources available for team ${teamId}`);
    }

    // Update URL
    updateURL();
  } catch (error) {
    console.error('Error changing team:', error);
    toast({
      title: 'Error',
      description: `Failed to change team: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Handle save query submission with specific typing
async function handleSaveQuery(formData: any) {
  try {
    // Convert to the expected type
    const queryRequest: CreateTeamQueryRequest = {
      team_id: formData.team_id ? parseInt(formData.team_id) : (teamsStore.currentTeamId || 0),
      name: formData.name,
      description: formData.description,
      query_content: formData.query_content
    };

    // Use the source-centric API to create the query
    const result = await sourcesStore.createSourceQuery(
      exploreStore.data.sourceId.toString(),
      queryRequest
    );

    if (result.success && result.data) {
      toast({
        title: 'Success',
        description: 'Query saved successfully',
        duration: TOAST_DURATION.SUCCESS,
      });

      // Update URL with query_id
      const timestamps = getTimestamps();
      const params = new URLSearchParams(route.query as Record<string, string>);

      // Safely access id from data
      params.set('query_id', result.data.id.toString());
      params.set('team', teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : '');
      params.set('source', exploreStore.data.sourceId.toString());
      params.set('limit', exploreStore.data.limit.toString());
      params.set('start_time', timestamps.start.toString());
      params.set('end_time', timestamps.end.toString());

      if (exploreStore.data.rawSql) {
        params.set('query', encodeURIComponent(exploreStore.data.rawSql.trim()));
      } else {
        params.delete('query');
      }

      router.replace({ query: Object.fromEntries(params.entries()) });
    }

    showSaveQueryModal.value = false;
  } catch (error) {
    toast({
      title: 'Error',
      description: `Failed to save query: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}
</script>

<template>
  <!-- Show loading state -->
  <div v-if="showLoadingState" class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4">
    <div class="space-y-4 p-4 animate-pulse">
      <div class="flex space-x-2">
        <Skeleton class="h-4 w-32" />
      </div>
      <div class="space-y-2">
        <Skeleton class="h-4 w-48" />
        <Skeleton class="h-4 w-40" />
      </div>
    </div>
  </div>

  <!-- Show empty state when no sources are available -->
  <div v-else-if="showEmptyState" class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4">
    <div class="text-center space-y-2">
      <h2 class="text-2xl font-semibold tracking-tight">No Sources Found</h2>
      <p class="text-muted-foreground">You need to configure a log source before you can explore logs.</p>
    </div>
    <Button @click="router.push({ name: 'NewSource' })">
      <Plus class="mr-2 h-4 w-4" />
      Add Your First Source
    </Button>
  </div>

  <!-- Main explorer UI when sources are available -->
  <div v-else class="flex flex-col h-full min-w-0 gap-3">
    <!-- Updated Control Bar - Team First -->
    <div class="flex items-center gap-2 h-9 mb-1">
      <!-- Team Selector -->
      <div class="w-[220px]">
        <Select :model-value="teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : ''"
          @update:model-value="handleTeamChange" class="h-7">
          <SelectTrigger class="h-7 py-0 text-xs">
            <SelectValue placeholder="Select a team">
              {{ selectedTeamName }}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id.toString()">
              {{ team.name }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Source Selector (filtered by team) -->
      <div class="w-[220px]">
        <Select :model-value="exploreStore.data.sourceId ? exploreStore.data.sourceId.toString() : ''"
          @update:model-value="(val) => exploreStore.setSource(parseInt(val))" class="h-7"
          :disabled="!teamsStore.currentTeamId || (sourcesStore.teamSources || []).length === 0">
          <SelectTrigger class="h-7 py-0 text-xs">
            <SelectValue placeholder="Select a source">
              {{ selectedSourceName }}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="source in sourcesStore.teamSources || []" :key="source.id" :value="source.id.toString()">
              {{ formatSourceName(source) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Divider -->
      <div class="h-5 w-px bg-border self-center"></div>

      <!-- Time Range Picker - Increased width -->
      <div class="w-[340px]">
        <DateTimePicker v-model="dateRange" class="h-7" buttonClass="py-0 h-7 text-xs" />
      </div>

      <!-- Spacer -->
      <div class="flex-1"></div>

      <!-- Saved Queries Dropdown (now with team context) -->
      <div class="w-[180px]">
        <SavedQueriesDropdown :selected-team-id="teamsStore.currentTeamId || undefined" @select="loadSavedQuery"
          @save="showSaveQueryForm" />
      </div>

      <!-- Limit Control - Changed to dropdown -->
      <div class="flex items-center gap-1 mr-2">
        <span class="text-xs text-muted-foreground whitespace-nowrap">Limit:</span>
        <Select :model-value="exploreStore.data.limit.toString()"
          @update:model-value="(val: string) => exploreStore.setLimit(parseInt(val))" class="w-[80px]">
          <SelectTrigger class="h-7 py-0 text-xs">
            <SelectValue placeholder="Limit" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="100">100</SelectItem>
            <SelectItem value="500">500</SelectItem>
            <SelectItem value="1000">1,000</SelectItem>
            <SelectItem value="5000">5,000</SelectItem>
            <SelectItem value="10000">10,000</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Search Button -->
      <Button @click="executeQuery"
        :disabled="isLoading || !selectedSourceDetails.database || !selectedSourceDetails.table"
        class="h-7 px-3 py-0 text-xs">
        <Search class="mr-2 h-3 w-3" />
        Search
      </Button>
    </div>

    <!-- Query Builder Tabs -->
    <div class="rounded-md border bg-card">
      <Tabs v-model="activeTab" class="w-full" default-value="filters">
        <TabsList class="grid w-full grid-cols-2">
          <TabsTrigger value="filters">
            Filter Builder
          </TabsTrigger>
          <TabsTrigger value="raw_sql">
            SQL Query
          </TabsTrigger>
        </TabsList>

        <div class="p-3">
          <TabsContent value="filters" class="mt-0 space-y-3">
            <SmartFilterBar v-model="exploreStore.data.filterConditions" :columns="exploreStore.data.columns"
              @search="executeQuery" class="border rounded-md bg-muted/20 p-2" />

            <!-- SQL Preview -->
            <Collapsible v-model:open="previewOpen">
              <CollapsibleTrigger class="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground">
                <ChevronRight class="h-3.5 w-3.5" :class="{ 'rotate-90': previewOpen }" />
                Preview SQL
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div class="mt-2 border rounded-md bg-muted/20 p-2">
                  <SqlPreview :sql="sqlPreviewState.sql" :is-valid="sqlPreviewState.isValid"
                    :error="sqlPreviewState.error || undefined" />
                </div>
              </CollapsibleContent>
            </Collapsible>
          </TabsContent>

          <TabsContent value="raw_sql" class="mt-0">
            <div class="border rounded-md bg-muted/20 p-2">
              <SQLEditor v-model="exploreStore.data.rawSql" :source-database="selectedSourceDetails.database"
                :source-table="selectedSourceDetails.table" :start-timestamp="getTimestamps().start"
                :end-timestamp="getTimestamps().end" @execute="executeQuery" />
            </div>
          </TabsContent>
        </div>
      </Tabs>
    </div>

    <!-- Results Area -->
    <div class="flex-1 min-h-0">
      <!-- Show Skeleton when loading -->
      <div v-if="isLoading && !sourcesStore.sourcesWithTeams.length" class="flex-1 flex justify-center items-center">
        <div class="flex flex-col items-center gap-4 text-center">
          <Skeleton class="h-12 w-12 rounded-full" />
          <div class="space-y-2">
            <Skeleton class="h-4 w-[250px]" />
            <Skeleton class="h-4 w-[200px]" />
          </div>
        </div>
      </div>
      <div v-else-if="isLoading" class="border rounded-md bg-card p-3 space-y-3">
        <!-- Simulate Table Header Skeleton -->
        <div class="flex gap-3">
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
        </div>
        <!-- Simulate Table Rows Skeleton -->
        <div class="space-y-2">
          <div v-for="n in 5" :key="n" class="flex gap-3">
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
          </div>
        </div>
      </div>
      <!-- Show Results when data is loaded -->
      <div v-else-if="exploreStore.data.logs?.length > 0" class="border rounded-md bg-card">
        <DataTable :columns="tableColumns" :data="exploreStore.data.logs" :stats="exploreStore.data.queryStats"
          :source-id="exploreStore.data.sourceId.toString()" />
      </div>
      <EmptyState v-else class="border rounded-md bg-card" />
    </div>

    <!-- Save Query Modal (now with team context) -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-content="queryToSave"
      @close="showSaveQueryModal = false" @save="handleSaveQuery" />
  </div>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}
</style>
