<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Plus, Play, Share } from 'lucide-vue-next'
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
import { translateLogchefQLToSQL } from '@/utils/logchefql/api'
import type { ColumnDef } from '@tanstack/vue-table'
import type { SavedQueryContent } from '@/api/types'
import type { Source } from '@/api/sources'
import { getErrorMessage } from '@/api/types'

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
const queryMode = computed({
  get: () => exploreStore.activeMode,
  set: (value) => exploreStore.setActiveMode(value)
})
const generatedSQL = ref('')
const isExecutingQuery = ref(false)
const sourceDetails = ref<Source | null>(null)
const queryEditorRef = ref()

// Add loading states for better UX
const isChangingTeam = ref(false)
const isChangingSource = ref(false)
const urlError = ref<string | null>(null)

// Loading and empty states
const showLoadingState = computed(() => {
  return sourcesStore.isLoading || isChangingTeam.value
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
  if (!exploreStore.sourceId) return 'Select a source'
  const source = sourcesStore.teamSources?.find(s => s.id === exploreStore.sourceId)
  return source ? formatSourceName(source) : 'Select a source'
})

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

// Function to update URL with current state
function updateUrlWithCurrentState() {
  const currentTime = exploreStore.timeRange
  const query: Record<string, string> = {}

  // Add team and source IDs
  if (teamsStore.currentTeamId) {
    query.team = teamsStore.currentTeamId.toString()
  }

  // Only add source ID if it's valid (non-zero)
  if (exploreStore.sourceId && exploreStore.sourceId > 0) {
    // Verify the source exists in the current team's sources
    const sourceExists = sourcesStore.teamSources.some(source => source.id === exploreStore.sourceId)

    if (sourceExists) {
      query.source = exploreStore.sourceId.toString()
    }
  }

  // Add limit
  query.limit = exploreStore.limit.toString()

  // Add time range if available
  if (currentTime) {
    // Convert to timestamps for URL
    const startDate = new Date(
      currentTime.start.year,
      currentTime.start.month - 1,
      currentTime.start.day,
      'hour' in currentTime.start ? currentTime.start.hour : 0,
      'minute' in currentTime.start ? currentTime.start.minute : 0,
      'second' in currentTime.start ? currentTime.start.second : 0
    )

    const endDate = new Date(
      currentTime.end.year,
      currentTime.end.month - 1,
      currentTime.end.day,
      'hour' in currentTime.end ? currentTime.end.hour : 0,
      'minute' in currentTime.end ? currentTime.end.minute : 0,
      'second' in currentTime.end ? currentTime.end.second : 0
    )

    query.start_time = startDate.getTime().toString()
    query.end_time = endDate.getTime().toString()
  }

  // Add query mode
  if (queryMode.value) {
    query.mode = queryMode.value
  }

  // Add the actual query based on the mode
  if (queryMode.value === 'dsl' && logchefQuery.value.trim()) {
    query.q = encodeURIComponent(logchefQuery.value.trim())
  } else if (queryMode.value === 'sql' && sqlQuery.value.trim()) {
    query.q = encodeURIComponent(sqlQuery.value.trim())
  }

  // Update URL without triggering navigation events
  router.replace({ query })
}

