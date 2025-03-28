<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
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
// Use store's loading state instead of local state
const isExecutingQuery = computed(() => exploreStore.isLoadingOperation('executeQuery'))
const sourceDetails = ref<Source | null>(null)
const queryEditorRef = ref()

// Add loading states for better UX
const isChangingTeam = ref(false)
const isChangingSource = ref(false)
const urlError = ref<string | null>(null)

// Add a new computed property to check if we can execute the query
const canExecuteQuery = computed(() => {
  return exploreStore.sourceId > 0 && 
         teamsStore.currentTeamId > 0 && 
         hasValidSource.value;
});

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
  
  // Return a placeholder if source details aren't loaded yet
  if (exploreStore.rawSql && typeof exploreStore.rawSql === 'string' && exploreStore.rawSql.includes('FROM ')) {
    // Improved regex to avoid extracting WHERE as a table name
    // Looking for an actual table name after FROM, not a SQL keyword
    const tableNameMatch = exploreStore.rawSql.match(/FROM\s+([^\s\n;()]+)(?:\s|$)/i);
    const extractedName = tableNameMatch?.[1] || '';
    
    // Make sure we're not returning SQL keywords as table names
    if (extractedName && 
        !['WHERE', 'SELECT', 'ORDER', 'GROUP', 'LIMIT', 'HAVING'].includes(extractedName.toUpperCase())) {
      return extractedName;
    }
  }
  
  // Default empty string if no valid table name found
  return ''; 
});

// Check if we have a valid source for querying
const hasValidSource = computed(() => {
  return !!sourceDetails.value?.connection;
});

// Loading and empty states using the centralized loading states
const showLoadingState = computed(() => {
  const currentTeamId = teamsStore.currentTeamId;
  return teamsStore.isLoadingTeams() ||
    (currentTeamId && sourcesStore.isLoadingTeamSources(currentTeamId)) ||
    isChangingTeam.value;
})

// Check if there are no teams available at all
const showNoTeamsState = computed(() => {
  return !showLoadingState.value && (!teamsStore.teams || teamsStore.teams.length === 0);
})

// Show empty sources state only when we have teams but no sources
const showEmptyState = computed(() => {
  return !showLoadingState.value && 
         !showNoTeamsState.value && 
         (!sourcesStore.teamSources || sourcesStore.teamSources.length === 0);
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

// Add timezone preference for display
const displayTimezone = computed(() => 
  localStorage.getItem('logchef_timezone') === 'utc' ? 'utc' : 'local'
);

// Add more detailed initialization tracking
const isInitializing = ref(true)
const isTeamsLoaded = ref(false)
const isSourcesLoaded = ref(false)
const pendingEditorInit = ref(false)

// Function to update URL with current state - simplified
function updateUrlWithCurrentState() {
  // Skip URL updates during initialization to prevent race conditions
  if (isInitializing.value) {
    console.log('Skipping URL update during initialization');
    return;
  }
  
  const currentTime = exploreStore.timeRange;
  const query: Record<string, string> = {};

  // Add team and source IDs
  if (teamsStore.currentTeamId) {
    query.team = teamsStore.currentTeamId.toString();
  }

  // Only add source ID if it's valid (non-zero)
  if (exploreStore.sourceId && exploreStore.sourceId > 0) {
    // Verify the source exists in the current team's sources
    const sourceExists = sourcesStore.teamSources.some(source => source.id === exploreStore.sourceId);
    if (sourceExists) {
      query.source = exploreStore.sourceId.toString();
    }
  }

  // Add limit
  query.limit = exploreStore.limit.toString();

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
    );

    const endDate = new Date(
      currentTime.end.year,
      currentTime.end.month - 1,
      currentTime.end.day,
      'hour' in currentTime.end ? currentTime.end.hour : 0,
      'minute' in currentTime.end ? currentTime.end.minute : 0,
      'second' in currentTime.end ? currentTime.end.second : 0
    );

    query.start_time = startDate.getTime().toString();
    query.end_time = endDate.getTime().toString();
  }

  // Add query mode from store
  query.mode = exploreStore.activeMode;

  // Get query content from the STORE
  let queryContent = "";
  if (exploreStore.activeMode === 'logchefql') {
    queryContent = exploreStore.logchefqlCode?.trim() || '';
  } else {
    queryContent = exploreStore.rawSql?.trim() || '';
  }

  if (queryContent) {
    query.q = encodeURIComponent(queryContent);
  } else {
    delete query.q; // Remove q if empty
  }

  console.log('Updating URL with query params:', query);

  // Update URL without triggering navigation events
  router.replace({ query });
}

