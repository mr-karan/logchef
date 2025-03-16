<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Plus, Play } from 'lucide-vue-next'
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
import { QueryBuilder } from '@/utils/query-builder'
import type { ColumnDef } from '@tanstack/vue-table'
import type { SavedQueryContent } from '@/api/savedQueries'
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
const showFieldsPanel = ref(false)
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

// Helper function to convert CalendarDateTime to timestamp
const getTimestampFromCalendarDate = (date?: any): number => {
  if (!date) return Math.floor(Date.now() / 1000);

  try {
    const dateObj = new Date(
      date.year,
      date.month - 1,
      date.day,
      'hour' in date ? date.hour : 0,
      'minute' in date ? date.minute : 0,
      'second' in date ? date.second : 0
    );
    return Math.floor(dateObj.getTime() / 1000);
  } catch (e) {
    console.error('Error converting calendar date to timestamp:', e);
    return Math.floor(Date.now() / 1000);
  }
};

// Get the active source table name (formatted as database.table_name)
const activeSourceTableName = computed(() => {
  if (sourceDetails.value?.connection) {
    return `${sourceDetails.value.connection.database}.${sourceDetails.value.connection.table_name}`;
  }
  return 'unknown.unknown'; // Default fallback
});

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

// Add isInitializing state
const isInitializing = ref(true)

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

  // Always update the query parameter when mode changes, explicitly removing it when empty
  if (queryMode.value === 'logchefql') {
    const logchefqlCode = exploreStore.logchefqlCode?.trim() || '';
    if (logchefqlCode) {
      query.q = encodeURIComponent(logchefqlCode);
    } else {
      // Explicitly delete the q parameter when empty
      delete query.q;
    }
  } else if (queryMode.value === 'sql') {
    // Handle case where rawSql might be an object
    const rawSql = typeof exploreStore.rawSql === 'string'
      ? exploreStore.rawSql
      : (typeof exploreStore.rawSql === 'object' && exploreStore.rawSql !== null && 'sql' in exploreStore.rawSql)
        ? (exploreStore.rawSql as any).sql || ''
        : '';
    
    if (rawSql.trim()) {
      query.q = encodeURIComponent(rawSql.trim());
    } else {
      // Explicitly delete the q parameter when empty
      delete query.q;
    }
  }

  // Update URL without triggering navigation events
  router.replace({ query })
}

