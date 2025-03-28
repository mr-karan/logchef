<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Plus, Play, RefreshCw } from 'lucide-vue-next'
import { Skeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { DateTimePicker } from '@/components/date-time-picker'
import { useExploreStore } from '@/stores/explore'
import { useTeamsStore } from '@/stores/teams'
import { useSourcesStore } from '@/stores/sources'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { now, getLocalTimeZone, CalendarDateTime } from '@internationalized/date'
import { createColumns } from './table/columns'
import DataTable from './table/data-table.vue'
import { formatSourceName } from '@/utils/format'
import SavedQueriesDropdown from '@/components/saved-queries/SavedQueriesDropdown.vue'
import SaveQueryModal from '@/components/saved-queries/SaveQueryModal.vue'
import QueryEditor from '@/components/query-editor/QueryEditor.vue'
import { FieldSideBar } from '@/components/field-sidebar'
import { translateToSQLConditions } from '@/utils/logchefql/api'
import { Parser as LogchefQLParser } from '@/utils/logchefql'
import { SQLParser } from '@/utils/clickhouse-sql/ast'; // Import SQLParser
import { QueryBuilder } from '@/utils/query-builder'
import { useExploreUrlSync } from '@/composables/useExploreUrlSync'; // Import the composable
import type { ColumnDef } from '@tanstack/vue-table'
import type { SavedQueryContent } from '@/api/savedQueries'
import type { Source } from '@/api/sources'
import { getErrorMessage } from '@/api/types'
import { useQueryExecution } from '@/composables/useQueryExecution'
import { useSourceTeamManagement } from '@/composables/useSourceTeamManagement'
import { useQueryMode } from '@/composables/useQueryMode'
import { useSavedQueries } from '@/composables/useSavedQueries'

// Define field type for auto-completion
interface FieldInfo {
  name: string;
  type: string;
  isTimestamp?: boolean;
  isSeverity?: boolean;
}

// Router and stores
const router = useRouter()
const route = useRoute()
const exploreStore = useExploreStore()
const teamsStore = useTeamsStore()
const sourcesStore = useSourcesStore()
const savedQueriesStore = useSavedQueriesStore()
const { toast } = useToast()
const { isInitializing, initializationError: urlError, initializeFromUrl, syncUrlFromState } = useExploreUrlSync(); // Use the composable

// Composables
const {
  localQueryError,
  isExecutingQuery,
  canExecuteQuery,
  isDirty,
  triggerQueryExecution
} = useQueryExecution()

const {
  isProcessingTeamChange,
  isProcessingSourceChange,
  currentTeamId,
  currentSourceId,
  sourceDetails,
  hasValidSource,
  availableTeams,
  availableSources,
  selectedTeamName,
  selectedSourceName,
  isFetchingTeams,
  isFetchingSources,
  availableFields,
  handleTeamChange,
  handleSourceChange
} = useSourceTeamManagement()

const {
  currentLogchefQuery,
  currentSqlQuery,
  handleModeChange
} = useQueryMode()

const {
  showSaveQueryModal,
  handleSaveQueryClick,
  handleSaveQuery,
  loadSavedQuery
} = useSavedQueries()

// Basic state
const showFieldsPanel = ref(false)
const tableColumns = ref<ColumnDef<Record<string, any>>[]>([])
const logchefQuery = ref('')
const sqlQuery = ref('')
const queryError = ref('')
const queryEditorRef = ref()

// UI state computed properties
const showLoadingState = computed(() => isInitializing.value && !urlError.value)
const showNoTeamsState = computed(() => !isInitializing.value && (!availableTeams.value || availableTeams.value.length === 0))
const showNoSourcesState = computed(() => 
  !isInitializing.value && 
  !showNoTeamsState.value && 
  currentTeamId.value > 0 && 
  (!availableSources.value || availableSources.value.length === 0)
)

// Use source details from the store
const activeSourceTableName = computed(() => sourcesStore.getCurrentSourceTableName || '');

// Add loading states for better UX
const isChangingTeam = ref(false)
const isChangingSource = ref(false)

// Date range computed property
const dateRange = computed({
  get() {
    return exploreStore.timeRange
  },
  set(value) {
    exploreStore.setTimeRange(value)
    // URL will be updated by the watch on exploreStore.timeRange
  }
})

// Add timezone preference for display
const displayTimezone = computed(() =>
  localStorage.getItem('logchef_timezone') === 'utc' ? 'utc' : 'local'
);

const pendingEditorInit = ref(false)

// Combined error display
const displayError = computed(() => localQueryError.value || exploreStore.error?.message)

// Watch for initialization completion to run initial query
watch(isInitializing, async (initializing, prevInitializing) => {
  if (prevInitializing && !initializing) {
    console.log("Initialization complete. Checking if initial query should run.")

    if (canExecuteQuery.value && (exploreStore.logchefqlCode || exploreStore.rawSql)) {
      console.log("Running initial query...")
      await triggerQueryExecution()
    } else {
      console.log("Skipping initial query (missing requirements)")
    }
  }
}, { immediate: false })

// Watch for source changes to fetch details
watch(currentSourceId, async (newSourceId, oldSourceId) => {
  if (newSourceId && newSourceId !== oldSourceId && !isInitializing.value) {
    setTimeout(async () => {
      if (exploreStore.sourceId === newSourceId) {
        await sourcesStore.loadSourceDetails(newSourceId)
      }
    }, 50)
  }
}, { immediate: false })

// Watch for time range changes to update SQL
watch(dateRange, (newTimeRange, oldTimeRange) => {
  if (isInitializing.value || exploreStore.activeMode !== 'sql' ||
    !hasValidSource.value || !newTimeRange || !oldTimeRange) {
    return
  }

  if (JSON.stringify(newTimeRange) === JSON.stringify(oldTimeRange)) {
    return
  }

  // Time range update handled by store watcher
})

// Mode changes now handled by handleModeChange function

// Handle editor query changes
const handleQueryChange = (data: { query: string, mode: string }) => {
  // Update local state for controlled components
  if (data.mode === 'logchefql') {
    logchefQuery.value = data.query;
  } else {
    sqlQuery.value = data.query;
  }
  // Store state is updated by two-way binding with computed properties
}

// Handle query submission from QueryEditor
const handleQuerySubmit = async (data: { query: string, mode: string }) => {
  // Check if we have a valid source before proceeding
  if (!hasValidSource.value) {
    toast({
      title: 'Source Not Ready',
      description: 'Please wait for source details to load or select a valid source.',
      variant: 'warning',
      duration: TOAST_DURATION.WARNING,
    });
    return;
  }

  const { query, mode: editorMode } = data;

  // Map the mode from editor to store format
  const storeMode = editorMode === 'logchefql' ? 'logchefql' : 'sql';

  // Update the store's active mode
  exploreStore.setActiveMode(storeMode);

  // Update the appropriate query state in the store
  if (storeMode === 'logchefql') {
    exploreStore.setLogchefqlCode(query);
    // Ensure local state is also updated for consistency
    logchefQuery.value = query;
  } else {
    // For SQL mode, just set the query as-is without any manipulation
    if (query && query.trim()) {
      exploreStore.setRawSql(query);
      sqlQuery.value = query;
    }
  }

  // Update URL with current parameters before executing the query
  syncUrlFromState(); // Call the sync function here

  // Clear any previous error
  queryError.value = '';

  try {
    // Build the final SQL based on the *current* store state and mode
    let sqlToExecute: string;
    if (storeMode === 'sql') {
      sqlToExecute = query;
    } else {
      const buildResult = QueryBuilder.buildSqlFromLogchefQL({
        tableName: activeSourceTableName.value,
        tsField: sourceDetails.value?._meta_ts_field || 'timestamp',
        startDateTime: exploreStore.timeRange!.start,
        endDateTime: exploreStore.timeRange!.end,
        limit: exploreStore.limit,
        logchefqlQuery: query
      });
      if (!buildResult.success) throw new Error(buildResult.error || "Failed to build query");
      sqlToExecute = buildResult.sql;
    }

    console.log('Setting last executed state:', {
      timeRange: JSON.stringify(exploreStore.timeRange),
      limit: exploreStore.limit,
      query: data.query
    });

    // Store current state before execution
    exploreStore.setLastExecutedState({
      timeRange: JSON.stringify(exploreStore.timeRange),
      limit: exploreStore.limit,
      query: data.query
    });

    // Execute the query with the determined final SQL
    const result = await exploreStore.executeQuery(sqlToExecute);

    if (!result.success && result.error) {
      queryError.value = result.error.message || 'An error occurred';
    } else {
      console.log('Query executed successfully, lastExecutedState:', exploreStore.lastExecutedState);
    }
  } catch (error) {
    console.error('Error executing query:', error);
    queryError.value = getErrorMessage(error);
  }
};

// Watch for changes in columns
watch(
  () => exploreStore.columns,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(
        newColumns,
        sourceDetails.value?._meta_ts_field || 'timestamp',
        localStorage.getItem('logchef_timezone') === 'utc' ? 'utc' : 'local'
      )
    }
  },
  { immediate: true }
)