// Initialize from URL parameters
async function initializeFromURL() {
  try {
    urlError.value = null

    // Set default time range
    const currentTime = now(getLocalTimeZone())
    const defaultStart = currentTime.subtract({ hours: 3 })
    const defaultEnd = currentTime
    let timeRangeSet = false

    // Process team parameter
    if (route.query.team && typeof route.query.team === 'string') {
      const teamId = parseInt(route.query.team)

      // Load teams if not already loaded
      if (!teamsStore.teams.length) {
        await teamsStore.loadTeams()
      }

      // Validate team ID
      const teamExists = teamsStore.teams.some(t => t.id === teamId)
      if (!teamExists) {
        urlError.value = `Team with ID ${teamId} not found. Selecting default team.`
        // Select first team as fallback
        if (teamsStore.teams.length > 0) {
          teamsStore.setCurrentTeam(teamsStore.teams[0].id)
          // Use cache parameter to prevent duplicate API calls
          await sourcesStore.loadTeamSources(teamsStore.teams[0].id, true)
        }
      } else {
        teamsStore.setCurrentTeam(teamId)
        // Use cache parameter to prevent duplicate API calls
        await sourcesStore.loadTeamSources(teamId, true)
      }
    } else if (teamsStore.teams.length > 0) {
      // No team in URL, select first team
      teamsStore.setCurrentTeam(teamsStore.teams[0].id)
      // Use cache parameter to prevent duplicate API calls
      await sourcesStore.loadTeamSources(teamsStore.teams[0].id, true)
    }

    // Process source parameter
    if (route.query.source && typeof route.query.source === 'string' && teamsStore.currentTeamId) {
      const sourceId = parseInt(route.query.source)

      // Validate source ID against current team
      const sourceExists = sourcesStore.teamSources.some(s => s.id === sourceId)
      if (!sourceExists) {
        urlError.value = `Source with ID ${sourceId} not found or not accessible by the selected team. Selecting default source.`

        // Only select a default source if the team has sources
        if (sourcesStore.teamSources.length > 0) {
          exploreStore.setSource(sourcesStore.teamSources[0].id)
          await fetchSourceDetails(sourcesStore.teamSources[0].id)
        } else {
          // Team has no sources, clear source selection
          exploreStore.setSource(0)
          sourceDetails.value = null
        }
      } else {
        exploreStore.setSource(sourceId)
        await fetchSourceDetails(sourceId)
      }
    } else if (sourcesStore.teamSources.length > 0) {
      // No source in URL, select first source if team has sources
      exploreStore.setSource(sourcesStore.teamSources[0].id)
      await fetchSourceDetails(sourcesStore.teamSources[0].id)
    } else {
      // Team has no sources, clear source selection
      exploreStore.setSource(0)
      sourceDetails.value = null
    }

    // Process limit parameter
    if (route.query.limit && typeof route.query.limit === 'string') {
      const limit = parseInt(route.query.limit)
      if (!isNaN(limit) && limit > 0 && limit <= 10000) {
        exploreStore.setLimit(limit)
      } else {
        exploreStore.setLimit(100) // Default limit
      }
    } else {
      exploreStore.setLimit(100) // Default limit
    }

    // Process time range parameters
    if (
      route.query.start_time &&
      route.query.end_time &&
      typeof route.query.start_time === 'string' &&
      typeof route.query.end_time === 'string'
    ) {
      try {
        const startValue = parseInt(route.query.start_time)
        const endValue = parseInt(route.query.end_time)

        if (!isNaN(startValue) && !isNaN(endValue)) {
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
          timeRangeSet = true
        }
      } catch (e) {
        console.error('Failed to parse time range from URL', e)
      }
    }

    // Set default time range if not set from URL
    if (!timeRangeSet) {
      exploreStore.setTimeRange({
        start: defaultStart,
        end: defaultEnd
      })
    }

    // Process query mode parameter
    if (route.query.mode && typeof route.query.mode === 'string') {
      const mode = route.query.mode as 'dsl' | 'sql'
      if (mode === 'dsl' || mode === 'sql') {
        queryMode.value = mode
      }
    }

    // Process query parameter
    if (route.query.q && typeof route.query.q === 'string') {
      try {
        const decodedQuery = decodeURIComponent(route.query.q)
        
        if (queryMode.value === 'dsl') {
          logchefQuery.value = decodedQuery
          exploreStore.setDslCode(decodedQuery)
        } else if (queryMode.value === 'sql') {
          sqlQuery.value = decodedQuery
          exploreStore.setRawSql(decodedQuery)
        }
      } catch (e) {
        console.error('Failed to parse query from URL', e)
      }
    }

    // Update URL to ensure it's consistent with the state
    updateUrlWithCurrentState()

  } catch (error) {
    console.error('Error initializing from URL:', error)
    urlError.value = 'Error initializing from URL parameters. Using default values.'

    // Set safe defaults
    if (teamsStore.teams.length > 0 && !teamsStore.currentTeamId) {
      teamsStore.setCurrentTeam(teamsStore.teams[0].id)
    }

    if (teamsStore.currentTeamId && sourcesStore.teamSources.length > 0 && !exploreStore.sourceId) {
      exploreStore.setSource(sourcesStore.teamSources[0].id)
    }

    const currentTime = now(getLocalTimeZone())
    exploreStore.setTimeRange({
      start: currentTime.subtract({ hours: 3 }),
      end: currentTime
    })

    exploreStore.setLimit(100)
    updateUrlWithCurrentState()
  }
}