// Modify the initialization logic
async function setupFromUrl() {
  try {
    // Wait for teams to be loaded
    if (!teamsStore.currentTeamId) {
      console.log('Waiting for teams to load')
      await teamsStore.loadTeams()
    }

    // Process team parameter
    if (route.query.team && typeof route.query.team === 'string') {
      const teamId = parseInt(route.query.team)
      if (!isNaN(teamId)) {
        // Check if the team exists
        const teamExists = teamsStore.teams.some(team => team.id === teamId)
        if (teamExists) {
          teamsStore.setCurrentTeam(teamId)
        } else {
          urlError.value = `Team with ID ${teamId} not found or not accessible by the current user.`
        }
      }
    }

    // Wait for sources to be loaded
    if (teamsStore.currentTeamId) {
      console.log('Loading sources for team ' + teamsStore.currentTeamId)
      await sourcesStore.loadTeamSources(teamsStore.currentTeamId)
    }

    // Track if we've already set a source to avoid setting sourceId = 0 temporarily
    let hasSetSource = false

    // Process source parameter
    if (route.query.source && typeof route.query.source === 'string' && teamsStore.currentTeamId) {
      const sourceId = parseInt(route.query.source)

      // Validate source ID against current team
      const sourceExists = sourcesStore.teamSources.some(s => s.id === sourceId)

      if (!sourceExists) {
        urlError.value = `Source with ID ${sourceId} not found or not accessible by the selected team.`

        // Only select a default source if the team has sources
        if (sourcesStore.teamSources.length > 0) {
          exploreStore.setSource(sourcesStore.teamSources[0].id)
          await fetchSourceDetails(sourcesStore.teamSources[0].id)
          hasSetSource = true
        }
        // We no longer set sourceId to 0 here
      } else {
        exploreStore.setSource(sourceId)
        await fetchSourceDetails(sourceId)
        hasSetSource = true
      }
    } else if (sourcesStore.teamSources.length > 0) {
      // No source in URL, select first source if team has sources
      exploreStore.setSource(sourcesStore.teamSources[0].id)
      await fetchSourceDetails(sourcesStore.teamSources[0].id)
      hasSetSource = true
    }
    // We no longer set sourceId to 0 here

    // Initialize even if we don't have a source by showing empty state
    if (!hasSetSource) {
      console.log('No sources available, showing empty state')
      sourceDetails.value = null
      // But we don't set sourceId to 0, which causes unnecessary unmounting
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
        }
      } catch (e) {
        console.error('Failed to parse time range from URL', e)
      }
    }

    // Process query mode parameter
    if (route.query.mode && typeof route.query.mode === 'string') {
      const mode = route.query.mode as 'logchefql' | 'sql'
      if (mode === 'logchefql' || mode === 'sql') {
        queryMode.value = mode
        exploreStore.setActiveMode(mode)
      }
    }

    // Process query parameter
    if (route.query.q && typeof route.query.q === 'string') {
      try {
        const decodedQuery = decodeURIComponent(route.query.q)

        if (queryMode.value === 'logchefql') {
          logchefQuery.value = decodedQuery
          exploreStore.setLogchefqlCode(decodedQuery)
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
    urlError.value = 'Error initializing view. Please try refreshing the page.'
  } finally {
    isInitializing.value = false
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

// Handle source change - avoid setting to 0
async function handleSourceChange(sourceId: string) {
  try {
    isChangingSource.value = true;
    urlError.value = null;

    if (!sourceId) {
      // Don't set sourceId to 0, just clear details
      sourceDetails.value = null;
      return;
    }

    const id = parseInt(sourceId);

    // Verify the source exists in the current team's sources
    const sourceExists = sourcesStore.teamSources.some(source => source.id === id);

    if (!sourceExists) {
      urlError.value = `Source with ID ${id} not found or not accessible by the selected team.`;
      // If invalid source, don't update the store
      return;
    }

    exploreStore.setSource(id);
    await fetchSourceDetails(id);

  } catch (error) {
    console.error('Error changing source:', error);
    urlError.value = 'Error changing source. Please try again.';
  } finally {
    isChangingSource.value = false;

    // Update URL after source change is complete and isChangingSource is set to false
    // This ensures the URL is updated with the final state
    updateUrlWithCurrentState();
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
    const tsFieldName = sourceDetails.value._meta_ts_field || 'timestamp';
    const tsField = fields.find(f => f.name === tsFieldName);
    if (tsField) {
      tsField.isTimestamp = true;
    } else {
      fields.push({
        name: tsFieldName,
        type: 'timestamp',
        isTimestamp: true
      });
    }

    // Add severity field if not already in the list
    const severityFieldName = sourceDetails.value._meta_severity_field || 'severity_text';
    const severityField = fields.find(f => f.name === severityFieldName);
    if (severityField) {
      severityField.isSeverity = true;
    } else {
      fields.push({
        name: severityFieldName,
        type: 'string',
        isSeverity: true
      });
    }
  }

  return fields;
});

// Watch for changes in columns
watch(
  () => exploreStore.columns,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(newColumns, sourceDetails.value?._meta_ts_field || 'timestamp')
    }
  },
  { immediate: true }
)