// Watch for changes in sourceId to fetch source details - with debounce
watch(
  () => exploreStore.sourceId,
  async (newSourceId, oldSourceId) => {
    // Skip during initialization to prevent redundant calls
    if (isInitializing.value) {
      console.log(`Skipping source details update during initialization phase`);
      return;
    }

    if (newSourceId !== oldSourceId) {
      if (newSourceId > 0) {
        console.log(`Source changed from ${oldSourceId} to ${newSourceId}, checking details`);

        // Verify the source exists in the current team's sources
        const sourceExists = sourcesStore.teamSources.some(source => source.id === newSourceId);

        if (sourceExists) {
          // Use a timeout to debounce rapid changes
          setTimeout(async () => {
            // Check if the source ID is still the same after the timeout
            if (exploreStore.sourceId === newSourceId) {
              // Fetch details for the new source (with improved caching)
              await sourcesStore.loadSourceDetails(newSourceId);
            }
          }, 50);
        } else {
          console.warn(`Source ${newSourceId} not found in current team's sources`);
          sourceDetails.value = null;
        }
      } else if (newSourceId === 0) {
        // Clear source details when source is reset
        sourceDetails.value = null;
      }
    }
  }
)

// Watch for changes in currentTeamId to ensure sources are updated
watch(
  () => teamsStore.currentTeamId,
  async (newTeamId, oldTeamId) => {
    if (newTeamId !== oldTeamId && newTeamId) {
      console.log(`Team changed from ${oldTeamId} to ${newTeamId}, updating sources`)

      // Load sources for the new team
      const sourcesResult = await sourcesStore.loadTeamSources(newTeamId, true)

      // If no sources or sources failed to load, clear source selection
      if (!sourcesResult.success || !sourcesResult.data || sourcesResult.data.length === 0) {
        exploreStore.setSource(0)
        sourceDetails.value = null
      } else {
        // Check if current source is valid for the new team
        const currentSourceExists = sourcesStore.teamSources.some(
          source => source.id === exploreStore.sourceId
        )

        if (!currentSourceExists && sourcesStore.teamSources.length > 0) {
          // Select first source from the new team
          exploreStore.setSource(sourcesStore.teamSources[0].id)
          await sourcesStore.loadSourceDetails(sourcesStore.teamSources[0].id)
        }
      }

      // Update URL with new state
      // updateUrlWithCurrentState() // URL update now handled by composable watcher
    }
  }
)