// Handle team change
async function handleTeamChange(teamId: string) {
  try {
    isChangingTeam.value = true
    urlError.value = null

    const parsedTeamId = parseInt(teamId)
    teamsStore.setCurrentTeam(parsedTeamId)

    // Load sources for the selected team
    const sourcesResult = await sourcesStore.loadTeamSources(parsedTeamId, true)

    // Handle case where team has no sources
    if (!sourcesResult.success || !sourcesResult.data || sourcesResult.data.length === 0) {
      // Clear source selection when team has no sources
      exploreStore.setSource(0)
      sourceDetails.value = null

      // Update URL with only team ID (no source)
      updateUrlWithCurrentState()
      return
    }

    // Reset source selection if current team doesn't have access to it
    const currentSourceExists = sourcesStore.teamSources.some(
      source => source.id === exploreStore.sourceId
    )

    if (!currentSourceExists) {
      // Select the first source from the new team
      if (sourcesStore.teamSources.length > 0) {
        exploreStore.setSource(sourcesStore.teamSources[0].id)
        await fetchSourceDetails(sourcesStore.teamSources[0].id)
      } else {
        // No sources in this team
        exploreStore.setSource(0)
        sourceDetails.value = null
      }
    }

  } catch (error) {
    console.error('Error changing team:', error)
    urlError.value = 'Error changing team. Please try again.'
  } finally {
    isChangingTeam.value = false

    // Update URL after team change is complete and isChangingTeam is set to false
    // This ensures the URL is updated with the final state
    updateUrlWithCurrentState()
  }
}

// Handle source change
async function handleSourceChange(sourceId: string) {
  try {
    isChangingSource.value = true
    urlError.value = null

    if (!sourceId) {
      exploreStore.setSource(0)
      sourceDetails.value = null
      return
    }

    const id = parseInt(sourceId)

    // Verify the source exists in the current team's sources
    const sourceExists = sourcesStore.teamSources.some(source => source.id === id)

    if (!sourceExists) {
      urlError.value = `Source with ID ${id} not found or not accessible by the selected team.`
      // If invalid source, don't update the store
      return
    }

    exploreStore.setSource(id)
    await fetchSourceDetails(id)

  } catch (error) {
    console.error('Error changing source:', error)
    urlError.value = 'Error changing source. Please try again.'
  } finally {
    isChangingSource.value = false

    // Update URL after source change is complete and isChangingSource is set to false
    // This ensures the URL is updated with the final state
    updateUrlWithCurrentState()
  }
}

// Fetch source details with caching to prevent redundant API calls
async function fetchSourceDetails(sourceId: number) {
  if (!sourceId || !teamsStore.currentTeamId) {
    sourceDetails.value = null
    return
  }

  // Check if we already have this source in the store's teamSources
  const cachedSource = sourcesStore.teamSources.find(s => s.id === sourceId)
  
  // If we have a complete cached source with columns, use it directly
  if (cachedSource && cachedSource.columns && cachedSource.columns.length > 0) {
    console.log(`Using cached source details for ID ${sourceId}`)
    sourceDetails.value = cachedSource
    return
  }

  try {
    // Only make the API call if we don't have complete data
    const result = await sourcesStore.getSource(sourceId, true) // Pass true to use cache
    if (result.success && result.data) {
      sourceDetails.value = result.data
      console.log(`Source details loaded for ID ${sourceId}`)
    } else {
      console.warn(`No source details returned for source ID: ${sourceId}`)
      sourceDetails.value = null
    }
  } catch (error) {
    console.error('Error fetching source details:', error)
    sourceDetails.value = null
  }
}

