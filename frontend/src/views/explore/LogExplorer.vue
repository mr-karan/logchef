<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Plus, Play, RefreshCw, Share2, Keyboard, Save, CalendarIcon, Info, HelpCircle, Eraser } from 'lucide-vue-next'
import { Skeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
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
import { now, getLocalTimeZone, type CalendarDateTime, type DateValue, toCalendarDateTime, parseDate, parseDateTime, fromDate } from '@internationalized/date'
import DataTable from './table/data-table.vue'
import { formatSourceName } from '@/utils/format'
import SaveQueryModal from '@/components/collections/SaveQueryModal.vue'
import QueryEditor from '@/components/query-editor/QueryEditor.vue'
import { FieldSideBar } from '@/components/field-sidebar'
import { getErrorMessage } from '@/api/types'
import { useSourceTeamManagement } from '@/composables/useSourceTeamManagement'
import { useSavedQueries } from '@/composables/useSavedQueries'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { SaveQueryFormData } from '@/views/explore/types'
import type { SavedTeamQuery } from '@/api/savedQueries'
import { useExploreUrlSync } from '@/composables/useExploreUrlSync'
import { useQuery } from '@/composables/useQuery'
import type { EditorMode } from '@/views/explore/types'
import type { ComponentPublicInstance } from 'vue'; // Import ComponentPublicInstance
import { type QueryCondition, parseAndTranslateLogchefQL, validateLogchefQLWithDetails } from '@/utils/logchefql/api';
import { QueryService } from '@/services/QueryService';
import LogHistogram from '@/components/visualizations/LogHistogram.vue';

// Router and stores
const router = useRouter()
const exploreStore = useExploreStore()
const teamsStore = useTeamsStore()
const sourcesStore = useSourcesStore()
const savedQueriesStore = useSavedQueriesStore()
const { toast } = useToast()
const { isInitializing, initializationError, initializeFromUrl, syncUrlFromState } = useExploreUrlSync();

// Composables
const {
  // Query content
  logchefQuery,
  sqlQuery,
  activeMode,

  // State
  queryError,
  sqlWarnings,
  isDirty,
  isExecutingQuery,
  canExecuteQuery,

  // Actions
  changeMode,
  executeQuery,
  handleTimeRangeUpdate,
  handleLimitUpdate
} = useQuery()

// Create default empty parsed query state
const EMPTY_PARSED_QUERY = { success: false, meta: { fieldsUsed: [], conditions: [] } };

// Parse the query after execution to highlight columns used in search
const lastParsedQuery = ref<{
  success: boolean;
  meta?: {
    fieldsUsed: string[];
    conditions: QueryCondition[];
  }
}>(EMPTY_PARSED_QUERY);

// Update the parsed query whenever a new query is executed
watch(() => exploreStore.lastExecutedState, (newState) => {
  if (!newState) {
    // Reset state if execution state is cleared
    lastParsedQuery.value = EMPTY_PARSED_QUERY;
    return;
  }

  if (activeMode.value === 'logchefql') {
    // Check if query is empty
    if (!logchefQuery.value || logchefQuery.value.trim() === '') {
      // Clear highlights for empty queries
      lastParsedQuery.value = EMPTY_PARSED_QUERY;
    } else {
      // Parse the query using LogchefQL parser
      const result = parseAndTranslateLogchefQL(logchefQuery.value);
      lastParsedQuery.value = result;
    }
  } else {
    // Reset when in SQL mode
    lastParsedQuery.value = EMPTY_PARSED_QUERY;
  }
}, { immediate: true });

// Also handle mode changes properly
watch(() => activeMode.value, (newMode, oldMode) => {
  // Original parsing logic
  if (newMode !== 'logchefql') {
    // Reset when switching away from LogchefQL
    lastParsedQuery.value = EMPTY_PARSED_QUERY;
  } else if (newMode === 'logchefql' && logchefQuery.value) {
    // Re-parse when switching back to LogchefQL and there's a query
    const result = parseAndTranslateLogchefQL(logchefQuery.value);
    lastParsedQuery.value = result;
  }

  // If switching to SQL mode, ensure the table name is correct
  if (newMode !== oldMode && newMode === 'sql' && activeSourceTableName.value) {
    // Update SQL query with the current table name
    updateSqlTableReference(activeSourceTableName.value);
  }
});

// Add computed property to get parsed query structure
const parsedQuery = computed(() => {
  return lastParsedQuery.value;
});

// Use structured data for query fields
const queryFields = computed(() => {
  if (!parsedQuery.value.success) return [];
  return parsedQuery.value.meta?.fieldsUsed || [];
});

// Use structured data for regex patterns
const regexHighlights = computed(() => {
  const highlights: Record<string, { pattern: string, isNegated: boolean }> = {};

  if (!parsedQuery.value.success) return highlights;

  // Extract only regex conditions
  const regexConditions = (parsedQuery.value.meta?.conditions || [])
    .filter((cond: QueryCondition) => cond.isRegex);

  // Process each regex condition
  regexConditions.forEach((cond: QueryCondition) => {
    let pattern = cond.value;
    // Remove quotes if they exist
    if ((pattern.startsWith('"') && pattern.endsWith('"')) ||
      (pattern.startsWith("'") && pattern.endsWith("'"))) {
      pattern = pattern.slice(1, -1);
    }

    highlights[cond.field] = {
      pattern,
      isNegated: cond.operator === '!~'
    };
  });

  return highlights;
});

const {
  isProcessingTeamChange,
  isProcessingSourceChange,
  isChangingContext,
  isLoadingSourceDetails,
  currentTeamId,
  currentSourceId,
  sourceDetails,
  hasValidSource,
  availableTeams,
  availableSources,
  selectedTeamName,
  selectedSourceName,
  availableFields,
  handleTeamChange,
  handleSourceChange
} = useSourceTeamManagement()

const {
  showSaveQueryModal,
  handleSaveQueryClick,
  handleSaveQuery,
  loadSavedQuery,
  updateSavedQuery,
  loadSourceQueries
} = useSavedQueries()

// Handle updating an existing query
async function handleUpdateQuery(queryId: string, formData: SaveQueryFormData) {
  // Ensure we have the necessary IDs
  if (!currentSourceId.value || !formData.team_id) {
    toast({
      title: 'Error',
      description: 'Missing source or team ID for update.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR
    });
    return;
  }

  try {
    // Use the correct store action: updateTeamSourceQuery
    const response = await savedQueriesStore.updateTeamSourceQuery(
      formData.team_id,
      currentSourceId.value, // Pass the current source ID
      queryId,
      {
        // Payload only includes fields allowed by updateTeamSourceQuery's type
        name: formData.name,
        description: formData.description,
        query_type: formData.query_type,
        query_content: formData.query_content
      }
    );

    if (response && response.success) {
      showSaveQueryModal.value = false;
      editQueryData.value = null; // Clear editing state

      toast({
        title: 'Success',
        description: 'Query updated successfully.',
        duration: TOAST_DURATION.SUCCESS,
        variant: 'success'
      });

      // Optional: Refresh the list if needed, though store should be reactive
      // await loadSourceQueries(currentTeamId.value, currentSourceId.value);

    } else if (response) {
      // Handle potential API error returned in response.error
      throw new Error(getErrorMessage(response.error) || 'Failed to update query');
    }
  } catch (error) {
    console.error("Error updating query:", error);
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR
    });
  }
}