// Watch for changes in sourceDetails from the store
watch(
  () => sourceDetails.value,
  (newSourceDetails, oldSourceDetails) => {
    if (newSourceDetails?.id !== oldSourceDetails?.id) {
      console.log('Source details changed in store, availableFields will update.');
      // Handle pending SQL restoration if needed (logic might move to store or be simplified)
      // Consider if pendingRawSql is still necessary with better store sync
    }
  },
  { immediate: true }
)

// Watch for time range changes to update SQL query reactively
watch(
  () => exploreStore.timeRange,
  (newTimeRange, oldTimeRange) => {
    // Skip during initialization or if mode is not SQL
    if (isInitializing.value || exploreStore.activeMode !== 'sql') {
      return;
    }

    // Ensure time range is valid and actually changed
    if (!newTimeRange || !newTimeRange.start || !newTimeRange.end || !sourceDetails.value) {
      console.warn("Skipping SQL time update: Invalid time range or missing source details.");
      return;
    }

    // Simple check if dates changed (more robust check might be needed if CalendarDateTime objects are complex)
    if (JSON.stringify(newTimeRange) === JSON.stringify(oldTimeRange)) {
      return;
    }

    console.log("LogExplorer: Time range changed, updating SQL query display.");
    const currentSql = exploreStore.rawSql;
    const tsField = sourceDetails.value._meta_ts_field || 'timestamp';

    // Attempt to parse the current SQL
    const parsedQuery = SQLParser.parse(currentSql, tsField);

    if (parsedQuery) {
      try {
        // Apply the new time range to the parsed AST
        const updatedAst = SQLParser.applyTimeRange(parsedQuery, tsField, newTimeRange.start, newTimeRange.end);
        const newSql = SQLParser.toSQL(updatedAst);

        // Update the store's rawSql - QueryEditor will react to this change
        exploreStore.setRawSql(newSql);

      } catch (error) {
        console.error("Error updating SQL query with new time range:", error);
        // Optionally show a warning toast
      }
    } else {
      console.warn("Could not parse existing SQL to update time range display. Query will use correct range on execution.");
      // Optional: Could generate default SQL here if parsing fails, but might overwrite user input unexpectedly.
    }
  },
  { deep: true } // Use deep watch for the timeRange object
);

