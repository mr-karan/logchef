<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Plus, Search, Play } from 'lucide-vue-next'
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
import QueryEditor from '@/components/editor/QueryEditor.vue'
import { parseLogchefQL, translateLogchefQLToSQL } from '@/utils/logchefql/api'
import type { ColumnDef } from '@tanstack/vue-table'
import type { SavedQueryContent } from '@/api/types'
import type { Source } from '@/api/sources'

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

// Basic state
const showSaveQueryModal = ref(false)
const tableColumns = ref<ColumnDef<Record<string, any>>[]>([])
const logchefQuery = ref('')
const sqlQuery = ref('')
const queryError = ref('')
const queryMode = ref('dsl') // 'dsl' or 'sql'
const generatedSQL = ref('')
const sqlParams = ref<Array<string | number>>([])
const isExecutingQuery = ref(false)
const sourceDetails = ref<Source | null>(null)
const queryEditorRef = ref()

// Loading and empty states
const showLoadingState = computed(() => {
  return sourcesStore.isLoading
})

const showEmptyState = computed(() => {
  return !showLoadingState.value && (!sourcesStore.teamSources || sourcesStore.teamSources.length === 0)
})

// Selected team name
const selectedTeamName = computed(() => {
  return teamsStore.currentTeam?.name || 'Select a team'
})

// Selected source name
const selectedSourceName = computed(() => {
  if (!exploreStore.data.sourceId) return 'Select a source'
  const source = sourcesStore.teamSources?.find(s => s.id === exploreStore.data.sourceId)
  return source ? formatSourceName(source) : 'Select a source'
})

// Date range computed property
const dateRange = computed({
  get() {
    return exploreStore.data.timeRange
  },
  set(value) {
    exploreStore.setTimeRange(value)
  }
})

// Initialize from URL parameters
function initializeFromURL() {
  if (route.query.team && typeof route.query.team === 'string') {
    teamsStore.setCurrentTeam(parseInt(route.query.team))
  }

  if (route.query.source && typeof route.query.source === 'string') {
    exploreStore.setSource(parseInt(route.query.source))
  }

  if (route.query.limit && typeof route.query.limit === 'string') {
    exploreStore.setLimit(parseInt(route.query.limit))
  } else {
    exploreStore.setLimit(100)
  }

  const currentTime = now(getLocalTimeZone())
  const defaultStart = currentTime.subtract({ hours: 3 })
  const defaultEnd = currentTime

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

      // Create CalendarDateTime objects
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
      exploreStore.setTimeRange({
        start: defaultStart,
        end: defaultEnd
      })
    }
  } else {
    exploreStore.setTimeRange({
      start: defaultStart,
      end: defaultEnd
    })
  }
}

// Handle team change
async function handleTeamChange(teamId: string) {
  const parsedTeamId = parseInt(teamId)
  teamsStore.setCurrentTeam(parsedTeamId)

  // Load sources for the selected team
  await sourcesStore.loadTeamSources(parsedTeamId)

  // Set a default source if sources are available
  if (sourcesStore.teamSources.length > 0) {
    // Check if current source is in the new team
    const currentSourceExists = sourcesStore.teamSources.some(
      source => source.id === exploreStore.data.sourceId
    )
    
    if (!currentSourceExists) {
      // Select the first source if current one isn't available
      exploreStore.setSource(sourcesStore.teamSources[0].id)
      await fetchSourceDetails(sourcesStore.teamSources[0].id)
    }
  } else {
    // No sources in this team
    exploreStore.setSource(0)
    sourceDetails.value = null
  }
}

// Fetch source details
async function fetchSourceDetails(sourceId: number) {
  if (!sourceId || !teamsStore.currentTeamId) {
    sourceDetails.value = null
    return
  }

  try {
    const source = await sourcesStore.getSource(sourceId)
    sourceDetails.value = source
    
    // Log basic info for debugging
    if (source) {
      console.log(`Source details loaded for ID ${sourceId}`)
    } else {
      console.warn(`No source details returned for source ID: ${sourceId}`)
    }
  } catch (error) {
    console.error('Error fetching source details:', error)
    sourceDetails.value = null
  }
}