// Basic state
const showFieldsPanel = ref(false)
// Define the type for the queryEditorRef
const queryEditorRef = ref<ComponentPublicInstance<{
  focus: (revealLastPosition?: boolean) => void;
  // Add other exposed methods/props if needed
}> | null>(null);
const isLoadingQuery = ref(false)
const editQueryData = ref<SavedTeamQuery | null>(null)

// Group by field with computed default value based on severity field
const groupByField = computed({
  get() {
    // Return the stored value or compute default
    return exploreStore.groupByField ||
      // Use severity field as default if available
      (sourcesStore.currentSourceDetails?._meta_severity_field ?
        sourcesStore.currentSourceDetails._meta_severity_field :
        '__none__');
  },
  set(value) {
    exploreStore.setGroupByField(value);
  }
});

// UI state computed properties
const showLoadingState = computed(() => isInitializing.value && !initializationError.value)

const showNoTeamsState = computed(() => !isInitializing.value && (!availableTeams.value || availableTeams.value.length === 0))

const showNoSourcesState = computed(() =>
  !isInitializing.value &&
  !showNoTeamsState.value &&
  currentTeamId.value !== null && currentTeamId.value > 0 &&
  (!availableSources.value || availableSources.value.length === 0)
)

// Computed property to show the "Source Not Connected" state.
// This should only be true AFTER details have loaded and the source is confirmed disconnected.
const showSourceNotConnectedState = computed(() => {
  // Don't show during init, if no teams/sources, or no source selected
  if (isInitializing.value || showNoTeamsState.value || showNoSourcesState.value || !currentSourceId.value) {
    return false;
  }
  // Don't show while details for the *current* source are loading
  if (sourcesStore.isLoadingSourceDetails(currentSourceId.value)) {
    return false;
  }
  // Show only if details *have* loaded (details exist and match current ID) AND the source is invalid/disconnected
  return sourcesStore.currentSourceDetails?.id === currentSourceId.value && !sourcesStore.hasValidCurrentSource;
});

// Use source details from the store
const activeSourceTableName = computed(() => sourcesStore.getCurrentSourceTableName || '');

// Better track when URL query params change
const currentRoute = useRoute();

// Track both query content and mode from URL
const lastQueryParam = ref(currentRoute.query.q);
const lastModeParam = ref(currentRoute.query.mode);

// Check if we're in edit mode (URL has query_id)
const isEditingExistingQuery = computed(() => !!currentRoute.query.query_id || !!exploreStore.selectedQueryId);
const queryIdFromUrl = computed(() => currentRoute.query.query_id as string | undefined);

// Computed property to determine if the query is savable/updatable
const canSaveOrUpdateQuery = computed(() => {
  return !!currentTeamId.value &&
    !!currentSourceId.value &&
    hasValidSource.value && // Ensure source is valid
    (!!exploreStore.logchefqlCode?.trim() || !!exploreStore.rawSql?.trim());
});

// Watch for URL query parameter changes
watch(() => [currentRoute.query.q, currentRoute.query.mode], ([newQueryParam, newModeParam]) => {
  // Handle mode change from URL
  if (newModeParam !== undefined && newModeParam !== lastModeParam.value) {
    lastModeParam.value = newModeParam as string;

    // Update mode in store based on URL
    const mode = (newModeParam as string).toLowerCase() === 'logchefql' ? 'logchefql' : 'sql';
    if (exploreStore.activeMode !== mode) {
      exploreStore.setActiveMode(mode);
    }
  }

  // Handle query content change from URL
  if (newQueryParam !== undefined && newQueryParam !== lastQueryParam.value) {
    lastQueryParam.value = newQueryParam as string;

    const decodedQuery = decodeURIComponent(newQueryParam as string);

    // Update content based on current mode (which may have just changed)
    if (exploreStore.activeMode === 'logchefql') {
      exploreStore.setLogchefqlCode(decodedQuery);
    } else {
      exploreStore.setRawSql(decodedQuery);
    }
  }
}, { immediate: true });

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


// Combined error display
const displayError = computed(() => queryError.value || exploreStore.error?.message)

// Watch for initialization completion to run initial query and load source details
watch(isInitializing, async (initializing, prevInitializing) => {
  if (prevInitializing && !initializing) {
    // If we have a valid source ID after initialization, load its details
    if (currentSourceId.value && currentSourceId.value > 0) {
      const sourceExists = availableSources.value.some(source => source.id === currentSourceId.value);
      if (sourceExists) {
        await sourcesStore.loadSourceDetails(currentSourceId.value);
      }
    }

    const queryId = queryIdFromUrl.value; // Get query ID from computed property

    if (queryId) {
      if (!currentTeamId.value) {
        toast({
          title: 'Error',
          description: 'Cannot load saved query because the team context is missing.',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR
        });
        return; // Exit if no team ID
      }

      try {
        // 1. Fetch the query details using the store
        isLoadingQuery.value = true;
        const fetchResult = await savedQueriesStore.fetchTeamSourceQueryDetails(
          currentTeamId.value,
          currentSourceId.value, // Pass the currentSourceId
          queryId
        );
        isLoadingQuery.value = false;

        if (fetchResult.success && savedQueriesStore.selectedQuery) {
          // 2. Pass the fetched query object to loadSavedQuery
          const loadResult = await loadSavedQuery(savedQueriesStore.selectedQuery);

          if (loadResult) {
            // Sync URL after successful load and application
            syncUrlFromState();
          }
        } else {
          // Fetching the query from the store failed
          throw new Error(fetchResult.error?.message || `Failed to fetch query details for ID ${queryId}`);
        }
      } catch (error) {
        isLoadingQuery.value = false; // Ensure loading is false on error
        toast({
          title: 'Error Loading Saved Query',
          description: getErrorMessage(error),
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR
        });
      }
    }

    // Make sure we have a valid time range before executing the query
    if (!exploreStore.timeRange || !exploreStore.timeRange.start || !exploreStore.timeRange.end) {
      // Set a default time range (last 1 hour)
      exploreStore.setTimeRange({
        start: now(getLocalTimeZone()).subtract({ hours: 1 }),
        end: now(getLocalTimeZone())
      });
    }

    // Now check if we can execute the query (either the one from URL or the loaded saved one)
    if (canExecuteQuery.value) {
      await executeQuery();
    }
  }
}, { immediate: false });