// Component lifecycle with improved initialization sequence
onMounted(async () => {
  console.log("LogExplorer component mounting - Simplified");
  try {
    // Call the initialization function from the composable
    await initializeFromUrl();

    // Initial query execution moved to a watch effect for better reliability

  } catch (error) {
    console.error("Error during LogExplorer mount:", error);
    toast({
      title: "Explorer Error",
      description: "Error initializing the explorer. Please try refreshing the page.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
});

onBeforeUnmount(() => {
  if (import.meta.env.MODE !== 'production') {
    console.log("LogExplorer unmounted")
  }
})
</script>

<template>
  <!-- Loading State -->
  <div v-if="showLoadingState" class="flex items-center justify-center h-[calc(100vh-12rem)]">
    <p class="text-muted-foreground animate-pulse">Loading Explorer...</p>
  </div>

  <!-- No Teams State -->
  <div v-else-if="showNoTeamsState"
    class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4 text-center">
    <h2 class="text-2xl font-semibold">No Teams Available</h2>
    <p class="text-muted-foreground max-w-md">
      You need to be part of a team to explore logs. Contact your administrator.
    </p>
    <Button variant="outline" @click="router.push({ name: 'Home' })">Go to Dashboard</Button>
  </div>

  <!-- No Sources State (Team Selected) -->
  <div v-else-if="showNoSourcesState" class="flex flex-col h-[calc(100vh-12rem)]">
    <!-- Header bar for team selection -->
    <div class="border-b py-2 px-3 flex items-center gap-3">
      <Select :model-value="currentTeamId?.toString() ?? ''" @update:model-value="handleTeamChange" class="w-40"
        :disabled="isProcessingTeamChange">
        <SelectTrigger class="h-8 text-sm">
          <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
            {{ team.name }}
          </SelectItem>
        </SelectContent>
      </Select>
      <span class="text-sm text-muted-foreground italic">No sources in this team.</span>
      <Button size="sm" class="ml-auto h-8" @click="router.push({ name: 'NewSource' })">
        Add Source
      </Button>
    </div>
    <!-- Empty state content -->
    <div class="flex flex-col items-center justify-center flex-1 gap-4 text-center">
      <h2 class="text-2xl font-semibold">No Log Sources Found</h2>
      <p class="text-muted-foreground max-w-md">
        The selected team '{{ selectedTeamName }}' has no sources configured. Add one or switch teams.
      </p>
    </div>
  </div>

  <!-- Main Explorer View -->
  <div v-else class="flex flex-col h-screen overflow-hidden">
    <!-- URL Error -->
    <div v-if="urlError"
      class="absolute top-0 left-0 right-0 bg-destructive/15 text-destructive px-4 py-2 z-10 flex items-center justify-between">
      <span class="text-sm">{{ urlError }}</span>
      <Button variant="ghost" size="sm" @click="initializationError = null" class="h-7 px-2">Dismiss</Button>
    </div>

    <!-- Filter Bar -->
    <div class="border-b bg-background py-2 px-3 flex flex-wrap items-center gap-x-3 gap-y-2">
      <!-- Team/Source Selectors -->
      <Select :model-value="currentTeamId?.toString() ?? ''" @update:model-value="handleTeamChange" class="w-36"
        :disabled="isProcessingTeamChange">
        <SelectTrigger class="h-8 text-sm">
          <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
            {{ team.name }}
          </SelectItem>
        </SelectContent>
      </Select>

      <Select :model-value="currentSourceId?.toString() ?? ''" @update:model-value="handleSourceChange" class="w-48"
        :disabled="isProcessingSourceChange || !currentTeamId || availableSources.length === 0">
        <SelectTrigger class="h-8 text-sm">
          <SelectValue placeholder="Select source">{{ selectedSourceName }}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-if="!currentTeamId" value="no-team" disabled>Select a team first</SelectItem>
          <SelectItem v-else-if="availableSources.length === 0" value="no-sources" disabled>No sources available</SelectItem>
          <SelectItem v-for="source in availableSources" :key="source.id" :value="source.id.toString()">
            {{ formatSourceName(source) }}
          </SelectItem>
        </SelectContent>
      </Select>

      <!-- Date/Time Picker -->
      <DateTimePicker v-model="dateRange" class="h-8" />

      <!-- Limit Dropdown -->
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm" class="h-8 w-[90px] text-sm justify-between px-2">
            <span>Limit:</span> {{ exploreStore.limit.toLocaleString() }}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>Results Limit</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem v-for="limit in [100, 500, 1000, 2000, 5000, 10000]" :key="limit"
            @click="exploreStore.setLimit(limit)" :disabled="exploreStore.limit === limit">
            {{ limit.toLocaleString() }} rows
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <!-- Actions -->
      <div class="ml-auto flex items-center gap-1.5">
        <SavedQueriesDropdown :source-id="currentSourceId" :team-id="currentTeamId" :use-current-team="true"
          @select="loadSavedQuery" @save="handleSaveQueryClick" class="h-8" />
        <Button variant="default" size="sm" class="h-8 px-3 flex items-center gap-1.5 transition-colors duration-200"
          :class="{ 'bg-primary/80 hover:bg-primary/70': isDirty && !isExecutingQuery }"
          :disabled="isExecutingQuery || !canExecuteQuery" @click="triggerQueryExecution">
          <Play class="h-3.5 w-3.5" />
          <span>{{ isExecutingQuery ? 'Running...' : (isDirty ? 'Run*' : 'Run') }}</span>
        </Button>
      </div>
    </div>

    <!-- Main Content Area -->
    <div class="flex flex-1 min-h-0">
      <FieldSideBar v-model:expanded="showFieldsPanel" :fields="availableFields" />

      <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden">
        <!-- Query Editor -->
        <div class="p-3 pb-2">
          <template v-if="currentSourceId && hasValidSource && exploreStore.timeRange">
            <QueryEditor ref="queryEditorRef" :sourceId="currentSourceId"
              :schema="sourceDetails?.columns?.reduce((acc, col) => ({ ...acc, [col.name]: { type: col.type } }), {}) || {}"
              :activeMode="exploreStore.activeMode === 'logchefql' ? 'logchefql' : 'clickhouse-sql'"
              :logchefqlValue="currentLogchefQuery" :sqlValue="currentSqlQuery"
              @update:logchefqlValue="currentLogchefQuery = $event" @update:sqlValue="currentSqlQuery = $event"
              :placeholder="exploreStore.activeMode === 'logchefql' ? 'Enter LogchefQL query...' : 'Enter SQL query...'"
              :tsField="sourceDetails?._meta_ts_field || 'timestamp'" :tableName="activeSourceTableName"
              :showFieldsPanel="showFieldsPanel" @change="handleQueryChange" @submit="triggerQueryExecution"
              @update:activeMode="handleModeChange" @toggle-fields="showFieldsPanel = !showFieldsPanel"
              class="rounded-md border bg-card" />
          </template>
          <template v-else-if="currentTeamId && !currentSourceId">
            <div class="text-center text-muted-foreground p-4 border rounded-md bg-muted/30">
              Please select a log source to begin exploring.
            </div>
          </template>
          <template v-else-if="currentSourceId && !hasValidSource">
            <div class="text-center text-destructive p-4 border border-destructive/50 rounded-md bg-destructive/10">
              Error loading source details. Please try again.
            </div>
          </template>

          <!-- Query Error -->
          <div v-if="displayError" class="mt-2 text-sm text-destructive bg-destructive/10 p-2 rounded">
            {{ displayError }}
          </div>
        </div>

        <!-- Results Section -->
        <div class="flex-1 overflow-hidden flex flex-col border-t">
          <!-- Stats Header -->
          <div v-if="exploreStore.queryStats && !isExecutingQuery"
            class="px-3 py-1.5 flex items-center justify-between bg-muted/20 text-xs flex-shrink-0 border-b">
            <div class="flex items-center gap-3">
              <span v-if="exploreStore.queryStats?.rows_read != null">
                Fetched: <strong class="font-medium">{{ exploreStore.queryStats.rows_read.toLocaleString() }}</strong>
                rows
              </span>
              <span v-if="exploreStore.queryStats?.execution_time_ms != null">
                Time: <strong class="font-medium">{{ (exploreStore.queryStats.execution_time_ms / 1000).toFixed(2)
                }}s</strong>
              </span>
              <span v-if="exploreStore.queryStats?.bytes_read != null">
                Data: <strong class="font-medium">{{ (exploreStore.queryStats.bytes_read / 1024 / 1024).toFixed(2) }}
                  MB</strong>
              </span>
            </div>
          </div>

          <!-- Results Area -->
          <div class="flex-1 overflow-hidden relative bg-background">
            <!-- Loading State -->
            <template v-if="isExecutingQuery">
              <div class="absolute inset-0 flex items-center justify-center bg-background/70 z-10">
                <p class="text-muted-foreground animate-pulse">Loading results...</p>
              </div>
            </template>

            <!-- Results Table -->
            <template v-if="!isExecutingQuery && exploreStore.logs?.length">
              <DataTable class="absolute inset-0 h-full w-full" :columns="tableColumns" :data="exploreStore.logs"
                :stats="exploreStore.queryStats" :source-id="currentSourceId?.toString() ?? ''"
                :timestamp-field="sourceDetails?._meta_ts_field" :severity-field="sourceDetails?._meta_severity_field"
                :timezone="displayTimezone" />
            </template>

            <!-- No Results State -->
            <template v-else-if="!isExecutingQuery && !exploreStore.logs?.length && exploreStore.lastExecutedState">
              <div class="h-full flex flex-col items-center justify-center p-10 text-center">
                <svg xmlns="http://www.w3.org/2000/svg" width="36" height="36" viewBox="0 0 24 24" fill="none"
                  stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                  class="text-muted-foreground mb-3">
                  <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"></path>
                  <polyline points="14 2 14 8 20 8"></polyline>
                  <line x1="12" x2="12" y1="18" y2="12"></line>
                  <line x1="9" x2="15" y1="15" y2="15"></line>
                </svg>
                <h3 class="text-lg font-medium mb-1">No Logs Found</h3>
                <p class="text-sm text-muted-foreground max-w-md">
                  Your query returned no results for the selected time range. Try adjusting the query or time.
                </p>
                <Button variant="outline" size="sm" class="mt-4 h-8" @click="exploreStore.setTimeRange({
                  start: now(getLocalTimeZone()).subtract({ hours: 24 }),
                  end: now(getLocalTimeZone())
                })">
                  Try Last 24 Hours
                </Button>
              </div>
            </template>

            <!-- Initial State -->
            <template v-else-if="!isExecutingQuery && !exploreStore.lastExecutedState && canExecuteQuery">
              <div class="h-full flex flex-col items-center justify-center p-10 text-center">
                <svg xmlns="http://www.w3.org/2000/svg" width="36" height="36" viewBox="0 0 24 24" fill="none"
                  stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                  class="text-muted-foreground mb-3">
                  <circle cx="11" cy="11" r="8"></circle>
                  <line x1="21" x2="16.65" y1="21" y2="16.65"></line>
                  <line x1="11" x2="11" y1="8" y2="14"></line>
                  <line x1="8" x2="14" y1="11" y2="11"></line>
                </svg>
                <h3 class="text-lg font-medium mb-1">Ready to Explore</h3>
                <p class="text-sm text-muted-foreground max-w-md">
                  Enter a query or use the default, then click 'Run' to see logs.
                </p>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- Save Query Modal -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-type="exploreStore.activeMode"
      :query-content="JSON.stringify({
        sourceId: currentSourceId,
        limit: exploreStore.limit,
        content: exploreStore.activeMode === 'logchefql' ? exploreStore.logchefqlCode : exploreStore.rawSql
      })" @close="showSaveQueryModal = false" @save="handleSaveQuery" />
  </div>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}