// Get available fields for auto-completion
const availableFields = computed((): FieldInfo[] => {
  // Use columns from source details or query results
  let fields: FieldInfo[] = [];

  if (sourceDetails.value && sourceDetails.value.columns && sourceDetails.value.columns.length > 0) {
    fields = sourceDetails.value.columns.map(column => ({
      name: column.name,
      type: column.type
    }));
  } else if (exploreStore.columns?.length > 0) {
    fields = exploreStore.columns.map(column => ({
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
  () => exploreStore.columns,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(newColumns)
    }
  },
  { immediate: true }
)

// Watch for changes in time range to update URL
watch(
  () => exploreStore.timeRange,
  () => {
    // Update URL when time range changes
    updateUrlWithCurrentState()
  }
)

// Watch for changes in sourceId to fetch source details
watch(
  () => exploreStore.sourceId,
  async (newSourceId, oldSourceId) => {
    if (newSourceId !== oldSourceId) {
      if (newSourceId > 0) {
        console.log(`Source changed from ${oldSourceId} to ${newSourceId}, checking details`)

        // Verify the source exists in the current team's sources
        const sourceExists = sourcesStore.teamSources.some(source => source.id === newSourceId)

        if (sourceExists) {
          // Fetch details for the new source (with caching)
          await fetchSourceDetails(newSourceId)
        } else {
          console.warn(`Source ${newSourceId} not found in current team's sources`)
          sourceDetails.value = null
        }
      } else if (newSourceId === 0) {
        // Clear source details when source is reset
        sourceDetails.value = null
      }
    }
  }
)

// Watch for changes in limit to update URL
watch(
  () => exploreStore.limit,
  () => {
    // Update URL when limit changes
    updateUrlWithCurrentState()
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
          await fetchSourceDetails(sourcesStore.teamSources[0].id)
        }
      }

      // Update URL with new state
      updateUrlWithCurrentState()
    }
  }
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

// Watch for changes in logchefQuery to update URL
watch(
  () => logchefQuery.value,
  (newQuery) => {
    if (queryMode.value === 'dsl') {
      // Debounce URL updates to avoid excessive history entries
      // Only update URL if query has content
      if (newQuery.trim()) {
        updateUrlWithCurrentState()
      }
    }
  },
  { deep: true }
)

// Watch for changes in sqlQuery to update URL
watch(
  () => sqlQuery.value,
  (newQuery) => {
    if (queryMode.value === 'sql') {
      // Debounce URL updates to avoid excessive history entries
      // Only update URL if query has content
      if (newQuery.trim()) {
        updateUrlWithCurrentState()
      }
    }
  },
  { deep: true }
)

// Watch for changes in queryMode to update URL
watch(
  () => queryMode.value,
  () => {
    updateUrlWithCurrentState()
  }
)

// Copy share URL to clipboard
const copyShareUrl = () => {
  // Get the current URL with query parameters
  updateUrlWithCurrentState()
  const shareUrl = window.location.href
  
  // Copy to clipboard
  navigator.clipboard.writeText(shareUrl).then(() => {
    toast({
      title: 'Success',
      description: 'Share URL copied to clipboard',
      duration: TOAST_DURATION.SUCCESS,
    })
  }).catch(err => {
    console.error('Failed to copy URL: ', err)
    toast({
      title: 'Error',
      description: 'Failed to copy URL to clipboard',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  })
}

// Handle query changes from editor
const handleQueryChange = (query: string) => {
  // If in DSL mode, update the LogchefQL query
  if (queryMode.value === 'dsl') {
    logchefQuery.value = query;
    exploreStore.setDslCode(query);
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
  // Update the mode in the store (this will also update the computed queryMode)
  exploreStore.setActiveMode(mode as 'dsl' | 'sql');

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

  // Update URL with current parameters before executing the query
  // This ensures that time ranges and limits are reflected in the URL
  updateUrlWithCurrentState();

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

    // Ensure we have source details
    if (!sourceDetails.value) {
      console.warn('Source details not available, attempting to fetch them');
      try {
        await fetchSourceDetails(exploreStore.sourceId);
        if (!sourceDetails.value) {
          throw new Error('Could not fetch source details');
        }
      } catch (error) {
        console.error('Failed to fetch source details:', error);
        queryError.value = 'Failed to fetch source details. Please try selecting the source again.';
        isExecutingQuery.value = false;
        return;
      }
    }

    // Check if the query is empty
    const isEmptyQuery = !logchefQuery.value.trim();
    let basicSql = '';

    if (isEmptyQuery) {
      // Use a default query if none provided, similar to SQL mode
      console.log('Empty LogchefQL query, using default SQL query');
      basicSql = `SELECT * FROM ${activeSourceTableName.value}`;
    } else {
      try {
        console.log('Executing LogchefQL query:', logchefQuery.value);
        console.log('Source details:', sourceDetails.value);
        console.log('Active source table name:', activeSourceTableName.value);

        // Parse the LogchefQL and convert to SQL with correct source table name
        // For query execution, use includeTimeRange=true to include time conditions
        basicSql = translateLogchefQLToSQL(logchefQuery.value, {
          table: activeSourceTableName.value,
          includeTimeRange: true,
          startTime: exploreStore.timeRange ? new Date(exploreStore.timeRange.start.toString()) : undefined,
          endTime: exploreStore.timeRange ? new Date(exploreStore.timeRange.end.toString()) : undefined,
          limit: exploreStore.limit
        });
      } catch (error) {
        console.error('LogchefQL parsing error:', error);
        // Only show an error if the user explicitly tries to run an invalid query
        queryError.value = 'Error parsing query. Please check your syntax and try again.';
        isExecutingQuery.value = false;
        return;
      }
    }

    console.log('Generated SQL:', basicSql);

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
    console.error('Query execution error:', error);
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

    // Ensure we have source details
    if (!sourceDetails.value) {
      console.warn('Source details not available, attempting to fetch them');
      try {
        await fetchSourceDetails(exploreStore.sourceId);
        if (!sourceDetails.value) {
          throw new Error('Could not fetch source details');
        }
      } catch (error) {
        console.error('Failed to fetch source details:', error);
        queryError.value = 'Failed to fetch source details. Please try selecting the source again.';
        isExecutingQuery.value = false;
        return;
      }
    }

    console.log('Executing SQL query:', sqlQuery.value);
    console.log('Source details:', sourceDetails.value);
    console.log('Active source table name:', activeSourceTableName.value);

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
    console.error('SQL query execution error:', error);
    queryError.value = error instanceof Error ? error.message : 'An error occurred while executing the SQL query';
  } finally {
    isExecutingQuery.value = false;
  }
}

// Handle loading saved query
async function loadSavedQuery(queryId: string, queryData?: any) {
  try {
    // If we have queryData passed directly from the dropdown, use it
    if (queryData) {
      console.log('Loading query from dropdown data:', queryData);
      
      const queryContent = queryData.content;
      
      // Set the query mode based on the query type
      if (queryData.queryType === 'dsl') {
        queryMode.value = 'dsl';
        // Set the DSL content if available
        if (queryContent.dslContent) {
          logchefQuery.value = queryContent.dslContent;
          exploreStore.setDslCode(queryContent.dslContent);
        }
        // Reset any existing SQL
        exploreStore.setRawSql('');
      } else {
        // Set SQL mode
        queryMode.value = 'sql';
        // Set raw SQL if available
        if (queryContent.rawSql) {
          sqlQuery.value = queryContent.rawSql;
          exploreStore.setRawSql(queryContent.rawSql);
        }
      }
      
      // Set timeRange if available
      if (queryContent.timeRange && queryContent.timeRange.absolute) {
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
        });
      }
      
      // Set limit if available
      if (queryContent.limit) {
        exploreStore.setLimit(queryContent.limit);
      }
      
      // Execute the query
      await handleQuerySubmit(queryMode.value);
      
      // Update URL to reflect the loaded query
      updateUrlWithCurrentState();
      
      toast({
        title: 'Success',
        description: 'Query loaded successfully',
        duration: TOAST_DURATION.SUCCESS,
      });
      
      return;
    }
    
    // If no queryData provided, fetch it from the API
    const teamId = teamsStore.currentTeamId;
    if (!teamId) {
      toast({
        title: 'Error',
        description: 'No team selected. Please select a team first.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      });
      return;
    }

    const queryResponse = await savedQueriesStore.fetchQuery(
      teamId,
      queryId
    );

    if (!queryResponse.success || !queryResponse.data) {
      throw new Error('Failed to load query');
    }

    const query = queryResponse.data;
    const queryContent = JSON.parse(query.query_content) as SavedQueryContent;

    // Set the query mode based on the query type
    if (query.query_type === 'dsl') {
      queryMode.value = 'dsl';
      // Set the DSL content if available
      if (queryContent.dslContent) {
        logchefQuery.value = queryContent.dslContent;
        exploreStore.setDslCode(queryContent.dslContent);
      }
      // Reset any existing SQL
      exploreStore.setRawSql('');
    } else {
      // Set SQL mode
      queryMode.value = 'sql';
      // Set raw SQL if available
      if (queryContent.rawSql) {
        sqlQuery.value = queryContent.rawSql;
        exploreStore.setRawSql(queryContent.rawSql);
      }
    }

    // Always set timeRange - queryContent.timeRange is guaranteed to exist
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
    });

    // Always set limit - queryContent.limit is guaranteed to exist
    exploreStore.setLimit(queryContent.limit);

    // Execute the query
    await handleQuerySubmit(queryMode.value);

    // Update URL to reflect the loaded query
    updateUrlWithCurrentState();

    toast({
      title: 'Success',
      description: 'Query loaded successfully',
      duration: TOAST_DURATION.SUCCESS,
    });
  } catch (error) {
    console.error('Error loading query:', error);
    toast({
      title: 'Error',
      description: error instanceof Error ? error.message : 'Failed to load query',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Check if query can be saved
function handleSaveQueryClick() {
  // Check if we're in DSL mode with empty content
  if (queryMode.value === 'dsl' && (!logchefQuery.value || !logchefQuery.value.trim())) {
    toast({
      title: 'Error',
      description: 'Cannot save an empty LogchefQL query. Please enter a valid query first.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
    return;
  }
  
  // Check if we're in SQL mode with empty content
  if (queryMode.value === 'sql' && (!sqlQuery.value || !sqlQuery.value.trim())) {
    toast({
      title: 'Error',
      description: 'Cannot save an empty SQL query. Please enter a valid query first.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
    return;
  }
  
  // If we have content, show the save modal
  showSaveQueryModal.value = true;
}

// Handle saving a query
async function handleSaveQuery(formData: any) {
  console.log("LogExplorer: handleSaveQuery called with formData:", formData);

  try {
    console.log("LogExplorer: Saving query with form data:", formData);

    // Parse the query content to get the proper structure
    let queryContent;
    try {
      queryContent = JSON.parse(formData.query_content);
      console.log("LogExplorer: Parsed query content:", queryContent);
    } catch (e) {
      console.error("LogExplorer: Error parsing query content:", e);
      queryContent = {};
    }

    // Ensure team_id is set
    if (!formData.team_id && teamsStore.currentTeamId) {
      console.log("LogExplorer: Setting team_id from teamsStore:", teamsStore.currentTeamId);
      formData.team_id = teamsStore.currentTeamId.toString();
    }

    console.log("LogExplorer: Calling savedQueriesStore.createQuery with:", {
      teamId: Number(formData.team_id),
      query: {
        name: formData.name,
        description: formData.description,
        source_id: exploreStore.sourceId || 0,
        query_type: formData.query_type,
        query_content: formData.query_content,
      }
    });

    // Create the query using the saved queries store
    const response = await savedQueriesStore.createQuery(
      Number(formData.team_id),
      {
        name: formData.name,
        description: formData.description,
        source_id: exploreStore.sourceId || 0,
        query_type: formData.query_type,
        query_content: formData.query_content,
      }
    );

    console.log("Query saved successfully:", response);

    // Close the modal
    showSaveQueryModal.value = false;

    // Show success toast
    toast({
      title: 'Success',
      description: 'Query saved successfully',
      duration: 3000,
    });
  } catch (error) {
    console.error("Error saving query:", error);

    // Show error toast
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: 5000,
    });
  }
}

// Component lifecycle
onMounted(async () => {
  try {
    console.log("LogExplorer component mounting")

    // Load teams first
    await teamsStore.loadTeams()

    // Initialize from URL parameters
    await initializeFromURL()

    // Set initial DSL code in the store
    exploreStore.setDslCode(logchefQuery.value)

    // Execute a default query if we have a valid source and time range
    if (exploreStore.canExecuteQuery) {
      await executeQuery()
    }
  } catch (error) {
    console.error("Error during LogExplorer mount:", error)
    urlError.value = "Error initializing the explorer. Please try refreshing the page."
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
  <div v-else-if="showEmptyState" class="flex flex-col h-[calc(100vh-12rem)]">
    <!-- Team selector bar -->
    <div class="border-b pb-3 mb-2">
      <div class="flex items-center justify-between">
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
        </div>
      </div>
    </div>

    <!-- Empty state content -->
    <div class="flex flex-col items-center justify-center flex-1 gap-4">
      <div class="text-center space-y-2">
        <h2 class="text-2xl font-semibold tracking-tight">No Sources Found in {{ selectedTeamName }}</h2>
        <p class="text-muted-foreground">
          This team doesn't have any log sources configured. You can add a source or switch to another team.
        </p>
      </div>
      <div class="flex gap-3">
        <Button @click="router.push({ name: 'NewSource' })">
          <Plus class="mr-2 h-4 w-4" />
          Add Source
        </Button>
        <Button variant="outline" v-if="teamsStore.teams.length > 1">
          Try switching teams using the selector above
        </Button>
      </div>
    </div>
  </div>

  <!-- Main content when not loading -->
  <div v-else class="flex flex-col gap-5">
    <!-- Navigation and Controls Section with improved design -->
    <div class="border-b pb-3 mb-2">
      <div class="flex flex-col gap-2">
        <!-- Error Alert -->
        <div v-if="urlError" class="bg-destructive/15 text-destructive px-4 py-2 rounded-md mb-2 flex items-center">
          <span class="text-sm">{{ urlError }}</span>
        </div>

        <div class="flex items-center justify-between">
          <!-- Left: Source Navigation (breadcrumb style) -->
          <div class="flex items-center space-x-2 text-sm">
            <Select :model-value="teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : ''"
              @update:model-value="handleTeamChange" class="h-8 min-w-[160px]" :disabled="isChangingTeam">
              <SelectTrigger>
                <SelectValue placeholder="Select a team">
                  <span v-if="isChangingTeam">Loading...</span>
                  <span v-else>{{ selectedTeamName }}</span>
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id.toString()">
                  {{ team.name }}
                </SelectItem>
              </SelectContent>
            </Select>

            <span class="text-muted-foreground">→</span>

            <Select :model-value="exploreStore.sourceId ? exploreStore.sourceId.toString() : ''"
              @update:model-value="handleSourceChange"
              :disabled="isChangingSource || !teamsStore.currentTeamId || (sourcesStore.teamSources || []).length === 0"
              class="h-8 min-w-[200px]">
              <SelectTrigger>
                <SelectValue placeholder="Select a source">
                  <span v-if="isChangingSource">Loading...</span>
                  <span v-else>{{ selectedSourceName }}</span>
                </SelectValue>
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
                  Limit: {{ exploreStore.limit.toLocaleString() }}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel>Results Limit</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem @click="exploreStore.setLimit(100)" :disabled="exploreStore.limit === 100">
                  100 rows
                </DropdownMenuItem>
                <DropdownMenuItem @click="exploreStore.setLimit(500)" :disabled="exploreStore.limit === 500">
                  500 rows
                </DropdownMenuItem>
                <DropdownMenuItem @click="exploreStore.setLimit(1000)" :disabled="exploreStore.limit === 1000">
                  1,000 rows
                </DropdownMenuItem>
                <DropdownMenuItem @click="exploreStore.setLimit(2000)" :disabled="exploreStore.limit === 2000">
                  2,000 rows
                </DropdownMenuItem>
                <DropdownMenuItem @click="exploreStore.setLimit(5000)" :disabled="exploreStore.limit === 5000">
                  5,000 rows
                </DropdownMenuItem>
                <DropdownMenuItem @click="exploreStore.setLimit(10000)" :disabled="exploreStore.limit === 10000">
                  10,000 rows
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            <!-- Vertical separator -->
            <div class="h-6 w-px bg-border mx-2.5"></div>

            <div class="flex items-center gap-2">
              <SavedQueriesDropdown :source-id="exploreStore.sourceId" :team-id="teamsStore.currentTeamId"
                :use-current-team="true" @select="loadSavedQuery" @save="handleSaveQueryClick" />

              <Button size="sm" variant="outline" class="h-8" @click="handleSaveQueryClick">
                Save
              </Button>

              <Button size="sm" variant="outline" class="h-8" @click="copyShareUrl">
                Share
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
    </div>

    <!-- Condensed Query Builder -->
    <div class="rounded-md border bg-card shadow-sm mb-4">
      <div class="flex items-start">
        <div class="flex-1">
          <QueryEditor ref="queryEditorRef" v-model="logchefQuery" :available-fields="availableFields"
            :placeholder="queryMode === 'dsl' ? 'Enter LogchefQL query or leave empty to run SELECT * FROM table' : 'Enter SQL query or leave empty for default query'"
            :error="queryError" :table-name="activeSourceTableName" height="32" @change="handleQueryChange"
            @submit="handleQuerySubmit" @mode-change="handleModeChange" />
        </div>
      </div>

      <!-- Helptext below editor with better padding -->
      <div class="pl-0 pr-4 py-2 text-xs text-muted-foreground bg-muted/20 border-t">
        <span v-if="queryMode === 'dsl'" class="italic">
          Filter conditions only, e.g. service_name='api' and severity_level='error'. Time filter and LIMIT are applied
          automatically. Leave empty to run SELECT * FROM table.
        </span>
        <span v-if="queryMode === 'sql'" class="italic">
          Write a complete SQL query or leave empty for default SELECT * query. Time filter and LIMIT will be applied
          automatically.
        </span>
      </div>
    </div>

    <!-- Results Section -->
    <div class="flex-1 border rounded-md shadow-sm bg-card overflow-hidden">
      <DataTable :columns="tableColumns" :data="exploreStore.logs || []" :stats="exploreStore.queryStats"
        :source-id="exploreStore.sourceId?.toString() || ''" />
    </div>

    <!-- Save Query Modal -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-content="JSON.stringify({
      filter_conditions: exploreStore.filterConditions,
      raw_sql: exploreStore.rawSql,
      logchefql_query: logchefQuery,
      sql_query: sqlQuery,
      query_mode: queryMode,
      generated_sql: generatedSQL,
      time_range: exploreStore.timeRange,
      limit: exploreStore.limit,
      sourceId: exploreStore.sourceId,
      team_id: teamsStore.currentTeamId
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
</style>