// Watch for source changes to fetch details AND saved queries
watch(
  () => currentSourceId.value, // Watch the computed property from useSourceTeamManagement
  async (newSourceId, oldSourceId) => {
    // Skip during initialization to prevent redundant calls
    if (isInitializing.value) {
      return;
    }

    if (newSourceId !== oldSourceId || (!oldSourceId && newSourceId)) {
      // Fetch Source Details (existing logic)
      if (newSourceId && newSourceId > 0) {
        // Verify source existence (using teamSources from useSourceTeamManagement)
        const sourceExists = availableSources.value.some(source => source.id === newSourceId);
        if (sourceExists) {
          // Fetch details (debounced)
          setTimeout(async () => {
            if (currentSourceId.value === newSourceId) { // Check if still the same after timeout
              await sourcesStore.loadSourceDetails(newSourceId);

              // If we're in SQL mode, ensure the table name is correct
              if (activeMode.value === 'sql' && sourcesStore.getCurrentSourceTableName) {
                updateSqlTableReference(sourcesStore.getCurrentSourceTableName);
              }
            }
          }, 50);

          // Fetch Saved Queries (no debounce needed?)
          if (currentTeamId.value) { // Ensure team ID is available
            await loadSourceQueries(currentTeamId.value, newSourceId);
          }
        }
      } else {
        // Clear saved queries if source is deselected
        if (currentTeamId.value) {
          await loadSourceQueries(currentTeamId.value, 0);
        }
      }
    }
  },
  { immediate: false } // Don't run immediately, wait for initialization
);

// Watch for changes in currentTeamId to update sources AND saved queries
watch(
  () => currentTeamId.value, // Watch the computed property from useSourceTeamManagement
  async (newTeamId, oldTeamId) => {
    // Skip during initialization
    if (isInitializing.value) {
      return;
    }

    if (newTeamId !== oldTeamId && newTeamId) {
      // Load sources for the new team (existing logic)
      const sourcesResult = await sourcesStore.loadTeamSources(newTeamId);
      let newSourceIdToLoadQueries: number | null = null;

      if (!sourcesResult.success || !sourcesResult.data || sourcesResult.data.length === 0) {
        exploreStore.setSource(0);
        newSourceIdToLoadQueries = 0; // Signal to load empty queries
      } else {
        const currentSourceExists = sourcesStore.teamSources.some(
          source => source.id === exploreStore.sourceId
        );
        if (!currentSourceExists && sourcesStore.teamSources.length > 0) {
          const firstSourceId = sourcesStore.teamSources[0].id;
          exploreStore.setSource(firstSourceId);
          await sourcesStore.loadSourceDetails(firstSourceId);
          newSourceIdToLoadQueries = firstSourceId; // Load queries for the new source
        } else {
          // If current source is still valid, load its queries
          newSourceIdToLoadQueries = exploreStore.sourceId;
        }
      }

      // Load Saved Queries for the new team/source combination
      if (newSourceIdToLoadQueries !== null) {
        await loadSourceQueries(newTeamId, newSourceIdToLoadQueries);
      }
    }
  },
  { immediate: false } // Don't run immediately
);

// Watch for changes in sourceDetails from the store
watch(
  () => sourceDetails.value,
  (newSourceDetails, oldSourceDetails) => {
    if (newSourceDetails?.id !== oldSourceDetails?.id) {
      // If this is the first time loading a valid source and initialization is complete,
      // execute the query automatically after a short delay to ensure everything is ready
      if (newSourceDetails && !oldSourceDetails && !isInitializing.value && canExecuteQuery.value) {
        setTimeout(() => {
          if (canExecuteQuery.value) {
            executeQuery();
          }
        }, 100);
      }
    }
  },
  { immediate: true }
);