/* Add fade transition for the SQL preview */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

/* Improved table height handling */
.h-full {
  height: 100% !important;
}

/* Enhanced flex layout for proper table expansion */
.flex.flex-1.min-h-0 {
  display: flex;
  width: 100%;
  min-height: 0;
  flex: 1 1 auto !important;
  max-height: 100%;
}

.flex.flex-1.min-h-0>div:last-child {
  flex: 1 1 auto;
  min-width: 0;
  width: 100%;
  display: flex;
  flex-direction: column;
}

/* Fix for main content area to ensure full height expansion */
.flex-1.flex.flex-col.h-full.min-w-0 {
  flex: 1 1 auto !important;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100% !important;
  max-height: 100%;
}

/* Force the results section to expand fully */
.flex-1.overflow-hidden.flex.flex-col.border-t {
  flex: 1 1 auto !important;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100% !important;
}

/* Fix DataTable height issues */
:deep(.datatable-wrapper) {
  height: 100%;
  display: flex;
  flex-direction: column;
}

:deep(.datatable-container) {
  flex: 1;
  overflow: auto;
}

/* Enhance table styling */
:deep(.table) {
  border-collapse: separate;
  border-spacing: 0;
}

:deep(.table th) {
  background-color: hsl(var(--muted));
  font-weight: 500;
  text-align: left;
  font-size: 0.85rem;
  color: hsl(var(--muted-foreground));
  padding: 0.75rem 1rem;
}

:deep(.table td) {
  padding: 0.65rem 1rem;
  border-bottom: 1px solid hsl(var(--border));
  font-size: 0.9rem;
}

:deep(.table tr:hover td) {
  background-color: hsl(var(--muted)/0.3);
}


/* Severity label styling */
:deep(.severity-label) {
  border-radius: 4px;
  padding: 2px 6px;
  font-size: 0.75rem;
  font-weight: 500;
  display: inline-block;
}

:deep(.severity-error) {
  background-color: hsl(var(--destructive)/0.15);
  color: hsl(var(--destructive));
}

:deep(.severity-warn),
:deep(.severity-warning) {
  background-color: hsl(var(--warning)/0.15);
  color: hsl(var(--warning));
}

:deep(.severity-info) {
  background-color: hsl(var(--info)/0.15);
  color: hsl(var(--info));
}

:deep(.severity-debug) {
  background-color: hsl(var(--muted)/0.5);
  color: hsl(var(--muted-foreground));
}
</style>