// Handle source change
async function handleSourceChange(sourceId: string) {
  if (!sourceId) {
    sourceDetails.value = null
    return
  }

  const id = parseInt(sourceId)
  exploreStore.setSource(id)
  await fetchSourceDetails(id)
}

// Handle loading saved query
async function loadSavedQuery(queryId: string) {
  try {
    // Use the current team ID from teamsStore instead of fetching teams
    const teamId = teamsStore.currentTeamId
    if (!teamId) {
      toast({
        title: 'Error',
        description: 'No team selected. Please select a team first.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      return
    }

    const queryResponse = await savedQueriesStore.fetchQuery(
      teamId,
      queryId
    )

    if (!queryResponse.success || !queryResponse.data) {
      throw new Error('Failed to load query')
    }

    const query = queryResponse.data
    const queryContent = JSON.parse(query.query_content) as SavedQueryContent

    // Update store with query data
    if (queryContent.activeTab === 'filters') {
      // Reset any existing SQL
      exploreStore.setRawSql('')
    } else {
      // Set raw SQL
      exploreStore.setRawSql(queryContent.rawSql)
    }

    if (queryContent.timeRange) {
      exploreStore.setTimeRange({
        start: new CalendarDateTime(
          new Date(queryContent.timeRange.absolute.start).getFullYear(),
          new Date(queryContent.timeRange.absolute.start).getMonth() + 1,
          new Date(queryContent.timeRange.absolute.start).getDate(),
          new Date(queryContent.timeRange.absolute.start).getHours(),
          new Date(queryContent.timeRange.absolute.start).getMinutes(),
          new Date(queryContent.timeRange.absolute.start).getSeconds()
        ),
        end: new CalendarDateTime(
          new Date(queryContent.timeRange.absolute.end).getFullYear(),
          new Date(queryContent.timeRange.absolute.end).getMonth() + 1,
          new Date(queryContent.timeRange.absolute.end).getDate(),
          new Date(queryContent.timeRange.absolute.end).getHours(),
          new Date(queryContent.timeRange.absolute.end).getMinutes(),
          new Date(queryContent.timeRange.absolute.end).getSeconds()
        )
      })
    }

    if (queryContent.limit) {
      exploreStore.setLimit(queryContent.limit)
    }

    // Execute the query
    await exploreStore.executeQuery()

    toast({
      title: 'Success',
      description: 'Query loaded successfully',
      duration: TOAST_DURATION.SUCCESS,
    })
  } catch (error) {
    toast({
      title: 'Error',
      description: error instanceof Error ? error.message : 'Failed to load query',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}

// Get available fields for auto-completion
const availableFields = computed((): FieldInfo[] => {
  // Use columns from source details or query results
  let fields: FieldInfo[] = [];
  
  if (sourceDetails.value?.columns?.length > 0) {
    fields = sourceDetails.value.columns.map(column => ({
      name: column.name,
      type: column.type
    }));
  } else if (exploreStore.data.columns?.length > 0) {
    fields = exploreStore.data.columns.map(column => ({
      name: column.name,
      type: column.type
    }));
  }

  // Add special metadata fields if available
  if (sourceDetails.value) {
    // Add timestamp field if not already in the list
    if (sourceDetails.value._meta_ts_field) {
      const tsField = fields.find(f => f.name === sourceDetails.value?._meta_ts_field);
      if (tsField) {
        tsField.isTimestamp = true;
      } else {
        fields.push({
          name: sourceDetails.value._meta_ts_field,
          type: 'timestamp',
          isTimestamp: true
        });
      }
    }

    // Add severity field if not already in the list
    if (sourceDetails.value._meta_severity_field) {
      const severityField = fields.find(f => f.name === sourceDetails.value?._meta_severity_field);
      if (severityField) {
        severityField.isSeverity = true;
      } else {
        fields.push({
          name: sourceDetails.value._meta_severity_field,
          type: 'string',
          isSeverity: true
        });
      }
    }
  }

  return fields;
});

// Watch for changes in columns
watch(
  () => exploreStore.data.columns,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(newColumns)
    }
  },
  { immediate: true }
)

// Watch for changes in sourceDetails
watch(
  () => sourceDetails.value,
  (newSourceDetails) => {
    if (newSourceDetails) {
      console.log('Source details changed, updating available fields')
      // The availableFields computed property will automatically update
    }
  },
  { immediate: true }
)

// Watch for changes in availableFields
watch(
  () => availableFields.value,
  (newFields) => {
    if (newFields && newFields.length > 0) {
      console.log('Available fields for LogchefQL editor:', newFields)
    }
  },
  { immediate: true }
)

// Handle query changes from editor
const handleQueryChange = (query: string) => {
  // If in DSL mode, update the LogchefQL query
  if (queryMode.value === 'dsl') {
    logchefQuery.value = query;
  } else {
    // In SQL mode, update the SQL query
    sqlQuery.value = query;
    console.log('SQL query updated:', sqlQuery.value);
  }

  // Always clear any error messages when the user is typing
  if (queryError.value) {
    queryError.value = '';
  }
}

// Handle mode changes between LogchefQL and SQL
const handleModeChange = (mode: string) => {
  queryMode.value = mode;

  // Simple test for LogchefQL -> SQL conversion
  if (mode === 'sql' && logchefQuery.value.trim() !== '') {
    try {
      console.log('Testing LogchefQL -> SQL conversion:');
      console.log('LogchefQL:', logchefQuery.value);

      // This should convert the LogchefQL to SQL without time range for display
      const result = translateLogchefQLToSQL(logchefQuery.value, {
        table: activeSourceName.value,
        includeTimeRange: false
      });

      console.log('Converted SQL for display:', result);
    } catch (error) {
      console.error('Error in test conversion:', error);
    }
  }

  // Clear any previous errors when switching modes
  queryError.value = '';
}

// Get the active source
const activeSource = computed(() => {
  if (!exploreStore.data.sourceId) return null;
  return sourcesStore.teamSources?.find(s => s.id === exploreStore.data.sourceId) || null;
});

// Get the active source table name (formatted as database.table_name)
const activeSourceTableName = computed(() => {
  if (sourceDetails.value?.connection) {
    return `${sourceDetails.value.connection.database}.${sourceDetails.value.connection.table_name}`;
  }
  return 'unknown.unknown'; // Default fallback
});

// Active source name for display, used for query templates
const activeSourceName = computed(() => {
  return activeSourceTableName.value;
});

// Handle query submission from either mode
const handleQuerySubmit = async (mode: string) => {
  // Use the current mode if not explicitly provided
  const currentMode = mode || queryMode.value;

  if (currentMode === 'dsl') {
    // Execute LogchefQL query
    await executeQuery();
  } else {
    // Execute SQL query directly
    await executeSQLQuery();
  }
};

// Execute the LogchefQL query
const executeQuery = async () => {
  if (!exploreStore.canExecuteQuery) {
    queryError.value = 'Please select a source and time range before executing the query';
    return;
  }

  try {
    isExecutingQuery.value = true;
    queryError.value = '';

    // Only at this point do we actually parse the query
    if (!logchefQuery.value.trim()) {
      queryError.value = 'Please enter a query';
      isExecutingQuery.value = false;
      return;
    }

    try {
      // Parse the LogchefQL and convert to SQL with correct source table name
      // For query execution, use includeTimeRange=true to include time conditions
      const basicSql = translateLogchefQLToSQL(logchefQuery.value, {
        table: activeSourceTableName.value,
        includeTimeRange: true,
        startTime: timeRange.value.start,
        endTime: timeRange.value.end,
        limit: queryLimit.value
      });

      // Store for reference
      generatedSQL.value = basicSql;

      // Pass this SQL to the explore store which will add time conditions and limit
      exploreStore.setRawSql(basicSql);

      // Execute the query (time range and limit will be added in the store)
      const result = await exploreStore.executeQuery();

      if (result && 'error' in result) {
        queryError.value = result.error as string;
      }
    } catch (error) {
      // Only show an error if the user explicitly tries to run an invalid query
      queryError.value = 'Error parsing query. Please check your syntax and try again.';
      isExecutingQuery.value = false;
      return;
    }
  } catch (error) {
    queryError.value = error instanceof Error ? error.message : 'An error occurred while executing the query';
  } finally {
    isExecutingQuery.value = false;
  }
};

// Execute a raw SQL query
const executeSQLQuery = async () => {
  if (!exploreStore.canExecuteQuery) {
    queryError.value = 'Please select a source and time range before executing the query';
    return;
  }

  try {
    isExecutingQuery.value = true;
    queryError.value = '';

    console.log('Executing SQL query:', sqlQuery.value);

    // Check for SQL query - if empty, use the default SQL query
    if (!sqlQuery.value.trim()) {
      // Use a default query if none provided
      sqlQuery.value = `SELECT * FROM ${activeSourceTableName.value}`;
      console.log('Using default SQL query:', sqlQuery.value);
    }

    // Store the SQL for reference
    generatedSQL.value = sqlQuery.value;

    // Pass the SQL to the store which will add time conditions and limit
    exploreStore.setRawSql(sqlQuery.value);

    // Execute the query
    const result = await exploreStore.executeQuery();

    if (result && 'error' in result) {
      queryError.value = result.error as string;
    }
  } catch (error) {
    queryError.value = error instanceof Error ? error.message : 'An error occurred while executing the SQL query';
  } finally {
    isExecutingQuery.value = false;
  }
}

// Component lifecycle
onMounted(async () => {
  try {
    console.log("LogExplorer component mounting")

    // Initialize from URL parameters first
    initializeFromURL()

    // Load teams
    await teamsStore.loadTeams()

    // Set default team if none selected
    if (!teamsStore.currentTeamId && teamsStore.teams.length > 0) {
      teamsStore.setCurrentTeam(teamsStore.teams[0].id)
    }

    // Load sources for the current team
    if (teamsStore.currentTeamId) {
      await sourcesStore.loadTeamSources(teamsStore.currentTeamId)
      
      // Set default source if none selected
      if (!exploreStore.data.sourceId && sourcesStore.teamSources.length > 0) {
        exploreStore.setSource(sourcesStore.teamSources[0].id)
        await fetchSourceDetails(sourcesStore.teamSources[0].id)
      } else if (exploreStore.data.sourceId) {
        await fetchSourceDetails(exploreStore.data.sourceId)
      }
    }
  } catch (error) {
    console.error("Error during LogExplorer mount:", error)
  }
})

onBeforeUnmount(() => {
  if (import.meta.env.MODE !== 'production') {
    console.log("LogExplorer unmounted")
  }
})
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

  <!-- Main content when not loading -->
  <div v-else class="flex flex-col gap-5">
    <!-- Navigation and Controls Section with improved design -->
    <div class="border-b pb-3 mb-2">
      <div class="flex items-center justify-between">
        <!-- Left: Source Navigation (breadcrumb style) -->
        <div class="flex items-center space-x-2 text-sm">
          <Select :model-value="teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : ''"
            @update:model-value="handleTeamChange" class="h-8 min-w-[160px]">
            <SelectTrigger>
              <SelectValue placeholder="Select a team">{{ selectedTeamName }}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id.toString()">
                {{ team.name }}
              </SelectItem>
            </SelectContent>
          </Select>

          <span class="text-muted-foreground">→</span>

          <Select :model-value="exploreStore.data.sourceId ? exploreStore.data.sourceId.toString() : ''"
            @update:model-value="handleSourceChange"
            :disabled="!teamsStore.currentTeamId || (sourcesStore.teamSources || []).length === 0"
            class="h-8 min-w-[200px]">
            <SelectTrigger>
              <SelectValue placeholder="Select a source">{{ selectedSourceName }}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="source in sourcesStore.teamSources || []" :key="source.id"
                :value="source.id.toString()">
                {{ formatSourceName(source) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <!-- Right: Time Range and Action Buttons -->
        <div class="flex items-center">
          <!-- Date Time Picker with enough width for full timestamps -->
          <DateTimePicker v-model="dateRange" class="h-8 min-w-[380px] max-w-[400px] truncate" />

          <!-- Vertical separator -->
          <div class="h-6 w-px bg-border mx-2.5"></div>

          <!-- Limit Dropdown -->
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" class="h-8 min-w-[100px]">
                Limit: {{ exploreStore.data.limit.toLocaleString() }}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuLabel>Results Limit</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem @click="exploreStore.setLimit(100)" :disabled="exploreStore.data.limit === 100">
                100 rows
              </DropdownMenuItem>
              <DropdownMenuItem @click="exploreStore.setLimit(500)" :disabled="exploreStore.data.limit === 500">
                500 rows
              </DropdownMenuItem>
              <DropdownMenuItem @click="exploreStore.setLimit(1000)" :disabled="exploreStore.data.limit === 1000">
                1,000 rows
              </DropdownMenuItem>
              <DropdownMenuItem @click="exploreStore.setLimit(2000)" :disabled="exploreStore.data.limit === 2000">
                2,000 rows
              </DropdownMenuItem>
              <DropdownMenuItem @click="exploreStore.setLimit(5000)" :disabled="exploreStore.data.limit === 5000">
                5,000 rows
              </DropdownMenuItem>
              <DropdownMenuItem @click="exploreStore.setLimit(10000)" :disabled="exploreStore.data.limit === 10000">
                10,000 rows
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <!-- Vertical separator -->
          <div class="h-6 w-px bg-border mx-2.5"></div>

          <div class="flex items-center gap-2">
            <SavedQueriesDropdown :source-id="exploreStore.data.sourceId" :team-id="teamsStore.currentTeamId"
              :use-current-team="true" @load-query="loadSavedQuery" />

            <Button size="sm" variant="outline" class="h-8" @click="showSaveQueryModal = true">
              Save
            </Button>

            <Button variant="default" size="sm" class="h-8 px-3 flex items-center gap-1"
              :disabled="isExecutingQuery || !exploreStore.canExecuteQuery" @click="handleQuerySubmit(queryMode)">
              <Play class="h-3.5 w-3.5" />
              <span>{{ isExecutingQuery ? 'Running...' : 'Run' }}</span>
            </Button>
          </div>
        </div>
      </div>
    </div>

    <!-- Condensed Query Builder -->
    <div class="rounded-md border bg-card shadow-sm mb-4">
      <div class="flex items-start">
        <div class="flex-1">
          <QueryEditor ref="queryEditorRef" v-model="logchefQuery" :available-fields="availableFields"
            :placeholder="'Enter LogchefQL query (e.g. service_name=\'api\' and severity_text=\'error\')'"
            :error="queryError" :table-name="activeSourceTableName" height="32" @change="handleQueryChange"
            @submit="handleQuerySubmit" @mode-change="handleModeChange" />
        </div>
      </div>

      <!-- Helptext below editor with better padding -->
      <div class="pl-0 pr-4 py-2 text-xs text-muted-foreground bg-muted/20 border-t">
        <span v-if="queryMode === 'dsl'" class="italic">
          Filter conditions only, e.g. service_name='api' and severity_level='error'. Time filter and LIMIT are applied
          automatically.
        </span>
        <span v-if="queryMode === 'sql'" class="italic">
          Write a complete SQL query. Time filter and LIMIT will be applied automatically to your query.
        </span>
      </div>
    </div>

    <!-- Results Section -->
    <div class="flex-1 border rounded-md shadow-sm bg-card overflow-hidden">
      <DataTable :columns="tableColumns" :data="exploreStore.data.logs || []" :stats="exploreStore.data.queryStats"
        :source-id="exploreStore.data.sourceId?.toString() || ''" />
    </div>

    <!-- Save Query Modal -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-content="JSON.stringify({
      filter_conditions: exploreStore.data.filterConditions,
      raw_sql: exploreStore.data.rawSql,
      logchefql_query: logchefQuery,
      sql_query: sqlQuery,
      query_mode: queryMode,
      generated_sql: generatedSQL,
      time_range: exploreStore.data.timeRange,
      limit: exploreStore.data.limit
    })" @close="showSaveQueryModal = false" />
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
</style>