// Completely revised initialization logic to prevent race conditions
async function setupFromUrl() {
  try {
    console.log('Starting setupFromUrl - initializing component from URL parameters');

    // Check if this is a saved query request first
    const hasSavedQueryParams = route.query.query_id && route.query.team;

    // STEP 1: Load teams first and wait for completion
    if (!teamsStore.currentTeamId) {
      console.log('Loading teams first');
      await teamsStore.loadTeams();
    }

    // Mark teams as loaded
    isTeamsLoaded.value = true;
    console.log('Teams loaded successfully');

    // STEP 2: Process team parameter (with proper error handling)
    let teamId = null;
    if (route.query.team && typeof route.query.team === 'string') {
      teamId = parseInt(route.query.team);
      if (!isNaN(teamId)) {
        // Check if the team exists
        const teamExists = teamsStore.teams.some(team => team.id === teamId);
        if (teamExists) {
          console.log(`Setting current team to ID ${teamId}`);
          teamsStore.setCurrentTeam(teamId);
        } else {
          console.warn(`Team with ID ${teamId} not found, using default team`);
          urlError.value = `Team with ID ${teamId} not found or not accessible by the current user.`;

          // Fall back to first available team if the requested one doesn't exist
          if (teamsStore.teams.length > 0) {
            teamId = teamsStore.teams[0].id;
            teamsStore.setCurrentTeam(teamId);
          }
        }
      }
    } else if (teamsStore.teams.length > 0) {
      // No team in URL, use first team
      teamId = teamsStore.teams[0].id;
      teamsStore.setCurrentTeam(teamId);
    }

    // STEP 3: Load sources for the selected team (or first team)
    if (teamsStore.currentTeamId) {
      console.log(`Loading sources for team ${teamsStore.currentTeamId}`);
      await sourcesStore.loadTeamSources(teamsStore.currentTeamId);
    }

    // Mark sources as loaded
    isSourcesLoaded.value = true;
    console.log('Sources loaded successfully');

    // STEP 4: Process source parameter - using a single consistent approach
    let sourceId = null;
    let hasSetSource = false;

    // First check URL parameter
    if (route.query.source && typeof route.query.source === 'string' && teamsStore.currentTeamId) {
      sourceId = parseInt(route.query.source);

      // Validate source ID against current team
      const sourceExists = sourcesStore.teamSources.some(s => s.id === sourceId);

      if (!sourceExists) {
        console.warn(`Source ID ${sourceId} not valid for current team, selecting first available source`);
        urlError.value = `Source with ID ${sourceId} not found or not accessible by the selected team.`;
        sourceId = null; // Reset for next check
      }
    }

    // If no valid source from URL, use first source if available
    if (!sourceId && sourcesStore.teamSources.length > 0) {
      sourceId = sourcesStore.teamSources[0].id;
    }

    // Set the source if we have a valid ID - with more caution to prevent duplicate calls
    if (sourceId) {
      console.log(`Setting source to ID ${sourceId}`);

      try {
        // Check if this sourceId is already set to avoid redundant updates
        if (exploreStore.sourceId !== sourceId) {
          exploreStore.setSource(sourceId);
        } else {
          console.log(`Source ID ${sourceId} already set, avoiding redundant update`);
        }

        hasSetSource = true;

        // Fetch source details with debounce to prevent race conditions
        console.log(`Fetching details for source ID ${sourceId}`);

        // Wrap in try/catch since we want to continue even if this fails
        try {
          await fetchSourceDetails(sourceId);
        } catch (detailsErr) {
          console.error("Error fetching source details:", detailsErr);
          // Continue with initialization even if details fetch fails
        }
      } catch (err) {
        console.error("Error setting source during initialization:", err);
        // Don't throw, just log - we'll continue with initialization
      }
    } else {
      console.log('No valid source available, showing empty state');
      sourceDetails.value = null;
    }

    // STEP 5: Process other URL parameters after source is set
    // This ensures we don't trigger unnecessary re-renders

    // Process limit parameter
    console.log('Processing limit parameter');
    if (route.query.limit && typeof route.query.limit === 'string') {
      const limit = parseInt(route.query.limit);
      if (!isNaN(limit) && limit > 0 && limit <= 10000) {
        exploreStore.setLimit(limit);
      } else {
        exploreStore.setLimit(100); // Default limit
      }
    } else {
      exploreStore.setLimit(100); // Default limit
    }

    // Process time range parameters
    console.log('Processing time range parameters');
    if (
      route.query.start_time &&
      route.query.end_time &&
      typeof route.query.start_time === 'string' &&
      typeof route.query.end_time === 'string'
    ) {
      try {
        const startValue = parseInt(route.query.start_time);
        const endValue = parseInt(route.query.end_time);

        if (!isNaN(startValue) && !isNaN(endValue)) {
          const startDate = new Date(startValue);
          const endDate = new Date(endValue);

          // Create CalendarDateTime objects
          const start = new CalendarDateTime(
            startDate.getFullYear(),
            startDate.getMonth() + 1,
            startDate.getDate(),
            startDate.getHours(),
            startDate.getMinutes(),
            startDate.getSeconds()
          );

          const end = new CalendarDateTime(
            endDate.getFullYear(),
            endDate.getMonth() + 1,
            endDate.getDate(),
            endDate.getHours(),
            endDate.getMinutes(),
            endDate.getSeconds()
          );

          exploreStore.setTimeRange({
            start,
            end
          });
        }
      } catch (e) {
        console.error('Failed to parse time range from URL', e);
      }
    }

    // Process query mode parameter
    console.log('Processing query mode parameter');
    if (route.query.mode && typeof route.query.mode === 'string') {
      const mode = route.query.mode as 'logchefql' | 'sql';
      if (mode === 'logchefql' || mode === 'sql') {
        queryMode.value = mode;
        exploreStore.setActiveMode(mode);
      }
    }

    // Process query parameter
    console.log('Processing query parameter');
    if (route.query.q && typeof route.query.q === 'string') {
      try {
        const decodedQuery = decodeURIComponent(route.query.q);
        
        // Preserve the query regardless of whether all details are loaded yet
        if (queryMode.value === 'logchefql') {
          logchefQuery.value = decodedQuery;
          exploreStore.setLogchefqlCode(decodedQuery);
        } else if (queryMode.value === 'sql') {
          // Preserve original SQL query without modification
          sqlQuery.value = decodedQuery;
          exploreStore.setRawSql(decodedQuery);
          
          // Important: For SQL queries with a table name, make sure we always
          // preserve the original query, regardless of whether source details are loaded
          if (decodedQuery.includes('FROM ')) {
            // Extract the table name from the query for logging
            const tableName = decodedQuery.match(/FROM\s+([^\s\n]+)/i)?.[1] || '<unknown>';
            console.log(`SQL query with table name "${tableName}" detected, preserving for execution`);
            
            // Store the original query as pending to ensure it doesn't get overwritten
            exploreStore.pendingRawSql = decodedQuery;
            
            // Prevent any default query generation by setting the raw SQL explicitly
            sqlQuery.value = decodedQuery;
            exploreStore.setRawSql(decodedQuery);
          }
        }
      } catch (e) {
        console.error('Failed to parse query from URL', e);
      }
    }

    // If this is a saved query URL, load the saved query
    if (hasSavedQueryParams && route.query.query_id) {
      console.log('Loading saved query from URL parameters');
      // Set flag to indicate editor should initialize after URL is processed
      pendingEditorInit.value = true;
      await loadSavedQuery(route.query.query_id as string);
      pendingEditorInit.value = false;
      return; // Skip updating URL as loadSavedQuery will handle it
    }

    // Only update URL if not loading a saved query
    if (!hasSavedQueryParams) {
      // Now it's safe to update the URL
      console.log('Setup complete, updating URL with initial state');

      // Set initialization complete first
      isInitializing.value = false;

      // Then wait for next tick and update URL
      nextTick(() => {
        updateUrlWithCurrentState();
      });
    }

  } catch (error) {
    console.error('Error initializing from URL:', error);
    urlError.value = 'Error initializing view. Please try refreshing the page.';

    // Show a toast for better visibility of the error
    toast({
      title: "Initialization Error",
      description: "Error initializing view. Please try refreshing the page.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  } finally {
    console.log('Initialization complete');
    isInitializing.value = false;
  }
}

// Handle team change with improved race condition handling
async function handleTeamChange(teamId: string) {
  try {
    // Skip if we're already changing team or initializing
    if (isChangingTeam.value) {
      console.log('Team change already in progress, skipping');
      return;
    }

    isChangingTeam.value = true;
    urlError.value = null;
    console.log(`Team change initiated to ID ${teamId}`);

    const parsedTeamId = parseInt(teamId);
    if (isNaN(parsedTeamId)) {
      console.warn('Invalid team ID provided');
      return;
    }

    // Set team synchronously to prevent race conditions
    teamsStore.setCurrentTeam(parsedTeamId);

    // Disable any pending editor operations during the transition
    pendingEditorInit.value = true;

    // Clear source details immediately to prevent stale data
    sourceDetails.value = null;

    // Load sources for the selected team
    console.log(`Loading sources for team ${parsedTeamId}`);
    const sourcesResult = await sourcesStore.loadTeamSources(parsedTeamId, true);

    // Handle case where team has no sources
    if (!sourcesResult.success || !sourcesResult.data || sourcesResult.data.length === 0) {
      console.log('Team has no sources, clearing source selection');
      // Clear source selection when team has no sources
      exploreStore.setSource(0);
      sourceDetails.value = null;
    } else {
      // Reset source selection if current team doesn't have access to it
      const currentSourceExists = sourcesStore.teamSources.some(
        source => source.id === exploreStore.sourceId
      );

      if (!currentSourceExists) {
        console.log('Current source not available in new team, selecting first source');
        // Select the first source from the new team
        if (sourcesStore.teamSources.length > 0) {
          const newSourceId = sourcesStore.teamSources[0].id;
          exploreStore.setSource(newSourceId);
          await fetchSourceDetails(newSourceId);
        }
      } else {
        // Current source is valid, just refresh its details
        console.log('Current source is valid in new team, refreshing details');
        await fetchSourceDetails(exploreStore.sourceId);
      }
    }

  } catch (error) {
    console.error('Error changing team:', error);
    urlError.value = 'Error changing team. Please try again.';
  } finally {
    isChangingTeam.value = false;
    pendingEditorInit.value = false;

    // Wait a tick to ensure state is stable before updating URL
    await nextTick();
    updateUrlWithCurrentState();
    console.log('Team change completed');
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

// No longer needed - using store's loading states instead
// const isLoadingSourceDetails = ref(false);
// const sourceDetailsLoadAttempted = ref(new Set<number>());

// Fetch source details with improved caching to prevent redundant API calls
async function fetchSourceDetails(sourceId: number) {
  if (!sourceId || !teamsStore.currentTeamId) {
    sourceDetails.value = null
    return
  }

  // Use the store's loading state tracking instead of local state
  if (sourcesStore.isLoadingSource(sourceId)) {
    console.log(`Already loading source details for ID ${sourceId}, skipping redundant call`);
    return;
  }

  // Check if we already have this source in the store's teamSources
  const cachedSource = sourcesStore.teamSources.find(s => s.id === sourceId);

  // If we have a complete cached source with columns, use it directly
  if (cachedSource && cachedSource.columns && cachedSource.columns.length > 0) {
    console.log(`Using cached source details for ID ${sourceId}`);

    // Only update if source details has changed to prevent unnecessary renders
    if (sourceDetails.value?.id !== cachedSource.id) {
      sourceDetails.value = cachedSource;
      console.log(`Source details loaded from cache for ID ${sourceId}`);
    }

    return;
  }

  try {
    // Only make the API call if we don't have complete data
    console.log(`Fetching source details from API for ID ${sourceId}`);
    console.log(`Current activeSourceTableName: "${activeSourceTableName.value}"`);
    const result = await sourcesStore.getSource(sourceId);

    if (result.success && result.data) {
      // Only update if details have changed or are not set
      if (!sourceDetails.value || sourceDetails.value.id !== result.data.id) {
        sourceDetails.value = result.data;
        console.log(`Source details loaded from API for ID ${sourceId}`);
      }
    } else {
      console.warn(`No source details returned for source ID: ${sourceId}`);
      sourceDetails.value = null;
    }
  } catch (error) {
    console.error('Error fetching source details:', error);
    sourceDetails.value = null;
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
      tableColumns.value = createColumns(
        newColumns, 
        sourceDetails.value?._meta_ts_field || 'timestamp',
        localStorage.getItem('logchef_timezone') === 'utc' ? 'utc' : 'local'
      )
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
              await fetchSourceDetails(newSourceId);
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

// Watch for changes in sourceDetails - with manual equality check and pending query handling
watch(
  () => sourceDetails.value,
  (newSourceDetails, oldSourceDetails) => {
    // Only log and update UI if actually changed (prevent duplicate notifications)
    if (newSourceDetails &&
      (!oldSourceDetails ||
        newSourceDetails.id !== oldSourceDetails.id ||
        JSON.stringify(newSourceDetails.columns) !== JSON.stringify(oldSourceDetails?.columns))) {
      console.log('Source details changed, updating available fields');
      // The availableFields computed property will automatically update
      
      // Check if we have a pending SQL query to restore
      if (exploreStore.pendingRawSql && queryMode.value === 'sql') {
        console.log('Source details loaded, restoring pending SQL query:', exploreStore.pendingRawSql);
        
        // Restore the pending query now that we have source details
        sqlQuery.value = exploreStore.pendingRawSql;
        exploreStore.setRawSql(exploreStore.pendingRawSql);
        
        // Clear the pending query to avoid reapplying it
        exploreStore.pendingRawSql = undefined;
        
        // Update the URL to show the restored query
        updateUrlWithCurrentState();
      }
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

// Handle query submission from QueryEditor
const handleQuerySubmit = async (data: { query: string, finalSql: string, mode: string }) => {
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
    
  const { query, finalSql, mode } = data;

  // Map the mode from editor to store format
  const mappedMode = mode === 'logchefql' ? 'logchefql' : 'sql';

  // Update the store's active mode
  exploreStore.setActiveMode(mappedMode);

  // Update the appropriate query state in the store
  if (mappedMode === 'logchefql') {
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
  updateUrlWithCurrentState();

  // Clear any previous error
  queryError.value = '';

  try {
    // Execute the query with the final SQL
    const result = await exploreStore.executeQuery(finalSql);
      
    // Store error message if query failed, but toast is already shown by base store
    if (!result.success && result.error) {
      queryError.value = result.error.message || 'An error occurred';
    }
  } catch (error) {
    console.error('Error executing query:', error);
    queryError.value = getErrorMessage(error);
    // No need to show toast here as the base store will handle it
  }
};

// Handle loading saved query
async function loadSavedQuery(queryId: string, queryData?: any) {
  try {
    // If we have queryData passed directly from the dropdown, use it
    if (queryData) {
      console.log('Loading query from dropdown data:', queryData);
      
      // Additional debug logging for query type
      console.log(`Query type from dropdown: "${queryData.query_type}", type: ${typeof queryData.query_type}`);

      const queryContent = queryData.content;
      
      // IMPORTANT: First completely reset the query editor state before loading saved query
      // This prevents any issues with mixing query modes or content
      logchefQuery.value = '';
      sqlQuery.value = '';
      exploreStore.setLogchefqlCode('');
      exploreStore.setRawSql('');
      
      // Force the editor to switch to the appropriate tab first
      // This is crucial to avoid incorrect query execution when a different tab was active
      const isLogchefQL = queryData.query_type && queryData.query_type.toLowerCase() === 'logchefql';
      
      if (isLogchefQL) {
        console.log('Setting mode to logchefql for saved query');
        queryMode.value = 'logchefql';
        exploreStore.setActiveMode('logchefql');
        
        // Ensure the QueryEditor UI tab is synchronized (force it to reset)
        if (queryEditorRef.value && typeof queryEditorRef.value.setActiveTab === 'function') {
          queryEditorRef.value.setActiveTab('logchefql');
        }
        
        // Set the LogchefQL content
        if (queryContent.content) {
          // Wait a tick to ensure the mode change is processed
          await nextTick();
          logchefQuery.value = queryContent.content;
          exploreStore.setLogchefqlCode(queryContent.content);
        }
      } else {
        console.log('Setting mode to sql for saved query');
        queryMode.value = 'sql';
        exploreStore.setActiveMode('sql');
        
        // Ensure the QueryEditor UI tab is synchronized (force it to reset)
        if (queryEditorRef.value && typeof queryEditorRef.value.setActiveTab === 'function') {
          queryEditorRef.value.setActiveTab('clickhouse-sql');
        }
        
        // Set SQL content
        if (queryContent.content) {
          // Wait a tick to ensure the mode change is processed
          await nextTick();
          const sqlContent = queryContent.content;
          sqlQuery.value = sqlContent;
          exploreStore.setRawSql(sqlContent);
          // Prevent immediate overwrite by setting the local state
          generatedSQL.value = sqlContent;
        }
      }

      console.log('Saved query loaded, mode:', queryMode.value, 
                  'content:', queryMode.value === 'logchefql' ? 
                    logchefQuery.value : sqlQuery.value);

      // Update URL with current parameters
      updateUrlWithCurrentState();

      // Wait another tick to ensure everything is updated before submitting
      await nextTick();
      
      // Execute the query
      await handleQuerySubmit(queryMode.value);
      return;
    }

    // Get the current team ID
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

    console.log(`Fetching saved query with ID: ${queryId} for team: ${teamId}`);
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

    console.log('Loaded query from API:', query);
    const queryContent = JSON.parse(query.query_content) as SavedQueryContent;
    console.log('Parsed query content:', queryContent);

    // IMPORTANT: First completely reset the query editor state before loading saved query
    // This prevents any issues with mixing query modes or content
    logchefQuery.value = '';
    sqlQuery.value = '';
    exploreStore.setLogchefqlCode('');
    exploreStore.setRawSql('');
    
    // Force the editor to switch to the appropriate tab first based on the saved query type
    // This is crucial to avoid incorrect query execution when a different tab was active
    if (query.query_type === 'logchefql') {
      console.log('Setting mode to logchefql for saved query from API');
      queryMode.value = 'logchefql';
      exploreStore.setActiveMode('logchefql');
      
      // Ensure the QueryEditor UI tab is synchronized (force it to reset)
      if (queryEditorRef.value && typeof queryEditorRef.value.setActiveTab === 'function') {
        queryEditorRef.value.setActiveTab('logchefql');
      }
      
      // Wait a tick to ensure the mode change is processed
      await nextTick();
      
      // Set the LogchefQL content if available
      if (queryContent.content) {
        logchefQuery.value = queryContent.content;
        exploreStore.setLogchefqlCode(queryContent.content);
      }
    } else {
      console.log('Setting mode to sql for saved query from API');
      queryMode.value = 'sql';
      exploreStore.setActiveMode('sql');
      
      // Ensure the QueryEditor UI tab is synchronized (force it to reset)
      if (queryEditorRef.value && typeof queryEditorRef.value.setActiveTab === 'function') {
        queryEditorRef.value.setActiveTab('clickhouse-sql');
      }
      
      // Wait a tick to ensure the mode change is processed
      await nextTick();
      
      // Set SQL content if available
      if (queryContent.content) {
        sqlQuery.value = queryContent.content;
        exploreStore.setRawSql(queryContent.content);
        // Prevent immediate overwrite by setting the local state
        generatedSQL.value = queryContent.content;
      }
    }
    
    console.log('Saved query loaded from API, mode:', queryMode.value, 
                'content:', queryMode.value === 'logchefql' ? 
                  logchefQuery.value : sqlQuery.value);

    // Update URL with current parameters
    updateUrlWithCurrentState();

    // Wait another tick to ensure everything is updated before submitting
    await nextTick();
    
    // Execute the query
    await handleQuerySubmit(queryMode.value);

    toast({
      title: 'Success',
      description: 'Query loaded successfully',
      duration: TOAST_DURATION.SUCCESS,
    });
  } catch (error) {
    console.error('Error loading query:', error);
    toast({
      title: 'Error',
      description: getErrorMessage(error),
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

    // Refresh the saved queries list to ensure the newly created query is available
    // with correct data in the dropdown
    if (response.success && teamsStore.currentTeamId) {
      console.log("Refreshing saved queries list after creating new query");
      await savedQueriesStore.fetchTeamQueries(teamsStore.currentTeamId, true);
    }

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

// Handle query changes from editor - simplified
const handleQueryChange = (data: { query: string, mode: string }) => {
  // Ensure data is an object before destructuring
  if (!data || typeof data !== 'object') {
    console.warn('Invalid data received in handleQueryChange:', data);
    return;
  }

  const { query, mode } = data;
  
  // Map the mode from editor to store format
  const mappedMode = mode === 'logchefql' ? 'logchefql' : 'sql';

  // Update the store's active mode if it changed
  if (queryMode.value !== mappedMode) {
    queryMode.value = mappedMode;
    exploreStore.setActiveMode(mappedMode);
  }

  // Update the appropriate query state in the store
  if (mappedMode === 'logchefql') {
    exploreStore.setLogchefqlCode(query);
    logchefQuery.value = query;
  } else {
    exploreStore.setRawSql(query);
    sqlQuery.value = query;
  }

  // Clear any error messages when the user is typing
  if (queryError.value) {
    queryError.value = '';
  }

  // Don't update URL on every keystroke - will be updated on query submission
}

// Component lifecycle with improved initialization sequence
onMounted(async () => {
  isInitializing.value = true;
  console.log("LogExplorer component mounting - Simplified");

  try {
    // 1. Load Teams
    await teamsStore.loadTeams();
    isTeamsLoaded.value = true;

    // 2. Set Team from URL or default
    let teamId = null;
    if (route.query.team && typeof route.query.team === 'string') {
      teamId = parseInt(route.query.team);
      if (isNaN(teamId) || !teamsStore.teams.some(t => t.id === teamId)) {
        urlError.value = `Invalid or inaccessible team ID: ${route.query.team}.`;
        teamId = teamsStore.teams[0]?.id || null; // Fallback
      }
    } else {
      teamId = teamsStore.teams[0]?.id || null; // Default to first team
    }
    if (teamId) {
      teamsStore.setCurrentTeam(teamId);
    } else {
      urlError.value = "No teams available or accessible.";
      isInitializing.value = false;
      return; // Stop initialization if no team
    }
    
    // Set default time range early to ensure it's always available
    setDefaultTimeRange();

    // 3. Load Sources for the selected team
    await sourcesStore.loadTeamSources(teamsStore.currentTeamId);
    isSourcesLoaded.value = true;

    // 4. Set Source from URL or default
    let sourceId = null;
    if (route.query.source && typeof route.query.source === 'string') {
      sourceId = parseInt(route.query.source);
      if (isNaN(sourceId) || !sourcesStore.teamSources.some(s => s.id === sourceId)) {
        urlError.value = `Invalid or inaccessible source ID: ${route.query.source} for team ${teamsStore.currentTeamId}.`;
        sourceId = sourcesStore.teamSources[0]?.id || null; // Fallback
      }
    } else {
      sourceId = sourcesStore.teamSources[0]?.id || null; // Default to first source
    }
    if (sourceId) {
      exploreStore.setSource(sourceId);
      await fetchSourceDetails(sourceId); // Fetch details needed for props
    } else {
      urlError.value = `No sources available for team ${teamsStore.currentTeamId}.`;
      // Don't stop initialization, allow user to select source
    }

    // 5. Set Limit, Time, Mode, Query from URL directly into the store
    // Limit
    const limit = parseInt(route.query.limit as string || '100');
    exploreStore.setLimit(!isNaN(limit) && limit > 0 && limit <= 10000 ? limit : 100);

    // Time Range
    if (route.query.start_time && route.query.end_time) {
      try {
        const startValue = parseInt(route.query.start_time as string);
        const endValue = parseInt(route.query.end_time as string);

        if (!isNaN(startValue) && !isNaN(endValue)) {
          const startDate = new Date(startValue);
          const endDate = new Date(endValue);

          // Create CalendarDateTime objects
          const start = new CalendarDateTime(
            startDate.getFullYear(),
            startDate.getMonth() + 1,
            startDate.getDate(),
            startDate.getHours(),
            startDate.getMinutes(),
            startDate.getSeconds()
          );

          const end = new CalendarDateTime(
            endDate.getFullYear(),
            endDate.getMonth() + 1,
            endDate.getDate(),
            endDate.getHours(),
            endDate.getMinutes(),
            endDate.getSeconds()
          );

          exploreStore.setTimeRange({ start, end });
        }
      } catch (e) {
        console.error('Failed to parse time range from URL, keeping default', e);
        // Use the default time range already set earlier
      }
    }

    // Mode
    const mode = (route.query.mode === 'sql' ? 'sql' : 'logchefql') as 'logchefql' | 'sql';
    exploreStore.setActiveMode(mode);
    queryMode.value = mode;

    // Query (set initial value in store for QueryEditor prop)
    const initialQuery = route.query.q ? decodeURIComponent(route.query.q as string) : "";
    if (mode === 'logchefql') {
      exploreStore.setLogchefqlCode(initialQuery);
      logchefQuery.value = initialQuery;
      exploreStore.setRawSql(""); // Clear other mode
      sqlQuery.value = "";
    } else {
      exploreStore.setRawSql(initialQuery);
      sqlQuery.value = initialQuery;
      exploreStore.setLogchefqlCode(""); // Clear other mode
      logchefQuery.value = "";
    }

    // 6. Update URL to reflect final initial state
    // Use nextTick to ensure all store updates are processed
    await nextTick();
    isInitializing.value = false; // Mark initialization complete BEFORE first URL update
    updateUrlWithCurrentState();

    // Initial query execution moved to a watch effect for better reliability

  } catch (error) {
    console.error("Error during LogExplorer mount:", error);
    urlError.value = "Error initializing the explorer.";
    toast({
      title: "Explorer Error",
      description: "Error initializing the explorer. Please try refreshing the page.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  } finally {
    isInitializing.value = false; // Ensure flag is reset
  }
});

// Helper to set default time range
function setDefaultTimeRange() {
  const nowDt = now(getLocalTimeZone());
  const oneHourAgoDt = nowDt.subtract({ hours: 1 });
  exploreStore.setTimeRange({
    start: oneHourAgoDt,
    end: nowDt
  });
  console.log('Default time range set in store.');
}

// Watch for conditions to be met for initial query execution
watch(
  [() => isInitializing.value, () => exploreStore.sourceId, () => hasValidSource.value, () => exploreStore.timeRange],
  async ([initializing, sourceId, isValidSource, timeRange], [prevInitializing]) => {
    // Only trigger once after initialization completes and all conditions are met
    if (!initializing && prevInitializing && sourceId > 0 && isValidSource && timeRange) {
      console.log("Conditions met for initial query execution.");
      try {
        // Build the initial SQL query explicitly
        const buildOptions: any = { // Use 'any' temporarily if BuildSqlOptions type causes issues here
          tableName: activeSourceTableName.value,
          tsField: sourceDetails.value?._meta_ts_field || 'timestamp',
          startDateTime: timeRange.start,
          endDateTime: timeRange.end,
          limit: exploreStore.limit,
        };

        let queryResult: { success: boolean; sql: string; error?: string | null };
        if (exploreStore.activeMode === 'logchefql') {
          buildOptions.logchefqlQuery = exploreStore.logchefqlCode;
          queryResult = QueryBuilder.buildSqlFromLogchefQL(buildOptions);
        } else {
          // For SQL mode, use the raw SQL if present, otherwise generate default
          queryResult = exploreStore.rawSql
            ? { success: true, sql: exploreStore.rawSql, error: null } // Assume raw SQL is valid for initial load
            : QueryBuilder.getDefaultSQLQuery(buildOptions);
        }

        if (queryResult.success) {
          await exploreStore.executeQuery(queryResult.sql); // Pass the built SQL
        } else {
          throw new Error(queryResult.error || "Failed to build initial query");
        }
      } catch (buildError: any) {
        console.error("Error building or executing initial query:", buildError);
        queryError.value = buildError.message || "Failed to run initial query";
        toast({
          title: "Initial Query Error",
          description: queryError.value,
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
      }
    } else if (!initializing && prevInitializing && (sourceId <= 0 || !isValidSource)) {
      console.log("Skipping initial query execution (no valid source after initialization).");
    }
  },
  { immediate: false } // Don't run immediately, wait for changes after mount
);

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

  <!-- Show empty state when no teams are available -->
  <div v-else-if="showNoTeamsState" class="flex flex-col h-[calc(100vh-12rem)]">
    <div class="flex flex-col items-center justify-center flex-1 gap-4">
      <div class="text-center space-y-2">
        <h2 class="text-2xl font-semibold tracking-tight">No Teams Available</h2>
        <p class="text-muted-foreground">
          You are not a member of any teams. Please contact your administrator to add you to a team.
        </p>
      </div>
      <div class="flex gap-3">
        <Button variant="outline" @click="router.push({ name: 'Home' })">
          Return to Dashboard
        </Button>
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
  <div v-else class="flex flex-col h-screen overflow-hidden">
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
                <span
                  v-if="isChangingSource || (teamsStore.currentTeamId && sourcesStore.isLoadingTeamSources(teamsStore.currentTeamId))">Loading...</span>
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
          <DateTimePicker v-model="dateRange" class="h-8" />

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
          :disabled="isExecutingQuery || !canExecuteQuery" @click="queryEditorRef?.submitQuery()">
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
      <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden">
        <!-- Query Editor Section -->
        <div class="p-3 pb-2">
          <!-- Only render QueryEditor when timeRange AND sourceDetails are available -->
          <template v-if="exploreStore.timeRange && sourceDetails">
            <div class="rounded-md border-t bg-card">
              <QueryEditor
                ref="queryEditorRef"
              :sourceId="exploreStore.sourceId || 0"
              :schema="sourceDetails?.columns?.reduce((acc, col) => ({ ...acc, [col.name]: { type: col.type } }), {}) || {}"
              :startDateTime="exploreStore.timeRange?.start"
              :endDateTime="exploreStore.timeRange?.end"
              :initialValue="exploreStore.activeMode === 'logchefql' ? exploreStore.logchefqlCode : exploreStore.rawSql"
              :initialTab="exploreStore.activeMode === 'logchefql' ? 'logchefql' : 'clickhouse-sql'"
              :placeholder="exploreStore.activeMode === 'logchefql' ? 'Enter LogchefQL query...' : 'Enter SQL query...'"
              :tsField="sourceDetails?._meta_ts_field || 'timestamp'" :tableName="activeSourceTableName"
              :limit="exploreStore.limit" :showFieldsPanel="showFieldsPanel" @change="handleQueryChange"
              @submit="handleQuerySubmit" @toggle-fields="showFieldsPanel = !showFieldsPanel" />
          </div>
        </template>

          <!-- Query Error Message -->
          <div v-if="queryError || exploreStore.error" class="mt-2 text-sm text-destructive bg-destructive/10 p-2 rounded">
            {{ queryError || (exploreStore.error && exploreStore.error.message) || 'An error occurred' }}
          </div>
        </div>

        <!-- Results Section -->
        <div class="flex-1 overflow-hidden flex flex-col border-t">
          <!-- Stats header - keep minimal -->
          <div class="px-3 py-1.5 flex items-center justify-between bg-muted/10 flex-shrink-0">
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

          <!-- Table container with proper scroll behavior - ensure it fills full available space -->
          <div class="flex-1 overflow-hidden relative">
            <!-- Show loading skeleton when executing query - use store loading state -->
            <template v-if="exploreStore.isLoadingOperation('executeQuery')">
              <div class="h-full flex flex-col items-center justify-center p-10">
                <div class="w-full max-w-5xl space-y-4">
                  <!-- Header skeleton -->
                  <div class="flex justify-between items-center mb-2">
                    <Skeleton class="h-7 w-32" />
                    <Skeleton class="h-7 w-24" />
                  </div>
                  
                  <!-- Table header skeleton -->
                  <div class="flex space-x-2 mb-2">
                    <Skeleton class="h-8 w-40" />
                    <Skeleton class="h-8 w-56" />
                    <Skeleton class="h-8 w-48" />
                    <Skeleton class="h-8 w-64" />
                  </div>
                  
                  <!-- Table rows skeletons -->
                  <div class="space-y-3">
                    <div v-for="i in 8" :key="i" class="flex space-x-2">
                      <Skeleton class="h-10 w-40" />
                      <Skeleton class="h-10 w-56" />
                      <Skeleton class="h-10 w-48" />
                      <Skeleton class="h-10 w-64" />
                    </div>
                  </div>
                </div>
              </div>
            </template>
            <template v-else-if="exploreStore.logs?.length">
              <div class="absolute inset-0 h-full">
                <DataTable :columns="tableColumns" :data="exploreStore.logs" :stats="exploreStore.queryStats"
                  :source-id="exploreStore.sourceId?.toString() || ''" :timestamp-field="sourceDetails?._meta_ts_field"
                  :severity-field="sourceDetails?._meta_severity_field" :timezone="displayTimezone" />
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
      sourceId: exploreStore.sourceId,
      timeRange: exploreStore.timeRange,
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