// Watch for changes in time range to update URL
watch(
  () => exploreStore.timeRange,
  () => {
    // Don't automatically update URL when time range changes
    // URL will be updated when the Run button is pressed
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
    // Don't automatically update URL when limit changes
    // URL will be updated when the Run button is pressed
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

// Watch for changes in queryMode to update URL
watch(
  () => queryMode.value,
  () => {
    updateUrlWithCurrentState();
  }
)

// Handle query submission from either mode
const handleQuerySubmit = async (data: any) => {
  // Handle both cases:
  // 1. Direct invocation from Run button with mode string
  // 2. Submission from QueryEditor with {query, mode} object
  let queryText: string;
  let mode: string;

  if (typeof data === 'string') {
    // Called from Run button with mode string
    mode = data;
    // Ensure we always have a string even if store values are undefined
    if (mode === 'logchefql') {
      queryText = exploreStore.logchefqlCode || '';
    } else {
      // Handle case where rawSql might be an object
      queryText = typeof exploreStore.rawSql === 'string'
        ? exploreStore.rawSql
        : (typeof exploreStore.rawSql === 'object' && exploreStore.rawSql !== null && 'sql' in exploreStore.rawSql)
          ? (exploreStore.rawSql as any).sql || ''
          : '';
    }
  } else {
    // Called from QueryEditor with {query, mode} object
    queryText = data.query || '';
    mode = data.mode;
  }

  // Map the mode from editor to store format
  const mappedMode = mode === 'logchefql' ? 'logchefql' : 'sql';

  // Update the store's active mode
  exploreStore.setActiveMode(mappedMode);

  // Update the appropriate query state in the store
  if (mappedMode === 'logchefql') {
    exploreStore.setLogchefqlCode(queryText);
    // Ensure local state is also updated for consistency
    logchefQuery.value = queryText;
  } else {
    console.log("Setting SQL query in store:", {
      query: queryText,
      existingQuery: exploreStore.rawSql,
      length: queryText.length,
    });

    // Make sure we have a valid string (not undefined/empty)
    if (queryText && queryText.trim()) {
      exploreStore.setRawSql(queryText);
      // Ensure local state is also updated for consistency
      sqlQuery.value = queryText;
    } else {
      console.log(
        "Empty SQL query received, generating default query as fallback"
      );

      // Generate a default query as a fallback
      const defaultQuery = QueryBuilder.getDefaultSQLQuery({
        tableName: activeSourceTableName.value || 'logs.vector_logs',
        tsField: sourceDetails.value?._meta_ts_field || 'timestamp',
        startTimestamp: Math.floor(exploreStore.timeRange?.start ? new Date(exploreStore.timeRange.start.toString()).getTime() / 1000 : (Date.now() / 1000) - 3600),
        endTimestamp: Math.floor(exploreStore.timeRange?.end ? new Date(exploreStore.timeRange.end.toString()).getTime() / 1000 : Date.now() / 1000),
        includeTimeFilter: true,
        limit: exploreStore.limit
      });

      console.log("Generated default query:", defaultQuery);
      exploreStore.setRawSql(defaultQuery);
      sqlQuery.value = defaultQuery;
    }
  }

  // Update URL with current parameters before executing the query
  updateUrlWithCurrentState();

  // Show loading state
  isExecutingQuery.value = true;
  queryError.value = '';

  try {
    // Ensure we have source details
    if (!sourceDetails.value && exploreStore.sourceId > 0) {
      try {
        await fetchSourceDetails(exploreStore.sourceId);
        if (!sourceDetails.value) {
          throw new Error('Could not fetch source details');
        }
      } catch (error) {
        console.error('Failed to fetch source details:', error);
        queryError.value = 'Failed to fetch source details. Please try selecting the source again.';
        return;
      }
    }

    // Execute query through the store - this will use the QueryBuilder internally
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

// Handle loading saved query
async function loadSavedQuery(queryId: string, queryData?: any) {
  try {
    // If we have queryData passed directly from the dropdown, use it
    if (queryData) {
      console.log('Loading query from dropdown data:', queryData);

      const queryContent = queryData.content;

      // Set the query mode based on the query type
      if (queryData.queryType === 'logchefql') {
        queryMode.value = 'logchefql';
        // Set the LogchefQL content if available
        if (queryContent.logchefqlContent) {
          logchefQuery.value = queryContent.logchefqlContent;
          exploreStore.setLogchefqlCode(queryContent.logchefqlContent);
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

    // Check if the response has an error
    if ('status' in queryResponse && queryResponse.status === 'error') {
      throw new Error(queryResponse.message || 'Failed to load query');
    }

    // Extract the data, handling both success property and direct data access
    const query = 'data' in queryResponse ? queryResponse.data : queryResponse;

    if (!query) {
      throw new Error('Failed to load query - no data returned');
    }

    const queryContent = JSON.parse(query.query_content) as SavedQueryContent;

    // Set the query mode based on the query type
    if (query.query_type === 'logchefql') {
      queryMode.value = 'logchefql';
      // Set the LogchefQL content if available
      if (queryContent.logchefqlContent) {
        logchefQuery.value = queryContent.logchefqlContent;
        exploreStore.setLogchefqlCode(queryContent.logchefqlContent);
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
  // Check if we're in LogchefQL mode with empty content
  if (exploreStore.activeMode === 'logchefql' && (!exploreStore.logchefqlCode || !exploreStore.logchefqlCode.trim())) {
    toast({
      title: 'Error',
      description: 'Cannot save an empty LogchefQL query. Please enter a valid query first.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
    return;
  }

  // Check if we're in SQL mode with empty content
  if (exploreStore.activeMode === 'sql' && (!exploreStore.rawSql || !exploreStore.rawSql.trim())) {
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

    // Create the query with all required fields
    const response = await savedQueriesStore.createQuery(
      Number(formData.team_id),
      {
        team_id: Number(formData.team_id), // Required by API
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

// Handle query changes from editor
const handleQueryChange = (data: any) => {
  // Ensure data is an object before destructuring
  if (!data || typeof data !== 'object') {
    console.warn('Invalid data received in handleQueryChange:', data);
    return;
  }

  const { query: inputQuery, mode } = data;
  // Ensure queryText is always a string, even if inputQuery is undefined
  const queryText = inputQuery || '';

  // Map the mode from editor to store format
  const mappedMode = mode === 'logchefql' ? 'logchefql' : 'sql';
  
  // Check if this is a mode switch
  const isModeSwitch = queryMode.value !== mappedMode;
  
  // Update the queryMode if it's changing
  if (isModeSwitch) {
    queryMode.value = mappedMode;
    exploreStore.setActiveMode(mappedMode);
  }

  // If in LogchefQL mode, update the LogchefQL query
  if (mappedMode === 'logchefql') {
    exploreStore.setLogchefqlCode(queryText);
    // Ensure local state is also updated for consistency
    logchefQuery.value = queryText;
  } else {
    console.log("Setting SQL query in store:", {
      query: queryText,
      existingQuery: exploreStore.rawSql,
      length: queryText.length,
    });

    // Make sure we have a valid string (not undefined/empty)
    if (queryText && queryText.trim()) {
      exploreStore.setRawSql(queryText);
      // Ensure local state is also updated for consistency
      sqlQuery.value = queryText;
    } else {
      console.log(
        "Empty SQL query received, generating default query as fallback"
      );

      // Generate a default query as a fallback
      const defaultQuery = QueryBuilder.getDefaultSQLQuery({
        tableName: activeSourceTableName.value || 'logs.vector_logs',
        tsField: sourceDetails.value?._meta_ts_field || 'timestamp',
        startTimestamp: Math.floor(exploreStore.timeRange?.start ? new Date(exploreStore.timeRange.start.toString()).getTime() / 1000 : (Date.now() / 1000) - 3600),
        endTimestamp: Math.floor(exploreStore.timeRange?.end ? new Date(exploreStore.timeRange.end.toString()).getTime() / 1000 : Date.now() / 1000),
        includeTimeFilter: true,
        limit: exploreStore.limit
      });

      console.log("Generated default query:", defaultQuery);
      exploreStore.setRawSql(defaultQuery);
      sqlQuery.value = defaultQuery;
    }
  }

  // Always clear any error messages when the user is typing
  if (queryError.value) {
    queryError.value = '';
  }
  
  // Always update the URL after handling query changes
  // This is especially important for mode switches
  updateUrlWithCurrentState();
}

// Component lifecycle
onMounted(async () => {
  try {
    console.log("LogExplorer component mounting")

    // Load teams first
    await teamsStore.loadTeams()

    // Initialize from URL parameters
    await setupFromUrl()

    // Set initial values in the store
    exploreStore.setLogchefqlCode(logchefQuery.value || '')
    exploreStore.setRawSql(sqlQuery.value || '')

    // Make sure local refs are in sync with store
    logchefQuery.value = exploreStore.logchefqlCode || ''
    sqlQuery.value = exploreStore.rawSql || ''

    // Execute a default query if we have a valid source and time range
    if (exploreStore.canExecuteQuery) {
      await handleQuerySubmit(queryMode.value)
    }

    // The QueryEditor component now handles its own initialization
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
  <div v-else class="flex flex-col h-[calc(100vh-8rem)]">
    <!-- Error Alert -->
    <div v-if="urlError"
      class="absolute top-0 left-0 right-0 bg-destructive/15 text-destructive px-4 py-2 z-10 flex items-center justify-between">
      <span class="text-sm">{{ urlError }}</span>
      <Button variant="ghost" size="sm" @click="urlError = null" class="h-7 px-2">Dismiss</Button>
    </div>

    <!-- Streamlined Filter Bar -->
    <div class="border-b bg-background py-2 px-3 flex items-center gap-3">
      <!-- Left side: Data selection controls -->
      <div class="flex items-center space-x-2 flex-wrap gap-y-2">
        <!-- Context selectors with more compact styling -->
        <div class="flex items-center gap-1">
          <Select :model-value="teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : ''"
            @update:model-value="handleTeamChange" class="w-32" :disabled="isChangingTeam">
            <SelectTrigger class="h-8 text-sm">
              <SelectValue placeholder="Team">
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

          <Select :model-value="exploreStore.sourceId ? exploreStore.sourceId.toString() : ''"
            @update:model-value="handleSourceChange"
            :disabled="isChangingSource || !teamsStore.currentTeamId || sourcesStore.teamSources?.length === 0"
            class="w-40">
            <SelectTrigger class="h-8 text-sm">
              <SelectValue placeholder="Log Source">
                <span v-if="isChangingSource">Loading...</span>
                <span v-else>{{ selectedSourceName }}</span>
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="source in sourcesStore.teamSources" :key="source.id" :value="source.id.toString()">
                {{ formatSourceName(source) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <!-- Query parameters with more compact styling -->
        <div class="flex items-center gap-1">
          <DateTimePicker v-model="dateRange" class="h-8 w-[210px]" />

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" class="h-8 min-w-[70px] text-sm">
                {{ exploreStore.limit.toLocaleString() }} rows
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
        </div>
      </div>

      <!-- Right side: Actions -->
      <div class="ml-auto flex items-center gap-1.5">
        <!-- Saved Queries -->
        <SavedQueriesDropdown :source-id="exploreStore.sourceId" :team-id="teamsStore.currentTeamId"
          :use-current-team="true" @select="loadSavedQuery" @save="handleSaveQueryClick" class="h-8" />

        <!-- Compact action buttons -->
        <Button variant="default" size="sm" class="h-8 px-3 flex items-center gap-1"
          :disabled="isExecutingQuery || !exploreStore.canExecuteQuery" @click="handleQuerySubmit(queryMode)">
          <Play class="h-3.5 w-3.5" />
          <span>{{ isExecutingQuery ? 'Running...' : 'Run' }}</span>
        </Button>
      </div>
    </div>

    <!-- Main Content Area -->
    <div class="flex flex-1 min-h-0">
      <!-- Use the FieldSideBar component without field-click event -->
      <FieldSideBar v-model:expanded="showFieldsPanel" :fields="availableFields" />

      <!-- Main Content: Query Editor and Results -->
      <div class="flex-1 flex flex-col h-full min-w-0" style="flex-basis: 75%;">
        <!-- Query Editor Section -->
        <div class="p-3 pb-2">
          <div class="rounded-md border-t bg-card">
            <QueryEditor ref="queryEditorRef" :sourceId="exploreStore.sourceId || 0"
              :schema="sourceDetails?.columns?.reduce((acc, col) => ({ ...acc, [col.name]: { type: col.type } }), {}) || {}"
              :startTimestamp="getTimestampFromCalendarDate(exploreStore.timeRange?.start)"
              :endTimestamp="getTimestampFromCalendarDate(exploreStore.timeRange?.end)"
              :initialValue="queryMode === 'logchefql' ? logchefQuery : sqlQuery"
              :initialTab="queryMode === 'logchefql' ? 'logchefql' : 'sql'"
              :placeholder="queryMode === 'logchefql' ? 'Enter LogchefQL query or leave empty to run SELECT * FROM table' : 'Enter SQL query or leave empty for default query'"
              :tsField="sourceDetails?._meta_ts_field || 'timestamp'" :tableName="activeSourceTableName"
              :limit="exploreStore.limit" :showFieldsPanel="showFieldsPanel" @change="handleQueryChange"
              @submit="handleQuerySubmit" @toggle-fields="showFieldsPanel = !showFieldsPanel" />
          </div>

          <!-- Query Error Message -->
          <div v-if="queryError" class="mt-2 text-sm text-destructive bg-destructive/10 p-2 rounded">
            {{ queryError }}
          </div>
        </div>

        <!-- Results Section -->
        <div class="flex-1 overflow-hidden flex flex-col border-t">
          <!-- Stats header - keep minimal -->
          <div class="px-3 py-1.5 flex items-center justify-between bg-muted/10">
            <div class="flex items-center gap-2">
              <!-- Remove "Results" text and use a more descriptive layout -->
              <div v-if="exploreStore.queryStats?.rows_read" class="flex items-center gap-1">
                <span class="text-xs text-muted-foreground">Fetched:</span>
                <span class="text-xs font-medium">{{ exploreStore.queryStats.rows_read.toLocaleString() }}</span>
                <span class="text-xs text-muted-foreground ml-0.5">rows</span>
              </div>

              <!-- Time indication with color -->
              <div v-if="exploreStore.queryStats" class="flex items-center gap-1 ml-2">
                <span class="text-xs text-muted-foreground">Time:</span>
                <div class="flex items-center">
                  <div :class="{
                    'bg-green-500': exploreStore.queryStats.execution_time_ms < 100,
                    'bg-yellow-500': exploreStore.queryStats.execution_time_ms >= 100 && exploreStore.queryStats.execution_time_ms < 1000,
                    'bg-orange-500': exploreStore.queryStats.execution_time_ms >= 1000 && exploreStore.queryStats.execution_time_ms < 5000,
                    'bg-red-500': exploreStore.queryStats.execution_time_ms >= 5000
                  }" class="h-2 w-2 rounded-full"></div>
                  <span class="text-xs ml-1 font-medium">
                    {{ exploreStore.queryStats.execution_time_ms < 1000 ?
                      Math.round(exploreStore.queryStats.execution_time_ms) + 'ms' :
                      (exploreStore.queryStats.execution_time_ms / 1000).toFixed(2) + 's' }} </span>
                </div>
              </div>

              <!-- Data read - formatted consistently -->
              <div v-if="exploreStore.queryStats?.bytes_read" class="flex items-center gap-1 ml-2">
                <span class="text-xs text-muted-foreground">Data:</span>
                <span class="text-xs font-medium">{{ (exploreStore.queryStats.bytes_read / 1024 / 1024).toFixed(2)
                  }}</span>
                <span class="text-xs text-muted-foreground">MB</span>
              </div>
            </div>
          </div>

          <!-- Table container with proper scroll behavior - give it max available height -->
          <div class="flex-1 h-full overflow-hidden relative">
            <template v-if="exploreStore.logs?.length">
              <div class="absolute inset-0 h-full">
                <DataTable :columns="tableColumns" :data="exploreStore.logs" :stats="exploreStore.queryStats"
                  :source-id="exploreStore.sourceId?.toString() || ''" :timestamp-field="sourceDetails?._meta_ts_field"
                  :severity-field="sourceDetails?._meta_severity_field" />
              </div>
            </template>
            <template v-else>
              <div class="h-full flex flex-col items-center justify-center p-10 text-center">
                <div class="text-muted-foreground mb-4">
                  <svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 24 24" fill="none"
                    stroke="currentColor" stroke-width="1" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M14 3v4a1 1 0 0 0 1 1h4"></path>
                    <path d="M17 21H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h7l5 5v11a2 2 0 0 1-2 2z"></path>
                    <line x1="9" y1="9" x2="10" y2="9"></line>
                    <line x1="9" y1="13" x2="15" y2="13"></line>
                    <line x1="9" y1="17" x2="15" y2="17"></line>
                  </svg>
                </div>
                <h3 class="text-lg font-medium mb-1">No logs found</h3>
                <p class="text-sm text-muted-foreground max-w-lg">
                  No logs match your current query and time range. Try adjusting your query,
                  expanding the time range, or selecting different filters to see results.
                </p>
                <div class="mt-4 flex gap-3">
                  <Button variant="outline" size="sm" class="h-8" @click="exploreStore.setTimeRange({
                    start: now(getLocalTimeZone()).subtract({ hours: 24 }),
                    end: now(getLocalTimeZone())
                  })">
                    Expand to 24h
                  </Button>
                </div>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- Save Query Modal -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-content="JSON.stringify({
      filter_conditions: exploreStore.filterConditions,
      raw_sql: exploreStore.rawSql,
      logchefql_query: exploreStore.logchefqlCode,
      sql_query: exploreStore.rawSql,
      query_mode: exploreStore.activeMode,
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

/* Ensure proper table height */
.h-full {
  height: 100% !important;
}

/* Fix flex layout issues */
.flex.flex-1.min-h-0 {
  display: flex;
  width: 100%;
  min-height: 0;
}

.flex.flex-1.min-h-0>div:last-child {
  flex: 1 1 auto;
  min-width: 0;
  width: 100%;
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

/* Style for long IDs to prevent table stretching */
:deep(.table .cell-id) {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