// Watch time range changes and update query dirty state
watch(
  () => exploreStore.timeRange,
  (newRange, oldRange) => {
    if (!newRange || !oldRange) return;
    if (JSON.stringify(newRange) === JSON.stringify(oldRange)) return;

    // If time range changed significantly, don't auto-execute query
    // Just update SQL and mark as dirty
    if (newRange && oldRange &&
      (newRange.start.toString() !== oldRange.start.toString() ||
        newRange.end.toString() !== oldRange.end.toString())) {

      // Update SQL if we're in SQL mode and have a standard pattern
      if (activeMode.value === 'sql') {
        const currentSql = sqlQuery.value?.trim() || '';
        if (!currentSql) {
          handleTimeRangeUpdate();
          return;
        }

        // Format dates for SQL
        const formatSqlDateTime = (date: DateValue): string => {
          try {
            const zonedDate = toCalendarDateTime(date);
            const isoString = zonedDate.toString();
            // Format as 'YYYY-MM-DD HH:MM:SS'
            return isoString.replace('T', ' ').substring(0, 19);
          } catch (e) {
            console.error("Error formatting date for SQL:", e);
            return '';
          }
        };

        const startDateStr = formatSqlDateTime(newRange.start);
        const endDateStr = formatSqlDateTime(newRange.end);

        // Get the user's timezone for new queries or when updating timezone-aware queries
        const userTimezone = getLocalTimeZone();

        // Pattern to detect: Timezone-aware time range - single toDateTime call with timezone
        const tzTimeRangePattern = /WHERE\s+`?(\w+)`?\s+BETWEEN\s+toDateTime\(['"](.+?)['"],\s*['"]([^'"]+)['"]\)(.*?)AND\s+toDateTime\(['"](.+?)['"],\s*['"]([^'"]+)['"]\)(.*?)(\s|$)/i;

        // Pattern to detect: Standard time range pattern
        const basicTimeRangePattern = /WHERE\s+`?(\w+)`?\s+BETWEEN\s+toDateTime\(['"](.+?)[']\)(.*?)AND\s+toDateTime\(['"](.+?)[']\)(.*?)(\s|$)/i;

        if (tzTimeRangePattern.test(currentSql)) {
          // Update timezone-aware time range
          const updatedSql = currentSql.replace(
            tzTimeRangePattern,
            `WHERE \`$1\` BETWEEN toDateTime('${startDateStr}', '$3')$4AND toDateTime('${endDateStr}', '$6')$7$8`
          );

          if (updatedSql !== currentSql) {
            // Only update if the SQL actually changed
            sqlQuery.value = updatedSql;
            // Don't call handleTimeRangeUpdate() as we've already updated the SQL
            return;
          }
        }
        else if (basicTimeRangePattern.test(currentSql)) {
          // Convert basic time range to timezone-aware
          const updatedSql = currentSql.replace(
            basicTimeRangePattern,
            `WHERE \`$1\` BETWEEN toDateTime('${startDateStr}', '${userTimezone}')$3AND toDateTime('${endDateStr}', '${userTimezone}')$5$6`
          );

          if (updatedSql !== currentSql) {
            // Only update if the SQL actually changed
            sqlQuery.value = updatedSql;
            // Don't call handleTimeRangeUpdate() as we've already updated the SQL
            return;
          }
        }
      }

      // Use the query builder's handler for time range updates
      // This will set isDirty and show a notification instead of updating SQL directly
      handleTimeRangeUpdate();
    }
  },
  { deep: true }
);

// Watch limit changes and update query dirty state
watch(
  () => exploreStore.limit,
  (newLimit, oldLimit) => {
    if (newLimit === oldLimit) return;

    // Use the query builder's handler for limit updates
    // This will set isDirty and show a notification instead of updating SQL directly
    handleLimitUpdate();
  }
);

// Function to copy current URL to clipboard
const copyUrlToClipboard = () => {
  try {
    navigator.clipboard.writeText(window.location.href);
    toast({
      title: "URL Copied",
      description: "The shareable link has been copied to your clipboard.",
      duration: TOAST_DURATION.INFO,
      variant: "success"
    });
  } catch (error) {
    console.error("Failed to copy URL: ", error);
    toast({
      title: "Copy Failed",
      description: "Failed to copy URL to clipboard.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    });
  }
}

// Component lifecycle with improved initialization sequence
onMounted(async () => {
  try {
    // Reset admin teams and load user teams to ensure we have the correct context
    teamsStore.resetAdminTeams();

    // Call the initialization function from the composable
    // This will now handle empty teams gracefully
    await initializeFromUrl();

    // Always force a reload of user teams to ensure we have the latest membership data
    await teamsStore.loadUserTeams(true);

    // Skip validation if there's an initialization error - it's already handled
    if (!initializationError.value) {
      // After loading teams, verify the current teamId is still valid
      // This handles cases where team membership changed elsewhere
      if (currentTeamId.value && !teamsStore.userBelongsToTeam(currentTeamId.value)) {
        console.log(`Current team ${currentTeamId.value} is no longer accessible, resetting selection`);

        // Select the first available team instead
        if (teamsStore.userTeams.length > 0) {
          exploreStore.setSource(0); // First clear the source
          teamsStore.setCurrentTeam(teamsStore.userTeams[0].id);
        } else {
          exploreStore.setSource(0);
          // Clear the current team by using 0, which will be internally converted to null
          teamsStore.setCurrentTeam(0);
        }
      }
    }
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

// --- Event Handlers for QueryEditor --- START
const updateLogchefqlValue = (newValue: string, isUserInput = false) => {
  // If this is from user input, update using the setter which marks it as not from URL
  if (isUserInput) {
    logchefQuery.value = newValue;
  } else {
    // Direct store update for programmatic/URL changes
    exploreStore.setLogchefqlCode(newValue);
  }
};

const updateSqlValue = (newValue: string, isUserInput = false) => {
  // If this is from user input, update using the setter which marks it as not from URL
  if (isUserInput) {
    sqlQuery.value = newValue;
  } else {
    // Direct store update for programmatic/URL changes
    exploreStore.setRawSql(newValue);
  }
};
// --- Event Handlers for QueryEditor --- END

// --- Internal Helper Functions for time conversion ---
function calendarDateTimeToTimestamp(dateTime: DateValue | null | undefined): number | null {
  if (!dateTime) return null;
  try {
    // Convert any DateValue to JS Date object using the local timezone
    const date = dateTime.toDate(getLocalTimeZone());
    return date.getTime();
  } catch (e) {
    console.error("Error converting DateValue to timestamp:", e);
    return null;
  }
}

// New handler for the Save/Update button
const handleSaveOrUpdateClick = async () => {
  // Check if we have a query_id in the URL or in the exploreStore
  const queryId = queryIdFromUrl.value || exploreStore.selectedQueryId;

  // Check if we can save
  if (!canSaveOrUpdateQuery.value) {
    toast({
      title: 'Cannot Save Query',
      variant: 'destructive',
      description: 'Missing required fields (Team, Source, Query).',
      duration: TOAST_DURATION.WARNING
    });
    return;
  }

  if (queryId && currentTeamId.value && currentSourceId.value) {
    // --- Update Existing Query Flow ---
    try {
      isLoadingQuery.value = true;
      const result = await savedQueriesStore.fetchTeamSourceQueryDetails(
        currentTeamId.value,
        currentSourceId.value,
        queryId
      );

      if (result.success && savedQueriesStore.selectedQuery) {
        const existingQuery = savedQueriesStore.selectedQuery;

        // Open the edit modal with the existing query's details
        showSaveQueryModal.value = true;
        editQueryData.value = existingQuery; // Pass only the valid SavedTeamQuery object
        // The modal component itself should handle getting the *current* editor content

      } else {
        throw new Error("Failed to load query details");
      }
    } catch (error) {
      console.error(`Error loading query for edit:`, error);
      toast({
        title: 'Error',
        description: 'Failed to load query details for editing.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
    } finally {
      isLoadingQuery.value = false;
    }
  } else {
    // --- Save New Query Flow ---
    editQueryData.value = null; // Reset edit data
    handleSaveQueryClick(); // Call original function to open modal
  }
};

onBeforeUnmount(() => {
  // Logging for dev troubleshooting can be enabled in development mode only
  if (import.meta.env.MODE !== 'production') {
    console.log("LogExplorer unmounted");
  }
});

// Additional debug for route changes
watch(() => currentRoute.fullPath, (newPath, oldPath) => {
  // No logging needed here
}, { immediate: true });

// Function to clear the query editor content
const clearQueryEditor = () => {
  // Update the store directly
  if (exploreStore.activeMode === 'logchefql') {
    exploreStore.setLogchefqlCode("");
  } else {
    exploreStore.setRawSql("");
  }
  // Clear any validation errors in the store or locally if needed
  queryError.value = ''; // Clear local query error

  // Focus the editor using the ref after clearing
  nextTick(() => {
    queryEditorRef.value?.focus(true);
  });
};

// Function to handle drill-down from DataTable to add a filter condition
const handleDrillDown = (data: { column: string; value: any; operator: string }) => {
  // Only handle in LogchefQL mode
  if (activeMode.value !== 'logchefql') return;

  const { column, value, operator } = data;

  // Create a new condition based on the column and value
  let newCondition = '';
  let formattedValue = '';

  // Format value appropriately
  if (value === null || value === undefined) {
    formattedValue = 'null';
  } else if (typeof value === 'string') {
    // Escape quotes in the string value
    const escapedValue = value.replace(/"/g, '\\"');
    formattedValue = `"${escapedValue}"`;
  } else if (typeof value === 'number' || typeof value === 'boolean') {
    formattedValue = String(value);
  } else {
    // Convert objects to string representation
    try {
      formattedValue = `"${JSON.stringify(value).replace(/"/g, '\\"')}"`;
    } catch (e) {
      formattedValue = `"${String(value).replace(/"/g, '\\"')}"`;
    }
  }

  // Create the condition based on the operator
  newCondition = `${column}${operator}${formattedValue}`;

  // Get the current query
  let currentQuery = logchefQuery.value?.trim() || '';

  // If there's already a query, append the new condition with "and"
  if (currentQuery) {
    // Check if we need to wrap existing query in parentheses
    if (currentQuery.includes(' or ') && !currentQuery.startsWith('(')) {
      currentQuery = `(${currentQuery})`;
    }
    currentQuery = `${currentQuery} and ${newCondition}`;
  } else {
    currentQuery = newCondition;
  }

  // Update the query
  logchefQuery.value = currentQuery;

  // Focus the editor and move cursor to the end of the query
  nextTick(() => {
    queryEditorRef.value?.focus(true);
  });

  // Optionally, execute the query automatically
  // executeQuery();
};

// Function to handle limit changes and update SQL query accordingly
const handleLimitChange = (newLimit: number) => {
  // Update the store limit
  exploreStore.setLimit(newLimit);

  // In SQL mode, we also need to update the SQL query with the new limit
  if (activeMode.value === 'sql') {
    const currentSql = sqlQuery.value?.trim() || '';
    if (!currentSql) return;

    try {
      // Use the parser to analyze the current query
      const analysis = QueryService.analyzeQuery(currentSql);

      if (analysis) {
        let updatedSql = currentSql;

        if (analysis.hasLimit) {
          // Replace existing LIMIT clause
          updatedSql = updatedSql.replace(/LIMIT\s+\d+/i, `LIMIT ${newLimit}`);
        } else {
          // Add LIMIT clause at the end if not present
          updatedSql = `${updatedSql}\nLIMIT ${newLimit}`;
        }

        // Only update if changed
        if (updatedSql !== currentSql) {
          sqlQuery.value = updatedSql;
        }
      }
    } catch (error) {
      console.error("Error updating LIMIT in SQL query:", error);
      // Don't modify the query if we can't safely parse it
    }
  }

  // Call the original limit update handler from useQuery
  handleLimitUpdate();
};

// Add a watch for table name changes to update SQL queries when the source changes
watch(
  () => activeSourceTableName.value,
  (newTableName, oldTableName) => {
    // Skip if either name is missing or if they're the same
    if (!newTableName || !oldTableName || newTableName === oldTableName) {
      return;
    }

    // Only update SQL queries - LogchefQL doesn't reference tables directly
    if (activeMode.value === 'sql') {
      const currentSql = sqlQuery.value || '';

      // Skip if there's no SQL query
      if (!currentSql.trim()) {
        return;
      }

      // Simple search/replace of the table name
      const updatedSql = currentSql.replace(oldTableName, newTableName);

      // Only update if changed
      if (updatedSql !== currentSql) {
        sqlQuery.value = updatedSql;
        exploreStore.setRawSql(updatedSql);
      }
    }
  }
);

// Helper function to update the table reference in SQL queries
function updateSqlTableReference(tableName: string) {
  if (!tableName) return;

  const currentSql = sqlQuery.value?.trim() || '';
  if (!currentSql) return;

  // Check if query has a FROM clause
  const fromMatch = /\bFROM\s+(`?[\w.]+`?)/i.exec(currentSql);
  if (fromMatch) {
    // Replace old table name with new one, preserving backticks if present
    const oldRef = fromMatch[1];
    const hasBackticks = oldRef.startsWith('`') && oldRef.endsWith('`');
    const newRef = hasBackticks ? `\`${tableName}\`` : tableName;

    const updatedSql = currentSql.replace(oldRef, newRef);
    if (updatedSql !== currentSql) {
      sqlQuery.value = updatedSql;
      exploreStore.setRawSql(updatedSql);
    }
  }
}

// Add a ref for the DateTimePicker
const dateTimePickerRef = ref<InstanceType<typeof DateTimePicker> | null>(null);

// Function to open the date time picker programmatically
const openDatePicker = () => {
  if (dateTimePickerRef.value) {
    dateTimePickerRef.value.openDatePicker();
  }
};

// Handle histogram time range zooming
function handleHistogramTimeRangeZoom(range: { start: Date; end: Date }) {
  try {
    // Convert native Dates directly to CalendarDateTime
    const start = toCalendarDateTime(fromDate(range.start, getLocalTimeZone()));
    const end = toCalendarDateTime(fromDate(range.end, getLocalTimeZone()));

    // Update the store's time range
    exploreStore.setTimeRange({ start, end });

    // Execute query immediately with new time range
    executeQuery();

    // Update URL state
    syncUrlFromState();
  } catch (e) {
    console.error('Error handling histogram time range:', e);
    toast({
      title: 'Time Range Error',
      description: 'There was an error updating the time range from chart selection.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR
    });
  }
}

// New function to handle the update:timeRange event from LogHistogram
function handleHistogramTimeRangeUpdate(range: { start: DateValue; end: DateValue }) {
  try {
    console.log('Updating time range from histogram selection:', range);

    // Update the dateRange computed property which will update the store
    dateRange.value = range;

    // Also trigger the date picker to update its visual state
    if (dateTimePickerRef.value) {
      // This signals the DateTimePicker component to update its UI
      nextTick(() => {
        executeQuery();
      });
    } else {
      // If we can't get the date picker ref, still execute the query
      executeQuery();
    }

    // Update URL state
    syncUrlFromState();
  } catch (e) {
    console.error('Error handling histogram time range update:', e);
    toast({
      title: 'Time Range Update Error',
      description: 'There was an error updating the date picker with the selected time range.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR
    });
  }
}

// Helper function to create example query with sort keys
const createExampleQueryWithSortKeys = (sortKeys: string[] = []) => {
  if (!sortKeys || sortKeys.length === 0) return '';

  // Filter out the timestamp field
  const tsField = sourceDetails.value?._meta_ts_field || '';
  const filteredKeys = sortKeys.filter(key => key !== tsField);

  // Create a simple example query using the first 2 non-timestamp sort keys
  const keysToUse = filteredKeys.slice(0, 2);

  if (keysToUse.length === 0) return '';

  if (activeMode.value === 'logchefql') {
    return keysToUse.map(key => `${key}="example"`).join(' and ');
  } else {
    // SQL example with first two non-timestamp keys
    return `SELECT * FROM ${activeSourceTableName.value} WHERE ${keysToUse.map(key => `\`${key}\` = 'example'`).join(' AND ')} LIMIT 100`;
  }
};

// Function to insert example query into editor
const showPerformanceTip = ref(false);

const insertExampleQuery = (sortKeys: string[] = []) => {
  const exampleQuery = createExampleQueryWithSortKeys(sortKeys);
  if (exampleQuery) {
    if (activeMode.value === 'logchefql') {
      logchefQuery.value = exampleQuery;
    } else {
      sqlQuery.value = exampleQuery;
    }
    nextTick(() => {
      queryEditorRef.value?.focus(true);
    });
  }
};
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
    <div class="border-b py-2 px-4 flex items-center h-12">
      <div class="flex items-center space-x-3">
        <Select :model-value="currentTeamId?.toString() ?? ''" @update:model-value="handleTeamChange"
          :disabled="isProcessingTeamChange">
          <SelectTrigger class="h-8 text-sm w-48">
            <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>Teams</SelectLabel>
              <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
                {{ team.name }}
              </SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
        <span class="text-sm text-muted-foreground italic">No sources in this team.</span>
      </div>
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

  <!-- Source Not Connected State -->
  <div v-else-if="showSourceNotConnectedState" class="flex flex-col h-screen overflow-hidden">
    <!-- Filter Bar with Team/Source Selection (similar to main explorer) -->
    <div class="border-b bg-background py-2 px-4 flex items-center h-12 shadow-sm">
      <!-- Data Source Group -->
      <div class="flex items-center space-x-2 min-w-0">
        <!-- Team Selector -->
        <Select :model-value="currentTeamId?.toString() ?? ''" @update:model-value="handleTeamChange"
          :disabled="isProcessingTeamChange">
          <SelectTrigger class="h-8 text-sm w-48">
            <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>Teams</SelectLabel>
              <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
                {{ team.name }}
              </SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <!-- Source Selector - Modified to show connection status -->
        <Select :model-value="currentSourceId?.toString() ?? ''" @update:model-value="handleSourceChange"
          :disabled="isProcessingSourceChange || !currentTeamId || availableSources.length === 0">
          <SelectTrigger class="h-8 text-sm w-64">
            <SelectValue placeholder="Select source">{{ selectedSourceName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>Log Sources</SelectLabel>
              <SelectItem v-if="!currentTeamId" value="no-team" disabled>Select a team first</SelectItem>
              <SelectItem v-else-if="availableSources.length === 0" value="no-sources" disabled>No sources available
              </SelectItem>
              <template v-for="source in availableSources" :key="source.id">
                <SelectItem :value="source.id.toString()">
                  <div class="flex items-center gap-2">
                    <span>{{ formatSourceName(source) }}</span>
                    <span v-if="!source.is_connected"
                      class="inline-flex items-center text-xs bg-destructive/15 text-destructive px-1.5 py-0.5 rounded">
                      Disconnected
                    </span>
                  </div>
                </SelectItem>
              </template>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
    </div>

    <!-- Source Not Connected Message -->
    <div class="flex-1 flex flex-col items-center justify-center p-8">
      <div class="max-w-xl w-full bg-destructive/10 border border-destructive/20 rounded-lg p-6 text-center">
        <svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" fill="none"
          stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
          class="mx-auto mb-4 text-destructive">
          <path d="M18 6 6 18"></path>
          <path d="m6 6 12 12"></path>
        </svg>
        <h2 class="text-xl font-semibold mb-2">Source Not Connected</h2>
        <p class="text-muted-foreground mb-4">
          The selected source "{{ selectedSourceName }}" is not properly connected to the database.
          Please check the source configuration or select a different source.
        </p>

        <div class="flex items-center justify-center gap-3">
          <Button variant="outline"
            @click="router.push({ name: 'SourceSettings', params: { sourceId: currentSourceId } })">
            Configure Source
          </Button>
          <Button variant="default" @click="router.push({ name: 'NewSource' })">
            Add New Source
          </Button>
        </div>
      </div>
    </div>
  </div>

  <!-- Main Explorer View -->
  <div v-else class="flex flex-col h-screen overflow-hidden">
    <!-- URL Error -->
    <div v-if="initializationError"
      class="absolute top-0 left-0 right-0 bg-destructive/15 text-destructive px-4 py-2 z-10 flex items-center justify-between">
      <span class="text-sm">{{ initializationError }}</span>
      <Button variant="ghost" size="sm" @click="initializationError = null" class="h-7 px-2">Dismiss</Button>
    </div>

    <!-- Filter Bar -->
    <div class="border-b bg-background py-2 px-4 flex items-center h-12 shadow-sm">
      <!-- Data Source Group -->
      <div class="flex items-center space-x-2 min-w-0">
        <!-- Team Selector -->
        <Select :model-value="currentTeamId?.toString() ?? ''" @update:model-value="handleTeamChange"
          :disabled="isProcessingTeamChange">
          <SelectTrigger class="h-8 text-sm w-48">
            <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>Teams</SelectLabel>
              <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
                {{ team.name }}
              </SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <!-- Source Selector -->
        <Select :model-value="currentSourceId?.toString() ?? ''" @update:model-value="handleSourceChange"
          :disabled="isProcessingSourceChange || !currentTeamId || availableSources.length === 0">
          <SelectTrigger class="h-8 text-sm w-64">
            <SelectValue placeholder="Select source">{{ selectedSourceName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>Log Sources</SelectLabel>
              <SelectItem v-if="!currentTeamId" value="no-team" disabled>Select a team first</SelectItem>
              <SelectItem v-else-if="availableSources.length === 0" value="no-sources" disabled>No sources available
              </SelectItem>
              <template v-for="source in availableSources" :key="source.id">
                <SelectItem :value="source.id.toString()">
                  <div class="flex items-center gap-2">
                    <span>{{ formatSourceName(source) }}</span>
                    <span v-if="!source.is_connected"
                      class="inline-flex items-center text-xs bg-destructive/15 text-destructive px-1.5 py-0.5 rounded">
                      Disconnected
                    </span>
                  </div>
                </SelectItem>
              </template>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      <!-- Divider -->
      <div class="h-6 w-px bg-border mx-3"></div>

      <!-- Time Controls Group -->
      <div class="flex items-center space-x-2 flex-grow">
        <!-- Date/Time Picker with ref -->
        <DateTimePicker ref="dateTimePickerRef" v-model="dateRange" class="h-8" />

        <!-- Limit Dropdown -->
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" class="h-8 text-sm justify-between px-2 min-w-[90px]">
              <span>Limit:</span> {{ exploreStore.limit.toLocaleString() }}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Results Limit</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem v-for="limit in [100, 500, 1000, 2000, 5000, 10000]" :key="limit"
              @click="handleLimitChange(limit)" :disabled="exploreStore.limit === limit">
              {{ limit.toLocaleString() }} rows
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <!-- Share Button -->
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="outline" size="sm" class="h-8 ml-2" @click="copyUrlToClipboard">
              <Share2 class="h-4 w-4 mr-1.5" />
              Share
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Copy shareable link</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>

    <!-- Main Content Area -->
    <div class="flex flex-1 min-h-0">
      <FieldSideBar v-model:expanded="showFieldsPanel" :fields="availableFields" />

      <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden">
        <!-- Query Editor Section -->
        <div class="px-4 py-3">
          <!-- Loading States and UI Components -->
          <template v-if="isChangingContext || (currentSourceId && isLoadingSourceDetails)">
            <!-- Loading indicator - shown during all loading states -->
            <div
              class="flex items-center justify-center text-muted-foreground p-6 border rounded-md bg-card shadow-sm animate-pulse">
              <div class="flex items-center space-x-2">
                <svg class="animate-spin h-5 w-5 text-primary" xmlns="http://www.w3.org/2000/svg" fill="none"
                  viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z">
                  </path>
                </svg>
                <span>{{ isChangingContext ? 'Loading context data...' : 'Loading source details...' }}</span>
              </div>
            </div>
          </template>

          <!-- Query Editor - only show when we have valid source and time range -->
          <template v-else-if="currentSourceId && hasValidSource && exploreStore.timeRange">
            <div class="bg-card shadow-sm rounded-md overflow-hidden">
              <QueryEditor ref="queryEditorRef" :sourceId="currentSourceId" :teamId="currentTeamId ?? 0"
                :schema="sourceDetails?.columns?.reduce((acc, col) => ({ ...acc, [col.name]: { type: col.type } }), {}) || {}"
                :activeMode="exploreStore.activeMode === 'logchefql' ? 'logchefql' : 'clickhouse-sql'"
                :value="exploreStore.activeMode === 'logchefql' ? logchefQuery : sqlQuery" @change="(event) => event.mode === 'logchefql' ?
                  updateLogchefqlValue(event.query, event.isUserInput) :
                  updateSqlValue(event.query, event.isUserInput)"
                :placeholder="exploreStore.activeMode === 'logchefql' ? 'Enter search criteria (e.g., level=&quot;error&quot; and status>400)' : 'Enter SQL query...'"
                :tsField="sourceDetails?._meta_ts_field || 'timestamp'" :tableName="activeSourceTableName"
                :showFieldsPanel="showFieldsPanel" @submit="executeQuery"
                @update:activeMode="(mode, isModeSwitchOnly) => changeMode(mode === 'logchefql' ? 'logchefql' : 'sql', isModeSwitchOnly)"
                @toggle-fields="showFieldsPanel = !showFieldsPanel" @select-saved-query="loadSavedQuery"
                @save-query="handleSaveOrUpdateClick" class="border-0 border-b" />

              <!-- Sort Key Optimization Hint (Collapsible) -->
              <div
                v-if="sourceDetails?.sort_keys && (sourceDetails.sort_keys.length > 1 || (sourceDetails.sort_keys.length === 1 && sourceDetails.sort_keys[0] !== sourceDetails?._meta_ts_field))"
                class="border-t bg-blue-50 dark:bg-blue-950/20">
                <button
                  class="w-full px-3 py-1.5 text-xs flex items-center justify-between text-blue-700 dark:text-blue-300 font-medium hover:bg-blue-100 dark:hover:bg-blue-900/30 transition-colors"
                  @click="showPerformanceTip = !showPerformanceTip">
                  <div class="flex items-center">
                    <Info class="text-blue-600 dark:text-blue-400 h-4 w-4 mr-2" />
                    <span>ClickHouse Performance Tip: Filter by
                      <span v-for="(key, index) in sourceDetails?.sort_keys || []" :key="key" class="inline-flex">
                        <code class="px-1 bg-blue-100 dark:bg-blue-900 rounded font-mono">{{ key }}</code>
                        <span v-if="index < (sourceDetails?.sort_keys?.length || 0) - 1" class="px-0.5">,</span>
                      </span>
                    </span>
                  </div>
                  <svg class="h-4 w-4 transition-transform" :class="{ 'rotate-180': showPerformanceTip }"
                    xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                  </svg>
                </button>

                <div v-if="showPerformanceTip"
                  class="px-3 pb-3 pt-0 text-xs text-blue-800 dark:text-blue-200 space-y-2">
                  <div class="flex items-center justify-between">
                    <p>Sort Keys:
                      <span v-for="(key, index) in sourceDetails?.sort_keys || []" :key="key" class="inline-flex">
                        <code class="px-1 bg-blue-100 dark:bg-blue-900 rounded font-mono">{{ key }}</code>
                        <span v-if="index < (sourceDetails?.sort_keys?.length || 0) - 1" class="px-0.5">,</span>
                      </span>
                    </p>
                    <Button variant="outline" size="sm"
                      class="h-6 text-blue-700 dark:text-blue-300 border-blue-300 dark:border-blue-700 px-2"
                      @click="insertExampleQuery(sourceDetails?.sort_keys || [])">
                      <Plus class="h-3 w-3 mr-1" />
                      Use Example
                    </Button>
                  </div>

                  <div class="border-l-2 border-blue-300 dark:border-blue-700 pl-3 space-y-1.5">
                    <p class="font-medium">Why this matters:</p>
                    <p class="text-blue-700 dark:text-blue-300">ClickHouse performs best when queries filter by sort
                      keys in order. This can significantly boost query speed.</p>
                    <p class="text-blue-600 dark:text-blue-400 italic text-xs">
                      Optimal filtering:
                      <span v-for="(key, index) in sourceDetails?.sort_keys || []" :key="key">
                        <code class="px-1 bg-blue-100 dark:bg-blue-900/50 rounded">{{ key }}</code>
                        <span v-if="index < (sourceDetails?.sort_keys?.length || 0) - 1"> then </span>
                      </span>
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </template>

          <!-- "Select source" message - only when no source selected -->
          <template v-else-if="currentTeamId && !currentSourceId">
            <div class="flex items-center justify-center text-muted-foreground p-6 border rounded-md bg-card shadow-sm">
              <div class="text-center">
                <div class="mb-2 text-muted-foreground/70">
                  <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none"
                    stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                    class="mx-auto mb-1">
                    <path
                      d="M17.8 19.2 16 11l3.5-3.5C21 6 21.5 4 21 3c-1-.5-3 0-4.5 1.5L13 8 4.8 6.2c-.5-.1-.9.1-1.1.5l-.3.5c-.2.5-.1 1 .3 1.3L9 12l-2 3H4l-1 1 3 2 2 3 1-1v-3l3-2 3.5 5.3c.3.4.8.5 1.3.3l.5-.2c.4-.3.6-.7.5-1.2z" />
                  </svg>
                </div>
                <p class="text-sm">Please select a log source to begin exploring.</p>
              </div>
            </div>
          </template>

          <!-- Loading fallback - for any other state -->
          <template v-else>
            <div class="flex items-center justify-center p-6 border rounded-md bg-card shadow-sm">
              <div class="text-center">
                <p class="text-sm text-muted-foreground">Loading explorer...</p>
              </div>
            </div>
          </template>

          <!-- Query Controls (only if NOT changing context and source is valid) -->
          <div class="mt-3 flex items-center justify-between border-t pt-3"
            v-if="!isChangingContext && currentSourceId && hasValidSource && exploreStore.timeRange">
            <div class="flex items-center gap-2">
              <Button variant="default" class="h-9 px-4 flex items-center gap-2 shadow-sm" :class="{
                'bg-amber-500 hover:bg-amber-600 text-amber-foreground': isDirty && !isExecutingQuery,
                'bg-primary hover:bg-primary/90 text-primary-foreground': isExecutingQuery
              }" :disabled="isExecutingQuery || !canExecuteQuery" @click="executeQuery">
                <Play v-if="!isExecutingQuery" class="h-4 w-4" />
                <RefreshCw v-else class="h-4 w-4 animate-spin" />
                <span>{{ isDirty ? 'Run Query*' : 'Run Query' }}</span>
                <div class="flex flex-col items-start ml-1 border-l border-current/20 pl-2 text-xs text-current">
                  <div class="flex items-center gap-1">
                    <Keyboard class="h-3 w-3" />
                    <span>Ctrl+Enter</span>
                  </div>
                </div>
              </Button>
              <!-- New Clear Button -->
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button variant="outline" size="sm" class="h-9 px-3 flex items-center gap-1.5"
                      @click="clearQueryEditor" :disabled="isExecutingQuery" aria-label="Clear query editor">
                      <Eraser class="h-3.5 w-3.5" />
                      <span>Clear</span>
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">
                    <p>Clear Query</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>

              <!-- Group By Selector - Moved here -->
              <div class="flex items-center gap-2">
                <label class="text-xs text-muted-foreground whitespace-nowrap">Group By:</label>
                <Select v-model="groupByField" class="max-w-[140px] h-8">
                  <SelectTrigger class="h-8 text-xs">
                    <SelectValue placeholder="No Grouping">
                      {{ groupByField === '__none__' ? 'No Grouping' : groupByField }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__none__">No Grouping</SelectItem>
                    <SelectItem v-for="field in availableFields" :key="field.name" :value="field.name">
                      {{ field.name }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <!-- Query Stats Preview -->
            <div class="text-xs text-muted-foreground flex items-center" v-if="exploreStore.lastExecutionTimestamp">
              <span>Last successful run: {{ new Date(exploreStore.lastExecutionTimestamp).toLocaleTimeString() }}</span>
            </div>
          </div>

          <!-- Query Error -->
          <div v-if="displayError" class="mt-2 text-sm text-destructive bg-destructive/10 p-2 rounded">
            <div class="font-medium">Query Error:</div>
            <div>{{ displayError }}</div>
            <div v-if="displayError.includes('Missing boolean operator')"
              class="mt-1.5 pt-1.5 border-t border-destructive/20 text-xs">
              <div class="font-medium">Hint:</div>
              <div>Use <code class="bg-muted px-1 rounded">and</code> or <code class="bg-muted px-1 rounded">or</code>
                between conditions.</div>
              <div class="mt-1">Example: <code class="bg-muted px-1 rounded">level="error" and
                service_name="api-gateway"</code></div>
            </div>
          </div>
        </div>

        <!-- Log Histogram Visualization -->
        <div class="px-4 pb-3" v-if="!isChangingContext && currentSourceId && hasValidSource && exploreStore.timeRange">
          <LogHistogram :time-range="exploreStore.timeRange" :is-loading="isExecutingQuery"
            :group-by="groupByField === '__none__' ? undefined : groupByField"
            @zoom-time-range="handleHistogramTimeRangeZoom" @update:timeRange="handleHistogramTimeRangeUpdate" />
        </div>

        <!-- Results Section -->
        <div class="flex-1 overflow-hidden flex flex-col border-t mt-2">
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
              <DataTable v-if="exploreStore.logs.length > 0 && exploreStore.columns?.length > 0"
                :key="`${exploreStore.sourceId}-${exploreStore.activeMode}-${exploreStore.queryId}`"
                :columns="exploreStore.columns as any" :data="exploreStore.logs" :stats="exploreStore.queryStats"
                :source-id="String(exploreStore.sourceId)" :team-id="teamsStore.currentTeamId"
                :timestamp-field="sourcesStore.currentSourceDetails?._meta_ts_field"
                :severity-field="sourcesStore.currentSourceDetails?._meta_severity_field" :timezone="displayTimezone"
                :query-fields="queryFields" :regex-highlights="regexHighlights" :active-mode="activeMode"
                @drill-down="handleDrillDown" />
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
                <Button variant="outline" size="sm" class="mt-4 h-8" @click="openDatePicker">
                  <CalendarIcon class="h-3.5 w-3.5 mr-2" />
                  Adjust Timerange
                </Button>
              </div>
            </template>

            <!-- Initial State -->
            <template v-else-if="!isExecutingQuery && !exploreStore.lastExecutedState && canExecuteQuery">
              <div class="h-full flex flex-col items-center justify-center p-10 text-center">
                <div class="bg-primary/5 p-6 rounded-full mb-4">
                  <svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 24 24" fill="none"
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                    class="text-primary">
                    <circle cx="11" cy="11" r="8"></circle>
                    <line x1="21" x2="16.65" y1="21" y2="16.65"></line>
                    <line x1="11" x2="11" y1="8" y2="14"></line>
                    <line x1="8" x2="14" y1="11" y2="11"></line>
                  </svg>
                </div>
                <h3 class="text-xl font-medium mb-2">Ready to Explore</h3>
                <p class="text-sm text-muted-foreground max-w-md mb-4">
                  Enter a query or use the default, then click 'Run' to see logs.
                </p>
                <Button variant="outline" size="sm" @click="executeQuery"
                  class="border-primary/20 text-primary hover:bg-primary/5 hover:text-primary hover:border-primary/30">
                  <Play class="h-3.5 w-3.5 mr-1.5" />
                  Run default query
                </Button>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- Save Query Modal -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-type="exploreStore.activeMode"
      :edit-data="editQueryData" :query-content="JSON.stringify({
        sourceId: currentSourceId,
        limit: exploreStore.limit,
        content: exploreStore.activeMode === 'logchefql' ? exploreStore.logchefqlCode : exploreStore.rawSql
      })" @close="showSaveQueryModal = false" @save="handleSaveQuery" @update="handleUpdateQuery" />
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

/* Subtle pulse animation for dirty button state */
@keyframes subtle-pulse {

  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(255, 255, 255, 0.2);
  }

  50% {
    box-shadow: 0 0 0 4px rgba(255, 255, 255, 0);
  }
}

.animate-subtle-pulse {
  animation: subtle-pulse 1.5s infinite ease-in-out;
}
</style>
